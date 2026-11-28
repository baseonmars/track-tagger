package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

const maxCompanionBytes = 4 << 20

func readCompanion(path string) ([]byte, os.FileMode, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !st.Mode().IsRegular() {
		return nil, 0, errors.New("companion must be a regular file")
	}
	if err = media.CheckLocal(path); err != nil {
		return nil, 0, err
	}
	if st.Size() > maxCompanionBytes {
		return nil, 0, errors.New("companion exceeds 4 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCompanionBytes+1))
	if len(data) > maxCompanionBytes {
		return nil, 0, errors.New("companion exceeds 4 MiB")
	}
	return data, st.Mode().Perm(), err
}

// appendNotesLines retains all original bytes and matches the last line ending.
func appendNotesLines(original []byte, lines ...string) ([]byte, error) {
	for _, b := range original {
		if b < 0x20 && b != '\r' && b != '\n' && b != '\t' && b != '\f' || b == 0x7f {
			return nil, errors.New("unsupported notes encoding or binary content")
		}
	}
	ending := "\n"
	if i := bytes.LastIndexAny(original, "\r\n"); i >= 0 {
		switch {
		case original[i] == '\r':
			ending = "\r"
		case i > 0 && original[i-1] == '\r':
			ending = "\r\n"
		}
	}
	existing := map[string]bool{}
	for _, line := range strings.FieldsFunc(strings.TrimPrefix(string(original), "\uFEFF"), func(r rune) bool { return r == '\r' || r == '\n' }) {
		existing[strings.Trim(line, " \t")] = true
	}
	var added []string
	for _, line := range lines {
		if strings.ContainsAny(line, "\r\n\x00") || strings.TrimSpace(line) == "" {
			return nil, errors.New("invalid notes entry")
		}
		if !utf8.Valid(original) {
			var err error
			line, err = charmap.Windows1252.NewEncoder().String(line)
			if err != nil {
				return nil, errors.New("notes entry cannot be represented in Windows-1252")
			}
		}
		if !existing[line] {
			added = append(added, line)
			existing[line] = true
		}
	}
	if len(added) == 0 {
		return original, nil
	}
	suffix := strings.Join(added, ending)
	switch {
	case bytes.HasSuffix(original, []byte(ending)):
		suffix += ending
	case len(original) != 0:
		suffix = ending + suffix
	}
	if len(original)+len(suffix) > maxCompanionBytes {
		return nil, errors.New("updated notes would exceed 4 MiB")
	}
	return append(bytes.Clone(original), suffix...), nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hashOrMissing uses an empty hash for absence and rejects non-regular files.
func hashOrMissing(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("target must be a regular file")
	}
	return media.Hash(path)
}

func (e *Engine) commitCompanion(job string, item *Item, src, target, beforeHash string, replacement []byte, remove bool) error {
	dest, err := e.destination(src)
	if err != nil {
		return err
	}
	if dest != target {
		return errors.New("companion destination changed")
	}
	var sourceHash string
	for _, ev := range item.Evidence {
		if ev.Path == src {
			sourceHash = ev.SHA256
			break
		}
	}
	if sourceHash == "" {
		return errors.New("companion must be a discovered input")
	}
	if beforeHash == "" {
		return errors.New("companion must exist before editing")
	}
	original, mode, err := readCompanion(target)
	if err != nil {
		return err
	}
	if hashBytes(original) != beforeHash {
		return errors.New("companion changed before staging")
	}
	j := FileJournal{JobID: job, Source: src, Target: target, BeforeHash: beforeHash, Metadata: original, Companion: true, Mode: mode, SourceHash: sourceHash}
	check := func() error {
		if err := e.revalidate(*item); err != nil {
			return err
		}
		current, err := e.targetAllowed(target)
		if err != nil || current != beforeHash {
			return fmt.Errorf("companion destination changed while staging: %s", target)
		}
		return nil
	}
	after, err := e.changeCompanion(j, replacement, remove, check)
	if err != nil || e.Mode != Write || beforeHash == after {
		return err
	}
	for i, ev := range item.Evidence {
		if ev.Path != src {
			continue
		}
		if remove {
			item.Evidence = append(item.Evidence[:i:i], item.Evidence[i+1:]...)
			break
		}
		st, err := os.Stat(src)
		if err != nil {
			return err
		}
		item.Evidence[i] = catalog.Evidence{Path: src, SHA256: after, Size: st.Size(), ModTime: st.ModTime().UnixNano()}
		break
	}
	return nil
}

func (e *Engine) changeCompanion(j FileJournal, replacement []byte, remove bool, check func() error) (string, error) {
	if !remove {
		j.AfterHash = hashBytes(replacement)
	}
	if j.AfterHash == j.BeforeHash {
		return j.AfterHash, nil
	}
	var stage string
	if !remove {
		f, err := os.CreateTemp(filepath.Dir(j.Target), catalog.ReservedFilePrefix+"*")
		if err != nil {
			return "", err
		}
		stage = f.Name()
		defer os.Remove(stage)
		if err = writeCompanionFile(f, replacement, j.Mode); err != nil {
			return "", err
		}
	}
	if err := check(); err != nil {
		return "", err
	}
	fid, err := e.store.journal(j)
	if err != nil {
		return "", err
	}
	if remove {
		err = media.Remove(j.Target)
	} else {
		err = media.Replace(stage, j.Target)
	}
	if err != nil {
		return "", err
	}
	if j.UndoOf != 0 {
		return j.AfterHash, e.store.finishUndo(fid, j.UndoOf, j.Target, j.AfterHash)
	}
	return j.AfterHash, e.store.finishFile(fid, j.Target, j.AfterHash, "committed")
}

func writeCompanionStage(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return writeCompanionFile(f, data, mode)
}

func writeCompanionFile(f *os.File, data []byte, mode os.FileMode) error {
	err := f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func (e *Engine) undoCompanion(j FileJournal) error {
	restore := j.Metadata
	remove := j.BeforeHash == ""
	if !remove && hashBytes(restore) != j.BeforeHash {
		return errors.New("original companion contents do not match the journal")
	}
	j.UndoOf, j.Source, j.BeforeHash, j.AfterHash = j.ID, j.Target, j.AfterHash, ""
	j.Metadata = nil
	j.SourceHash = ""
	check := func() error {
		real, err := canonical(j.Target)
		if err != nil || real != j.Target {
			return errors.New("undo target path changed")
		}
		hash, err := hashOrMissing(j.Target)
		if err != nil || hash != j.BeforeHash {
			return fmt.Errorf("companion changed during undo: %s", j.Target)
		}
		return nil
	}
	_, err := e.changeCompanion(j, restore, remove, check)
	return err
}
