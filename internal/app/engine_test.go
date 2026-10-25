package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	flac "github.com/go-flac/go-flac/v2"
	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

func engineFixture(t *testing.T, mode Mode) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	audio := filepath.Join(source, "t01.flac")
	f, err := os.Create(audio)
	if err != nil {
		t.Fatal(err)
	}
	ff := flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}, {Type: flac.Application, Data: []byte("TESTpreserve extension")}}, Frames: bytes.NewReader([]byte{0xff, 0xf8, 0, 1, 2, 3, 4, 5})}
	if _, err = ff.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "notes.txt"), []byte("Artist: Band\nDate: 1970-01-20\nVenue: Hall\nCity: London\n01. First Song\n"), 0644); err != nil {
		t.Fatal(err)
	}
	e, err := New(Options{Source: source, State: filepath.Join(root, "state"), Destination: filepath.Join(root, "output"), Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e, filepath.Join(e.Source, filepath.Base(audio))
}
func fixturePlan(t *testing.T, e *Engine) Plan {
	t.Helper()
	p, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Recordings) != 1 || p.Recordings[0].Status != "ready" {
		t.Fatalf("unexpected plan: %+v", p)
	}
	return p
}
func checkedHash(t *testing.T, path string) string {
	t.Helper()
	h, err := media.Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func alreadyTagged(t *testing.T, path string, tags map[string]string, artwork string) {
	t.Helper()
	stage := filepath.Join(filepath.Dir(path), ".track-tagger-test-stage")
	if _, err := media.Stage(path, stage, tags, artwork, false); err != nil {
		t.Fatal(err)
	}
	if err := media.Replace(stage, path); err != nil {
		t.Fatal(err)
	}
}

func TestAlreadyTaggedWriteCompletesWithoutReplacement(t *testing.T) {
	e, audio := engineFixture(t, Write)
	proposal := fixturePlan(t, e).Recordings[0]
	alreadyTagged(t, audio, proposal.Tracks[0].Tags, proposal.Artwork)
	stamp := time.Unix(1_500_000_000, 0)
	if err := os.Chtimes(audio, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	p := fixturePlan(t, e)
	jobs, err := e.Apply(p)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v %v", jobs, err)
	}
	files, err := e.store.Files(jobs[0])
	if err != nil || len(files) != 0 {
		t.Fatalf("unchanged file journaled: %v %v", files, err)
	}
	after, err := os.ReadFile(audio)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unchanged file bytes changed", err)
	}
	st, err := os.Stat(audio)
	if err != nil || !st.ModTime().Equal(stamp) {
		t.Fatal("unchanged file modification time changed", err)
	}
	known, err := e.store.known(audio)
	if err != nil || known != checkedHash(t, audio) {
		t.Fatal("unchanged output hash missing", err)
	}
	fresh, err := e.Scan()
	if err != nil || len(fresh.Recordings) != 1 || fresh.Recordings[0].Status != "completed" {
		t.Fatalf("rescan: %+v %v", fresh, err)
	}
	other, err := New(e.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if err = other.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	fresh, err = other.Scan()
	if err != nil || len(fresh.Recordings) != 1 || fresh.Recordings[0].Status != "completed" {
		t.Fatalf("reopened rescan: %+v %v", fresh, err)
	}
	other.startBrowsing()
	waitLibraryCompleted := func(after uint64) uint64 {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			status, items := other.librarySnapshot()
			if status.Phase == "ready" && status.Generation > after && len(items) == 1 && items[0].Status == "completed" {
				return status.Generation
			}
			time.Sleep(10 * time.Millisecond)
		}
		status, items := other.librarySnapshot()
		t.Fatalf("library did not publish completed item: %+v %+v", status, items)
		return 0
	}
	generation := waitLibraryCompleted(0)
	other.reconcileLibrary()
	waitLibraryCompleted(generation)
	jobsAgain, err := other.Apply(fresh)
	if err != nil || len(jobsAgain) != 0 {
		t.Fatalf("completed plan created another job: %v %v", jobsAgain, err)
	}
	history, err := other.store.History()
	if err != nil || len(history) != 1 {
		t.Fatalf("reconciliation created another job: %v %v", history, err)
	}
}

