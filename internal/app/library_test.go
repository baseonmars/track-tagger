package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"track-tagger/internal/catalog"
	"track-tagger/internal/webui"
)

func libraryFixture(t *testing.T, count int) (*Engine, []catalog.Discovery) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		dir := filepath.Join(source, fmt.Sprintf("show-%02d", i))
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "t01.mp3"), []byte("synthetic audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	e, err := New(Options{Source: source, State: filepath.Join(root, "state"), Mode: DryRun})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	ds, err := catalog.Discover(e.Source)
	if err != nil || len(ds) != count {
		t.Fatalf("discovery = %+v, %v", ds, err)
	}
	return e, ds
}

func libraryRecording(d catalog.Discovery, fingerprint string) catalog.Recording {
	return catalog.Recording{ID: d.ID, Path: d.Path, RelativePath: d.RelativePath, Status: "ready", Fingerprint: fingerprint}
}

func awaitLibraryStatus(t *testing.T, w WebBackend, accept func(webui.Status) bool) webui.Status {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		s, err := w.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if accept(s) {
			return s
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("library status did not reach expected state: %+v", s)
		}
	}
}

func receiveLibraryID(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(3 * time.Second):
		t.Fatal("validation hook was not called")
		return ""
	}
}

func TestLibraryProgressiveSnapshotsResponsiveToWriterAndPriority(t *testing.T) {
	e, ds := libraryFixture(t, 3)
	called := make(chan string, 3)
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		called <- d.ID
		if d.ID == ds[0].ID {
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return catalog.Recording{}, ctx.Err()
			}
		}
		if d.ID == ds[2].ID {
			select {
			case <-releaseSecond:
			case <-ctx.Done():
				return catalog.Recording{}, ctx.Err()
			}
		}
		return libraryRecording(d, d.ID), nil
	}
	w := WebBackend{E: e}
	w.Status(context.Background())
	if got := receiveLibraryID(t, called); got != ds[0].ID {
		t.Fatalf("first validation = %s, want %s", got, ds[0].ID)
	}
	e.mu.Lock()
	responsive := make(chan error, 1)
	go func() {
		s, err := w.Status(context.Background())
		if err == nil && (s.Phase != "validating" || s.Total != 3 || s.Scanned != 0) {
			err = fmt.Errorf("incomplete snapshot = %+v", s)
		}
		if err == nil {
			rows, x := w.List(context.Background())
			err = x
			if err == nil && len(rows) != 3 {
				err = fmt.Errorf("list has %d placeholders", len(rows))
			}
		}
		if err == nil {
			_, err = w.Get(context.Background(), ds[2].ID)
		}
		responsive <- err
	}()
	select {
	case err := <-responsive:
		e.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		e.mu.Unlock()
		t.Fatal("Status/List/Get blocked on the writer mutex or validation")
	}
	close(releaseFirst)
	if got := receiveLibraryID(t, called); got != ds[2].ID {
		t.Fatalf("requested recording was not next: got %s, want %s", got, ds[2].ID)
	}
	s := awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Scanned == 1 })
	if s.Phase != "validating" || s.Recordings[0].Fingerprint != ds[0].ID || s.Recordings[2].Status != "loading" {
		t.Fatalf("progressive snapshot = %+v", s)
	}
	close(releaseSecond)
	if got := receiveLibraryID(t, called); got != ds[1].ID {
		t.Fatalf("remaining validation = %s, want %s", got, ds[1].ID)
	}
	awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" && s.Scanned == 3 })
}

func TestLibraryCoalescesInvalidationsWithoutPublishingCancelledResult(t *testing.T) {
	e, ds := libraryFixture(t, 1)
	entered := make(chan struct{})
	var calls atomic.Int32
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return libraryRecording(d, "stale"), nil
		}
		return libraryRecording(d, "fresh"), nil
	}
	w := WebBackend{E: e}
	w.Status(context.Background())
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("initial validation did not start")
	}
	e.invalidateRecordings(ds[0].ID)
	e.invalidateRecordings(ds[0].ID)
	s := awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
	if len(s.Recordings) != 1 || s.Recordings[0].Fingerprint != "fresh" || calls.Load() != 2 {
		t.Fatalf("cancelled generation published or refresh repeated: %+v, calls %d", s, calls.Load())
	}
}

func TestLibraryScopedInvalidationRetainsOtherValidatedRow(t *testing.T) {
	e, ds := libraryFixture(t, 2)
	refreshEntered := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var firstCalls, secondCalls atomic.Int32
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		if d.ID == ds[0].ID {
			if firstCalls.Add(1) == 2 {
				close(refreshEntered)
				select {
				case <-releaseRefresh:
				case <-ctx.Done():
					return catalog.Recording{}, ctx.Err()
				}
			}
			return libraryRecording(d, "first"), nil
		}
		secondCalls.Add(1)
		return libraryRecording(d, "second"), nil
	}
	w := WebBackend{E: e}
	awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
	e.invalidateRecordings(ds[0].ID)
	select {
	case <-refreshEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("selected refresh did not start")
	}
	s, err := w.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != "validating" || s.Scanned != 1 || s.Recordings[0].Status != "loading" || s.Recordings[0].Fingerprint != "" || s.Recordings[1].Fingerprint != "second" {
		t.Fatalf("unaffected validated row was lost: %+v", s)
	}
	close(releaseRefresh)
	awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
	if firstCalls.Load() != 2 || secondCalls.Load() != 1 {
		t.Fatalf("scoped refresh validated other row: first %d, second %d", firstCalls.Load(), secondCalls.Load())
	}
}

