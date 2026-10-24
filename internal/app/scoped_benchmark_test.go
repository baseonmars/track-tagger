package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"track-tagger/internal/media"
	"track-tagger/internal/webui"
)

// BenchmarkScopedOperations times one selected recording while unrelated
// recordings contain larger payloads. Fixture construction and the reset
// needed to make every Apply perform a write are outside the timed section.
func BenchmarkScopedOperations(b *testing.B) {
	for _, unrelated := range []int{0, 16} {
		for _, operation := range []string{"SaveReview", "Apply"} {
			b.Run(fmt.Sprintf("%s/unrelated=%d", operation, unrelated), func(b *testing.B) {
				e, selected, id, review := scopedBenchmarkFixture(b, unrelated+1)
				defer e.Close()
				backend := WebBackend{e}
				ctx := context.Background()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if operation == "Apply" {
						b.StopTimer()
						writeBenchmarkFLAC(b, selected, 1<<20)
						plan, err := e.Scan()
						if err != nil {
							b.Fatal(err)
						}
						ready := false
						for _, item := range plan.Recordings {
							if item.ID == id && item.Status == "ready" {
								ready = true
							}
						}
						if !ready {
							b.Fatal("selected recording is not ready after reset")
						}
						b.StartTimer()
					}
					var err error
					if operation == "SaveReview" {
						err = backend.SaveReview(ctx, id, review)
					} else {
						err = backend.Apply(ctx, id)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				if operation == "Apply" {
					b.StopTimer()
					info, err := media.Inspect(selected)
					if err != nil || info.Tags["TITLE"] != "First Song" {
						b.Fatalf("Apply did not write the selected file: %+v, %v", info.Tags, err)
					}
				}
			})
		}
	}
}

func scopedBenchmarkFixture(b *testing.B, recordings int) (*Engine, string, string, webui.Review) {
	b.Helper()
	root := b.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		b.Fatal(err)
	}
	var selected string
	for i := 0; i < recordings; i++ {
		dir := filepath.Join(source, fmt.Sprintf("show-%02d", i))
		if err := os.Mkdir(dir, 0755); err != nil {
			b.Fatal(err)
		}
		path := filepath.Join(dir, "t01.flac")
		size := int64(8 << 20)
		if i == 0 {
			selected = path
			size = 1 << 20
		}
		writeBenchmarkFLAC(b, path, size)
		note := []byte("Artist: Band\nDate: 1970-01-20\nVenue: Hall\nCity: London\n01. First Song\n")
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), note, 0644); err != nil {
			b.Fatal(err)
		}
	}
	e, err := New(Options{Source: source, State: filepath.Join(root, "state"), Mode: Write})
	if err != nil {
		b.Fatal(err)
	}
	selected = filepath.Join(e.Source, "show-00", "t01.flac")
	if err := e.OpenStorage(); err != nil {
		b.Fatal(err)
	}
	plan, err := e.Scan()
	if err != nil {
		b.Fatal(err)
	}
	for _, item := range plan.Recordings {
		if item.Path != filepath.Dir(selected) {
			continue
		}
		if item.Status != "ready" || len(item.Tracks) != 1 {
			b.Fatalf("selected recording is not ready: %+v", item)
		}
		track := item.Tracks[0]
		review := webui.Review{
			Approve:     true,
			Fingerprint: item.Fingerprint,
			Metadata:    map[string]string{"ARTIST": item.Artist, "ALBUM": item.Album, "DATE": item.Date},
			Tracks:      []webui.Track{{File: track.Path, Title: track.Title, Artist: track.Artist, Track: track.Number, Disc: track.Disc}},
		}
		return e, selected, item.ID, review
	}
	b.Fatal("selected recording was not discovered")
	return nil, "", "", webui.Review{}
}

func writeBenchmarkFLAC(b *testing.B, path string, size int64) {
	b.Helper()
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	header := append([]byte{'f', 'L', 'a', 'C', 128, 0, 0, 34}, make([]byte, 34)...)
	if _, err = f.Write(header); err == nil {
		err = f.Truncate(size)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		b.Fatal(err)
	}
}