func TestMixedWriteJournalsOnlyChangedTrack(t *testing.T) {
	e, first := engineFixture(t, Write)
	second := filepath.Join(e.Source, "t02.flac")
	b, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(second, b, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(e.Source, "notes.txt"), []byte("Artist: Band\nDate: 1970-01-20\nVenue: Hall\nCity: London\n01. First Song\n02. Second Song\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p := fixturePlan(t, e)
	if len(p.Recordings[0].Tracks) != 2 {
		t.Fatal("fixture did not find both tracks")
	}
	for _, track := range p.Recordings[0].Tracks {
		if track.Path == first {
			alreadyTagged(t, first, track.Tags, p.Recordings[0].Artwork)
		}
	}
	p = fixturePlan(t, e)
	firstHash := checkedHash(t, first)
	jobs, err := e.Apply(p)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("apply: %v %v", jobs, err)
	}
	files, err := e.store.Files(jobs[0])
	if err != nil || len(files) != 1 || files[0].Target != second {
		t.Fatalf("wrong journal rows: %v %v", files, err)
	}
	if checkedHash(t, first) != firstHash {
		t.Fatal("already tagged track changed")
	}
	for _, path := range []string{first, second} {
		known, err := e.store.known(path)
		if err != nil || known != checkedHash(t, path) {
			t.Fatalf("missing output hash for %s: %q %v", path, known, err)
		}
	}
	fresh, err := e.Scan()
	if err != nil || fresh.Recordings[0].Status != "completed" {
		t.Fatalf("mixed recording not complete: %+v %v", fresh, err)
	}
}

func TestUnchangedOutputEditBeforeCompletionRejected(t *testing.T) {
	e, audio := engineFixture(t, Write)
	p := fixturePlan(t, e)
	alreadyTagged(t, audio, p.Recordings[0].Tracks[0].Tags, p.Recordings[0].Artwork)
	p = fixturePlan(t, e)
	reads := 0
	e.strictFingerprint = func(path string) (catalog.Evidence, error) {
		ev, err := catalog.Fingerprint(path)
		if path == audio {
			reads++
			if reads == 2 && err == nil {
				f, x := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if x != nil {
					return ev, x
				}
				_, x = f.Write([]byte{7})
				if closeErr := f.Close(); x == nil {
					x = closeErr
				}
				if x != nil {
					return ev, x
				}
			}
		}
		return ev, err
	}
	jobs, err := e.Apply(p)
	if err == nil || len(jobs) != 1 || reads < 2 {
		t.Fatalf("intervening edit accepted: jobs=%v reads=%d err=%v", jobs, reads, err)
	}
	known, err := e.store.known(audio)
	if err != nil || known != "" {
		t.Fatal("failed completion recorded unchanged output", err)
	}
	var completed string
	if err := e.store.db.QueryRow("SELECT evidence FROM completions WHERE id=?", e.completionKey(p.Recordings[0].ID)).Scan(&completed); err == nil {
		t.Fatal("failed completion recorded evidence")
	}
}

func TestOwnedNoopCopyRetainsCollisionChecks(t *testing.T) {
	e, source := engineFixture(t, Copy)
	p := fixturePlan(t, e)
	first, err := e.Apply(p)
	if err != nil || len(first) != 1 {
		t.Fatalf("initial copy: %v %v", first, err)
	}
	if _, err = e.store.db.Exec("DELETE FROM completions WHERE id=?", e.completionKey(p.Recordings[0].ID)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.Destination, filepath.Base(source))
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	p = fixturePlan(t, e)
	second, err := e.Apply(p)
	if err != nil || len(second) != 1 {
		t.Fatalf("owned no-op copy: %v %v", second, err)
	}
	files, err := e.store.Files(second[0])
	if err != nil || len(files) != 0 {
		t.Fatalf("owned no-op copy journaled: %v %v", files, err)
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("owned no-op copy changed bytes", err)
	}
	stAfter, err := os.Stat(target)
	if err != nil || !stAfter.ModTime().Equal(st.ModTime()) {
		t.Fatal("owned no-op copy changed modification time", err)
	}
	fresh, err := e.Scan()
	if err != nil || fresh.Recordings[0].Status != "completed" {
		t.Fatalf("owned no-op copy not completed: %+v %v", fresh, err)
	}
	if _, err = e.store.db.Exec("DELETE FROM completions WHERE id=?", e.completionKey(p.Recordings[0].ID)); err != nil {
		t.Fatal(err)
	}
	p = fixturePlan(t, e)
	if err = os.WriteFile(target, []byte("unrelated output"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(p); err == nil {
		t.Fatal("copy accepted externally edited destination")
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "unrelated output" {
		t.Fatal("copy changed externally edited destination", err)
	}
}

func TestCompletionAndUnchangedHashesCommitTogether(t *testing.T) {
	e, audio := engineFixture(t, Write)
	if err := e.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.db.Exec("CREATE TRIGGER reject_completion BEFORE INSERT ON completions BEGIN SELECT RAISE(ABORT, 'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := e.store.finishCompletion("test", "evidence", map[string]string{audio: "hash"}); err == nil {
		t.Fatal("completion insert unexpectedly succeeded")
	}
	known, err := e.store.known(audio)
	if err != nil || known != "" {
		t.Fatalf("output hash escaped rolled back completion: %q %v", known, err)
	}
}

func TestInterruptedApplyRecovery(t *testing.T) {
	for _, point := range []string{"before-replacement", "after-replacement", "external-edit"} {
		t.Run(point, func(t *testing.T) {
			e, audio := engineFixture(t, Write)
			if err := e.OpenStorage(); err != nil {
				t.Fatal(err)
			}
			original, err := media.Inspect(audio)
			if err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(filepath.Dir(audio), "stage")
			after, err := media.Stage(audio, stage, map[string]string{"TITLE": "New title"}, "", false)
			if err != nil {
				t.Fatal(err)
			}
			job, err := e.store.newJob("recording", "interrupted fixture")
			if err != nil {
				t.Fatal(err)
			}
			fid, err := e.store.journal(FileJournal{JobID: job, Source: audio, Target: audio, BeforeHash: original.FileHash, AfterHash: after.FileHash, Metadata: original.Metadata})
			if err != nil {
				t.Fatal(err)
			}
			if point == "after-replacement" {
				if err = media.Replace(stage, audio); err != nil {
					t.Fatal(err)
				}
			}
			if point == "external-edit" {
				if err = os.WriteFile(audio, []byte("external content"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			err = e.recover()
			if point == "external-edit" {
				if err == nil {
					t.Fatal("accepted external edit")
				}
				if checkedHash(t, audio) == original.FileHash {
					t.Fatal("overwrote external edit")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rows, err := e.store.Files(job)
			if err != nil || len(rows) != 1 || rows[0].ID != fid {
				t.Fatalf("journal: %v %v", rows, err)
			}
			want := "aborted"
			if point == "after-replacement" {
				want = "committed"
				known, err := e.store.known(audio)
				if err != nil || known != after.FileHash {
					t.Fatal("recovery did not register output")
				}
			}
			if rows[0].Status != want {
				t.Fatalf("status %q want %q", rows[0].Status, want)
			}
			if err = e.recover(); err != nil {
				t.Fatal("recovery not idempotent", err)
			}
		})
	}
}

func TestInterruptedUndoRecovery(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-replacement", true: "after-replacement"}[replaced], func(t *testing.T) {
			e, audio := engineFixture(t, Write)
			original := checkedHash(t, audio)
			jobs, err := e.Apply(fixturePlan(t, e))
			if err != nil || len(jobs) != 1 {
				t.Fatalf("apply %v %v", jobs, err)
			}
			files, err := e.store.Files(jobs[0])
			if err != nil || len(files) != 1 {
				t.Fatalf("journal %v %v", files, err)
			}
			j := files[0]
			current, err := media.Inspect(audio)
			if err != nil {
				t.Fatal(err)
			}
			stage := filepath.Join(filepath.Dir(audio), "undo-stage")
			restored, err := media.StageRestore(audio, stage, j.Metadata)
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.store.journal(FileJournal{JobID: j.JobID, Source: audio, Target: audio, BeforeHash: current.FileHash, AfterHash: restored.FileHash, Metadata: current.Metadata, UndoOf: j.ID})
			if err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err = media.Replace(stage, audio); err != nil {
					t.Fatal(err)
				}
			}
			if err = e.recover(); err != nil {
				t.Fatal(err)
			}
			files, err = e.store.Files(j.JobID)
			if err != nil {
				t.Fatal(err)
			}
			wantOriginal, wantUndo := "committed", "aborted"
			if replaced {
				wantOriginal, wantUndo = "undone", "undo"
				if checkedHash(t, audio) != original {
					t.Fatal("original bytes not restored")
				}
			}
			for _, row := range files {
				want := wantUndo
				if row.ID == j.ID {
					want = wantOriginal
				}
				if row.Status != want {
					t.Errorf("row %d status=%q want=%q", row.ID, row.Status, want)
				}
			}
			if err = e.recover(); err != nil {
				t.Fatal("second recovery", err)
			}
		})
	}
}

func TestLateInputRejectsSavedPlan(t *testing.T) {
	e, audio := engineFixture(t, Write)
	before := checkedHash(t, audio)
	plan := fixturePlan(t, e)
	if err := os.WriteFile(filepath.Join(e.Source, "late-notes.txt"), []byte("Additional concert evidence\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(plan); err == nil {
		t.Fatal("accepted stale plan after new input")
	}
	if checkedHash(t, audio) != before {
		t.Fatal("stale plan changed audio")
	}
}
func TestReviewedCorrectionsStaleAfterExternalNotesEdit(t *testing.T) {
	e, _ := engineFixture(t, Write)
	p := fixturePlan(t, e)
	item := p.Recordings[0]
	item.Tracks[0].Title = "Manually corrected title"
	if err := e.SaveReview(item.ID, item); err != nil {
		t.Fatal(err)
	}
	fresh, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Recordings[0].Reviewed || fresh.Recordings[0].Tracks[0].Title != item.Tracks[0].Title {
		t.Fatal("manual correction missing")
	}
	f, err := os.OpenFile(filepath.Join(e.Source, "notes.txt"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("\nNew external evidence\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	fresh, err = e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	got := fresh.Recordings[0]
	if got.Reviewed || got.Status != "needs-review" || !strings.Contains(strings.Join(got.ReviewReasons, " "), "stale") {
		t.Fatalf("stale correction trusted: %+v", got)
	}
}
func TestDestinationSymlinkRejected(t *testing.T) {
	e, audio := engineFixture(t, Copy)
	plan := fixturePlan(t, e)
	outside := filepath.Join(t.TempDir(), "external.flac")
	if err := os.WriteFile(outside, []byte("external bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.Destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.Destination, filepath.Base(audio))); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := e.Apply(plan); err == nil {
		t.Fatal("accepted destination symlink")
	}
	b, err := os.ReadFile(outside)
	if err != nil || string(b) != "external bytes" {
		t.Fatal("external target modified")
	}
}
func TestConcurrentWriterLock(t *testing.T) {
	e, _ := engineFixture(t, Write)
	if err := e.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	other, err := OpenStore(e.State)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	unlock, err := e.store.acquire()
	if err != nil {
		t.Fatal(err)
	}
	if release, err := other.acquire(); err == nil {
		release()
		unlock()
		t.Fatal("concurrent writer acquired lock")
	}
	unlock()
	release, err := other.acquire()
	if err != nil {
		t.Fatal("released lock remained held", err)
	}
	release()
}

func TestCopyUpdateRefusesChangedSourceAudio(t *testing.T) {
	e, source := engineFixture(t, Copy)
	plan := fixturePlan(t, e)
	jobs, err := e.Apply(plan)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("initial copy: jobs=%v err=%v", jobs, err)
	}
	target := filepath.Join(e.Destination, filepath.Base(source))
	outputBefore := checkedHash(t, target)
	f, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte{0xff, 0xf8, 9, 8, 7, 6}); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	sourceChanged := checkedHash(t, source)
	sourceInfo, err := media.Inspect(source)
	if err != nil {
		t.Fatal(err)
	}
	targetInfo, err := media.Inspect(target)
	if err != nil {
		t.Fatal(err)
	}
	if sourceInfo.AudioHash == targetInfo.AudioHash {
		t.Fatal("fixture failed to change source audio payload")
	}
	fresh := fixturePlan(t, e)
	if _, err = e.Apply(fresh); err == nil {
		t.Fatal("copy update accepted changed source audio")
	}
	if checkedHash(t, target) != outputBefore {
		t.Fatal("refused copy update changed existing output")
	}
	if checkedHash(t, source) != sourceChanged {
		t.Fatal("refused copy update changed source audio")
	}
}
