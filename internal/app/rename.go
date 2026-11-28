package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

// renameFilename produces a portable component. Keep this in sync with the
// filename preview in internal/webui/assets/review.js.
func renameFilename(number int, title string) string {
	prefix := fmt.Sprintf("%02d ", number)
	title = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(title))
	maxTitleBytes := 255 - len(prefix) - len(".flac")
	if len(title) > maxTitleBytes {
		for !utf8.RuneStart(title[maxTitleBytes]) {
			maxTitleBytes--
		}
		title = title[:maxTitleBytes]
	}
	return prefix + title + ".flac"
}

func renameDestination(path string, t catalog.Track, enabled bool) string {
	if enabled && !t.Skip {
		return filepath.Join(filepath.Dir(path), renameFilename(t.Number, t.Title))
	}
	return path
}

func renamePathKey(path string) string {
	return norm.NFC.String(cases.Fold().String(path))
}

var embeddedFingerprint = regexp.MustCompile(`(?mi)(?:\.flac[ \t]*[: \t][ \t]*[0-9a-f]{8,128}[ \t]*\r?$|^[0-9a-f]{32,128}[ \t]+\*?.+\.flac[ \t]*\r?$|^MD5 \(.+\.flac\) = [0-9a-f]{32}[ \t]*\r?$)`)

func checkRenameManifests(item Item) error {
	for _, ev := range item.Evidence {
		name := strings.TrimSuffix(strings.ToLower(filepath.Base(ev.Path)), ".txt")
		extension := filepath.Ext(name)
		if extension == "" {
			extension = "." + name
		}
		switch extension {
		case ".ffp", ".md5", ".sfv", ".sha", ".sha1", ".sha256", ".sha512", ".crc32":
			return fmt.Errorf("cannot rename files while checksum manifest %s is present; keep Rename FLAC files unchecked to apply tags", filepath.Base(ev.Path))
		}
		switch strings.ToLower(filepath.Ext(ev.Path)) {
		case ".txt", ".nfo":
			data, _, err := readCompanion(ev.Path)
			if err == nil && embeddedFingerprint.Match(data) {
				return fmt.Errorf("cannot rename files while %s contains fingerprint entries; keep Rename FLAC files unchecked to apply tags", filepath.Base(ev.Path))
			}
		}
	}
	return nil
}

type renameOutput struct {
	Source        string
	Prior         string
	Target        string
	Backup        string
	Stage         string
	Before        string
	After         string
	Metadata      []byte
	AfterEvidence catalog.Evidence
	Companion     bool
	FileMode      os.FileMode
	SourceHash    string
}

type renameBatch struct {
	Key, Job            string
	Source, Destination string
	Mode                Mode
	Undo                bool
	Item                Item
	Phase               string
	Outputs             []renameOutput
}

func (b renameBatch) root() string {
	if b.Mode == Copy {
		return b.Destination
	}
	return b.Source
}

func (e *Engine) renameMoves(item Item) ([]renameOutput, error) {
	if !item.RenameFiles && e.Mode != Copy {
		return nil, nil
	}
	outputs, changed, err := e.renameOutputs(item)
	if !changed {
		return nil, err
	}
	return outputs, err
}

func (e *Engine) renameOutputs(item Item) ([]renameOutput, bool, error) {
	var outputs []renameOutput
	changed := false
	for _, t := range item.Tracks {
		ordinary, err := e.destination(t.Path)
		if err != nil {
			return nil, false, err
		}
		prior := ordinary
		if e.Mode == Copy {
			prior, err = e.store.copyTarget(e.completionKey(item.ID), t.Path, ordinary)
			if err != nil {
				return nil, false, err
			}
		}
		target := renameDestination(ordinary, t, item.RenameFiles)
		changed = changed || target != ordinary || (prior != "" && prior != target)
		outputs = append(outputs, renameOutput{Source: t.Path, Prior: prior, Target: target})
	}
	if changed && e.Mode == Copy {
		for _, ev := range item.Evidence {
			if catalog.CanTag(ev.Path) {
				continue
			}
			target, err := e.destination(ev.Path)
			if err != nil {
				return nil, false, err
			}
			outputs = append(outputs, renameOutput{Source: ev.Path, Prior: target, Target: target})
		}
	}
	return outputs, changed, nil
}

