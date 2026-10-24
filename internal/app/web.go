package app

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/text/encoding/charmap"
	"io"
	"os"
	"reflect"
	"strings"
	"track-tagger/internal/media"
	"unicode/utf8"

	"track-tagger/internal/catalog"
	"track-tagger/internal/webui"
)

type WebBackend struct{ E *Engine }

func (w WebBackend) List(ctx context.Context) ([]webui.Recording, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status, _ := w.E.librarySnapshot()
	rows := make([]webui.Recording, len(status.Recordings))
	for i, r := range status.Recordings {
		rows[i] = librarySummary(r)
	}
	return rows, nil
}
func webRecord(r Item) webui.Recording {
	out := webui.Recording{ID: r.ID, Fingerprint: r.Fingerprint, Path: r.Path, RelativePath: r.RelativePath, Reviewed: r.Reviewed, PlaybackOnly: r.PlaybackOnly, DateUnknown: r.DateUnknown, Status: r.Status, Metadata: map[string]string{"ARTIST": r.Artist, "ALBUM": r.Album, "DATE": r.Date}, Artwork: r.ArtworkCandidates, SelectedArtwork: r.Artwork, Reasons: r.ReviewReasons}
	for _, ev := range r.Evidence {
		if ev.ModTime > out.LastModified {
			out.LastModified = ev.ModTime
		}
	}
	for _, t := range r.Tracks {
		out.Tracks = append(out.Tracks, webui.Track{File: t.Path, Title: t.Title, Artist: t.Artist, Track: t.Number, Disc: t.Disc})
	}
	for _, ev := range r.Evidence {
		out.Evidence = append(out.Evidence, fmt.Sprintf("%s · SHA256 %s", ev.Path, ev.SHA256))
	}
	for _, path := range r.Notes {
		if media.CheckLocal(path) != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 4<<20))
		f.Close()
		if err != nil {
			continue
		}
		text := string(b)
		if !utf8.Valid(b) {
			text, _ = charmap.Windows1252.NewDecoder().String(text)
		}
		out.Notes = append(out.Notes, webui.Note{Path: path, Text: text})
	}
	return out
}
func (w WebBackend) Get(ctx context.Context, id string) (webui.Recording, error) {
	if err := ctx.Err(); err != nil {
		return webui.Recording{}, err
	}
	w.E.prioritize(id)
	status, _ := w.E.librarySnapshot()
	for _, r := range status.Recordings {
		if r.ID == id {
			r = cloneWebRecording(r)
			w.E.registerPlayback(&r)
			return r, nil
		}
	}
	if status.Phase == "validating" {
		return webui.Recording{ID: id, Status: "loading", Metadata: map[string]string{}}, nil
	}
	return webui.Recording{}, errors.New("recording not found")
}
func (w WebBackend) SaveReview(ctx context.Context, id string, v webui.Review) error {
	if !v.Approve {
		return errors.New("explicit review acknowledgment required")
	}
	w.E.invalidateRecordings(id)
	defer w.E.invalidateRecordings(id)
	w.E.validationMu.Lock()
	defer w.E.validationMu.Unlock()
	w.E.mu.Lock()
	defer w.E.mu.Unlock()
	if err := w.E.OpenStorage(); err != nil {
		return err
	}
	base, err := w.E.recording(id)
	if err != nil {
		return err
	}
	r := Item{Recording: base}
	// The discovery result remains the authority for allowed track paths.
	r.Tracks = append([]catalog.Track(nil), base.Tracks...)

	if v.Fingerprint != r.Fingerprint {
		return errors.New("inputs changed since this page was loaded; refresh and review again")
	}
	for k := range v.Metadata {
		if k != "ARTIST" && k != "ALBUM" && k != "DATE" {
			return fmt.Errorf("unsupported album field %s", k)
		}
	}
	r.Artist = v.Metadata["ARTIST"]
	r.Album = v.Metadata["ALBUM"]
	r.Date = v.Metadata["DATE"]
	r.DateUnknown = v.DateUnknown
	r.Artwork = v.Artwork
	if len(v.Tracks) != len(r.Tracks) {
		return errors.New("map every file")
	}
	for i, t := range v.Tracks {
		r.Tracks[i].Path = t.File
		r.Tracks[i].Title = t.Title
		r.Tracks[i].Artist = t.Artist
		r.Tracks[i].Disc = t.Disc
		r.Tracks[i].Number = t.Track
	}
	return w.E.saveReview(base, r)
}
func (w WebBackend) Apply(ctx context.Context, id string) error {
	return w.applySelection(ctx, id, func(r Item) error {
		if r.Status != "ready" {
			return fmt.Errorf("recording is %s", r.Status)
		}
		return nil
	})
}
func (w WebBackend) applySelection(ctx context.Context, id string, check func(Item) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e := w.E
	e.invalidateRecordings(id)
	defer e.invalidateRecordings(id)
	e.validationMu.Lock()
	defer e.validationMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	path, err := e.recordingPath(id)
	if err != nil {
		return err
	}
	placeholder := Item{}
	placeholder.ID, placeholder.Path, placeholder.Status = id, path, "ready"
	_, err = e.applyLocked(Plan{Version: 1, Source: e.Source, Recordings: []Item{placeholder}}, check)
	return err
}
func (w WebBackend) Undo(ctx context.Context, id string) error {
	_, err := w.E.Undo(id, true)
	return err
}
func (w WebBackend) History(ctx context.Context) ([]webui.Job, error) {
	w.E.mu.Lock()
	defer w.E.mu.Unlock()
	if err := w.E.OpenStorage(); err != nil {
		return nil, err
	}
	jobs, err := w.E.store.History()
	var out []webui.Job
	for _, j := range jobs {
		out = append(out, webui.Job{ID: j.ID, RecordingID: j.RecordingID, Status: j.Status, Summary: j.Summary})
	}
	return out, err
}

