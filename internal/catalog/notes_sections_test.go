package catalog

import (
	"reflect"
	"strings"
	"testing"
)

func TestCP1252NotesLimitCountsRawBytes(t *testing.T) {
	// Each Windows-1252 byte expands to two UTF-8 bytes. The input remains
	// below the raw-file limit while its decoded representation exceeds it.
	s := "Artist: Band\nDate: 1989-09-05\nVenue: Hall\nSource: " + strings.Repeat("\xe9", (4<<20)-128) + "\n01. Song\n"
	r := only(t, fixture(t, "show", 1, s))
	if r.Status != "ready" || r.Tracks[0].Title != "Song" {
		t.Fatalf("raw-under-limit Windows-1252 notes rejected: %v, %v", r.Status, r.ReviewReasons)
	}
}

func TestOversizedRawNotesRequireReview(t *testing.T) {
	r := only(t, fixture(t, "show", 1, "Artist: Band\nDate: 1989-09-05\nVenue: Hall\n01. Song\n"+strings.Repeat("x", 4<<20)))
	if !hasReason(r, "notes exceed 4 MiB") || r.Tracks[0].Title != "" {
		t.Fatalf("oversized raw notes were parsed: %v", r.ReviewReasons)
	}
}

// These cases are synthetic and independent of the research evaluation corpus.
func TestNotesSectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		notes  string
		want   map[string]string
		review bool
	}{
		{"written sets", "Set One:\n01. First\nSet Two\n01. Second\nDisc Ten\n01. Last\n", map[string]string{"1:1": "First", "2:1": "Second", "10:1": "Last"}, false},
		{"technical numbered rows", "01. Song\nTransfer notes:\n01. Deck calibration\n02. Cable change\n", map[string]string{"1:1": "Song"}, false},
		{"checksum numbered rows", "Checksums:\n01. abcdef0123456789\n02. abcdef0123456789\nTracklist:\n01. Song\n", map[string]string{"1:1": "Song"}, false},
		{"technical explicit resume", "Source:\n01. Recorder setting\nSet Two\n01. Song\n", map[string]string{"2:1": "Song"}, false},
		{"technical identifier resume", "Transfer notes:\n01. Calibration\nd2t01 Song\n02. Next\n", map[string]string{"2:1": "Song", "2:2": "Next"}, false},
		{"medley punctuation", "01. First >\n    Second ->\n\tThird [reprise]\n02. Last\n", map[string]string{"1:1": "First > Second -> Third [reprise]", "1:2": "Last"}, false},
		{"medley duration", "01. First >\t05:22\n    Second\n", map[string]string{"1:1": "First >\t05:22 Second"}, false},
		{"decorated headings", "-Set 2-\n01. First\nDisc 3 (46:53)\n01. Second\nDisc 4 - 42:07\n01. Last\n", map[string]string{"2:1": "First", "3:1": "Second", "4:1": "Last"}, false},
		{"metadata stops medley", "01. First >\n    Venue: Hall\n02. Last\n", map[string]string{"1:1": "First >", "1:2": "Last"}, false},
		{"heading stops medley", "01. First >\n    Notes:\n    Second\n02. Last\n", map[string]string{"1:1": "First >", "1:2": "Last"}, false},
		{"numbered title colon", "01 Song:\nd2t01 Other Song:\n", map[string]string{"1:1": "Song:", "2:1": "Other Song:"}, false},
		{"same indent is ambiguous", "    01. First >\n    Possible prose\n    02. Last\n", map[string]string{"1:1": "First >", "1:2": "Last"}, true},
		{"deeper indent joins", "    01. First >\n        Second\n    02. Last\n", map[string]string{"1:1": "First > Second", "1:2": "Last"}, false},
		{"fingerprint stops medley", "01. First >\n    file.flac:0123456789abcdef0123456789abcdef\n", map[string]string{"1:1": "First >"}, false},
		{"md5 stops medley", "01. First >\n    0123456789abcdef0123456789abcdef *file.flac\n", map[string]string{"1:1": "First >"}, false},
		{"sfv stops medley", "01. First >\n    file.flac ABCD1234\n", map[string]string{"1:1": "First >"}, false},
		{"sha section", "SHA-256:\n01. Technical row\nTracklist:\n01. Song\n", map[string]string{"1:1": "Song"}, false},
		{"duration table before tracks", "length expanded size cdr WAVE problems fmt ratio filename\n01. Technical row\nSet One\n01. Song\n", map[string]string{"1:1": "Song"}, false},
		{"blank stops medley", "01. First >\n\n    Second\n02. Last\n", map[string]string{"1:1": "First >", "1:2": "Last"}, false},
		{"technical stops medley", "01. First >\n    Source:\n    01. Calibration\n", map[string]string{"1:1": "First >"}, false},
		{"inline source metadata", "Source: microphone > DAT\nTransfer: January 1st, 2019\n01. Song\n", map[string]string{"1:1": "Song"}, false},
		{"unproven continuation", "01. First\n    Possible second title\n02. Last\n", map[string]string{"1:1": "First", "1:2": "Last"}, true},
		{"encore keeps numbering", "Set Two\n01. First\nEncore:\n02. Last\n", map[string]string{"2:1": "First", "2:2": "Last"}, false},
		{"duplicate after encore", "01. First\nEncore:\n01. Last\n", map[string]string{"1:1": ""}, true},
		{"ambiguous section", "01. First\nSet Two - acoustic\n01. Second\n", map[string]string{"1:1": ""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Recording{}
			titles := map[string]string{}
			parseNotes(&r, tc.notes, titles, map[string]bool{}, map[string]bool{})
			if !reflect.DeepEqual(titles, tc.want) {
				t.Errorf("titles = %#v, want %#v", titles, tc.want)
			}
			if (len(r.ReviewReasons) > 0) != tc.review {
				t.Errorf("review reasons = %v, want review %v", r.ReviewReasons, tc.review)
			}
		})
	}
}
