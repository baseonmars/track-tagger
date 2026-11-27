package app

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func notesItem(t *testing.T, e *Engine) Item {
	t.Helper()
	p, err := e.Scan()
	if err != nil || len(p.Recordings) != 1 {
		t.Fatalf("scan: %+v, %v", p, err)
	}
	return p.Recordings[0]
}

func checkNotesBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q, %v, want %q", path, got, err, want)
	}
}

func TestAppendNotesLinesPreservesBytesAndLineEndings(t *testing.T) {
	for _, tc := range []struct{ original, suffix string }{
		{"\uFEFFArtist: Björk\r\nCredits: Tape holder\r\n", notesSignature + "\r\n"},
		{"Credits: Ren\xe9\r\n01. First Song", "\r\n" + notesSignature},
		{"01. First Song\rCredits: Tape holder\r", notesSignature + "\r"},
		{"01. First Song", "\n" + notesSignature},
		{"Credits\n " + notesSignature + "\t\n", ""},
	} {
		got, err := appendNotesLines([]byte(tc.original), notesSignature)
		if err != nil || string(got) != tc.original+tc.suffix {
			t.Fatalf("%q: got %q, %v; want %q", tc.original, got, err, tc.original+tc.suffix)
		}
	}
	if _, err := appendNotesLines([]byte{0xff, 0xfe, 'c', 0}, notesSignature); err == nil {
		t.Fatal("edited unsupported encoding")
	}
}

func TestShowNotesSettingAndReviewedWrite(t *testing.T) {
	ctx := context.Background()
	e, _ := engineFixture(t, Write)
	w := WebBackend{E: e}
	source := filepath.Join(e.Source, "notes.txt")
	original := []byte("Artist: Band\r\nDate: 1970-01-20\r\nVenue: Hall\r\nCity: London\r\n01. First Song\r\nCredits: Ren\xe9")
	if err := os.WriteFile(source, original, 0644); err != nil {
		t.Fatal(err)
	}
	on, err := w.ShowNotesSignature(ctx)
	if err != nil || on {
		t.Fatalf("new setting: %t, %v", on, err)
	}
	if _, err := e.Apply(fixturePlan(t, e)); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, source, original)
	if err := w.SaveShowNotesSignature(ctx, true); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(e.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if on, err = (WebBackend{E: reopened}).ShowNotesSignature(ctx); err != nil || !on {
		t.Fatalf("reopened setting: %t, %v", on, err)
	}
	item := notesItem(t, e)
	if item.NotesPreview == nil || item.NotesPreview.Target != source || item.NotesPreview.Addition != "\r\n"+notesSignature || item.Reviewed {
		t.Fatalf("unexpected proposal: %+v", item)
	}
	item.AddNotesSignature = true
	if err = e.SaveReview(item.ID, item); err != nil {
		t.Fatal(err)
	}
	if err := w.SaveShowNotesSignature(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(fixturePlan(t, e)); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, source, original)
	if err := w.SaveShowNotesSignature(ctx, true); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.Apply(fixturePlan(t, e))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v, %v", jobs, err)
	}
	want := append(bytes.Clone(original), "\r\n"+notesSignature...)
	checkNotesBytes(t, source, want)
	next := notesItem(t, e)
	if next.Status != "completed" || next.NotesPreview == nil || !strings.Contains(next.NotesPreview.Reason, "already") {
		t.Fatalf("unexpected repeat preview: %+v", next)
	}
	next.Status = "ready"
	if _, err = e.Apply(Plan{Version: 1, Source: e.Source, Recordings: []Item{next}}); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, source, want)
	if _, err = e.Undo(jobs[0], true); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, source, original)
}

