package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"track-tagger/internal/catalog"
)

// A malicious or unusual external edit can preserve stat values. Browsing is
// allowed to cache old evidence, but approving or applying it must still fail.
func TestBrowseCacheNeverAuthorizesStaleWrites(t *testing.T) {
	for _, action := range []string{"review", "apply"} {
		t.Run(action, func(t *testing.T) {
			e, path := engineFixture(t, Write)
			plan, err := e.browse()
			if err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.WriteAt([]byte{99}, st.Size()-1); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
				t.Fatal(err)
			}
			cached, err := e.browse()
			if err != nil {
				t.Fatal(err)
			}
			if cached.Recordings[0].Fingerprint != plan.Recordings[0].Fingerprint {
				t.Fatal("expected unchanged-stat evidence cache hit")
			}
			if action == "review" {
				err = e.SaveReview(plan.Recordings[0].ID, plan.Recordings[0])
			} else {
				_, err = e.Apply(plan)
			}
			if err == nil || !(strings.Contains(err.Error(), "changed") || strings.Contains(err.Error(), "stale")) {
				t.Fatalf("stale %s accepted: %v", action, err)
			}
		})
	}
}

func TestBrowseRefreshesChangedAndAddedInputs(t *testing.T) {
	e, _ := engineFixture(t, DryRun)
	first, err := e.browse()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(e.Source+"/notes.txt", []byte("Artist: Other Band\nDate: 1970-01-20\nVenue: Hall\n01. Different Song\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err := e.browse()
	if err != nil {
		t.Fatal(err)
	}
	if next.Recordings[0].Artist != "Other Band" || next.Recordings[0].Fingerprint == first.Recordings[0].Fingerprint {
		t.Fatal("changed notes not reflected")
	}
	if err = os.WriteFile(e.Source+"/t02.mp3", []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	next, err = e.browse()
	if err != nil {
		t.Fatal(err)
	}
	if !next.Recordings[0].PlaybackOnly || len(next.Recordings[0].Tracks) != 2 {
		t.Fatal("new audio not discovered")
	}
}

func TestPlaybackDoesNotWaitForScanLock(t *testing.T) {
	e, _ := engineFixture(t, DryRun)
	w := WebBackend{e}
	awaitWebLibrary(t, e)
	rs, err := w.List(context.Background())
	if err != nil || len(rs) != 1 {
		t.Fatalf("library summaries: %+v, %v", rs, err)
	}
	r, err := w.Get(context.Background(), rs[0].ID)
	if err != nil || len(r.Tracks) != 1 || r.Tracks[0].PlaybackID == "" {
		t.Fatalf("playback registration: %+v, %v", r, err)
	}
	e.mu.Lock()
	done := make(chan error, 1)
	go func() {
		a, err := w.OpenAudio(context.Background(), r.ID, r.Tracks[0].PlaybackID)
		if err == nil {
			err = a.Content.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		e.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		e.mu.Unlock()
		<-done
		t.Fatal("playback blocked on scan/write mutex")
	}
}

// Run explicitly with -run '^$' -bench BenchmarkBrowseLargePayload.
func BenchmarkBrowseLargePayload(b *testing.B) {
	root := b.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		b.Fatal(err)
	}
	writeBenchmarkFLAC(b, filepath.Join(source, "t01.flac"), 128<<20)
	for _, mode := range []string{"strict", "cold", "warm"} {
		b.Run(mode, func(b *testing.B) {
			e, err := New(Options{Source: source, State: filepath.Join(root, "state"), Mode: DryRun})
			if err != nil {
				b.Fatal(err)
			}
			defer e.Close()
			if _, err = e.browse(); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "cold" {
					e.browseCache = catalog.BrowseCache{}
				}
				if mode == "strict" {
					_, err = e.Scan()
				} else {
					_, err = e.browse()
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
