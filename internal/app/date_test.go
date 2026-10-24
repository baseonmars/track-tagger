package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"track-tagger/internal/media"
)

func TestAmbiguousDateSuggestionRequiresReviewBeforeWrite(t *testing.T) {
	e, audio := engineFixture(t, Write)
	if err := os.WriteFile(filepath.Join(e.Source, "notes.txt"), []byte("Artist: Band\nDate: 10/09/1969\nVenue: Hall\nCity: London\n01. First Song\n"), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Recordings) != 1 || p.Recordings[0].Date != "1969-10-09" || p.Recordings[0].Status != "needs-review" || !strings.HasPrefix(p.Recordings[0].Album, "1969-10-09") {
		t.Fatalf("unexpected suggestion: %+v", p.Recordings)
	}
	if jobs, err := e.Apply(p); err != nil || len(jobs) != 0 {
		t.Fatalf("unreviewed suggestion applied: jobs=%v, err=%v", jobs, err)
	}
	p.Recordings[0].Status = "ready"
	if jobs, err := e.Apply(p); err == nil || !strings.Contains(err.Error(), "requires review") || len(jobs) != 0 {
		t.Fatalf("unreviewed ready plan applied: jobs=%v, err=%v", jobs, err)
	}
	after, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unreviewed suggestion changed audio file")
	}
}

func TestReviewedDateWriteAndCopy(t *testing.T) {
	for _, mode := range []Mode{Write, Copy} {
		for _, date := range []string{"", "1970", "1970-02", "1972-02-29"} {
			for _, existing := range []string{"", "1969-12-31"} {
				t.Run(string(mode)+"/"+date+"/existing="+existing, func(t *testing.T) {
					e, audio := engineFixture(t, mode)
					if existing != "" {
						stage := filepath.Join(t.TempDir(), "existing.flac")
						if _, err := media.Stage(audio, stage, map[string]string{"DATE": existing}, "", true); err != nil {
							t.Fatal(err)
						}
						if err := media.Replace(stage, audio); err != nil {
							t.Fatal(err)
						}
					}
					original, err := media.Inspect(audio)
					if err != nil {
						t.Fatal(err)
					}
					p, err := e.Scan()
					if err != nil {
						t.Fatal(err)
					}
					item := p.Recordings[0]
					item.Date = date
					item.DateUnknown = date == ""
					if date != "" {
						item.Date = " " + date + " "
					}
					if err = e.SaveReview(item.ID, item); err != nil {
						t.Fatal(err)
					}
					saved, err := e.Scan()
					if err != nil {
						t.Fatal(err)
					}
					got := saved.Recordings[0]
					if !got.Reviewed || got.Date != date || got.DateUnknown != (date == "") {
						t.Fatalf("saved review: %+v", got)
					}
					b, err := json.Marshal(saved)
					if err != nil {
						t.Fatal(err)
					}
					var restored Plan
					if err = json.Unmarshal(b, &restored); err != nil {
						t.Fatal(err)
					}
					if restored.Recordings[0].DateUnknown != got.DateUnknown {
						t.Fatal("plan lost unknown acknowledgment")
					}
					jobs, err := e.Apply(restored)
					if err != nil {
						t.Fatal(err)
					}
					if len(jobs) != 1 {
						t.Fatalf("jobs: %v", jobs)
					}
					target := audio
					if mode == Copy {
						target = filepath.Join(e.Destination, "t01.flac")
					}
					output, err := media.Inspect(target)
					if err != nil {
						t.Fatal(err)
					}
					want := date
					if date == "" {
						want = existing
					}
					if output.Tags["DATE"] != want {
						t.Fatalf("DATE=%q want %q", output.Tags["DATE"], want)
					}
					if date == "" && existing == "" {
						if _, ok := output.Tags["DATE"]; ok {
							t.Fatal("unknown added empty DATE")
						}
					}
					if output.AudioHash != original.AudioHash {
						t.Fatal("audio changed")
					}
					if mode == Copy && checkedHash(t, audio) != original.FileHash {
						t.Fatal("copy modified source")
					}
					if _, err = e.Undo(jobs[0], true); err != nil {
						t.Fatal(err)
					}
					undone, err := media.Inspect(target)
					if err != nil {
						t.Fatal(err)
					}
					if undone.Tags["DATE"] != existing || undone.AudioHash != original.AudioHash {
						t.Fatal("undo did not restore original date/audio")
					}
				})
			}
		}
	}
}

func TestDateReviewRequiresExplicitUnknown(t *testing.T) {
	for _, tt := range []struct {
		date    string
		unknown bool
	}{{"", false}, {"1970", true}, {"1970-13", false}} {
		e, _ := engineFixture(t, Write)
		p := fixturePlan(t, e)
		item := p.Recordings[0]
		item.Date = tt.date
		item.DateUnknown = tt.unknown
		if err := e.SaveReview(item.ID, item); err == nil {
			t.Fatalf("accepted date=%q unknown=%v", tt.date, tt.unknown)
		}
	}
	e, _ := engineFixture(t, Write)
	p := fixturePlan(t, e)
	p.Recordings[0].Date = ""
	p.Recordings[0].DateUnknown = true
	if _, err := e.Apply(p); err == nil || !strings.Contains(err.Error(), "Reviewed") {
		t.Fatalf("edited unreviewed plan: %v", err)
	}
}

func TestUnknownReviewPersistsAndBecomesStale(t *testing.T) {
	e, _ := engineFixture(t, Write)
	p := fixturePlan(t, e)
	item := p.Recordings[0]
	item.Date = ""
	item.DateUnknown = true
	if err := e.SaveReview(item.ID, item); err != nil {
		t.Fatal(err)
	}
	opts := e.Options
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	fresh, err := reopened.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.Recordings[0].DateUnknown || !fresh.Recordings[0].Reviewed {
		t.Fatal("reopened store lost unknown acknowledgment")
	}
	if err = os.WriteFile(filepath.Join(e.Source, "new.txt"), []byte("new evidence"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = reopened.SaveReview(item.ID, item); err == nil {
		t.Fatal("stale unknown review accepted")
	}
	fresh, err = reopened.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Recordings[0].Reviewed || fresh.Recordings[0].DateUnknown {
		t.Fatal("stale unknown acknowledgment trusted")
	}
}

func TestMissingDateAlbumFallback(t *testing.T) {
	e, _ := engineFixture(t, DryRun)
	if err := os.WriteFile(filepath.Join(e.Source, "notes.txt"), []byte("Artist: Band\nVenue: Hall\nCity: London\n01. First Song\n"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	item := p.Recordings[0]
	if item.Album != "Hall, London" || item.Status != "needs-review" {
		t.Fatalf("missing-date preview: %+v", item)
	}
}
