package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type watchEntry struct {
	key, path string
	since     time.Time
	done      bool
}
type watchSchedule map[string]*watchEntry

// observe measures stability from when this process observed the inputs, rather
// than trusting copied or backdated source mtimes.
func (s watchSchedule) observe(item Item, now time.Time) *watchEntry {
	b, _ := json.Marshal(struct {
		Fingerprint                  string
		Reviewed                     bool
		Tracks                       any
		Artwork, Artist, Album, Date string
	}{item.Fingerprint, item.Reviewed, item.Tracks, item.Artwork, item.Artist, item.Album, item.Date})
	h := sha256.Sum256(b)
	key := hex.EncodeToString(h[:])
	entry := s[item.ID]
	if entry == nil || entry.key != key {
		entry = &watchEntry{key: key, path: item.Path, since: now}
		s[item.ID] = entry
	}
	return entry
}
func (s watchSchedule) changed(path string, now time.Time) {
	for _, entry := range s {
		rel, err := filepath.Rel(entry.path, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			entry.since = now
			entry.done = false
		}
	}
}
func (e *watchEntry) ready(now time.Time, settle time.Duration) bool {
	return !e.done && !now.Before(e.since.Add(settle))
}

// Watch recursively watches local directories, with periodic reconciliation for
// missed events. Polling is an explicit backend and the fallback after watcher
// failures. Every recording must be observed unchanged for the settle interval.
func (e *Engine) Watch(ctx context.Context, poll bool, reconcile time.Duration, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	if reconcile <= 0 {
		reconcile = 5 * time.Minute
	}
	settle := e.Settle
	if settle < 0 {
		settle = 0
	}
	var watcher *fsnotify.Watcher
	var events <-chan fsnotify.Event
	var failures <-chan error
	closeWatcher := func() {
		if watcher != nil {
			_ = watcher.Close()
			watcher = nil
		}
		events = nil
		failures = nil
	}
	defer closeWatcher()
	addTree := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				return watcher.Add(path)
			}
			return nil
		})
	}
	fallback := func(err error) {
		log(fmt.Sprintf("filesystem watcher failed: %v; switching to 15-second polling", err))
		closeWatcher()
		poll = true
	}
	if !poll {
		var err error
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			fallback(err)
		} else if err = addTree(e.Source); err != nil {
			fallback(err)
		} else {
			events = watcher.Events
			failures = watcher.Errors
		}
	}
	if poll {
		log("watching with polling every 15 seconds")
	}
	schedule := watchSchedule{}
	lastScan := time.Time{}
	lastEvent := time.Time{}
	dirty := true
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	scan := func(now time.Time) {
		plan, err := e.Scan()
		lastScan = now
		dirty = false
		if err != nil {
			log("scan failed: " + err.Error())
			return
		}
		present := map[string]bool{}
		for _, item := range plan.Recordings {
			present[item.ID] = true
			entry := schedule.observe(item, now)
			if item.Status != "ready" {
				continue
			}
			if !entry.ready(now, settle) {
				continue
			}
			if e.Mode == DryRun {
				log("ready (preview only): " + item.Path)
				entry.done = true
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			one := Plan{Version: plan.Version, Source: plan.Source, Recordings: []Item{item}}
			jobs, err := e.Apply(one)
			if err != nil {
				log("failed: " + item.Path + ": " + err.Error())
				continue
			}
			entry.done = true
			log(fmt.Sprintf("completed: %s (jobs %v)", item.Path, jobs))
		}
		for id := range schedule {
			if !present[id] {
				delete(schedule, id)
			}
		}
	}
	scan(time.Now())
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				fallback(fmt.Errorf("event channel closed"))
				dirty = true
				continue
			}
			now := time.Now()
			schedule.changed(event.Name, now)
			dirty = true
			lastEvent = now
			if event.Has(fsnotify.Create) && watcher != nil {
				if err := addTree(event.Name); err != nil { // A create can disappear before it is inspected; reconciliation handles it.
					if !os.IsNotExist(err) {
						fallback(err)
					}
				}
			}
		case err, ok := <-failures:
			if !ok {
				err = fmt.Errorf("error channel closed")
			}
			fallback(err)
			dirty = true
		case now := <-tick.C:
			due := now.Sub(lastScan) >= reconcile
			if poll && now.Sub(lastScan) >= 15*time.Second {
				due = true
			}
			if dirty && now.Sub(lastEvent) >= time.Second {
				due = true
			}
			if !dirty {
				for _, entry := range schedule {
					if entry.ready(now, settle) && entry.since.Add(settle).After(lastScan) {
						due = true
						break
					}
				}
			}
			if due {
				scan(now)
			}
		}
	}
}