// ApplyReview binds both actions to the submitted page and fresh source evidence.
func (w WebBackend) ApplyReview(ctx context.Context, id string, v webui.Review) error {
	if w.E.Mode == DryRun {
		return errors.New("preview mode cannot change media; restart with --write or --copy")
	}
	saved := false
	if v.Approve {
		if err := w.SaveReview(ctx, id, v); err != nil {
			return err
		}
		saved = true
	}
	err := w.applySubmitted(ctx, id, v)
	if err != nil && saved {
		return fmt.Errorf("review saved but not applied: %w; reopen the recording and use Apply saved review to retry", err)
	}
	return err
}
func (w WebBackend) applySubmitted(ctx context.Context, id string, v webui.Review) error {
	return w.applySelection(ctx, id, func(r Item) error {
		if v.Fingerprint != r.Fingerprint {
			return errors.New("inputs changed since this page was loaded; refresh and review again")
		}
		if !r.Reviewed {
			return errors.New("save an approved review before applying")
		}
		expected := webui.Review{DateUnknown: r.DateUnknown, Metadata: map[string]string{"ARTIST": r.Artist, "ALBUM": r.Album, "DATE": r.Date}, Artwork: r.Artwork}
		for _, t := range r.Tracks {
			expected.Tracks = append(expected.Tracks, webui.Track{File: t.Path, Title: t.Title, Artist: t.Artist, Disc: t.Disc, Track: t.Number})
		}
		tracks := append([]webui.Track(nil), v.Tracks...)
		for i := range tracks {
			tracks[i].Artist = strings.TrimSpace(tracks[i].Artist)
		}
		if v.DateUnknown != expected.DateUnknown || !reflect.DeepEqual(v.Metadata, expected.Metadata) || v.Artwork != expected.Artwork || !reflect.DeepEqual(tracks, expected.Tracks) {
			return errors.New("submitted values differ from the saved review; approve and save your edits before applying")
		}
		if r.Status != "ready" && r.Status != "failed" {
			return fmt.Errorf("recording is %s; refresh before applying", r.Status)
		}
		return nil
	})
}
