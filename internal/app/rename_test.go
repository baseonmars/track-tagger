package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	flac "github.com/go-flac/go-flac/v2"
	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

func renameFixtureAudio(t *testing.T, path string, marker byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	recording := flac.File{
		Meta: []*flac.MetaDataBlock{
			{Type: flac.StreamInfo, Data: make([]byte, 34)},
			{Type: flac.Application, Data: []byte("TESTkeep unknown blocks")},
		},
		Frames: bytes.NewReader([]byte{0xff, 0xf8, 0, 1, 2, marker}),
	}
	_, err = recording.WriteTo(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewRenameMismatchLeavesFirstFileUntouched(t *testing.T) {
	e, first := engineFixture(t, Write)
	var notes strings.Builder
	notes.WriteString("Artist: Grateful Dead\nDate: 1979-11-08\nVenue: Hall\nCity: London\n")
	before := make(map[string]string)
	for i := 1; i <= 11; i++ {
		p := filepath.Join(e.Source, fmt.Sprintf("t%02d.flac", i))
		renameFixtureAudio(t, p, byte(i))
		before[p] = checkedHash(t, p)
		if i < 11 {
			fmt.Fprintf(&notes, "%02d. Song %d\n", i, i)
		}
	}
	notesPath := filepath.Join(e.Source, "notes.txt")
	if err := os.WriteFile(notesPath, []byte(notes.String()), 0644); err != nil {
		t.Fatal(err)
	}
	notesHash := checkedHash(t, notesPath)

	s := realWeb(t, e, true)
	pagePath, page := s.recordingPage()
	for _, snippet := range []string{"Rename FLAC files", "Leave this file untouched", "track count mismatch", `data-original="t01.flac"`} {
		if !strings.Contains(page, snippet) {
			t.Fatalf("missing review control or context %q", snippet)
		}
	}
	form := renderedForm(t, page, "/review")
	form.Set("rename_files", "yes")
	form.Set("track_skip", first)
	for i := 0; i < 11; i++ {
		form["track_number"][i] = fmt.Sprint(i)
		form["track_title"][i] = fmt.Sprintf("Song %d", i)
	}
	form["track_title"][0] = ""
	form["track_disc"][0] = ""
	form["track_title"][1] = "Uncle John's Band"
	form["track_title"][2] = "Déjà Vu"
	s.request(http.MethodPost, "/review", form, http.StatusSeeOther)
	for p, hash := range before {
		if checkedHash(t, p) != hash {
			t.Fatalf("save changed %s", p)
		}
	}
	page = s.request(http.MethodGet, pagePath, nil, http.StatusOK)
	if !strings.Contains(page, `name="rename_files" value="yes" checked`) {
		t.Fatal("saved rename decision missing")
	}
	originalSongOne := filepath.Join(e.Source, "t02.flac")
	info, err := media.Inspect(originalSongOne)
	if err != nil {
		t.Fatal(err)
	}
	form.Del("approve")
	s.request(http.MethodPost, "/apply", form, http.StatusSeeOther)
	requireAbsent(t, originalSongOne)
	songOne, err := media.Inspect(filepath.Join(e.Source, "01 Uncle John's Band.flac"))
	if err != nil || songOne.AudioHash != info.AudioHash || songOne.Tags["TRACKNUMBER"] != "1" || songOne.Tags["TRACKTOTAL"] != "10" || songOne.Tags["TITLE"] != "Uncle John's Band" {
		t.Fatalf("wrong mapping or tags: %+v %v", songOne, err)
	}
	if _, err := os.Stat(filepath.Join(e.Source, "02 Déjà Vu.flac")); err != nil {
		t.Fatal(err)
	}
	if checkedHash(t, first) != before[first] || checkedHash(t, notesPath) != notesHash {
		t.Fatal("apply changed the excluded file or notes")
	}
	p, err := e.Scan()
	if err != nil || len(p.Recordings) != 1 || p.Recordings[0].Status != "completed" || !p.Recordings[0].Reviewed {
		t.Fatalf("completion expired after rename: %+v %v", p, err)
	}
	jobs, err := e.store.History()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("history: %+v %v", jobs, err)
	}
	if _, err = e.Undo(jobs[0].ID, true); err != nil {
		t.Fatal(err)
	}
	for p, hash := range before {
		if checkedHash(t, p) != hash {
			t.Fatalf("undo failed to restore original %s", p)
		}
	}
	requireAbsent(t, filepath.Join(e.Source, "01 Uncle John's Band.flac"))
}

func TestRenameCopyMovesOwnedCopiesOnly(t *testing.T) {
	e, first := engineFixture(t, Copy)
	original := checkedHash(t, first)
	p := fixturePlan(t, e)
	if _, err := e.Apply(p); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(e.Destination, filepath.Base(first))
	if _, err := os.Stat(prior); err != nil {
		t.Fatal(err)
	}
	r := p.Recordings[0]
	r.RenameFiles = true
	r.Tracks[0].Title = "Déjà Vu / Reprise?"
	if err := e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.Apply(fixturePlan(t, e))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v %v", jobs, err)
	}
	requireAbsent(t, prior)
	target := filepath.Join(e.Destination, "01 Déjà Vu _ Reprise_.flac")
	tagged, err := media.Inspect(target)
	if err != nil || tagged.Tags["TITLE"] != "Déjà Vu / Reprise?" {
		t.Fatalf("copy metadata: %+v %v", tagged, err)
	}
	if original != checkedHash(t, first) || checkedHash(t, filepath.Join(e.Source, "notes.txt")) != checkedHash(t, filepath.Join(e.Destination, "notes.txt")) {
		t.Fatal("source or companion changed")
	}
	if _, err = e.Undo(jobs[0], true); err != nil {
		t.Fatal(err)
	}
	restored, err := media.Inspect(target)
	if err != nil || restored.Tags["TITLE"] != "First Song" {
		t.Fatalf("undo of copied metadata: %+v %v", restored, err)
	}
	requireAbsent(t, prior)
}

