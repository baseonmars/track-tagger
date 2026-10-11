package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type fakeBackend struct {
	applied, undone, saved int
	review                 Review
}

func (b *fakeBackend) List(context.Context) ([]Recording, error) {
	return []Recording{{ID: "one", Path: "concert", Status: "needs-review"}}, nil
}
func (b *fakeBackend) Get(context.Context, string) (Recording, error) {
	return Recording{ID: "one", Path: "concert", Fingerprint: "source-version-1", Metadata: map[string]string{"ARTIST": "<script>alert(1)</script>"}, Tracks: []Track{{File: "t01.flac", Title: "Opening [intro]", Disc: 1, Track: 1}}, Artwork: []string{"cover.jpg"}, SelectedArtwork: "cover.jpg", Notes: []Note{{Path: "notes.txt", Text: "Original notes <script>unsafe</script>"}}}, nil
}
func (b *fakeBackend) SaveReview(_ context.Context, _ string, r Review) error {
	b.saved++
	b.review = r
	return nil
}
func (b *fakeBackend) Apply(context.Context, string) error { b.applied++; return nil }
func (b *fakeBackend) Undo(context.Context, string) error  { b.undone++; return nil }
func (b *fakeBackend) History(context.Context) ([]Job, error) {
	return []Job{{ID: "job", Summary: "Original metadata available"}}, nil
}
func csrfFrom(t *testing.T, h http.Handler) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	m := regexp.MustCompile(`name="csrf" value="([a-f0-9]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) != 2 {
		t.Fatal("missing csrf token")
	}
	return m[1]
}
func post(h http.Handler, path string, v url.Values, origin string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	h.ServeHTTP(w, r)
	return w
}
func TestPreviewGatesMediaButSavesReview(t *testing.T) {
	b := &fakeBackend{}
	h, err := New(Config{}, b)
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFrom(t, h)
	v := url.Values{"csrf": {csrf}, "id": {"one"}}
	for _, path := range []string{"/apply", "/undo"} {
		if w := post(h, path, v, "http://localhost"); w.Code != 403 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	v.Set("artist", "Artist")
	v.Set("album", "1970-01-20 - Venue")
	v.Set("date", "1970-01-20")
	v.Set("track_file", "t01.flac")
	v.Set("track_title", "Opening [intro]")
	v.Set("track_disc", "1")
	v.Set("track_number", "2")
	v.Set("artwork", "cover.jpg")
	v.Set("approve", "yes")
	v.Set("fingerprint", "source-version-1")
	if w := post(h, "/review", v, "http://localhost"); w.Code != 303 {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	if b.review.Metadata["ARTIST"] != "Artist" || b.review.Artwork != "cover.jpg" || len(b.review.Tracks) != 1 || b.review.Tracks[0].Title != "Opening [intro]" || b.review.Tracks[0].Track != 2 {
		t.Fatalf("form mapping: %+v", b.review)
	}
	if b.applied != 0 || b.undone != 0 || b.saved != 1 || !b.review.Approve || b.review.Fingerprint != "source-version-1" {
		t.Fatalf("bad mutation counts: %+v", b)
	}
}
func TestRemoteRequiresAuthentication(t *testing.T) {
	for _, c := range []Config{{Addr: "0.0.0.0:8787"}, {Remote: true}, {Addr: ":8787", Remote: true, Token: "short"}} {
		if _, err := New(c, &fakeBackend{}); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	token := "12345678901234567890"
	h, err := New(Config{Addr: ":8787", Remote: true, Token: token}, &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"", "wrong", token} {
		r := httptest.NewRequest("GET", "http://server/", nil)
		if password != "" {
			r.SetBasicAuth("user", password)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if password == token {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("status %d, want %d", w.Code, want)
		}
	}
}
func TestCSRFDNSRebindingAndEscaping(t *testing.T) {
	b := &fakeBackend{}
	h, _ := New(Config{Writable: true}, b)
	csrf := csrfFrom(t, h)
	for _, tc := range []struct{ token, origin string }{{"", ""}, {csrf, "https://evil.example"}} {
		w := post(h, "/apply", url.Values{"csrf": {tc.token}, "id": {"one"}}, tc.origin)
		if w.Code != 403 {
			t.Fatalf("CSRF allowed %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://evil.example/", nil))
	if w.Code != 403 {
		t.Fatal("rebound host allowed")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if strings.Contains(w.Body.String(), "<script>alert") {
		t.Fatal("unescaped metadata")
	}
	if w := post(h, "/apply", url.Values{"csrf": {csrf}, "id": {"one"}}, "http://localhost"); w.Code != 303 {
		t.Fatalf("valid apply %d", w.Code)
	}
	if b.applied != 1 {
		t.Fatal("apply not called")
	}
}

func TestReviewRequiresDisplayedFingerprint(t *testing.T) {
	b := &fakeBackend{}
	h, _ := New(Config{}, b)
	csrf := csrfFrom(t, h)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if !strings.Contains(w.Body.String(), `name="fingerprint" value="source-version-1"`) {
		t.Fatal("displayed evidence fingerprint missing from review form")
	}
	v := url.Values{"csrf": {csrf}, "id": {"one"}, "approve": {"yes"}}
	if w := post(h, "/review", v, "http://localhost"); w.Code != http.StatusBadRequest {
		t.Fatalf("missing fingerprint accepted: %d", w.Code)
	}
	if b.saved != 0 {
		t.Fatal("review saved without displayed source fingerprint")
	}
}

func TestReviewRejectsIncompleteOrInvalidTrackFields(t *testing.T) {
	b := &fakeBackend{}
	h, _ := New(Config{}, b)
	csrf := csrfFrom(t, h)
	v := url.Values{"csrf": {csrf}, "id": {"one"}, "fingerprint": {"source-version-1"}, "track_file": {"a.flac"}, "track_title": {"Opening"}}
	if w := post(h, "/review", v, "http://localhost"); w.Code != 400 {
		t.Fatal("accepted incomplete mapping")
	}
	v.Set("track_disc", "1")
	for _, number := range []string{"0", "-1", "a", "1.5"} {
		v.Set("track_number", number)
		if w := post(h, "/review", v, "http://localhost"); w.Code != 400 {
			t.Fatalf("accepted track number %s", number)
		}
	}
	if b.saved != 0 {
		t.Fatal("saved malformed track mapping")
	}
}

func TestEditableMetadataMappingsArtworkAndNotes(t *testing.T) {
	h, _ := New(Config{}, &fakeBackend{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	for _, field := range []string{`name="artist"`, `name="album"`, `name="date"`, `name="track_file" value="t01.flac"`, `name="track_title" value="Opening [intro]"`, `name="track_disc"`, `name="track_number"`, `<select id="artwork"`, `<option value="cover.jpg" selected>`, `Original notes &lt;script&gt;unsafe&lt;/script&gt;`} {
		if !strings.Contains(w.Body.String(), field) {
			t.Errorf("rendered form missing %s", field)
		}
	}
}
