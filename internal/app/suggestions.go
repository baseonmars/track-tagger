package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
	"track-tagger/internal/notesmodel"
)

const (
	maxSuggestionBytes = 4 << 20
	maxSuggestionLines = 16 << 10
)

type contextNotePredictor interface {
	PredictContext(context.Context, map[string][]string) ([]notesmodel.Span, error)
}

type noteSuggestionSnapshot struct {
	item       Item
	notes      map[string][]string
	generation uint64
	datePolicy catalog.DatePolicy
}

// Suggestions derives optional review proposals from the current local note
// snapshot. Existing review saves still validate the normal recording inputs.
func (w WebBackend) Suggestions(ctx context.Context, id, fingerprint string) ([]catalog.NoteSuggestion, error) {
	empty := []catalog.NoteSuggestion{}
	if w.NoteModel == nil {
		return empty, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fingerprint == "" {
		return nil, catalog.ErrStaleNoteSuggestion
	}
	if !w.E.noteSuggestionsBusy.CompareAndSwap(false, true) {
		return nil, catalog.ErrNoteSuggestionsBusy
	}
	defer w.E.noteSuggestionsBusy.Store(false)
	snapshot, err := w.noteSuggestionInput(ctx, id, fingerprint)
	if err != nil {
		return nil, err
	}
	if len(snapshot.notes) == 0 {
		return empty, nil
	}
	var spans []notesmodel.Span
	if model, ok := w.NoteModel.(contextNotePredictor); ok {
		spans, err = model.PredictContext(ctx, snapshot.notes)
	} else {
		spans, err = w.NoteModel.Predict(snapshot.notes)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proposals := catalog.GuardNoteSuggestions(snapshot.item.Recording, snapshot.notes, spans, snapshot.datePolicy)
	w.E.validationMu.RLock()
	defer w.E.validationMu.RUnlock()
	status, items := w.E.librarySnapshot()
	if status.Generation != snapshot.generation || !status.Validated || w.E.DatePolicy() != snapshot.datePolicy {
		return nil, catalog.ErrStaleNoteSuggestion
	}
	for _, item := range items {
		if item.ID == id && item.Fingerprint == fingerprint && suggestionEligible(item) {
			return proposals, nil
		}
	}
	return nil, catalog.ErrStaleNoteSuggestion
}

func suggestionEligible(item Item) bool {
	return !item.Reviewed && !item.PlaybackOnly && item.Status == "needs-review" && len(item.Notes) > 0
}

func (w WebBackend) noteSuggestionInput(ctx context.Context, id, fingerprint string) (noteSuggestionSnapshot, error) {
	w.E.validationMu.RLock()
	defer w.E.validationMu.RUnlock()
	status, items := w.E.librarySnapshot()
	if !status.Validated {
		return noteSuggestionSnapshot{}, catalog.ErrStaleNoteSuggestion
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if item.Fingerprint != fingerprint {
			break
		}
		if !suggestionEligible(item) {
			return noteSuggestionSnapshot{}, nil
		}
		if err := safePath(w.E.Source, item.Path); err != nil {
			return noteSuggestionSnapshot{}, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		if err := catalog.CheckSuggestionMembership(ctx, w.E.Source, item.Recording); err != nil {
			return noteSuggestionSnapshot{}, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		notes, err := readSuggestionNotes(ctx, w.E.Source, item.Recording)
		return noteSuggestionSnapshot{
			item: item, notes: notes, generation: status.Generation, datePolicy: w.E.DatePolicy(),
		}, err
	}
	return noteSuggestionSnapshot{}, catalog.ErrStaleNoteSuggestion
}

func readSuggestionNotes(ctx context.Context, root string, recording catalog.Recording) (map[string][]string, error) {
	available := make(map[string]catalog.Evidence, len(recording.Evidence))
	for _, evidence := range recording.Evidence {
		available[evidence.Path] = evidence
	}
	notes := make(map[string][]string, len(recording.Notes))
	remaining := maxSuggestionBytes
	remainingLines := maxSuggestionLines
	newline := strings.NewReplacer("\r\n", "\n", "\r", "\n")
	for _, path := range recording.Notes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		evidence, ok := available[path]
		if !ok || evidence.Size < 0 {
			return nil, catalog.ErrStaleNoteSuggestion
		}
		if evidence.Size > int64(remaining) {
			return nil, nil // Abstain if all notes cannot be checked together.
		}
		if err := safePath(root, path); err != nil {
			return nil, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		if err := media.CheckLocal(path); err != nil {
			return nil, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		name, err := catalog.SuggestionNoteName(recording.Path, path)
		if err != nil {
			return nil, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.Join(catalog.ErrStaleNoteSuggestion, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(f, evidence.Size+1))
		f.Close()
		checksum := sha256.Sum256(data)
		if readErr != nil || int64(len(data)) != evidence.Size || hex.EncodeToString(checksum[:]) != evidence.SHA256 {
			return nil, errors.Join(catalog.ErrStaleNoteSuggestion, readErr)
		}
		remaining -= len(data)
		text := string(data)
		if !utf8.Valid(data) {
			text, _ = charmap.Windows1252.NewDecoder().String(text)
		}
		text = newline.Replace(strings.TrimPrefix(text, "\uFEFF"))
		if text == "" {
			notes[name] = nil
			continue
		}
		text = strings.TrimSuffix(text, "\n")
		lineCount := strings.Count(text, "\n") + 1
		if lineCount > remainingLines {
			return nil, nil // Abstain if all lines cannot be checked together.
		}
		remainingLines -= lineCount
		notes[name] = strings.Split(text, "\n")
	}
	return notes, nil
}