func TestShowNotesCopyUndoGuardsExternalEdit(t *testing.T) {
	ctx := context.Background()
	e, _ := engineFixture(t, Copy)
	source := filepath.Join(e.Source, "notes.txt")
	destination := filepath.Join(e.Destination, "notes.txt")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	w := WebBackend{E: e}
	if err = w.SaveShowNotesSignature(ctx, true); err != nil {
		t.Fatal(err)
	}
	item := notesItem(t, e)
	if item.NotesPreview == nil || item.NotesPreview.Target != destination || item.NotesPreview.Addition != notesSignature+"\n" || item.Status != "needs-review" {
		t.Fatalf("unexpected copy preview: %+v", item)
	}
	if err = e.SaveReview(item.ID, item); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.Apply(fixturePlan(t, e))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v, %v", jobs, err)
	}
	signed := append(bytes.Clone(original), notesSignature+"\n"...)
	checkNotesBytes(t, source, original)
	checkNotesBytes(t, destination, signed)
	next := notesItem(t, e)
	if next.Status != "completed" || next.NotesPreview == nil || next.NotesPreview.Addition != "" {
		t.Fatalf("unexpected copy completion: %+v", next)
	}
	next.Status = "ready"
	if _, err = e.Apply(Plan{Version: 1, Source: e.Source, Recordings: []Item{next}}); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, destination, signed)
	if err = os.WriteFile(destination, []byte("external edit"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Undo(jobs[0], true); err == nil || !strings.Contains(err.Error(), "external edit") {
		t.Fatalf("undo accepted external edit: %v", err)
	}
	checkNotesBytes(t, destination, []byte("external edit"))
	if err = os.WriteFile(destination, signed, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Undo(jobs[0], true); err != nil {
		t.Fatal(err)
	}
	checkNotesBytes(t, source, original)
	checkNotesBytes(t, destination, original)
}

func TestShowNotesReviewSelectsOnlyOneFile(t *testing.T) {
	e, audio := engineFixture(t, Write)
	audioBefore := checkedHash(t, audio)
	source := filepath.Join(e.Source, "notes.txt")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(e.Source, "credits.nfo")
	credits := []byte("Source and transfer credits\n")
	if err = os.WriteFile(selected, credits, 0644); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(e.Source, "unsupported.nfo")
	binary := []byte{'A', 0, 'B', 0}
	if err = os.WriteFile(unsafe, binary, 0644); err != nil {
		t.Fatal(err)
	}
	web := realWeb(t, e, true)
	settings := web.request(http.MethodGet, "/settings", nil, http.StatusOK)
	checkbox := regexp.MustCompile(`<input[^>]+name="notes_signature"[^>]*>`)
	if input := checkbox.FindString(settings); input == "" || strings.Contains(input, "checked") {
		t.Fatalf("missing or preselected preference: %q", input)
	}
	form := renderedForm(t, settings, "/settings")
	form.Set("notes_signature", "yes")
	web.request(http.MethodPost, "/settings", form, http.StatusSeeOther)
	_, page := web.recordingPage()
	if !strings.Contains(page, "Choose which show-notes file to edit.") || !strings.Contains(page, "credits.nfo") {
		t.Fatal("missing preview for ambiguous notes file")
	}
	apply := renderedForm(t, page, "/apply")
	apply.Set("approve", "yes")
	apply.Set("notes_signature", "yes")
	if result := web.request(http.MethodPost, "/apply", apply, http.StatusConflict); !strings.Contains(result, "Choose which show-notes file to edit.") {
		t.Fatalf("apply did not explain ambiguous target: %s", result)
	}
	if checkedHash(t, audio) != audioBefore {
		t.Fatal("ambiguous notes target allowed an audio write")
	}
	checkNotesBytes(t, source, original)
	checkNotesBytes(t, selected, credits)
	_, page = web.recordingPage()
	apply = renderedForm(t, page, "/apply")
	apply.Set("approve", "yes")
	apply.Set("notes_signature", "yes")
	apply.Set("notes_target", filepath.Join(e.State, "history.sqlite"))
	web.request(http.MethodPost, "/apply", apply, http.StatusConflict)
	apply.Set("notes_target", unsafe)
	if result := web.request(http.MethodPost, "/apply", apply, http.StatusConflict); !strings.Contains(result, "unsupported notes encoding") {
		t.Fatalf("apply did not explain unsafe notes: %s", result)
	}
	if checkedHash(t, audio) != audioBefore {
		t.Fatal("unsafe notes target allowed an audio write")
	}
	apply.Set("notes_target", selected)
	web.request(http.MethodPost, "/apply", apply, http.StatusSeeOther)
	checkNotesBytes(t, source, original)
	checkNotesBytes(t, selected, append(bytes.Clone(credits), notesSignature+"\n"...))
	checkNotesBytes(t, unsafe, binary)
}

func TestWatcherAppliesSavedShowNotesChoice(t *testing.T) {
	e, _ := engineFixture(t, Write)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := (WebBackend{E: e}).SaveShowNotesSignature(ctx, true); err != nil {
		t.Fatal(err)
	}
	item := notesItem(t, e)
	if item.Status != "needs-review" || item.NotesPreview == nil || item.NotesPreview.Addition == "" {
		t.Fatalf("unapproved signature must await review: %+v", item)
	}
	if err := e.SaveReview(item.ID, item); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- e.Watch(ctx, true, time.Second, func(line string) {
			if strings.HasPrefix(line, "completed: ") {
				cancel()
			}
		})
	}()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.Source, "notes.txt"))
	if err != nil || !bytes.HasSuffix(data, []byte(notesSignature+"\n")) {
		t.Fatalf("watcher did not use approved signature: %q, %v", data, err)
	}
}
