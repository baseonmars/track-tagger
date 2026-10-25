package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

type Mode string

const (
	DryRun Mode = "dry-run"
	Write  Mode = "write"
	Copy   Mode = "copy"
)

type Options struct {
	Source, Destination, State string
	Mode                       Mode
	Settle                     time.Duration
}
type Item struct {
	catalog.Recording
	Reviewed bool `json:"Reviewed"`
}
type Plan struct {
	Version    int
	Source     string
	Recordings []Item
}
type Engine struct {
	strictFingerprint func(string) (catalog.Evidence, error)
	Options
	store        *Store
	streams      map[string]playbackFile
	browseCache  catalog.BrowseCache
	outputCache  catalog.BrowseCache
	mu           sync.Mutex
	streamMu     sync.Mutex
	previewMu    sync.Mutex
	validationMu sync.RWMutex
	libraryMu    sync.Mutex
	library      libraryState
}

func New(o Options) (*Engine, error) {
	var err error
	if o.Mode == "" {
		o.Mode = DryRun
	}
	if o.Mode != DryRun && o.Mode != Write && o.Mode != Copy {
		return nil, errors.New("invalid mode")
	}
	o.Source, err = canonical(o.Source)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(o.Source)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("source must be a directory")
	}
	if o.Mode == Copy {
		if o.Destination == "" {
			return nil, errors.New("copy requires destination")
		}
		o.Destination, err = canonical(o.Destination)
		if err != nil {
			return nil, err
		}
		if err = rejectOverlap(o.Source, o.Destination); err != nil {
			return nil, err
		}
	}
	if o.State == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		o.State = filepath.Join(base, "track-tagger")
	}
	o.State, err = canonical(o.State)
	if err != nil {
		return nil, err
	}
	if err = rejectOverlap(o.Source, o.State); err != nil {
		return nil, fmt.Errorf("application storage: %w", err)
	}
	if o.Destination != "" {
		if err = rejectOverlap(o.Destination, o.State); err != nil {
			return nil, err
		}
	}
	return &Engine{Options: o}, nil
}
func (e *Engine) OpenStorage() error {
	if e.store != nil {
		return nil
	}
	s, err := OpenStore(e.State)
	if err == nil {
		e.store = s
	}
	return err
}
func (e *Engine) Close() error {
	e.stopBrowsing()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.store != nil {
		return e.store.Close()
	}
	return nil
}
func evidence(r catalog.Recording) string {
	b, _ := json.Marshal(r.Evidence)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (e *Engine) Scan() (Plan, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.scan(false) }
func (e *Engine) browse() (Plan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scan(true)
}
func (e *Engine) scan(browse bool) (Plan, error) {
	var rs []catalog.Recording
	var err error
	if browse {
		e.previewMu.Lock()
		rs, err = catalog.ScanCached(e.Source, &e.browseCache)
		e.previewMu.Unlock()
	} else {
		rs, err = catalog.Scan(e.Source)
	}
	p := Plan{Version: 1, Source: e.Source}
	if err != nil {
		return p, err
	}
	for _, r := range rs {
		item, err := e.decorate(r, browse)
		if err != nil {
			return p, err
		}
		p.Recordings = append(p.Recordings, item)
	}
	return p, nil
}

// decorate uses current saved decisions; callers hold the writer mutex.
func (e *Engine) decorate(r catalog.Recording, browse bool) (Item, error) {
	item := Item{Recording: r}
	if e.store != nil {
		var saved Item
		ev, ok, err := e.store.review(r.ID, &saved)
		if err != nil {
			return Item{}, err
		}
		if ok {
			if ev == evidence(r) && sameTrackFiles(saved.Tracks, r.Tracks) {
				item = saved
				item.Evidence = r.Evidence
			} else {
				item.ReviewReasons = append(item.ReviewReasons, "saved review is stale: inputs changed")
				item.Status = "needs-review"
			}
		}
	}
	waiting := item.Status == "waiting"
	for _, ev := range r.Evidence {
		if time.Since(time.Unix(0, ev.ModTime)) < e.Settle {
			waiting = true
		}
	}
	for _, t := range item.Tracks {
		if !catalog.CanTag(t.Path) {
			continue
		}
		info, err := media.InspectMetadata(t.Path)
		if err != nil {
			waiting = true
			item.ReviewReasons = append(item.ReviewReasons, filepath.Base(t.Path)+": "+err.Error())
			continue
		}
		if !item.Reviewed {
			for k, v := range t.Tags {
				if old := info.Tags[k]; old != "" && old != v {
					item.ReviewReasons = append(item.ReviewReasons, filepath.Base(t.Path)+": existing "+k+" conflicts")
				}
			}
		}
	}
	if waiting {
		item.Status = "waiting"
	} else if len(item.ReviewReasons) > 0 && !item.Reviewed {
		item.Status = "needs-review"
	} else {
		item.Status = "ready"
	}
	if item.Status == "ready" && e.store != nil {
		var done string
		err := e.store.db.QueryRow("SELECT evidence FROM completions WHERE id=?", e.completionKey(item.ID)).Scan(&done)
		if err == nil && done == evidence(r) {
			unchanged := true
			paths := []string{}
			if e.Mode == Copy {
				for _, ev := range item.Evidence {
					paths = append(paths, ev.Path)
				}
			} else {
				for _, t := range item.Tracks {
					paths = append(paths, t.Path)
				}
			}
			for _, path := range paths {
				target, x := e.destination(path)
				if x != nil {
					unchanged = false
					break
				}
				var h string
				if browse {
					ev, err := e.outputCache.Fingerprint(target)
					h, x = ev.SHA256, err
				} else {
					h, x = media.Hash(target)
				}
				known, y := e.store.known(target)
				if x != nil || y != nil || h != known || known == "" {
					unchanged = false
					break
				}
			}
			if unchanged {
				item.Status = "completed"
			}
		}
	}
	if e.store != nil && item.Status != "waiting" {
		var hold string
		if e.store.db.QueryRow("SELECT id FROM holds WHERE id=?", item.ID).Scan(&hold) == nil {
			item.Status = "needs-review"
			item.Reviewed = false
			item.ReviewReasons = append(item.ReviewReasons, "metadata was undone; review to resume tagging")
		}
	}
	if r.PlaybackOnly {
		item.PlaybackOnly = true
		item.Reviewed = false
		if !waiting {
			item.Status = "playback-only"
		}
	}
	return item, nil
}

func validateReview(base catalog.Recording, item *Item) error {
	if base.PlaybackOnly {
		return errors.New("this recording is playback-only; metadata writing currently supports FLAC-only recordings")
	}
	if len(base.Tracks) != len(item.Tracks) {
		return errors.New("review must map every discovered audio file")
	}
	files := map[string]bool{}
	for _, t := range base.Tracks {
		files[t.Path] = true
	}
	keys := map[string]bool{}
	if strings.TrimSpace(item.Artist) == "" || strings.TrimSpace(item.Album) == "" {
		return errors.New("artist and album are required")
	}
	item.Date = strings.TrimSpace(item.Date)
	if item.DateUnknown {
		if item.Date != "" {
			return errors.New("date must be empty when Date unknown is selected")
		}
	} else {
		if item.Date == "" {
			return errors.New("enter a date or explicitly select Date unknown")
		}
		if err := catalog.ValidateDate(item.Date); err != nil {
			return err
		}
	}
	for _, t := range item.Tracks {
		if !files[t.Path] {
			return fmt.Errorf("duplicate or unknown file %s", t.Path)
		}
		delete(files, t.Path)
		if t.Number < 1 || t.Disc < 1 || strings.TrimSpace(t.Title) == "" {
			return errors.New("every file needs a title and positive disc/track numbers")
		}
		key := fmt.Sprintf("%d/%d", t.Disc, t.Number)
		if keys[key] {
			return errors.New("duplicate disc/track mapping")
		}
		keys[key] = true
	}
	if item.Artwork != "" {
		found := false
		for _, a := range base.ArtworkCandidates {
			if a == item.Artwork {
				found = true
			}
		}
		if !found {
			return errors.New("artwork must be a discovered image")
		}
	}
	for i := range item.Tracks {
		item.Tracks[i].Artist = strings.TrimSpace(item.Tracks[i].Artist)
	}
	catalog.RefreshPlan(&item.Recording)
	return nil
}
func (e *Engine) SaveReview(id string, item Item) error {
	e.invalidateRecordings(id)
	e.validationMu.Lock()
	defer e.validationMu.Unlock()
	defer e.invalidateRecordings(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.OpenStorage(); err != nil {
		return err
	}
	r, err := e.strictRecording(id, item.Path)
	if err != nil {
		return err
	}
	return e.saveReview(r, item)
}

// recording locates an ID without reading unrelated file contents.
func (e *Engine) recording(id string) (catalog.Recording, error) {
	path, err := e.recordingPath(id)
	if err != nil {
		return catalog.Recording{}, err
	}
	return e.strictRecording(id, path)
}
func (e *Engine) recordingPath(id string) (string, error) {
	e.libraryMu.Lock()
	path := ""
	for _, item := range e.library.items {
		if item.ID == id {
			path = item.Path
			break
		}
	}
	e.libraryMu.Unlock()
	if path != "" {
		return path, nil
	}
	ds, err := catalog.Discover(e.Source)
	if err != nil {
		return "", err
	}
	for _, d := range ds {
		if d.ID == id {
			return d.Path, nil
		}
	}
	return "", errors.New("recording not found")
}
func (e *Engine) strictRecording(id, path string) (catalog.Recording, error) {
	if err := safePath(e.Source, path); err != nil {
		return catalog.Recording{}, err
	}
	fingerprint := e.strictFingerprint
	if fingerprint == nil {
		fingerprint = catalog.Fingerprint
	}
	r, err := catalog.Validate(e.Source, catalog.Discovery{ID: id, Path: path}, fingerprint)
	if err != nil {
		return r, err
	}
	if r.ID != id {
		return r, errors.New("recording path changed")
	}
	return r, nil
}
func (e *Engine) saveReview(r catalog.Recording, item Item) error {
	if evidence(r) != evidence(item.Recording) {
		return errors.New("inputs changed; refresh before reviewing")
	}
	if err := validateReview(r, &item); err != nil {
		return err
	}
	item.ID, item.Path, item.RelativePath = r.ID, r.Path, r.RelativePath
	item.Evidence, item.Fingerprint = r.Evidence, r.Fingerprint
	item.Reviewed, item.Status = true, "ready"
	var previous Item
	_, existed, err := e.store.review(r.ID, &previous)
	if err != nil {
		return err
	}
	if !existed {
		previous = Item{Recording: r}
	}
	return e.store.saveReviewDecision(r.ID, evidence(r), item, e.completionKey(r.ID), !sameProposal(previous.Recording, item.Recording))
}

func planIDs(p Plan) []string {
	ids := make([]string, 0, len(p.Recordings))
	for _, r := range p.Recordings {
		ids = append(ids, r.ID)
	}
	return ids
}
func (e *Engine) Apply(p Plan) ([]string, error) {
	e.invalidateRecordings(planIDs(p)...)
	e.validationMu.Lock()
	defer e.validationMu.Unlock()
	defer e.invalidateRecordings(planIDs(p)...)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.applyLocked(p, nil)
}
func (e *Engine) applyLocked(p Plan, check func(Item) error) ([]string, error) {
	if p.Version != 1 || p.Source != e.Source {
		return nil, errors.New("plan source/version mismatch")
	}
	if e.Mode == DryRun {
		return nil, errors.New("media changes disabled: select --write or --copy")
	}
	if err := e.OpenStorage(); err != nil {
		return nil, err
	}
	unlock, err := e.store.acquire()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = e.recover(); err != nil {
		return nil, err
	}
	var ids []string
	for _, item := range p.Recordings {
		if item.Status != "ready" {
			continue
		}
		base, err := e.strictRecording(item.ID, item.Path)
		if err != nil || (check == nil && evidence(base) != evidence(item.Recording)) {
			return ids, errors.New("plan is stale: recording inputs changed")
		}
		if check != nil {
			current, x := e.decorate(base, false)
			if x != nil {
				return ids, x
			}
			if x = check(current); x != nil {
				return ids, x
			}
			// Use the freshly validated, current saved proposal.
			item = current
		}
		if item.Path != base.Path {
			return ids, errors.New("plan path changed")
		}
		if err = validateReview(base, &item); err != nil {
			return ids, err
		}
		if base.Status == "waiting" {
			return ids, errors.New("recording is waiting for local complete files")
		}
		if !item.Reviewed && !sameProposal(base, item.Recording) {
			return ids, errors.New("edited plans require Reviewed: true")
		}
		if !item.Reviewed && len(base.ReviewReasons) > 0 {
			return ids, errors.New("recording requires review")
		}
		for _, ev := range base.Evidence {
			if time.Since(time.Unix(0, ev.ModTime)) < e.Settle {
				return ids, errors.New("recording is still settling")
			}
		}
		id, err := e.applyItem(item)
		if id != "" {
			ids = append(ids, id)
		}
		if err != nil {
			return ids, err
		}
	}
	return ids, nil
}
func (e *Engine) destination(src string) (string, error) {
	if err := safePath(e.Source, src); err != nil {
		return "", err
	}
	if e.Mode == Write {
		return src, nil
	}
	rel, err := filepath.Rel(e.Source, src)
	if err != nil {
		return "", err
	}
	p := filepath.Join(e.Destination, rel)
	return p, safePath(e.Destination, p)
}
func (e *Engine) targetAllowed(target string) (string, error) {
	root := e.Source
	if e.Mode == Copy {
		root = e.Destination
	}
	if err := safePath(root, target); err != nil {
		return "", err
	}
	h, err := media.Hash(target)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if e.Mode == Write {
		return h, nil
	}
	known, err := e.store.known(target)
	if err != nil {
		return "", err
	}
	if known == "" || known != h {
		return "", fmt.Errorf("destination collision or external edit: %s", target)
	}
	return h, nil
}
func (e *Engine) applyItem(item Item) (id string, err error) {
	type output struct {
		src, target string
		tags        map[string]string
		art         string
		audio       bool
	}
	var outputs []output
	for _, t := range item.Tracks {
		dest, x := e.destination(t.Path)
		if x != nil {
			return "", x
		}
		outputs = append(outputs, output{t.Path, dest, t.Tags, item.Artwork, true})
	}
	if e.Mode == Copy {
		for _, ev := range item.Evidence {
			if strings.EqualFold(filepath.Ext(ev.Path), ".flac") {
				continue
			}
			dest, x := e.destination(ev.Path)
			if x != nil {
				return "", x
			}
			outputs = append(outputs, output{src: ev.Path, target: dest})
		}
	}

	sort.Slice(outputs, func(i, j int) bool { return outputs[i].src < outputs[j].src })
	if e.Mode == Copy {
		for _, o := range outputs {
			if _, err = e.targetAllowed(o.target); err != nil {
				return "", err
			}
		}
	}
	id, err = e.store.newJob(item.ID, "metadata update; original checksum documents retained; whole-file MD5 changes")
	if err != nil {
		return "", err
	}
	defer func() {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if x := e.store.jobStatus(id, status); err == nil {
			err = x
		}
	}()
	skipped := make(map[string]string)
	for _, o := range outputs {
		var in media.Info
		var before, sourceHash string
		var original []byte
		var x error
		if o.audio {
			in, x = media.Inspect(o.src)
			if x != nil {
				return id, x
			}
			sourceHash = in.FileHash
		} else {
			sourceHash, x = media.Hash(o.src)
			if x != nil {
				return id, x
			}
		}
		if e.Mode == Write {
			before = sourceHash
		} else {
			before, x = e.targetAllowed(o.target)
			if x != nil {
				return id, x
			}
		}
		if o.audio {
			original = in.Metadata
			if !item.Reviewed {
				for k, v := range o.tags {
					if old := in.Tags[k]; old != "" && old != v {
						return id, fmt.Errorf("existing %s conflicts in %s", k, o.src)
					}
				}
			}
			if before != "" && e.Mode == Copy {
				prior, x := media.Inspect(o.target)
				if x != nil {
					return id, x
				}
				if prior.FileHash != before {
					return id, errors.New("destination changed before staging")
				}
				if prior.AudioHash != in.AudioHash {
					return id, errors.New("source audio differs from the existing copy; use a new destination")
				}
				original = prior.Metadata
			}
		}
		if err = os.MkdirAll(filepath.Dir(o.target), 0755); err != nil {
			return id, err
		}
		temp, x := os.CreateTemp(filepath.Dir(o.target), ".track-tagger-*")
		if x != nil {
			return id, x
		}
		tempName := temp.Name()
		temp.Close()
		os.Remove(tempName)
		var after string
		x = func() error {
			defer os.Remove(tempName)
			if o.audio {
				staged, x := media.StageInspected(o.src, tempName, in, o.tags, o.art, item.Reviewed)
				if x != nil {
					return x
				}
				after = staged.FileHash
			} else {
				if x := copyFile(o.src, tempName); x != nil {
					return x
				}
				after, x = media.Hash(tempName)
				if x != nil {
					return x
				}
			}
			if before == after {
				skipped[o.target] = after
				return nil
			}
			if x = e.revalidate(item, tempName); x != nil {
				return x
			}
			check, x := media.Hash(o.src)
			if x != nil || check != sourceHash {
				return errors.New("source changed while staging")
			}
			check, x = e.targetAllowed(o.target)
			if x != nil || check != before {
				return errors.New("destination changed while staging")
			}
			fid, x := e.store.journal(FileJournal{JobID: id, Source: o.src, Target: o.target, BeforeHash: before, AfterHash: after, Metadata: original})
			if x != nil {
				return x
			}
			if x = media.Replace(tempName, o.target); x != nil {
				return x
			}
			return e.store.finishFile(fid, o.target, after, "committed")
		}()
		if x != nil {
			return id, x
		}
		// Own writes change evidence. Rebase only the file just committed, never other inputs.
		if e.Mode == Write {
			for i := range item.Evidence {
				if item.Evidence[i].Path == o.src {
					st, x := os.Stat(o.src)
					if x != nil {
						return id, x
					}
					item.Evidence[i].SHA256 = after
					item.Evidence[i].Size = st.Size()
					item.Evidence[i].ModTime = st.ModTime().UnixNano()
				}
			}
		}
	}
	fresh, x := e.revalidateRecording(item)
	if x != nil {
		return id, x
	}
	for target, want := range skipped {
		got, x := e.targetAllowed(target)
		if x != nil {
			return id, fmt.Errorf("revalidate unchanged output %s: %w", target, x)
		}
		if got != want {
			return id, fmt.Errorf("unchanged output changed before completion: %s", target)
		}
	}
	item.Fingerprint = fresh.Fingerprint
	if err = e.store.finishCompletion(e.completionKey(item.ID), evidence(item.Recording), skipped); err != nil {
		return id, err
	}
	if item.Reviewed {
		item.Evidence = fresh.Evidence
		if err = e.store.saveReview(item.ID, evidence(item.Recording), item); err != nil {
			return id, err
		}
	}
	return id, nil
}
func (e *Engine) completionKey(id string) string {
	return string(e.Mode) + "|" + e.Destination + "|" + id
}
func copyFile(src, dst string) error {
	if err := media.CheckLocal(src); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = out.ReadFrom(in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return out.Close()
}
func (e *Engine) recover() error {
	files, err := e.store.pendingFiles()
	if err != nil {
		return err
	}
	if len(files) > 0 {
		// Interrupted work can belong to a different recording than this request.
		e.invalidateLibrary()
		defer e.invalidateLibrary()
	}
	for _, j := range files {
		if j.Status != "staged" && j.Status != "undo-staged" {
			continue
		}
		h, err := media.Hash(j.Target)
		if os.IsNotExist(err) && j.BeforeHash == "" {
			_, err = e.store.db.Exec("UPDATE files SET status='aborted' WHERE id=?", j.ID)
			if err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if h == j.AfterHash {
			if j.UndoOf > 0 {
				err = e.store.finishUndo(j.ID, j.UndoOf, j.Target, h)
			} else {
				err = e.store.finishFile(j.ID, j.Target, h, "committed")
			}
			if err != nil {
				return err
			}
		} else if h == j.BeforeHash {
			_, err = e.store.db.Exec("UPDATE files SET status='aborted' WHERE id=?", j.ID)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("interrupted job %s has externally changed file %s", j.JobID, j.Target)
		}
	}
	return nil
}
func (e *Engine) Undo(job string, write bool) ([]FileJournal, error) {
	if write {
		e.mu.Lock()
		var recording string
		if e.OpenStorage() == nil {
			_ = e.store.db.QueryRow("SELECT recording FROM jobs WHERE id=?", job).Scan(&recording)
		}
		e.mu.Unlock()
		if recording != "" {
			e.invalidateRecordings(recording)
			defer e.invalidateRecordings(recording)
		}
		e.validationMu.Lock()
		defer e.validationMu.Unlock()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.OpenStorage(); err != nil {
		return nil, err
	}
	files, err := e.store.Files(job)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("job not found")
	}
	if !write {
		return files, nil
	}
	if e.Mode == DryRun {
		return nil, errors.New("undo requires --write")
	}
	unlock, err := e.store.acquire()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = e.recover(); err != nil {
		return nil, err
	}
	files, err = e.store.Files(job)
	if err != nil {
		return nil, err
	}
	for _, j := range files {
		if j.Status != "committed" || len(j.Metadata) == 0 {
			continue
		}
		real, x := canonical(j.Target)
		if x != nil || real != j.Target {
			return files, errors.New("undo target path changed")
		}
		h, x := media.Hash(j.Target)
		if x != nil || h != j.AfterHash {
			return files, fmt.Errorf("undo refused: external edit or later job: %s", j.Target)
		}
	}
	for _, j := range files {
		if j.Status != "committed" || len(j.Metadata) == 0 {
			continue
		}
		t, x := os.CreateTemp(filepath.Dir(j.Target), ".track-tagger-undo-*")
		if x != nil {
			return files, x
		}
		name := t.Name()
		t.Close()
		os.Remove(name)
		x = func() error {
			defer os.Remove(name)
			info, x := media.StageRestore(j.Target, name, j.Metadata)
			if x != nil {
				return x
			}
			h, x := media.Hash(j.Target)
			if x != nil || h != j.AfterHash {
				return errors.New("file changed during undo")
			}
			current, x := media.Inspect(j.Target)
			if x != nil {
				return x
			}
			fid, x := e.store.journal(FileJournal{JobID: job, Source: j.Target, Target: j.Target, BeforeHash: h, AfterHash: info.FileHash, Metadata: current.Metadata, UndoOf: j.ID})
			if x != nil {
				return x
			}
			if x = media.Replace(name, j.Target); x != nil {
				return x
			}
			return e.store.finishUndo(fid, j.ID, j.Target, info.FileHash)
		}()
		if x != nil {
			return files, x
		}
	}
	_, err = e.store.db.Exec("INSERT OR IGNORE INTO holds SELECT recording FROM jobs WHERE id=?", job)
	if err != nil {
		return files, err
	}
	err = e.store.jobStatus(job, "undone")
	return files, err
}

// SameInputs also detects newly added files, which validating old evidence alone cannot.
func SameInputs(a, b catalog.Recording) bool { return reflect.DeepEqual(a.Evidence, b.Evidence) }

func sameProposal(a, b catalog.Recording) bool {
	return a.Artist == b.Artist && a.Album == b.Album && a.Date == b.Date && a.DateUnknown == b.DateUnknown && a.Artwork == b.Artwork && reflect.DeepEqual(a.Tracks, b.Tracks)
}
func (e *Engine) revalidate(item Item, temp string) error {
	_, err := e.revalidateRecording(item)
	return err
}
func (e *Engine) revalidateRecording(item Item) (catalog.Recording, error) {
	r, err := e.strictRecording(item.ID, item.Path)
	if err != nil {
		return r, err
	}
	if r.ID != item.ID || !SameInputs(item.Recording, r) {
		return r, errors.New("recording inputs changed while staging")
	}
	return r, nil
}

// A parser upgrade may discover additional formats without changing file
// evidence. Such a review must not hide newly discovered tracks.
func sameTrackFiles(a, b []catalog.Track) bool {
	if len(a) != len(b) {
		return false
	}
	paths := map[string]bool{}
	for _, t := range a {
		paths[t.Path] = true
	}
	for _, t := range b {
		if !paths[t.Path] {
			return false
		}
		delete(paths, t.Path)
	}
	return len(paths) == 0
}
