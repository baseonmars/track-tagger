package catalog

import (
	"fmt"
	"strings"
	"testing"
)

func TestDashOpeningWithTechnicalProvenanceAndSeventeenTracks(t *testing.T) {
	var notes strings.Builder
	notes.WriteString("Zero - December 31, 1993\nMystic Theatre - Petaluma, CA\n\n")
	notes.WriteString("Recording Info:\nCassette master > first generation cassette\n\n")
	notes.WriteString("Transfer Info:\nMastered from cassette on\nJanuary 13, 2010\n\nSet 1\n")
	for _, entry := range []struct{ disc, start, end int }{{1, 1, 5}, {2, 1, 3}} {
		for n := entry.start; n <= entry.end; n++ {
			fmt.Fprintf(&notes, "d%dt%02d Song %d-%d\n", entry.disc, n, entry.disc, n)
		}
	}
	notes.WriteString("Set 2\n")
	for _, entry := range []struct{ disc, start, end int }{{2, 4, 7}, {3, 1, 3}} {
		for n := entry.start; n <= entry.end; n++ {
			fmt.Fprintf(&notes, "d%dt%02d Song %d-%d\n", entry.disc, n, entry.disc, n)
		}
	}
	notes.WriteString("Encore\nd3t04 Song 3-4\nd3t05 Song 3-5\n\nSHNTOOL OUTPUT:\nlength expanded size cdr WAVE problems fmt ratio filename\n  5:23.00 100 B cxx -- ---xx flac 0.5000 d1t01.flac\n")
	files := make([]string, 0, 17)
	for disc, count := range map[int]int{1: 5, 2: 7, 3: 5} {
		for n := 1; n <= count; n++ {
			files = append(files, fmt.Sprintf("d%dt%02d.flac", disc, n))
		}
	}
	r := dateFixture(t, "zero-show", map[string]string{"notes.txt": notes.String()}, files...)
	if r.Artist != "Zero" || r.Date != "1993-12-31" || r.Venue != "Mystic Theatre" || r.City != "Petaluma, CA" || r.Album != "1993-12-31 - Mystic Theatre, Petaluma, CA" || r.Status != "ready" {
		t.Fatalf("opening or technical section: %+v", r)
	}
	if len(r.Tracks) != 17 {
		t.Fatalf("tracks = %d, want 17", len(r.Tracks))
	}
	for _, track := range r.Tracks {
		want := fmt.Sprintf("Song %d-%d", track.Disc, track.Number)
		if track.Title != want {
			t.Fatalf("%s = %q, want %q", track.RelativePath, track.Title, want)
		}
	}
}

func TestDashOpeningVariantsAndArtistPrecedence(t *testing.T) {
	for _, dash := range []string{"-", "–", "—"} {
		note := "AC/DC - The Band " + dash + " December 31, 1993\nMystic-Theatre " + dash + " Petaluma, CA\n01. Song\n"
		r := dateFixture(t, "show", map[string]string{"notes.txt": note}, "t01.flac")
		if r.Artist != "AC/DC - The Band" || r.Venue != "Mystic-Theatre" || r.Date != "1993-12-31" || r.City != "Petaluma, CA" || hasReason(r, "artist inferred") {
			t.Fatalf("dash %q: %+v", dash, r)
		}
	}
	for _, first := range []string{"Zero: Live - December 31, 1993", "Zero @ Hall - December 31, 1993"} {
		r := dateFixture(t, "show", map[string]string{"notes.txt": first + "\nMystic Theatre - Petaluma, CA\n01. Song\n"}, "t01.flac")
		if r.Artist != "" || r.Venue != "" || !hasReason(r, "artist is missing") {
			t.Fatalf("metadata-like opening accepted: %+v", r)
		}
	}
	r := dateFixture(t, "show", map[string]string{"notes.txt": "Zero - December 31, 1993\nMystic - Main Theatre - Petaluma, CA\n01. Song\n"}, "t01.flac")
	if r.Artist != "Zero" || r.Venue != "" || r.City != "" || !hasReason(r, "artist inferred") {
		t.Fatalf("ambiguous location inferred: %+v", r)
	}
	r = dateFixture(t, "show", map[string]string{"a.txt": "Mystery Header\n01. Song\n", "z.txt": "Artist: Certain Band\nDate: 1993-12-31\nVenue: Hall\n"}, "t01.flac")
	if r.Artist != "Certain Band" || hasReason(r, "artist inferred") {
		t.Fatalf("explicit artist did not take precedence: %+v", r)
	}
	r = dateFixture(t, "show", map[string]string{"a.txt": "Zero - December 31, 1993\nMystic Theatre - Petaluma, CA\n01. Song\n", "z.txt": "Artist: Different Band\n"}, "t01.flac")
	if r.Artist != "" || !hasReason(r, "conflicting artist") || hasReason(r, "artist inferred") {
		t.Fatalf("structured artist conflict lost: %+v", r)
	}
	r = dateFixture(t, "show", map[string]string{"a.txt": "First Candidate\n01. Song\n", "z.txt": "Second Candidate\nDate: 1993-12-31\n"}, "t01.flac")
	if r.Artist != "" || !hasReason(r, "conflicting artist") || !hasReason(r, "artist inferred") {
		t.Fatalf("fallback conflict lost: %+v", r)
	}
}

func TestFirstLineArtistExclusions(t *testing.T) {
	for _, first := range []string{"December 31, 1993", "Recording Info:", "Transfer Info:", "Set 1", "d1t01 Song", "01 Song", "Artist:", "SHNTOOL OUTPUT:"} {
		if got := firstLineArtist("\n" + first + "\nAnother Band\n"); got != "" {
			t.Errorf("first line %q became %q", first, got)
		}
	}
	if got := firstLineArtist("Zero - December 31, 1993\n"); got != "Zero" {
		t.Fatalf("date suffix retained: %q", got)
	}
	if got := firstLineArtist("Zero - December 31, 1993 - January 1, 1994\n"); got != "" {
		t.Fatalf("ambiguous suffix unexpectedly accepted: %q", got)
	}
}

func TestFirstLineArtistDateSuggestions(t *testing.T) {
	for _, tc := range []struct {
		name, opening, wantDate, dateReason string
	}{
		{"BOM and blank ISO", "\ufeff\n\nZero - 1993-12-31", "1993-12-31", ""},
		{"named", "Zero - December 31, 1993", "1993-12-31", ""},
		{"numeric", "Zero - 12/31/1993", "1993-12-31", ""},
		{"ambiguous numeric", "Zero - 03/04/1993", "1993-03-04", "ambiguous numeric concert date"},
		{"invalid", "Zero - February 30, 1993", "", "invalid concert date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dateFixture(t, "show", map[string]string{"notes.txt": tc.opening + "\n01. Song\n"}, "t01.flac")
			if r.Artist != "Zero" || r.Date != tc.wantDate || r.Status != "needs-review" || !hasReason(r, "artist inferred from first line") || (tc.dateReason != "" && !hasReason(r, tc.dateReason)) {
				t.Fatalf("fallback date suggestion: %+v", r)
			}
		})
	}
}
