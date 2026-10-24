package app

import (
	"context"

	"track-tagger/internal/catalog"
	"track-tagger/internal/webui"
)

// Published pages are immutable and use a separate mutex, so hashing and
// writers never hold up browser reads. previewMu serializes the evidence cache
// shared with synchronous observations used by tests and benchmarks.
type libraryState struct {
	started, stopped   bool
	generation         uint64
	revision           uint64
	status             webui.Status
	items              []Item
	wake               chan struct{}
	done               chan struct{}
	cancel             context.CancelFunc
	stop               context.CancelFunc
	priority           string
	refreshAll         bool
	dirty              map[string]bool
	reconcileRequested bool
	// Test hooks run only on the worker, outside libraryMu.
	validate func(context.Context, catalog.Discovery) (catalog.Recording, error)
}

func (e *Engine) startBrowsing() {
	e.libraryMu.Lock()
	defer e.libraryMu.Unlock()
	if e.library.started || e.library.stopped {
		return
	}
	l := &e.library
	l.started = true
	l.refreshAll = true
	l.generation++
	l.revision++
	l.status = webui.Status{Phase: "validating", Generation: l.generation}
	l.wake, l.done = make(chan struct{}, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	l.stop = cancel
	l.wake <- struct{}{}
	go e.libraryWorker(ctx, l.wake, l.done)
}

func (e *Engine) invalidateLibrary() { e.invalidateRecordings() }

// A slow observation must finish even when the polling interval is shorter
// than a payload read. Remember one follow-up instead of cancelling it.
func (e *Engine) reconcileLibrary() {
	e.libraryMu.Lock()
	if e.library.started && !e.library.stopped && e.library.status.Phase == "validating" {
		e.library.reconcileRequested = true
		e.libraryMu.Unlock()
		return
	}
	e.libraryMu.Unlock()
	e.invalidateLibrary()
}

// RefreshBrowsing is called for a page navigation; status polling remains a
// cached read. A new page retries a failed discovery in the background.
func (w WebBackend) RefreshBrowsing() {
	w.E.startBrowsing()
	w.E.libraryMu.Lock()
	failed := w.E.library.status.Phase == "error"
	w.E.libraryMu.Unlock()
	if failed {
		w.E.invalidateLibrary()
	}
}
func (e *Engine) invalidateRecordings(ids ...string) {
	e.libraryMu.Lock()
	defer e.libraryMu.Unlock()
	l := &e.library
	if !l.started || l.stopped {
		return
	}
	if len(ids) == 0 {
		l.refreshAll = true
	} else {
		if l.dirty == nil {
			l.dirty = map[string]bool{}
		}
		for _, id := range ids {
			l.dirty[id] = true
			l.priority = id
		}
	}
	l.generation++
	if l.cancel != nil {
		l.cancel()
	}
	l.revision++
	l.status.Generation, l.status.Phase, l.status.Validated = l.revision, "validating", false
	l.status.Scanned, l.status.Error = 0, ""
	rows := append([]webui.Recording(nil), l.status.Recordings...)
	items := append([]Item(nil), l.items...)
	for i := range rows {
		if l.refreshAll || l.dirty[rows[i].ID] {
			rows[i].Status, rows[i].Fingerprint = "loading", ""
		} else {
			l.status.Scanned++
		}
	}
	for i := range items {
		if l.refreshAll || l.dirty[items[i].ID] {
			items[i].Status, items[i].Fingerprint = "loading", ""
		}
	}
	l.status.Recordings, l.items = rows, items
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) stopBrowsing() {
	e.libraryMu.Lock()
	l := &e.library
	if !l.started {
		l.stopped = true
		e.libraryMu.Unlock()
		return
	}
	l.stopped = true
	l.stop()
	if l.cancel != nil {
		l.cancel()
	}
	done := l.done
	e.libraryMu.Unlock()
	<-done
}
func (w WebBackend) StopBrowsing() { w.E.stopBrowsing() }

func (e *Engine) librarySnapshot() (webui.Status, []Item) {
	e.startBrowsing()
	e.libraryMu.Lock()
	defer e.libraryMu.Unlock()
	s := e.library.status
	s.Recordings = append([]webui.Recording(nil), s.Recordings...)
	return s, append([]Item(nil), e.library.items...)
}

func (w WebBackend) Status(ctx context.Context) (webui.Status, error) {
	if err := ctx.Err(); err != nil {
		return webui.Status{}, err
	}
	s, _ := w.E.librarySnapshot()
	// Status is a summary: source notes and audio registrations are never read.
	for i := range s.Recordings {
		s.Recordings[i] = librarySummary(s.Recordings[i])
	}
	return s, nil
}
func librarySummary(r webui.Recording) webui.Recording {
	r = cloneWebRecording(r)
	r.Notes, r.Evidence = nil, nil
	r.Artwork, r.SelectedArtwork = nil, ""
	return r
}

func (e *Engine) libraryWorker(ctx context.Context, wake <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
		// Drain coalesced requests before taking the current generation.
		for {
			select {
			case <-wake:
				continue
			default:
			}
			break
		}
		e.libraryMu.Lock()
		generation := e.library.generation
		work, cancel := context.WithCancel(ctx)
		e.library.cancel = cancel
		hook := e.library.validate
		full := e.library.refreshAll
		dirty := make(map[string]bool, len(e.library.dirty))
		for id, v := range e.library.dirty {
			dirty[id] = v
		}
		previous, oldItems := e.library.status, e.library.items
		e.libraryMu.Unlock()
		e.validateLibrary(work, generation, hook, full, dirty, previous, oldItems)
		cancel()
	}
}

