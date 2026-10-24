package webui

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIssueSummaryGroupsOverlappingTrackDiagnostics(t *testing.T) {
	r := Recording{Reasons: []string{"artist is missing", "unmatched file without explicit track number: t01.flac", "unmatched audio file: t01.flac", "unmatched audio file: t02.flac"}}
	got := issueSummary(r)
	if len(got) != 2 || got[1] != "2 audio files need a track title or number confirmed." {
		t.Fatalf("summary: %v", got)
	}
	got = issueSummary(Recording{Reasons: []string{"possible other performer in track 1:13: two songs from Nate Wood's opening set"}})
	if len(got) != 1 || !strings.Contains(got[0], "Track 13 (disc 1)") {
		t.Fatalf("missing affected track: %v", got)
	}
}

func TestIssueSummaryExplainsSuggestedAndMissingNumericDates(t *testing.T) {
	reasons := []string{"ambiguous numeric concert date: 10/09/1969", "conflicting filename evidence for concert date"}
	for _, tc := range []struct {
		name, date, ambiguity, conflict string
	}{
		{"suggested", "1969-10-09", "proposed concert date uses month/day order", "Check the proposed concert date"},
		{"missing", "", "could use month/day or day/month order", "then enter the concert date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := issueSummary(Recording{Metadata: map[string]string{"DATE": tc.date}, Reasons: reasons})
			joined := strings.Join(got, " ")
			if len(got) != 2 || !strings.Contains(joined, tc.ambiguity) || !strings.Contains(joined, tc.conflict) {
				t.Fatalf("date guidance = %v", got)
			}
		})
	}
}

func TestReviewFormShowsSuggestedDateAndWarning(t *testing.T) {
	b := &fakeBackend{recording: &Recording{
		ID: "one", Path: "show", Fingerprint: "fp", Status: "needs-review",
		Metadata: map[string]string{"ARTIST": "Grateful Dead", "DATE": "1969-10-09", "ALBUM": "1969-10-09 - Reed College, Portland, OR"},
		Reasons:  []string{"ambiguous numeric concert date: 10/09/1969"},
		Tracks:   []Track{{File: "t01.flac", Disc: 1, Track: 1, Title: "Song"}},
	}}
	h, err := New(Config{}, b)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	body := w.Body.String()
	for _, want := range []string{`id="date" name="date" type="text" maxlength="10" aria-describedby="date-hint" value="1969-10-09"`, `id="album" name="album" value="1969-10-09 - Reed College, Portland, OR"`, "The proposed concert date uses month/day order. Check the notes and correct the date if needed."} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered suggestion missing %q", want)
		}
	}
}

type displayBackend struct{ fakeBackend }

func (*displayBackend) List(context.Context) ([]Recording, error) {
	return []Recording{{ID: "one", Path: "/private/music/recording", Status: "needs-review", Tracks: []Track{{File: "/private/music/recording/t01.flac"}}, Reasons: []string{"unmatched audio file: t01.flac"}}}, nil
}
func (*displayBackend) Get(context.Context, string) (Recording, error) {
	return Recording{ID: "one", Path: "/private/music/recording", Fingerprint: "fp", Tracks: []Track{{File: "/private/music/recording/t01.flac", Disc: 1, Track: 1}}}, nil
}
func TestLibraryShowsActionsAndShortNames(t *testing.T) {
	h, err := New(Config{}, &displayBackend{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/", nil))
	s := w.Body.String()
	for _, want := range []string{"Review recording", ">recording</a>", "Your music is unchanged.", "1 audio file needs"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(s, "/private/") {
		t.Fatal("library exposes full paths")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	s = w.Body.String()
	if !strings.Contains(s, `name="track_file" value="/private/music/recording/t01.flac"`) || !strings.Contains(s, `class="filename">t01.flac`) {
		t.Fatal("short file display must preserve exact form path")
	}
	if !strings.Contains(s, "<details><summary>Source details") || strings.Contains(s, "<details open") {
		t.Fatal("technical details should start collapsed")
	}
}
func TestHistoryIsItsOwnView(t *testing.T) {
	h, _ := New(Config{}, &fakeBackend{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/history", nil))
	s := w.Body.String()
	if !strings.Contains(s, "<h1>History &amp; undo</h1>") || strings.Contains(s, "No recordings") || strings.Contains(s, "recording-card") {
		t.Fatal("history mixed with recordings")
	}
}
func TestTrackArtistReviewField(t *testing.T) {
	b := &fakeBackend{}
	h, _ := New(Config{}, b)
	v := url.Values{"csrf": {csrfFrom(t, h)}, "id": {"one"}, "fingerprint": {"fp"}, "approve": {"yes"}, "date": {"1970"}, "track_file": {"a.flac"}, "track_title": {"Opening"}, "track_disc": {"1"}, "track_number": {"1"}, "track_artist": {"Guest"}}
	w := post(h, "/review", v, "http://localhost")
	if w.Code != 303 || b.review.Tracks[0].Artist != "Guest" {
		t.Fatalf("artist not saved: %d %+v", w.Code, b.review)
	}
	v["track_artist"] = []string{"One", "Two"}
	if post(h, "/review", v, "http://localhost").Code != 400 {
		t.Fatal("mismatched artist array accepted")
	}
}
