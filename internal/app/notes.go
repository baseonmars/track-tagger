package app

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

const notesSignatureSetting = "show_notes_signature"
const notesSignature = "Tagged with Track Tagger"

type NotesPreview struct {
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Addition string `json:"addition,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type notesEdit struct {
	NotesPreview
	beforeHash     string
	replacement    []byte
	alreadyPresent bool
}

func (e *Engine) showNotesSignature() (bool, error) {
	if e.store == nil {
		return false, nil
	}
	return e.store.setting(notesSignatureSetting)
}

func notesTarget(item Item) string {
	if item.NotesTarget == "" && len(item.Notes) == 1 {
		return item.Notes[0]
	}
	return item.NotesTarget
}

func validateNotesTarget(item *Item, discovered []string) error {
	if item.NotesTarget != "" && !slices.Contains(discovered, item.NotesTarget) {
		return errors.New("select a discovered show-notes file")
	}
	item.Notes = discovered
	if item.AddNotesSignature {
		item.NotesTarget = notesTarget(*item)
	}
	return nil
}

func (e *Engine) planNotesEdit(item Item, source string, lines ...string) notesEdit {
	edit := notesEdit{NotesPreview: NotesPreview{Source: source}}
	switch {
	case len(item.Notes) == 0:
		edit.Reason = "No local show-notes file is available."
		return edit
	case source == "":
		edit.Reason = "Choose which show-notes file to edit."
		return edit
	case !slices.Contains(item.Notes, source):
		edit.Reason = "The selected show-notes file is no longer available."
		return edit
	}
	target := source
	if e.Mode != DryRun {
		var err error
		target, err = e.destination(source)
		if err != nil {
			edit.Reason = fmt.Sprintf("Cannot safely edit %s: %v", filepath.Base(source), err)
			return edit
		}
	}
	edit.Target = target
	setError := func(err error) {
		edit.Reason = fmt.Sprintf("Cannot safely edit %s: %v", filepath.Base(target), err)
	}
	data, _, err := readCompanion(source)
	if err != nil {
		setError(err)
		return edit
	}
	sourceHash := hashBytes(data)
	matches := false
	for _, ev := range item.Evidence {
		if ev.Path == source && ev.SHA256 == sourceHash {
			matches = true
			break
		}
	}
	if !matches {
		setError(errors.New("notes changed during preview; refresh the recording"))
		return edit
	}
	edit.beforeHash = sourceHash
	if e.Mode == Copy {
		hash, err := e.targetAllowed(target)
		if err != nil {
			setError(err)
			return edit
		}
		if hash != "" {
			edit.beforeHash = hash
			isCurrent, err := e.store.companionOutput(target, sourceHash, hash)
			if err != nil {
				setError(err)
				return edit
			}
			if isCurrent {
				data, _, err = readCompanion(target)
				if err != nil || hashBytes(data) != hash {
					setError(errors.New("destination notes changed during preview"))
					return edit
				}
			}
		}
	}
	updated, err := appendNotesLines(data, lines...)
	if err != nil {
		setError(err)
		return edit
	}
	if bytes.Equal(data, updated) {
		edit.alreadyPresent = true
		edit.Reason = "The requested entry is already in the show notes."
		return edit
	}
	edit.replacement = updated
	edit.Addition = string(updated[len(data):])
	return edit
}

func (e *Engine) previewSignature(item *Item) {
	if !item.NotesSignatureEnabled || item.PlaybackOnly {
		return
	}
	for _, path := range item.Notes {
		edit := e.planNotesEdit(*item, path, notesSignature)
		item.notesChoices = append(item.notesChoices, edit.NotesPreview)
	}
	if !item.AddNotesSignature {
		return
	}
	target := notesTarget(*item)
	for i := range item.notesChoices {
		if item.notesChoices[i].Source == target {
			item.NotesPreview = &item.notesChoices[i]
			return
		}
	}
	edit := e.planNotesEdit(*item, target, notesSignature)
	item.NotesPreview = &edit.NotesPreview
}
