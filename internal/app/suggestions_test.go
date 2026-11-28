package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"track-tagger/internal/catalog"
	"track-tagger/internal/notesmodel"
)

type suggestionTestPredictor struct {
	input          map[string][]string
	requestContext context.Context
}

func (p *suggestionTestPredictor) Predict(notes map[string][]string) ([]notesmodel.Span, error) {
	p.input = notes
	return []notesmodel.Span{{Field: "venue", Note: "notes.txt", Line: 2, Start: 0, End: 11}}, nil
}

func (p *suggestionTestPredictor) PredictContext(ctx context.Context, notes map[string][]string) ([]notesmodel.Span, error) {
	p.requestContext = ctx
	return p.Predict(notes)
}

func TestWebSuggestionsBindScopedNoteBytesAndPageFingerprint(t *testing.T) {
	e, audio := engineFixture(t, DryRun)
	path := filepath.Join(e.Source, "notes.txt")
	text := []byte("\uFEFFArtist: Band\r\nDate: 1970-01-20\r\nBjörk Arena\r\n02. Other Song\r\n")
	if err := os.WriteFile(path, text, 0644); err != nil {
		t.Fatal(err)
	}
	awaitWebLibrary(t, e)
	model := &suggestionTestPredictor{}
	backend := WebBackend{E: e, NoteModel: model}
	rows, err := backend.List(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %+v, %v", rows, err)
	}
	page, err := backend.Get(t.Context(), rows[0].ID)
	if err != nil || page.Status != "needs-review" {
		t.Fatalf("page: %+v, %v", page, err)
	}
	audioHash := checkedHash(t, audio)
	e.strictFingerprint = func(string) (catalog.Evidence, error) {
		return catalog.Evidence{}, errors.New("suggestions must not fingerprint audio")
	}
	got, err := backend.Suggestions(t.Context(), page.ID, page.Fingerprint)
	if err != nil || len(got) != 1 || got[0].Quote != "Björk Arena" || got[0].Field != "VENUE" {
		t.Fatalf("suggestions: %+v, %v", got, err)
	}
	if model.requestContext != t.Context() {
		t.Fatal("request context did not reach note inference")
	}
	want := map[string][]string{"notes.txt": {"Artist: Band", "Date: 1970-01-20", "Björk Arena", "02. Other Song"}}
	if !reflect.DeepEqual(model.input, want) {
		t.Fatalf("model input: %+v", model.input)
	}
	if before, after := audioHash, checkedHash(t, audio); before != after {
		t.Fatal("media changed during suggestions")
	}
	extra := filepath.Join(e.Source, "02-1970-01-21.flac")
	if err := os.WriteFile(extra, []byte("audio filename inventory"), 0644); err != nil {
		t.Fatal(err)
	}
	model.input = nil
	if _, err := backend.Suggestions(t.Context(), page.ID, page.Fingerprint); !errors.Is(err, catalog.ErrStaleNoteSuggestion) || model.input != nil {
		t.Fatalf("changed filename inventory reached model: %v, %+v", err, model.input)
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	text[len(text)-4] = 'X'
	if err := os.WriteFile(path, text, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	model.input = nil
	if _, err := backend.Suggestions(t.Context(), page.ID, page.Fingerprint); !errors.Is(err, catalog.ErrStaleNoteSuggestion) || model.input != nil {
		t.Fatalf("same-size stale content reached model: %v, %+v", err, model.input)
	}
}

type blockingSuggestionPredictor struct {
	started chan struct{}
	release chan struct{}
}

func (p blockingSuggestionPredictor) Predict(map[string][]string) ([]notesmodel.Span, error) {
	close(p.started)
	<-p.release
	return []notesmodel.Span{{Field: "venue", Note: "notes.txt", Line: 2, Start: 0, End: 11}}, nil
}

func TestWebSuggestionsRejectDatePolicyChangeDuringInference(t *testing.T) {
	e, _ := engineFixture(t, DryRun)
	path := filepath.Join(e.Source, "notes.txt")
	text := []byte("Artist: Band\nDate: 03/04/1970\nBjörk Arena\n02. Other Song\n")
	if err := os.WriteFile(path, text, 0644); err != nil {
		t.Fatal(err)
	}
	awaitWebLibrary(t, e)
	model := blockingSuggestionPredictor{started: make(chan struct{}), release: make(chan struct{})}
	defer close(model.release)
	backend := WebBackend{E: e, NoteModel: model}
	rows, err := backend.List(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %+v, %v", rows, err)
	}
	view, err := backend.Get(t.Context(), rows[0].ID)
	if err != nil || view.Status != "needs-review" {
		t.Fatalf("page: %+v, %v", view, err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := backend.Suggestions(t.Context(), view.ID, view.Fingerprint)
		result <- err
	}()
	select {
	case <-model.started:
	case err := <-result:
		t.Fatalf("model never started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("model did not start")
	}
	if _, err := backend.Suggestions(t.Context(), view.ID, view.Fingerprint); !errors.Is(err, catalog.ErrNoteSuggestionsBusy) {
		t.Fatalf("second request during inference: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := backend.Suggestions(ctx, view.ID, view.Fingerprint); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled request during inference: %v", err)
	}
	toggled := make(chan struct{})
	go func() {
		backend.SetUSDates(false)
		close(toggled)
	}()
	select {
	case <-toggled:
	case <-time.After(2 * time.Second):
		t.Fatal("date setting could not change during inference")
	}
	// Release the model before reading its result.
	model.release <- struct{}{}
	select {
	case err := <-result:
		if !errors.Is(err, catalog.ErrStaleNoteSuggestion) {
			t.Fatalf("stale inference result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("inference did not return")
	}
}

func TestReadSuggestionNotesBoundsTotalLineCount(t *testing.T) {
	root := t.TempDir()
	var recording catalog.Recording
	recording.Path = root
	add := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		evidence, err := catalog.Fingerprint(path)
		if err != nil {
			t.Fatal(err)
		}
		recording.Evidence = append(recording.Evidence, evidence)
		recording.Notes = append(recording.Notes, path)
	}
	add("first.txt", strings.Repeat("x\r\n", maxSuggestionLines))
	got, err := readSuggestionNotes(t.Context(), root, recording)
	if err != nil || len(got["first.txt"]) != maxSuggestionLines {
		t.Fatalf("at line limit, got %d lines and error %v", len(got["first.txt"]), err)
	}
	add("second.txt", "y")
	got, err = readSuggestionNotes(t.Context(), root, recording)
	if err != nil || got != nil {
		t.Fatalf("over line limit, got %d notes and error %v", len(got), err)
	}
}
