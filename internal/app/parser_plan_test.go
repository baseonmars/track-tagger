package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"track-tagger/internal/media"
)

// Saved plans contain the old parser's proposal, but their evidence hashes can
// remain current after a parser upgrade. Proposal differences still need review.
func TestObsoleteParserProposalRequiresReview(t *testing.T) {
	for _, field := range []string{"artist", "album", "date", "title", "number", "disc"} {
		t.Run(field, func(t *testing.T) {
			e, audio := engineFixture(t, Write)
			p := fixturePlan(t, e)
			before := checkedHash(t, audio)
			old := &p.Recordings[0]
			switch field {
			case "artist":
				old.Artist = "Old inferred performer"
			case "album":
				old.Album = "Old inferred album"
			case "date":
				old.Date = "1970-02-20"
			case "title":
				old.Tracks[0].Title = "Old inferred title"
			case "number":
				old.Tracks[0].Number = 2
			case "disc":
				old.Tracks[0].Disc = 2
			}
			if jobs, err := e.Apply(p); err == nil || !strings.Contains(err.Error(), "Reviewed") || len(jobs) != 0 {
				t.Fatalf("obsolete unreviewed proposal accepted: jobs=%v err=%v", jobs, err)
			}
			if checkedHash(t, audio) != before {
				t.Fatal("rejected proposal changed media")
			}
		})
	}
}

func TestReviewedParserCorrectionRetainsIntegrityChecks(t *testing.T) {
	for _, scenario := range []string{"valid", "unknown-file", "missing-file", "invalid-number", "changed-notes", "changed-audio"} {
		t.Run(scenario, func(t *testing.T) {
			e, audio := engineFixture(t, Write)
			p := fixturePlan(t, e)
			item := &p.Recordings[0]
			item.Tracks[0].Title = "Reviewed correction"
			item.Reviewed = true
			original, err := media.Inspect(audio)
			if err != nil {
				t.Fatal(err)
			}
			wantError := ""
			switch scenario {
			case "unknown-file":
				item.Tracks[0].Path = filepath.Join(e.Source, "t99.flac")
				wantError = "unknown file"
			case "missing-file":
				item.Tracks = nil
				wantError = "every discovered audio file"
			case "invalid-number":
				item.Tracks[0].Number = 0
				wantError = "positive disc/track"
			case "changed-notes", "changed-audio":
				path := audio
				if scenario == "changed-notes" {
					path = filepath.Join(e.Source, "notes.txt")
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data[len(data)-1] ^= 1
				if err = os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
				if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
					t.Fatal(err)
				}
				wantError = "stale"
			}
			before := checkedHash(t, audio)
			jobs, err := e.Apply(p)
			if wantError != "" {
				if err == nil || !strings.Contains(err.Error(), wantError) || len(jobs) != 0 {
					t.Fatalf("unsafe reviewed plan accepted: jobs=%v err=%v, want %q", jobs, err, wantError)
				}
				if checkedHash(t, audio) != before {
					t.Fatal("rejected reviewed plan changed media")
				}
				return
			}
			if err != nil || len(jobs) != 1 {
				t.Fatalf("valid reviewed correction rejected: jobs=%v err=%v", jobs, err)
			}
			after, err := media.Inspect(audio)
			if err != nil {
				t.Fatal(err)
			}
			if after.Tags["TITLE"] != "Reviewed correction" || after.AudioHash != original.AudioHash {
				t.Fatal("reviewed correction did not preserve audio and apply the approved title")
			}
		})
	}
}
