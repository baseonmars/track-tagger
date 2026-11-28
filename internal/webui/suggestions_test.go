package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"track-tagger/internal/catalog"
)

type suggestionBackend struct {
	fakeBackend
	calls           int
	id, fingerprint string
	err             error
}

func (b *suggestionBackend) Suggestions(_ context.Context, id, fingerprint string) ([]catalog.NoteSuggestion, error) {
	b.calls++
	b.id, b.fingerprint = id, fingerprint
	return []catalog.NoteSuggestion{{
		Field: "CITY", Value: "Montréal", Note: "show.txt", Line: 1,
		Start: 2, End: 10, Quote: "Montréal", Context: "🎶 Montréal <span>",
	}}, b.err
}

func TestNoteSuggestionRouteUsesExplicitOptInAndReviewIdentity(t *testing.T) {
	b := &suggestionBackend{fakeBackend: fakeBackend{recording: &Recording{
		ID: "one", Fingerprint: "source", Status: "needs-review",
	}}}
	for _, enabled := range []bool{false, true} {
		h, err := New(Config{NoteSuggestions: enabled}, b)
		if err != nil {
			t.Fatal(err)
		}
		csrf := csrfFrom(t, h)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/recording?id=one", nil))
		if strings.Contains(w.Body.String(), `id="find-note-suggestions"`) != enabled {
			t.Fatalf("opt-in %v: unexpected review UI", enabled)
		}
		form := url.Values{"csrf": {csrf}, "id": {"one"}, "fingerprint": {"source"}}
		w = post(h, "/note-suggestions", form, "http://localhost")
		if !enabled {
			if w.Code != http.StatusNotFound || b.calls != 0 {
				t.Fatalf("disabled route invoked backend: %d, %d calls", w.Code, b.calls)
			}
			continue
		}
		var rows []catalog.NoteSuggestion
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 1 {
			t.Fatalf("suggestion result %d: %s", w.Code, w.Body)
		}
		if rows[0].Quote != "Montréal" || rows[0].Context != "🎶 Montréal <span>" || b.calls != 1 ||
			b.id != "one" || b.fingerprint != "source" || b.saved+b.applied+b.undone != 0 {
			t.Fatalf("unexpected review-only suggestion %v: %+v", rows, b)
		}
	}
}

func TestNoteSuggestionRouteRejectsUntrustedAndStaleRequests(t *testing.T) {
	b := &suggestionBackend{err: errors.Join(catalog.ErrStaleNoteSuggestion, errors.New("private path"))}
	h, err := New(Config{NoteSuggestions: true}, b)
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFrom(t, h)
	base := url.Values{"csrf": {csrf}, "id": {"one"}, "fingerprint": {"stale"}}
	for _, tc := range []struct {
		name, origin, csrf, fingerprint string
		want                            int
	}{
		{"cross origin", "http://example.test", csrf, "stale", http.StatusForbidden},
		{"missing CSRF", "", "", "stale", http.StatusForbidden},
		{"missing fingerprint", "", csrf, "", http.StatusBadRequest},
		{"stale", "", csrf, "stale", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base.Set("csrf", tc.csrf)
			base.Set("fingerprint", tc.fingerprint)
			w := post(h, "/note-suggestions", base, tc.origin)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "private path") {
				t.Fatalf("got %d: %s", w.Code, w.Body)
			}
		})
	}
	if b.calls != 1 {
		t.Fatalf("backend called %d times", b.calls)
	}

	h, err = New(Config{NoteSuggestions: true, Remote: true, Addr: ":8787", Token: "long-authentication-token"}, b)
	if err != nil {
		t.Fatal(err)
	}
	if w := post(h, "/note-suggestions", base, "http://localhost"); w.Code != http.StatusUnauthorized || b.calls != 1 {
		t.Fatalf("unauthenticated request got %d with %d backend calls", w.Code, b.calls)
	}
}

func TestNeedsReviewOnlyNoteSuggestionControl(t *testing.T) {
	b := &suggestionBackend{}
	h, err := New(Config{NoteSuggestions: true}, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Recording{
		{ID: "one", Fingerprint: "source", Status: "ready"},
		{ID: "one", Fingerprint: "source", Status: "needs-review", PlaybackOnly: true},
		{ID: "one", Fingerprint: "source", Status: "needs-review", Reviewed: true},
		{ID: "one", Fingerprint: "", Status: "needs-review"},
	} {
		b.recording = &rec
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/recording?id=one", nil))
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "find-note-suggestions") || b.calls != 0 {
			t.Fatalf("ineligible recording %v showed suggestion control", rec)
		}
	}
}