func (e *Engine) publishLibrary(generation uint64, s webui.Status, items []Item) bool {
	e.libraryMu.Lock()
	defer e.libraryMu.Unlock()
	if e.library.stopped || generation != e.library.generation {
		return false
	}
	e.library.revision++
	s.Generation = e.library.revision
	e.library.status = s
	e.library.items = items
	if s.Phase == "ready" {
		e.library.refreshAll = false
		e.library.dirty = nil
		if e.library.reconcileRequested {
			e.library.reconcileRequested = false
			e.library.generation++
			e.library.refreshAll = true
			select {
			case e.library.wake <- struct{}{}:
			default:
			}
		}
	}
	return true
}

func (e *Engine) validateLibrary(ctx context.Context, generation uint64, hook func(context.Context, catalog.Discovery) (catalog.Recording, error), full bool, dirty map[string]bool, previous webui.Status, oldItems []Item) {
	var ds []catalog.Discovery
	var err error
	if full {
		ds, err = catalog.Discover(e.Source)
	} else {
		for _, item := range oldItems {
			ds = append(ds, catalog.Discovery{ID: item.ID, Path: item.Path, RelativePath: item.RelativePath})
		}
	}
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		e.publishLibrary(generation, webui.Status{Phase: "error", Error: err.Error()}, nil)
		return
	}
	items := make([]Item, len(ds))
	rows := make([]webui.Recording, len(ds))
	finished := make([]bool, len(ds))
	completed := 0
	for i, d := range ds {
		items[i] = Item{Recording: catalog.Recording{ID: d.ID, Path: d.Path, RelativePath: d.RelativePath, Status: "loading"}}
		rows[i] = webui.Recording{ID: d.ID, Path: d.Path, RelativePath: d.RelativePath, Status: "loading"}
		if !full && !dirty[d.ID] {
			items[i], rows[i] = oldItems[i], previous.Recordings[i]
			finished[i] = true
			completed++
		}
	}
	s := webui.Status{Phase: "validating", Total: len(ds), Scanned: completed, Recordings: rows}
	if !e.publishLibrary(generation, s, items) {
		return
	}
	for completed < len(ds) {
		if ctx.Err() != nil {
			return
		}
		e.libraryMu.Lock()
		priority := e.library.priority
		e.libraryMu.Unlock()
		next := -1
		for i, d := range ds {
			if !finished[i] && (next < 0 || d.ID == priority) {
				next = i
				if d.ID == priority {
					break
				}
			}
		}
		i, d := next, ds[next]
		e.validationMu.RLock()
		var r catalog.Recording
		if ctx.Err() == nil {
			if hook != nil {
				r, err = hook(ctx, d)
			} else {
				if err = safePath(e.Source, d.Path); err == nil {
					r, err = catalog.Validate(e.Source, d, func(path string) (catalog.Evidence, error) {
						if err := ctx.Err(); err != nil {
							return catalog.Evidence{}, err
						}
						e.previewMu.Lock()
						defer e.previewMu.Unlock()
						return e.browseCache.FingerprintContext(ctx, path)
					})
				}
			}
		}
		item := items[i]
		failedJob := false
		if err == nil && ctx.Err() == nil {
			e.mu.Lock()
			if ctx.Err() == nil {
				item, err = e.decorate(r, true)
			}
			if err == nil && item.Status == "ready" && e.store != nil {
				jobs, x := e.store.History()
				if x == nil {
					for _, j := range jobs {
						if j.RecordingID == item.ID {
							failedJob = j.Status == "failed"
							break
						}
					}
				}
			}
			e.mu.Unlock()
		}
		// Notes are part of the immutable detail snapshot, read by this worker.
		if ctx.Err() != nil {
			e.validationMu.RUnlock()
			return
		}
		if err != nil {
			item.Status = "failed"
			item.ReviewReasons = []string{err.Error()}
			item.Fingerprint = ""
		}
		row := webRecord(item)
		if failedJob {
			row.Status = "failed"
		}
		e.validationMu.RUnlock()
		if ctx.Err() != nil {
			return
		}
		items = append([]Item(nil), items...)
		items[i] = item
		rows = append([]webui.Recording(nil), rows...)
		rows[i] = row
		finished[i] = true
		completed++
		s.Recordings, s.Scanned = rows, completed
		if completed == len(ds) {
			s.Phase, s.Validated = "ready", true
		}
		if !e.publishLibrary(generation, s, items) {
			return
		}
	}
	if len(ds) == 0 || completed == len(ds) {
		if full && ctx.Err() == nil {
			recordings := make([]catalog.Recording, len(items))
			for i, item := range items {
				recordings[i] = item.Recording
			}
			e.previewMu.Lock()
			e.browseCache.Retain(recordings)
			e.previewMu.Unlock()
		}
		s.Phase, s.Validated = "ready", true
		e.publishLibrary(generation, s, items)
	}
}

// waitLibrary is used by the watcher only; browser requests always take a snapshot.
func (e *Engine) libraryPlan() (Plan, bool) {
	s, items := e.librarySnapshot()
	return Plan{Version: 1, Source: e.Source, Recordings: items}, s.Phase == "ready"
}

func (e *Engine) prioritize(id string) {
	e.libraryMu.Lock()
	e.library.priority = id
	e.libraryMu.Unlock()
}

// Clone before registering playback IDs: snapshots must never be mutated by a request.
func cloneWebRecording(r webui.Recording) webui.Recording {
	r.Tracks = append([]webui.Track(nil), r.Tracks...)
	r.Artwork = append([]string(nil), r.Artwork...)
	r.Reasons = append([]string(nil), r.Reasons...)
	r.Evidence = append([]string(nil), r.Evidence...)
	r.Notes = append([]webui.Note(nil), r.Notes...)
	metadata := make(map[string]string, len(r.Metadata))
	for k, v := range r.Metadata {
		metadata[k] = v
	}
	r.Metadata = metadata
	return r
}
