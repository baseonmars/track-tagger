package catalog

import (
	"testing"

	"track-tagger/internal/notesmodel"
)

func TestGuardNoteSuggestionsUsesExactScopedEvidence(t *testing.T) {
	notes := map[string][]string{
		"show.txt": {"", "Björk Theatre — Stockholm, SE", "21/03/2015", "", "Previous show", "Berlin", "01. Song"},
	}
	recording := Recording{
		Notes: []string{"show.txt"},
		Tracks: []Track{
			{Path: "2015-03-21t01.flac"},
		},
	}
	spans := []notesmodel.Span{
		{Field: "venue", Note: "show.txt", Line: 1, Start: 0, End: 13},
		{Field: "city", Note: "show.txt", Line: 1, Start: 16, End: 29},
		{Field: "date", Note: "show.txt", Line: 2, Start: 0, End: 10},
		{Field: "artist", Note: "show.txt", Line: 5, Start: 0, End: 6},
		{Field: "artist", Note: "not-a-note", Line: 1, Start: 0, End: 4},
		{Field: "artist", Note: "show.txt", Line: 1, Start: -1, End: 4},
	}
	want := []string{"Stockholm, SE", "2015-03-21", "Björk Theatre"}
	got := GuardNoteSuggestions(recording, notes, spans, ReviewAmbiguous)
	if len(got) != len(want) {
		t.Fatalf("got %+v, want three source-backed proposals", got)
	}
	for i, value := range want {
		if got[i].Value != value || got[i].Note != "show.txt" {
			t.Errorf("proposal %d = %+v, want %s", i, got[i], value)
		}
	}
	if got[2].Quote != "Björk Theatre" || got[2].Album != "Björk Theatre" || !got[2].AlbumSafe {
		t.Fatalf("unexpected venue evidence: %+v", got[2])
	}
}

func TestGuardNoteSuggestionsRejectsConflicts(t *testing.T) {
	notes := map[string][]string{
		"show.txt":  {"Hall", "2025-03-21"},
		"other.txt": {"Track list:", "01. A Song", "", "Venue: Another Hall"},
	}
	recording := Recording{Notes: []string{"show.txt", "other.txt"}, Tracks: []Track{
		{Path: "2025-03-21t01.flac"}, {Path: "2025-03-22t02.flac"},
	}}
	spans := []notesmodel.Span{
		{Field: "date", Note: "show.txt", Line: 1, Start: 0, End: 10},
		{Field: "venue", Note: "show.txt", Line: 0, Start: 0, End: 4},
	}
	if got := GuardNoteSuggestions(recording, notes, spans, AssumeUS); len(got) != 0 {
		t.Fatalf("conflicting evidence emitted %+v", got)
	}
}

func TestSuggestionDatesFollowPolicyAndFilenameEvidence(t *testing.T) {
	for _, tc := range []struct {
		desc, quote, want string
		policy            DatePolicy
		names             map[string]bool
	}{
		{desc: "US assumption", quote: "04/03/2025", want: "2025-04-03", policy: AssumeUS},
		{desc: "ambiguous without US assumption", quote: "04/03/2025", policy: ReviewAmbiguous},
		{desc: "obvious day first", quote: "21/03/2025", want: "2025-03-21", policy: ReviewAmbiguous},
		{desc: "filename corroborates day first", quote: "04/03/2025", want: "2025-03-04", policy: ReviewAmbiguous, names: map[string]bool{"2025-03-04": true}},
		{desc: "filename conflicts with date", quote: "21/03/2025", policy: AssumeUS, names: map[string]bool{"2025-03-22": true}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, ok := suggestionDate(tc.quote, tc.names, tc.policy, true)
			if ok != (tc.want != "") || ok && got != tc.want {
				t.Fatalf("date %q, accepted %t, want %q", got, ok, tc.want)
			}
		})
	}
}
