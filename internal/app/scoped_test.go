package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"track-tagger/internal/catalog"
	"track-tagger/internal/webui"
)

func scopedReview(item Item) webui.Review {
	v := webui.Review{
		Approve:     true,
		Fingerprint: item.Fingerprint,
		Metadata:    map[string]string{"ARTIST": item.Artist, "ALBUM": item.Album, "DATE": item.Date},
	}
	for _, track := range item.Tracks {
		v.Tracks = append(v.Tracks, webui.Track{File: track.Path, Title: track.Title, Artist: track.Artist, Track: track.Number, Disc: track.Disc})
	}
	return v
}

func TestScopedWebOperationsIgnoreBrokenOtherRecording(t *testing.T) {
	e, audio := engineFixture(t, Write)
	p := fixturePlan(t, e)
	selected := p.Recordings[0]
	other := filepath.Join(e.Source, "other")
	if err := os.Mkdir(other, 0755); err != nil {
		t.Fatal(err)
	}
	// The unrelated file is intentionally malformed. The strict fingerprint
	// hook below records any attempt to read its payload during either action.
	if err := os.WriteFile(filepath.Join(other, "t01.flac"), []byte("malformed audio"), 0644); err != nil {
		t.Fatal(err)
	}
	var fingerprinted []string
	e.strictFingerprint = func(path string) (catalog.Evidence, error) {
		fingerprinted = append(fingerprinted, path)
		return catalog.Fingerprint(path)
	}
	w := WebBackend{E: e}
	if err := w.SaveReview(context.Background(), selected.ID, scopedReview(selected)); err != nil {
		t.Fatal(err)
	}
	if len(fingerprinted) == 0 {
		t.Fatal("selected review did not freshly fingerprint inputs")
	}
	for _, path := range fingerprinted {
		if path != audio && path != filepath.Join(e.Source, "notes.txt") {
			t.Fatalf("review fingerprinted unrelated path: %s", path)
		}
	}
	fingerprinted = nil
	if err := w.Apply(context.Background(), selected.ID); err != nil {
		t.Fatal(err)
	}
	if len(fingerprinted) == 0 {
		t.Fatal("selected apply did not freshly fingerprint inputs")
	}
	for _, path := range fingerprinted {
		if path != audio && path != filepath.Join(e.Source, "notes.txt") {
			t.Fatalf("apply fingerprinted unrelated path: %s", path)
		}
	}
}

func TestScopedReviewRejectsAddedAndRemovedSelectedInputs(t *testing.T) {
	for _, change := range []string{"added playback", "removed notes"} {
		t.Run(change, func(t *testing.T) {
			e, _ := engineFixture(t, Write)
			selected := fixturePlan(t, e).Recordings[0]
			if change == "added playback" {
				if err := os.WriteFile(filepath.Join(e.Source, "t02.mp3"), []byte("synthetic audio"), 0644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(filepath.Join(e.Source, "notes.txt")); err != nil {
				t.Fatal(err)
			}
			err := (WebBackend{E: e}).SaveReview(context.Background(), selected.ID, scopedReview(selected))
			if err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatalf("stale selected review accepted: %v", err)
			}
		})
	}
}

func TestScopedApplyRejectsSameStatSelectedEdit(t *testing.T) {
	e, audio := engineFixture(t, Write)
	selected := fixturePlan(t, e).Recordings[0]
	w := WebBackend{E: e}
	if err := w.SaveReview(context.Background(), selected.ID, scopedReview(selected)); err != nil {
		t.Fatal(err)
	}
	before := checkedHash(t, audio)
	st, err := os.Stat(audio)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(audio, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteAt([]byte{99}, st.Size()-1)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("edit source: %v, %v", writeErr, closeErr)
	}
	if err := os.Chtimes(audio, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := w.Apply(context.Background(), selected.ID); err == nil {
		t.Fatal("same-stat external edit was applied")
	}
	if checkedHash(t, audio) == before {
		t.Fatal("test did not change source bytes")
	}
}

func TestScopedReviewRejectsForgedTrackFile(t *testing.T) {
	e, audio := engineFixture(t, Write)
	selected := fixturePlan(t, e).Recordings[0]
	other := filepath.Join(e.Source, "other")
	if err := os.Mkdir(other, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(other, "t01.flac")
	if err := os.WriteFile(foreign, data, 0644); err != nil {
		t.Fatal(err)
	}
	sourceHash, foreignHash := checkedHash(t, audio), checkedHash(t, foreign)
	forged := scopedReview(selected)
	forged.Tracks[0].File = foreign
	err = (WebBackend{E: e}).SaveReview(context.Background(), selected.ID, forged)
	if err == nil || !strings.Contains(err.Error(), "unknown file") {
		t.Fatalf("forged track file accepted: %v", err)
	}
	var saved Item
	_, exists, err := e.store.review(selected.ID, &saved)
	if err != nil || exists {
		t.Fatalf("invalid review persisted: exists %v, error %v", exists, err)
	}
	if checkedHash(t, audio) != sourceHash || checkedHash(t, foreign) != foreignHash {
		t.Fatal("rejected review changed source bytes")
	}
}
