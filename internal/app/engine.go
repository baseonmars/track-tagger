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
	Options
	store *Store
	mu    sync.Mutex
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
func (e *Engine) Scan() (Plan, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.scan() }
func (e *Engine) scan() (Plan, error) {
	rs, err := catalog.Scan(e.Source)
	p := Plan{Version: 1, Source: e.Source}
	if err != nil {
		return p, err
	}
	for _, r := range rs {
		item := Item{Recording: r}
		if e.store != nil {
			var saved Item
			ev, ok, err := e.store.review(r.ID, &saved)
			if err != nil {
				return p, err
			}
			if ok {
				if ev == evidence(r) {
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
			info, err := media.Inspect(t.Path)
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
					h, x := media.Hash(target)
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
		p.Recordings = append(p.Recordings, item)
	}
	return p, nil
}
func validateReview(base catalog.Recording, item *Item) error {
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
	if _, err := time.Parse("2006-01-02", item.Date); err != nil {
		return errors.New("date must be YYYY-MM-DD")
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
	catalog.RefreshPlan(&item.Recording)
	return nil
}
func (e *Engine) SaveReview(id string, item Item) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.OpenStorage(); err != nil {
		return err
	}
	rs, err := catalog.Scan(e.Source)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.ID == id {
			if evidence(r) != evidence(item.Recording) {
				return errors.New("inputs changed; refresh before reviewing")
			}
			if err = validateReview(r, &item); err != nil {
				return err
			}
			item.ID = r.ID
			item.Path = r.Path
			item.RelativePath = r.RelativePath
			item.Evidence = r.Evidence
			item.Reviewed = true
			item.Status = "ready"
			if err = e.store.saveReview(id, evidence(r), item); err != nil {
				return err
			}
			_, err = e.store.db.Exec("DELETE FROM holds WHERE id=?", id)
			return err
		}
	}
	return errors.New("recording not found")
}
func (e *Engine) Apply(p Plan) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
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
	fresh, err := catalog.Scan(e.Source)
	if err != nil {
		return nil, err
	}
	byID := map[string]catalog.Recording{}
	for _, r := range fresh {
		byID[r.ID] = r
	}
	var ids []string
	for _, item := range p.Recordings {
		if item.Status != "ready" {
			continue
		}
		base, ok := byID[item.ID]
		if !ok || evidence(base) != evidence(item.Recording) {
			return ids, errors.New("plan is stale: recording inputs changed")
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
	for _, o := range outputs {
		if _, err = e.targetAllowed(o.target); err != nil {
			return "", err
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
	for _, o := range outputs {
		before, x := e.targetAllowed(o.target)
		if x != nil {
			return id, x
		}
		sourceHash, x := media.Hash(o.src)
		if x != nil {
			return id, x
		}
		var original []byte
		if o.audio {
			in, x := media.Inspect(o.src)
			if x != nil {
				return id, x
			}
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
		x = func() error {
			defer os.Remove(tempName)
			if o.audio {
				_, x := media.Stage(o.src, tempName, o.tags, o.art, item.Reviewed)
				if x != nil {
					return x
				}
			} else {
				if x := copyFile(o.src, tempName); x != nil {
					return x
				}
			}
			after, x := media.Hash(tempName)
			if x != nil {
				return x
			}
			if before == after {
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
					h, x := media.Hash(o.src)
					if x != nil {
						return id, x
					}
					item.Evidence[i].SHA256 = h
					item.Evidence[i].Size = st.Size()
					item.Evidence[i].ModTime = st.ModTime().UnixNano()
				}
			}
		}
	}
	if err = e.revalidate(item, ""); err != nil {
		return id, err
	}
	if _, err = e.store.db.Exec("INSERT INTO completions VALUES(?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence", e.completionKey(item.ID), evidence(item.Recording)); err != nil {
		return id, err
	}
	if item.Reviewed {
		rs, x := catalog.Scan(item.Path)
		if x != nil {
			return id, x
		}
		for _, r := range rs {
			if r.ID == item.ID {
				item.Evidence = r.Evidence
				item.Fingerprint = r.Fingerprint
				break
			}
		}
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
	return a.Artist == b.Artist && a.Album == b.Album && a.Date == b.Date && a.Artwork == b.Artwork && reflect.DeepEqual(a.Tracks, b.Tracks)
}
func (e *Engine) revalidate(item Item, temp string) error {
	rs, err := catalog.Scan(item.Path)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if r.ID != item.ID {
			continue
		}
		var evs []catalog.Evidence
		for _, ev := range r.Evidence {
			if ev.Path != temp {
				evs = append(evs, ev)
			}
		}
		r.Evidence = evs
		if !SameInputs(item.Recording, r) {
			return errors.New("recording inputs changed while staging")
		}
		return nil
	}
	return errors.New("recording disappeared")
}