// Preflight checks every output before staging, using actual directory entries
// so aliases, directories and symlinks cannot masquerade as owned files.
func (b *renameBatch) preflight(s *Store) error {
	prior := make(map[string]renameOutput, len(b.Outputs))
	destinations := make(map[string]renameOutput, len(b.Outputs))
	sources := make(map[string]string, len(b.Item.Evidence))
	for _, ev := range b.Item.Evidence {
		sources[ev.Path] = ev.SHA256
	}
	for i := range b.Outputs {
		o := &b.Outputs[i]
		for _, path := range []string{o.Prior, o.Target} {
			if path == "" {
				continue
			}
			if err := safePath(b.root(), path); err != nil {
				return err
			}
		}
		key := renamePathKey(o.Target)
		if other, ok := destinations[key]; ok {
			return fmt.Errorf("destination collision between %s and %s: %s", o.Source, other.Source, o.Target)
		}
		destinations[key] = *o

		h, err := renameHash(o.Prior)
		if err == nil && h == "" && b.Mode == Copy {
			o.Prior = ""
			continue
		}
		if err != nil {
			return err
		}
		if b.Mode == Copy {
			known, err := s.known(o.Prior)
			if err != nil {
				return err
			}
			if known == "" || h != known {
				return fmt.Errorf("destination collision or external edit for %s: %s", o.Source, o.Prior)
			}
		} else if h != sources[o.Prior] {
			return fmt.Errorf("source changed before renaming: %s", o.Prior)
		}
		key = renamePathKey(o.Prior)
		if other, ok := prior[key]; ok {
			return fmt.Errorf("case collision between %s and %s", o.Prior, other.Prior)
		}
		o.Before = h
		prior[key] = *o
	}

	seenDirs := map[string]bool{}
	for _, o := range b.Outputs {
		dir := filepath.Dir(o.Target)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) && b.Mode == Copy {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			key := renamePathKey(path)
			dest, ok := destinations[key]
			if !ok {
				continue
			}
			owner, allowed := prior[key]
			if !allowed || norm.NFC.String(owner.Prior) != norm.NFC.String(path) {
				return fmt.Errorf("destination collision for %s: %s conflicts with %s", dest.Source, dest.Target, path)
			}
		}
	}
	return nil
}

