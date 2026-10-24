package service

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	flac "github.com/go-flac/go-flac/v2"
	"track-tagger/internal/app"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "music")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	return Options{Options: app.Options{Source: source, State: filepath.Join(root, "state"), Mode: app.DryRun}, Addr: "127.0.0.1:0", Poll: true}
}

func TestStartStop(t *testing.T) {
	opts := testOptions(t)
	s, err := Start(context.Background(), opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.URL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, s.URL(), nil)
	req.Host = "untrusted.example"
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("untrusted host status = %d", resp.StatusCode)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); s.Stop() }()
	}
	wg.Wait()
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	// Reopening the same store and port verifies shutdown released ownership.
	opts.Addr = s.URL()[len("http://"):]
	next, err := Start(context.Background(), opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	next.Stop()
}

func TestBindConflictDoesNotStartWatcher(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	opts := testOptions(t)
	opts.Addr = l.Addr().String()
	if _, err := Start(context.Background(), opts, func(string) { t.Error("watcher started after bind failure") }); err == nil {
		t.Fatal("expected bind error")
	}
	if _, err := os.Stat(opts.State); !os.IsNotExist(err) {
		t.Fatalf("bind failure created storage: %v", err)
	}
	opts.FallbackPort = true
	s, err := Start(context.Background(), opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if s.URL() == "http://"+opts.Addr {
		t.Fatal("did not select a different port")
	}
}

func TestStopWaitsForWatcher(t *testing.T) {
	opts := testOptions(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, err := Start(context.Background(), opts, func(string) {
		once.Do(func() { close(entered); <-release })
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	defer close(release)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not start")
	}
	go s.Stop()
	select {
	case <-s.Done():
		t.Fatal("stop completed while watcher was active")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestInvalidStartup(t *testing.T) {
	for _, setup := range []func(*Options){
		func(o *Options) { o.Addr = "0.0.0.0:0" },
		func(o *Options) { o.Remote = true; o.Token = "short" },
		func(o *Options) { o.State = filepath.Join(o.Source, "state") },
		func(o *Options) { o.Mode = app.Copy; o.Destination = o.Source },
		func(o *Options) { o.Addr = "127.0.0.1:invalid"; o.FallbackPort = true },
	} {
		opts := testOptions(t)
		setup(&opts)
		if s, err := Start(context.Background(), opts, nil); err == nil {
			s.Stop()
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, testOptions(t), nil); err != context.Canceled {
		t.Fatalf("canceled start: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, testOptions(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not stop service")
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestStorageFailureReleasesListener(t *testing.T) {
	opts := testOptions(t)
	if err := os.WriteFile(opts.State, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		t.Fatal(err)
	}
	opts.Addr = l.Addr().String()
	l.Close()
	if s, err := Start(context.Background(), opts, nil); err == nil {
		s.Stop()
		t.Fatal("expected storage error")
	}
	l, err = net.Listen("tcp", opts.Addr)
	if err != nil {
		t.Fatalf("startup failure leaked listener: %v", err)
	}
	l.Close()
}

func TestShutdownKeepsStorageOpenForActiveReview(t *testing.T) {
	opts := testOptions(t)
	var audio bytes.Buffer
	fixture := flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}}, Frames: bytes.NewReader([]byte{0xff, 0xf8, 0, 1, 2, 3})}
	if _, err := fixture.WriteTo(&audio); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.Source, "t01.flac"), audio.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.Source, "notes.txt"), []byte("Artist: Band\nDate: 1970-01-20\nVenue: Hall\nCity: London\n01. First Song\n"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := app.New(opts.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	plan, err := e.Scan()
	if err != nil || len(plan.Recordings) != 1 {
		t.Fatalf("fixture scan: %v, %+v", err, plan)
	}
	item := plan.Recordings[0]
	l, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	writeResult := make(chan error, 1)
	// Pause an accepted request before its final storage write. This exercises
	// the real lifecycle and SQLite engine without enabling media mutations.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		err := e.SaveReview(item.ID, item)
		writeResult <- err
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	s := startSession(context.Background(), opts, e, l, h, nil)
	t.Cleanup(s.Stop)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	requestResult := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(s.URL())
		if err == nil {
			resp.Body.Close()
		}
		requestResult <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	go s.Stop()
	select {
	case <-s.Done():
		t.Fatal("storage owner stopped before active request finished")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	if err := <-writeResult; err != nil {
		t.Fatalf("active request lost its storage during shutdown: %v", err)
	}
	if err := <-requestResult; err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	reopened, err := app.New(opts.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.Scan()
	if err != nil || len(saved.Recordings) != 1 || !saved.Recordings[0].Reviewed {
		t.Fatalf("in-flight review was not persisted: %v, %+v", err, saved)
	}
	got, err := os.ReadFile(filepath.Join(opts.Source, "t01.flac"))
	if err != nil || !bytes.Equal(got, audio.Bytes()) {
		t.Fatal("preview service altered fixture media")
	}
}