func TestStopBrowsingCancelsAndWaitsForWorker(t *testing.T) {
	e, _ := libraryFixture(t, 1)
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return catalog.Recording{}, ctx.Err()
	}
	w := WebBackend{E: e}
	w.Status(context.Background())
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("validation did not start")
	}
	stopped := make(chan struct{})
	go func() { w.StopBrowsing(); close(stopped) }()
	select {
	case <-stopped:
		select {
		case <-cancelled:
		default:
			t.Fatal("StopBrowsing returned before worker observed cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StopBrowsing did not wait for worker")
	}
}

func TestLibraryValidationFailureIsolatedToOneRecording(t *testing.T) {
	e, ds := libraryFixture(t, 2)
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		if d.ID == ds[0].ID {
			return catalog.Recording{}, fmt.Errorf("synthetic validation failure")
		}
		return libraryRecording(d, "second-ready"), nil
	}
	w := WebBackend{E: e}
	s := awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
	if s.Scanned != 2 || s.Recordings[0].Status != "failed" || s.Recordings[0].Fingerprint != "" || s.Recordings[1].Status != "ready" || s.Recordings[1].Fingerprint != "second-ready" {
		t.Fatalf("validation failure affected another recording: %+v", s)
	}
	failed, err := w.Get(context.Background(), ds[0].ID)
	if err != nil || failed.Status != "failed" || len(failed.Reasons) != 1 {
		t.Fatalf("failed detail unavailable: %+v, %v", failed, err)
	}
	ready, err := w.Get(context.Background(), ds[1].ID)
	if err != nil || ready.Status != "ready" || ready.Fingerprint != "second-ready" {
		t.Fatalf("valid detail lost: %+v, %v", ready, err)
	}
}

func TestLibraryDetailReturnsIndependentSnapshot(t *testing.T) {
	e, ds := libraryFixture(t, 1)
	audio := filepath.Join(ds[0].Path, "t01.mp3")
	note := filepath.Join(ds[0].Path, "notes.txt")
	if err := os.WriteFile(note, []byte("source note"), 0644); err != nil {
		t.Fatal(err)
	}
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		r := libraryRecording(d, "original-fingerprint")
		r.Artist = "Original Artist"
		r.Tracks = []catalog.Track{{Path: audio, Title: "Original Title", Number: 1, Disc: 1}}
		r.Notes = []string{note}
		r.ArtworkCandidates = []string{filepath.Join(d.Path, "cover.jpg")}
		r.Evidence = []catalog.Evidence{{Path: audio, SHA256: "synthetic"}}
		r.ReviewReasons = []string{"original reason"}
		return r, nil
	}
	w := WebBackend{E: e}
	awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
	first, err := w.Get(context.Background(), ds[0].ID)
	if err != nil || len(first.Tracks) != 1 || len(first.Notes) != 1 {
		t.Fatalf("initial detail = %+v, %v", first, err)
	}
	first.Metadata["ARTIST"] = "Mutated"
	first.Tracks[0].Title = "Mutated"
	first.Notes[0].Text = "Mutated"
	first.Artwork[0] = "Mutated"
	first.Evidence[0] = "Mutated"
	first.Reasons[0] = "Mutated"
	second, err := w.Get(context.Background(), ds[0].ID)
	if err != nil || second.Metadata["ARTIST"] != "Original Artist" || second.Tracks[0].Title != "Original Title" || second.Notes[0].Text != "source note" || second.Artwork[0] != filepath.Join(ds[0].Path, "cover.jpg") || second.Evidence[0] == "Mutated" || second.Reasons[0] != "original reason" {
		t.Fatalf("detail mutated published snapshot: %+v, %v", second, err)
	}
}

func TestLibraryDetailAvailableBeforeInitialValidation(t *testing.T) {
	e, ds := libraryFixture(t, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return catalog.Recording{}, ctx.Err()
		}
		return libraryRecording(d, "ready"), nil
	}
	w := WebBackend{E: e}
	r, err := w.Get(context.Background(), ds[0].ID)
	if err != nil || r.ID != ds[0].ID || r.Status != "loading" {
		t.Fatalf("early detail request = %+v, %v", r, err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("early detail did not start validation")
	}
	close(release)
	awaitLibraryStatus(t, w, func(s webui.Status) bool { return s.Phase == "ready" })
}

func TestLibraryPeriodicReconcileCoalescesBehindCurrentValidation(t *testing.T) {
	e, _ := libraryFixture(t, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var cancelled atomic.Bool
	e.library.validate = func(ctx context.Context, d catalog.Discovery) (catalog.Recording, error) {
		count := calls.Add(1)
		if count == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				cancelled.Store(true)
				return catalog.Recording{}, ctx.Err()
			}
		}
		return libraryRecording(d, fmt.Sprintf("pass-%d", count)), nil
	}
	w := WebBackend{E: e}
	w.Status(context.Background())
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("initial validation did not start")
	}
	for i := 0; i < 20; i++ {
		e.reconcileLibrary()
	}
	if cancelled.Load() || calls.Load() != 1 {
		t.Fatalf("periodic reconciliation interrupted validation: cancelled %v, calls %d", cancelled.Load(), calls.Load())
	}
	close(release)
	s := awaitLibraryStatus(t, w, func(s webui.Status) bool {
		return s.Phase == "ready" && len(s.Recordings) == 1 && s.Recordings[0].Fingerprint == "pass-2"
	})
	if cancelled.Load() || calls.Load() != 2 {
		t.Fatalf("periodic refresh did not coalesce: %+v, cancelled %v, calls %d", s, cancelled.Load(), calls.Load())
	}
}