func reserveRenamePath(dir, kind string) (string, error) {
	f, err := os.CreateTemp(dir, catalog.ReservedFilePrefix+kind+"-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	closeErr := f.Close()
	return name, errors.Join(closeErr, os.Remove(name))
}

func (e *Engine) applyRename(item Item, outputs []renameOutput) (string, error) {
	b := renameBatch{Source: e.Source, Destination: e.Destination, Mode: e.Mode, Item: item, Phase: "evacuating", Outputs: outputs}
	var note notesEdit
	if item.Reviewed && item.AddNotesSignature && item.NotesSignatureEnabled {
		note = e.planNotesEdit(item, notesTarget(item), notesSignature)
		if note.replacement == nil && !note.alreadyPresent {
			return "", errors.New(note.Reason)
		}
		if note.replacement != nil && e.Mode == Write {
			b.Outputs = append(b.Outputs, renameOutput{Source: note.Source, Prior: note.Target, Target: note.Target})
		}
	}
	if err := b.preflight(e.store); err != nil {
		return "", err
	}
	tracks := make(map[string]catalog.Track, len(item.Tracks))
	for _, t := range item.Tracks {
		tracks[t.Path] = t
	}
	sourceHashes := make(map[string]string, len(item.Evidence))
	for _, ev := range item.Evidence {
		sourceHashes[ev.Path] = ev.SHA256
	}
	err := b.stage(func(o *renameOutput) error {
		t, audio := tracks[o.Source]
		if !audio {
			return e.stageRenameCompanion(o, note, sourceHashes[o.Source])
		}
		if t.Skip && e.Mode == Write {
			o.After = o.Before
			return nil
		}
		in, err := media.Inspect(o.Source)
		if err != nil {
			return err
		}
		if in.FileHash != sourceHashes[o.Source] {
			return fmt.Errorf("source changed while staging: %s", o.Source)
		}
		o.Metadata = in.Metadata
		copySource := o.Source
		if o.Prior != "" && e.Mode == Copy {
			prior, err := media.Inspect(o.Prior)
			if err != nil {
				return err
			}
			if prior.FileHash != o.Before {
				return fmt.Errorf("destination changed while staging: %s", o.Prior)
			}
			if prior.AudioHash != in.AudioHash {
				return fmt.Errorf("source audio differs from the existing copy for %s; use a new destination", o.Source)
			}
			o.Metadata = prior.Metadata
			copySource = o.Prior
		}
		if t.Skip {
			o.Metadata = nil
			hash := in.FileHash
			if o.Prior != "" {
				hash = o.Before
			}
			return o.stageCopy(copySource, hash)
		}
		out, err := media.StageInspected(o.Source, o.Stage, in, t.Tags, item.Artwork, true)
		o.After = out.FileHash
		return err
	})
	if err != nil {
		b.removeStages()
		return "", err
	}
	return e.startRename(&b)
}

func (e *Engine) stageRenameCompanion(o *renameOutput, note notesEdit, sourceHash string) error {
	if o.Source == note.Source && note.replacement != nil {
		path := o.Prior
		if path == "" {
			path = o.Source
		}
		data, mode, err := readCompanion(path)
		if err != nil {
			return err
		}
		if hashBytes(data) != note.beforeHash {
			return fmt.Errorf("notes changed before staging: %s", path)
		}
		o.Metadata, o.FileMode, o.Companion, o.SourceHash = data, mode, true, sourceHash
		o.After = hashBytes(note.replacement)
		return writeCompanionStage(o.Stage, note.replacement, mode)
	}
	if o.Prior != "" && o.Before != sourceHash {
		preserved, err := e.store.companionOutput(o.Prior, sourceHash, o.Before)
		if err != nil {
			return err
		}
		if preserved {
			o.After = o.Before
			return nil
		}
	}
	return o.stageCopy(o.Source, sourceHash)
}

func (o *renameOutput) stageCopy(path, want string) error {
	if err := copyFile(path, o.Stage); err != nil {
		return err
	}
	hash, err := media.Hash(o.Stage)
	if err == nil && hash != want {
		return fmt.Errorf("source changed while staging: %s", path)
	}
	o.After = hash
	return err
}

func (b *renameBatch) stage(transform func(*renameOutput) error) error {
	for i := range b.Outputs {
		o := &b.Outputs[i]
		dir := filepath.Dir(o.Target)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		var err error
		o.Stage, err = reserveRenamePath(dir, "rename-stage")
		if err != nil {
			return err
		}
		if err = transform(o); err != nil {
			return err
		}
		if o.Prior == o.Target && o.Before == o.After {
			if err = os.Remove(o.Stage); err != nil && !os.IsNotExist(err) {
				return err
			}
			o.Stage = ""
			continue
		}
		o.AfterEvidence, err = catalog.Fingerprint(o.Stage)
		if err != nil {
			return err
		}
		if o.AfterEvidence.SHA256 != o.After {
			return fmt.Errorf("rename stage changed while preparing: %s", o.Stage)
		}
		o.AfterEvidence.Path = o.Target
		if err = media.SyncDirectory(dir); err != nil {
			return err
		}
		if o.Prior != "" {
			o.Backup, err = reserveRenamePath(filepath.Dir(o.Prior), "rename-backup")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *renameBatch) removeStages() {
	for _, o := range b.Outputs {
		if o.Stage != "" {
			_ = os.Remove(o.Stage)
		}
	}
}

func (e *Engine) startRename(b *renameBatch) (string, error) {
	// Staging can take a while. Do not publish any files until the entire
	// recording and every destination have been checked again.
	if _, err := e.checkRenameEvidence(b, b.originalEvidence()); err != nil {
		b.removeStages()
		return "", err
	}
	if err := b.verifyStages(); err != nil {
		b.removeStages()
		return "", err
	}
	if !b.Undo {
		b.Job = strconv.FormatInt(time.Now().UnixNano(), 10)
		b.Key = b.Job
	}
	if err := e.store.startRename(b); err != nil {
		return "", err
	}
	if err := e.commitRename(b); err != nil {
		return b.Job, fmt.Errorf("rename job %s paused: %w; retry using the same source, mode and state directory", b.Job, err)
	}
	return b.Job, nil
}

func (b *renameBatch) verifyStages() error {
	tracked := make(map[string]renameOutput)
	for _, o := range b.Outputs {
		if o.Prior != "" {
			tracked[renamePathKey(o.Prior)] = o
			if err := expectRenameHash(o.Prior, o.Before); err != nil {
				return err
			}
		}
		if o.Stage != "" {
			if err := expectRenameHash(o.Stage, o.After); err != nil {
				return err
			}
		}
	}
	for _, o := range b.Outputs {
		if prior, ok := tracked[renamePathKey(o.Target)]; ok {
			// If the two spellings are distinct on this filesystem, check the
			// target separately so an unrelated file cannot slip through.
			from, err := os.Lstat(prior.Prior)
			if err != nil {
				return err
			}
			to, err := os.Lstat(o.Target)
			if err == nil && os.SameFile(from, to) {
				continue
			}
		}
		if err := expectRenameHash(o.Target, ""); err != nil {
			return fmt.Errorf("destination collision for %s: %w", o.Source, err)
		}
	}
	return nil
}

func renameHash(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return hashOrMissing(path)
}

func expectRenameHash(path, want string) error {
	hash, err := renameHash(path)
	if err != nil {
		return err
	}
	if hash != want {
		return fmt.Errorf("file changed or destination is occupied: %s", path)
	}
	return nil
}

func (e *Engine) commitRename(b *renameBatch) error {
	if b.Phase == "committed" {
		return e.cleanupRename(b)
	}
	if err := b.checkPaths(); err != nil {
		return err
	}
	want, err := b.currentEvidence()
	if err != nil {
		return err
	}
	if b.Phase == "evacuating" {
		for _, o := range b.Outputs {
			if o.Backup == "" {
				continue
			}
			h, err := renameHash(o.Backup)
			if err != nil {
				return err
			}
			if h == o.Before {
				continue
			}
			if _, err = e.checkRenameEvidence(b, want); err != nil {
				return err
			}
			if err = expectRenameHash(o.Prior, o.Before); err != nil {
				return err
			}
			if err = media.MoveExclusive(o.Prior, o.Backup); err != nil {
				return err
			}
			if b.Mode == Write {
				delete(want, norm.NFC.String(o.Prior))
			}
		}
		b.Phase = "publishing"
		if err = e.store.updateRename(b); err != nil {
			return err
		}
	}
	for _, o := range b.Outputs {
		if o.Stage == "" {
			continue
		}
		h, err := renameHash(o.Stage)
		if err != nil {
			return err
		}
		if h == "" {
			continue
		}
		if _, err = e.checkRenameEvidence(b, want); err != nil {
			return err
		}
		if err = expectRenameHash(o.Stage, o.After); err != nil {
			return err
		}
		if err = media.MoveExclusive(o.Stage, o.Target); err != nil {
			return fmt.Errorf("publish destination for %s: %w", o.Source, err)
		}
		if b.Mode == Write {
			want[norm.NFC.String(o.Target)] = o.AfterEvidence
		}
	}
	fresh, err := e.checkRenameEvidence(b, want)
	if err != nil {
		return err
	}
	for _, o := range b.Outputs {
		if err := expectRenameHash(o.Target, o.After); err != nil {
			return err
		}
	}
	if err = e.store.finishRename(b, fresh); err != nil {
		return err
	}
	return e.cleanupRename(b)
}

func (b *renameBatch) checkPaths() error {
	for _, o := range b.Outputs {
		for _, path := range []string{o.Prior, o.Target, o.Backup, o.Stage} {
			if path == "" {
				continue
			}
			if err := safePath(b.root(), path); err != nil {
				return err
			}
		}
	}
	return nil
}

// currentEvidence derives the only acceptable source state from the durable
// phase and the presence of verified stages and backups. The exclusive moves
// each change two names atomically, so a crash needs no guessed track mapping.
func (b *renameBatch) currentEvidence() (map[string]catalog.Evidence, error) {
	want := b.originalEvidence()
	for _, o := range b.Outputs {
		if o.Stage == "" {
			if err := expectRenameHash(o.Target, o.After); err != nil {
				return nil, err
			}
			continue
		}
		if o.Backup != "" {
			h, err := renameHash(o.Backup)
			if err != nil {
				return nil, err
			}
			switch {
			case h == o.Before:
				if b.Mode == Write {
					delete(want, norm.NFC.String(o.Prior))
				}
				if b.Phase == "evacuating" {
					if err = expectRenameHash(o.Prior, ""); err != nil {
						return nil, err
					}
				}
			case h == "" && b.Phase == "evacuating":
				if err = expectRenameHash(o.Prior, o.Before); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("rename backup is missing or changed: %s", o.Backup)
			}
		}
	}
	for _, o := range b.Outputs {
		if o.Stage == "" {
			continue
		}
		h, err := renameHash(o.Stage)
		if err != nil {
			return nil, err
		}
		switch {
		case h == o.After:
			if b.Phase == "publishing" {
				if err = expectRenameHash(o.Target, ""); err != nil {
					return nil, err
				}
			}
		case h == "" && b.Phase == "publishing":
			if err = expectRenameHash(o.Target, o.After); err != nil {
				return nil, err
			}
			if b.Mode == Write {
				want[norm.NFC.String(o.Target)] = o.AfterEvidence
			}
		default:
			return nil, fmt.Errorf("rename stage is missing or changed: %s", o.Stage)
		}
	}
	return want, nil
}

func (e *Engine) checkRenameEvidence(b *renameBatch, want map[string]catalog.Evidence) (catalog.Recording, error) {
	if err := safePath(b.Source, b.Item.Path); err != nil {
		return catalog.Recording{}, err
	}
	fingerprint := e.strictFingerprint
	if fingerprint == nil {
		fingerprint = catalog.Fingerprint
	}
	fresh, err := e.DatePolicy().ValidateRenameInputs(b.Source, b.Item.Path, fingerprint)
	if err != nil {
		return fresh, err
	}
	if fresh.Status == "waiting" || len(fresh.Evidence) != len(want) {
		return fresh, errors.New("recording inputs changed during renaming")
	}
	for _, found := range fresh.Evidence {
		expected, ok := want[norm.NFC.String(found.Path)]
		if !ok || found.SHA256 != expected.SHA256 || found.Size != expected.Size || found.ModTime != expected.ModTime {
			return fresh, fmt.Errorf("recording input changed during renaming: %s", found.Path)
		}
	}
	return fresh, nil
}

func (e *Engine) cleanupRename(b *renameBatch) error {
	if err := b.checkPaths(); err != nil {
		return err
	}
	for _, o := range b.Outputs {
		if o.Backup != "" {
			if err := expectRenameHash(o.Target, o.After); err != nil {
				return err
			}
		}
	}
	for _, o := range b.Outputs {
		if o.Backup == "" {
			continue
		}
		h, err := renameHash(o.Backup)
		if err != nil {
			return err
		}
		if h == "" {
			continue
		}
		if h != o.Before {
			return fmt.Errorf("rename backup changed before cleanup: %s", o.Backup)
		}
		if err = media.Remove(o.Backup); err != nil {
			return err
		}
	}
	return e.store.removeRename(b.Key)
}

// resumeRenames runs with both writer locks held. Pending work for a different
// source or destination stays available to the original command.
func (e *Engine) resumeRenames(job string, recordingIDs ...string) (map[string]renameBatch, error) {
	pending, err := e.store.pendingRenames(e.Source, e.Destination, e.Mode, job)
	if err != nil {
		return nil, err
	}
	completed := make(map[string]renameBatch)
	selected := make(map[string]bool, len(recordingIDs))
	for _, id := range recordingIDs {
		selected[id] = true
	}
	dirty := false
	for _, b := range pending {
		if len(selected) > 0 && !selected[b.Item.ID] {
			continue
		}
		if !dirty {
			e.invalidateLibrary()
			defer e.invalidateLibrary()
			dirty = true
		}
		if err := e.commitRename(&b); err != nil {
			return completed, fmt.Errorf("resume rename job %s: %w", b.Job, err)
		}
		if !b.Undo {
			completed[b.Item.ID] = b
		}
	}
	return completed, nil
}

func (e *Engine) undoRename(job, source, path string, files []FileJournal) error {
	id := ""
	if err := e.store.db.QueryRow("SELECT recording FROM jobs WHERE id=?", job).Scan(&id); err != nil {
		return err
	}
	base, err := catalog.ScanRecording(source, path)
	if err != nil {
		return err
	}
	if base.ID != id {
		return errors.New("recording path changed before undo")
	}
	b := renameBatch{Key: "undo-" + job, Job: job, Source: source, Mode: Write, Undo: true, Item: Item{Recording: base}, Phase: "evacuating"}
	originals := make(map[string]FileJournal)
	for _, f := range files {
		if f.Status != "committed" || len(f.Metadata) == 0 {
			continue
		}
		if err := expectRenameHash(f.Target, f.AfterHash); err != nil {
			return fmt.Errorf("undo refused: external edit or later job: %w", err)
		}
		later, err := e.store.laterOutput(f.Target, f.ID)
		if err != nil {
			return err
		}
		if later {
			return fmt.Errorf("undo refused: later job changed %s", f.Target)
		}
		b.Outputs = append(b.Outputs, renameOutput{Source: f.Target, Prior: f.Target, Target: f.Source})
		originals[f.Target] = f
	}
	if len(b.Outputs) == 0 {
		return nil
	}
	if err = b.preflight(e.store); err != nil {
		return err
	}
	err = b.stage(func(o *renameOutput) error {
		f := originals[o.Source]
		if f.Companion {
			o.After = hashBytes(f.Metadata)
			if o.After != f.BeforeHash {
				return errors.New("original companion contents do not match the journal")
			}
			return writeCompanionStage(o.Stage, f.Metadata, f.Mode)
		}
		restored, err := media.StageRestore(o.Source, o.Stage, f.Metadata)
		if err != nil {
			return err
		}
		o.After = restored.FileHash
		if o.After != f.BeforeHash {
			return fmt.Errorf("original file contents do not match history: %s", f.Source)
		}
		return nil
	})
	if err != nil {
		b.removeStages()
		return err
	}
	_, err = e.startRename(&b)
	return err
}

func (b *renameBatch) originalEvidence() map[string]catalog.Evidence {
	want := make(map[string]catalog.Evidence, len(b.Item.Evidence))
	for _, ev := range b.Item.Evidence {
		want[norm.NFC.String(ev.Path)] = ev
	}
	return want
}

// RecoverRenames resumes journaled moves before a writable scan or watch tries
// to discover recordings whose original names have been moved aside.
func (e *Engine) RecoverRenames() error {
	if e.Mode == DryRun {
		return nil
	}
	e.validationMu.Lock()
	defer e.validationMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.OpenStorage(); err != nil {
		return err
	}
	batches, err := e.store.pendingRenames(e.Source, e.Destination, e.Mode, "")
	if err != nil || len(batches) == 0 {
		return err
	}
	unlock, err := e.store.acquire()
	if err != nil {
		return err
	}
	defer unlock()
	_, err = e.resumeRenames("")
	return err
}