func TestRenameChecksAllDestinationsBeforeMoving(t *testing.T) {
	for _, scenario := range []string{"duplicate", "case", "manifest"} {
		t.Run(scenario, func(t *testing.T) {
			e, first := engineFixture(t, Write)
			second := filepath.Join(e.Source, "t02.flac")
			renameFixtureAudio(t, second, 2)
			before := []string{checkedHash(t, first), checkedHash(t, second)}
			switch scenario {
			case "case":
				if err := os.Mkdir(filepath.Join(e.Source, "02 SECOND SONG.FLAC"), 0755); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(filepath.Join(e.Source, "release.ffp"), []byte("t01.flac:0123456789abcdef0123456789abcdef\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			p, err := e.Scan()
			if err != nil {
				t.Fatal(err)
			}
			r := p.Recordings[0]
			r.RenameFiles = true
			r.Tracks[1].Title = "Second Song"
			want := "t02.flac"
			switch scenario {
			case "duplicate":
				r.Tracks[1].Disc, r.Tracks[1].Number, r.Tracks[1].Title = 2, 1, r.Tracks[0].Title
				want = "t01.flac"
			case "manifest":
				want = "checksum manifest release.ffp"
			}
			if err = e.SaveReview(r.ID, r); err != nil {
				t.Fatal(err)
			}
			jobs, err := e.Apply(fixturePlan(t, e))
			if err == nil || !strings.Contains(err.Error(), want) || len(jobs) != 0 || (scenario == "duplicate" && !strings.Contains(err.Error(), "t02.flac")) {
				t.Fatalf("conflict: jobs=%v err=%v", jobs, err)
			}
			for i, path := range []string{first, second} {
				if checkedHash(t, path) != before[i] {
					t.Fatalf("preflight changed %s", path)
				}
			}
			requireAbsent(t, filepath.Join(e.Source, "01 First Song.flac"))
		})
	}
}

func TestRenameRetriesAfterAllOriginalPathsAreEvacuated(t *testing.T) {
	for _, retry := range []string{"explicit apply", "startup scan"} {
		t.Run(retry, func(t *testing.T) {
			e, first := engineFixture(t, Write)
			original := checkedHash(t, first)
			r := fixturePlan(t, e).Recordings[0]
			r.RenameFiles = true
			if err := e.SaveReview(r.ID, r); err != nil {
				t.Fatal(err)
			}
			p := fixturePlan(t, e)
			if _, err := e.store.db.Exec("CREATE TRIGGER interrupt_transition BEFORE UPDATE OF plan ON rename_batches BEGIN SELECT RAISE(FAIL, 'interrupted'); END"); err != nil {
				t.Fatal(err)
			}
			jobs, err := e.Apply(p)
			if err == nil || len(jobs) != 1 {
				t.Fatalf("expected interrupted rename: %v %v", jobs, err)
			}
			target := filepath.Join(e.Source, "01 First Song.flac")
			requireAbsent(t, first)
			requireAbsent(t, target)
			if _, err = e.store.db.Exec("DROP TRIGGER interrupt_transition"); err != nil {
				t.Fatal(err)
			}
			if err = e.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(e.Options)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			if retry == "startup scan" {
				if err = restarted.RecoverRenames(); err != nil {
					t.Fatal(err)
				}
			} else {
				got, err := restarted.Apply(p)
				if err != nil || !reflect.DeepEqual(got, jobs) {
					t.Fatalf("retry did not finish the original job: %v %v", got, err)
				}
			}
			fresh, err := restarted.Scan()
			if err != nil || len(fresh.Recordings) != 1 || fresh.Recordings[0].Status != "completed" || !fresh.Recordings[0].RenameFiles {
				t.Fatalf("resumed review: %+v %v", fresh, err)
			}
			if _, err := restarted.Undo(jobs[0], true); err != nil {
				t.Fatal(err)
			}
			if checkedHash(t, first) != original {
				t.Fatal("undo did not recover the original file")
			}
			matches, err := filepath.Glob(filepath.Join(e.Source, catalog.ReservedFilePrefix+"*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("leftover stages or backups: %v %v", matches, err)
			}
		})
	}
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent: %v", path, err)
	}
}

func TestRenameCanSwapReviewedPaths(t *testing.T) {
	e, first := engineFixture(t, Write)
	left := filepath.Join(e.Source, "01 First Song.flac")
	right := filepath.Join(e.Source, "02 Second Song.flac")
	if err := os.Rename(first, left); err != nil {
		t.Fatal(err)
	}
	renameFixtureAudio(t, right, 2)
	oldLeft, err := media.Inspect(left)
	if err != nil {
		t.Fatal(err)
	}
	oldRight, err := media.Inspect(right)
	if err != nil {
		t.Fatal(err)
	}
	r := notesItem(t, e)
	r.RenameFiles = true
	r.Tracks[0].Number, r.Tracks[0].Title = 2, "Second Song"
	r.Tracks[1].Number, r.Tracks[1].Title = 1, "First Song"
	if err = e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	jobs, err := e.Apply(fixturePlan(t, e))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v %v", jobs, err)
	}
	leftInfo, err := media.Inspect(left)
	if err != nil {
		t.Fatal(err)
	}
	rightInfo, err := media.Inspect(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftInfo.AudioHash != oldRight.AudioHash || rightInfo.AudioHash != oldLeft.AudioHash {
		t.Fatal("tracks remained in their original files")
	}
	if _, err = e.Undo(jobs[0], true); err != nil {
		t.Fatal(err)
	}
	if checkedHash(t, left) != oldLeft.FileHash || checkedHash(t, right) != oldRight.FileHash {
		t.Fatal("undo did not restore the original files")
	}
}

func TestRenameAndApprovedNotesShareUndo(t *testing.T) {
	for _, mode := range []Mode{Write, Copy} {
		t.Run(string(mode), func(t *testing.T) {
			e, audio := engineFixture(t, mode)
			notes := filepath.Join(e.Source, "notes.txt")
			before, err := os.ReadFile(notes)
			if err != nil {
				t.Fatal(err)
			}
			if err = (WebBackend{E: e}).SaveShowNotesSignature(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			r := notesItem(t, e)
			r.RenameFiles, r.AddNotesSignature = true, true
			if err = e.SaveReview(r.ID, r); err != nil {
				t.Fatal(err)
			}
			jobs, err := e.Apply(fixturePlan(t, e))
			if err != nil || len(jobs) != 1 {
				t.Fatalf("apply: %v %v", jobs, err)
			}
			targetNotes, err := e.destination(notes)
			if err != nil {
				t.Fatal(err)
			}
			want := append(bytes.Clone(before), notesSignature+"\n"...)
			checkNotesBytes(t, targetNotes, want)
			if mode == Copy {
				checkNotesBytes(t, notes, before)
				r = notesItem(t, e)
				r.AddNotesSignature = false
				r.Tracks[0].Title = "Another title"
				if err = e.SaveReview(r.ID, r); err != nil {
					t.Fatal(err)
				}
				if _, err = e.Apply(fixturePlan(t, e)); err != nil {
					t.Fatal(err)
				}
				checkNotesBytes(t, targetNotes, want)
				return
			}
			if _, err = e.Undo(jobs[0], true); err != nil {
				t.Fatal(err)
			}
			checkNotesBytes(t, notes, before)
			if _, err = os.Stat(audio); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFilenameSanitizationMatchesPreview(t *testing.T) {
	for _, fixture := range []struct {
		number      int
		title, want string
	}{
		{1, "Uncle John's Band", "01 Uncle John's Band.flac"},
		{2, "Déjà Vu.", "02 Déjà Vu..flac"},
		{123, "  reprise / part 2: a?  ", "123 reprise _ part 2_ a_.flac"},
		{99, strings.Repeat("🎶", 70), "99 " + strings.Repeat("🎶", 61) + ".flac"},
	} {
		if got := renameFilename(fixture.number, fixture.title); got != fixture.want {
			t.Fatalf("unexpected name %q, want %q", got, fixture.want)
		}
	}
}

func TestRenameRetriesAfterPublishingOneTrack(t *testing.T) {
	e, first := engineFixture(t, Write)
	second := filepath.Join(e.Source, "t02.flac")
	renameFixtureAudio(t, second, 2)
	r := notesItem(t, e)
	r.Tracks[1].Title = "Second Song"
	r.RenameFiles = true
	if err := e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	p := fixturePlan(t, e)
	target := filepath.Join(e.Source, "01 First Song.flac")
	e.strictFingerprint = func(path string) (catalog.Evidence, error) {
		if path == target {
			return catalog.Evidence{}, fmt.Errorf("simulated interruption after publishing track one")
		}
		return catalog.Fingerprint(path)
	}
	jobs, err := e.Apply(p)
	if err == nil || len(jobs) != 1 {
		t.Fatalf("interrupted rename: %v %v", jobs, err)
	}
	requireAbsent(t, first)
	requireAbsent(t, second)
	requireAbsent(t, filepath.Join(e.Source, "02 Second Song.flac"))
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	e.strictFingerprint = nil
	if got, err := e.Apply(p); err != nil || !reflect.DeepEqual(got, jobs) {
		t.Fatalf("resume: %v %v", got, err)
	}
	final, err := os.Stat(target)
	if err != nil || !os.SameFile(st, final) {
		t.Fatalf("retry duplicated or rewrote a published track: %v", err)
	}
}

func TestCaseOnlyRenameUsesExactSpelling(t *testing.T) {
	e, first := engineFixture(t, Write)
	old := filepath.Join(e.Source, "01 FIRST SONG.flac")
	if err := os.Rename(first, old); err != nil {
		t.Fatal(err)
	}
	r := fixturePlan(t, e).Recordings[0]
	r.RenameFiles = true
	if err := e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(fixturePlan(t, e)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(e.Source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "01 First Song.flac" {
			return
		}
	}
	t.Fatal("requested capitalization was not applied")
}

func TestSavedRenameIsManual(t *testing.T) {
	e, first := engineFixture(t, Copy)
	r := fixturePlan(t, e).Recordings[0]
	r.RenameFiles = true
	if err := e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- e.Watch(ctx, true, time.Second, nil) }()
	defer func() {
		cancel()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	// Wait past the first watcher tick after validation completed.
	deadline := time.After(5 * time.Second)
	for {
		status, _ := e.librarySnapshot()
		if status.Validated {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not validate the recording")
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(1100 * time.Millisecond)
	requireAbsent(t, filepath.Join(e.Destination, "01 First Song.flac"))
	if _, err := os.Stat(first); err != nil {
		t.Fatal(err)
	}
}

func TestScopedApplyLeavesUnrelatedPendingRenameAlone(t *testing.T) {
	e, first := engineFixture(t, Write)
	r := fixturePlan(t, e).Recordings[0]
	r.RenameFiles = true
	if err := e.SaveReview(r.ID, r); err != nil {
		t.Fatal(err)
	}
	paused := fixturePlan(t, e)
	_, err := e.store.db.Exec("CREATE TRIGGER stop_rename BEFORE UPDATE OF plan ON rename_batches BEGIN SELECT RAISE(FAIL, 'interrupted'); END")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(paused); err == nil {
		t.Fatal("expected rename to pause")
	}
	requireAbsent(t, first)

	secondDir := filepath.Join(e.Source, "another-show")
	if err = os.Mkdir(secondDir, 0755); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(secondDir, "t01.flac")
	renameFixtureAudio(t, second, 2)
	notes, err := os.ReadFile(filepath.Join(e.Source, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(secondDir, "notes.txt"), notes, 0644); err != nil {
		t.Fatal(err)
	}
	e.strictFingerprint = func(path string) (catalog.Evidence, error) {
		if filepath.Dir(path) != secondDir {
			t.Errorf("scoped apply read an unrelated payload: %s", path)
		}
		return catalog.Fingerprint(path)
	}
	if jobs, err := e.Apply(fixturePlan(t, e)); err != nil || len(jobs) != 1 {
		t.Fatalf("unrelated interrupted rename blocked a scoped apply: %v, %v", jobs, err)
	}
	requireAbsent(t, filepath.Join(e.Source, "01 First Song.flac"))
}
