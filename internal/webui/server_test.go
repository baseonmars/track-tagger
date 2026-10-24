package webui

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type progressBackend struct {
	fakeBackend
	status Status
}

func (b *progressBackend) Status(context.Context) (Status, error) { return b.status, nil }

func TestStatusEndpointIsAuthenticatedAndOmitsPrivateDetails(t *testing.T) {
	b := &progressBackend{status: Status{Phase: "validating", Scanned: 1, Total: 3, Generation: 2, Recordings: []Recording{{ID: "one", Path: `C:\private\concert`, Fingerprint: "secret-hash", Status: "loading", Metadata: map[string]string{"ARTIST": "Band", "DATE": "1970"}, Tracks: []Track{{File: `C:\private\song.flac`}}, Notes: []Note{{Text: "private notes"}}}}}}
	h, err := New(Config{Remote: true, Addr: ":8787", Token: "long-authentication-token"}, b)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/status", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "http://localhost/status", nil)
	r.SetBasicAuth("reviewer", "long-authentication-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	for _, secret := range []string{"private", "secret-hash", "song.flac", "notes"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("status exposed %q", secret)
		}
	}
	var result statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Phase != "validating" || result.Scanned != 1 || result.Total != 3 || len(result.Recordings) != 1 || result.Recordings[0].Title != "Band · 1970" || result.Recordings[0].Ready {
		t.Fatalf("unexpected progress: %+v", result)
	}
}

func TestPendingReviewActionsRenderDisabled(t *testing.T) {
	b := &progressBackend{fakeBackend: fakeBackend{recording: &Recording{ID: "one", Status: "loading"}}, status: Status{Phase: "validating", Total: 2}}
	h, err := New(Config{Writable: true}, b)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	for _, fragment := range []string{`data-validated="false"`, `id="save-review" type="submit" disabled`, `id="apply-review" type="submit" formaction="/apply" disabled`, `id="scan-progress"`} {
		if !strings.Contains(w.Body.String(), fragment) {
			t.Fatalf("pending page missing %s", fragment)
		}
	}
	b.recording = &Recording{ID: "one", Status: "ready", Fingerprint: "validated-source"}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if !strings.Contains(w.Body.String(), `data-validated="true"`) {
		t.Fatal("ready recording remained blocked while other recordings validate")
	}
}

type fakeBackend struct {
	applied, undone, saved int
	review                 Review
	recording              *Recording
}

func (b *fakeBackend) List(context.Context) ([]Recording, error) {
	return []Recording{{ID: "one", Path: "concert", Status: "needs-review"}}, nil
}
func (b *fakeBackend) Get(context.Context, string) (Recording, error) {
	if b.recording != nil {
		return *b.recording, nil
	}
	return Recording{ID: "one", Path: "concert", Fingerprint: "source-version-1", Metadata: map[string]string{"ARTIST": "<script>alert(1)</script>"}, Tracks: []Track{{File: "t01.flac", Title: "Opening [intro]", Disc: 1, Track: 1}}, Artwork: []string{"cover.jpg"}, SelectedArtwork: "cover.jpg", Notes: []Note{{Path: "notes.txt", Text: "Original notes <script>unsafe</script>"}}}, nil
}
func (b *fakeBackend) SaveReview(_ context.Context, _ string, r Review) error {
	b.saved++
	b.review = r
	return nil
}
func (b *fakeBackend) ApplyReview(ctx context.Context, id string, r Review) error {
	if r.Approve {
		if err := b.SaveReview(ctx, id, r); err != nil {
			return err
		}
	}
	return b.Apply(ctx, id)
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
	if w := post(h, "/apply", url.Values{"csrf": {csrf}, "id": {"one"}, "fingerprint": {"source-version-1"}, "date": {"1970"}, "approve": {"yes"}}, "http://localhost"); w.Code != 303 {
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

func TestReviewAcceptsFlexibleDatesOrExplicitUnknown(t *testing.T) {
	for _, tc := range []struct {
		name, date string
		unknown    bool
		wantStatus int
	}{
		{name: "year", date: "1970", wantStatus: http.StatusSeeOther},
		{name: "month", date: "1970-04", wantStatus: http.StatusSeeOther},
		{name: "day", date: "1970-04-03", wantStatus: http.StatusSeeOther},
		{name: "trim date", date: " 1970-04 ", wantStatus: http.StatusSeeOther},
		{name: "unknown", unknown: true, wantStatus: http.StatusSeeOther},
		{name: "blank", wantStatus: http.StatusBadRequest},
		{name: "unknown with date", date: "1970", unknown: true, wantStatus: http.StatusBadRequest},
		{name: "invalid month", date: "1970-13", wantStatus: http.StatusBadRequest},
		{name: "invalid day", date: "1970-02-30", wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeBackend{}
			h, _ := New(Config{}, b)
			v := url.Values{
				"csrf":         {csrfFrom(t, h)},
				"id":           {"one"},
				"artist":       {"Artist"},
				"album":        {"Album"},
				"date":         {tc.date},
				"date_unknown": {""},
				"approve":      {"yes"},
				"fingerprint":  {"source-version-1"},
				"track_file":   {"t01.flac"},
				"track_title":  {"Opening"},
				"track_disc":   {"1"},
				"track_number": {"1"},
			}
			if tc.unknown {
				v.Set("date_unknown", "yes")
			}
			w := post(h, "/review", v, "http://localhost")
			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusSeeOther {
				if b.review.DateUnknown != tc.unknown || b.review.Metadata["DATE"] != strings.TrimSpace(tc.date) {
					t.Fatalf("saved date state: %+v", b.review)
				}
			} else if b.saved != 0 {
				t.Fatal("saved invalid date state")
			}
		})
	}
}

func TestDateAttentionAndGoogleSearchAreExplicitAndBounded(t *testing.T) {
	b := &fakeBackend{recording: &Recording{
		ID:          "one",
		Path:        `/Users/dan/private/Concert folder`,
		Fingerprint: "source-version-1",
		Metadata:    map[string]string{"ARTIST": "Artist & <Band>", "ALBUM": "Venue, City"},
		Notes:       []Note{{Path: "private.txt", Text: "PRIVATE NOTE CONTENT"}},
	}}
	h, _ := New(Config{}, b)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Concert date needs attention") || !strings.Contains(body, "Enter a date or mark it unknown") {
		t.Fatal("missing attention cue for blank date")
	}
	if !strings.Contains(body, `name="date_unknown"`) || !strings.Contains(body, `name="date" type="text" maxlength="10" aria-describedby="date-hint"`) || !strings.Contains(body, "Use YYYY, YYYY-MM, or YYYY-MM-DD") || strings.Contains(body, `name="date" type="date"`) {
		t.Fatal("date form does not expose flexible date and unknown controls")
	}
	if !strings.Contains(body, `target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer"`) || !strings.Contains(body, "Search Google for concert date (opens in new tab)") {
		t.Fatal("Google search link is not explicitly safe or labeled")
	}
	m := regexp.MustCompile(`href="(https://www\.google\.com/search\?q=[^"]+)" target="_blank"`).FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatal("missing Google search URL")
	}
	u, err := url.Parse(html.UnescapeString(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.Query().Get("q"), "Artist & <Band> Venue, City"; got != want {
		t.Fatalf("query %q, want %q, URL %q", got, want, m[1])
	}
	for _, private := range []string{"/Users/dan", "Concert folder", "PRIVATE NOTE CONTENT", "private.txt"} {
		if strings.Contains(m[1], private) {
			t.Fatalf("search URL leaked %q", private)
		}
	}
	if strings.Contains(body, "https://www.google.com/search") && strings.Contains(body, "<img") {
		t.Fatal("Google search must not load automatically")
	}
}

func TestDateUnknownCuePreservesExistingDates(t *testing.T) {
	b := &fakeBackend{recording: &Recording{ID: "one", Path: "concert", DateUnknown: true, Metadata: map[string]string{"DATE": "1970-01-20"}}}
	h, _ := New(Config{}, b)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
	if !strings.Contains(w.Body.String(), "Date unknown.") || !strings.Contains(w.Body.String(), "Existing date tags will be preserved.") {
		t.Fatal("missing acknowledged unknown-date cue")
	}
	if !strings.Contains(w.Body.String(), `name="date" type="text" maxlength="10" aria-describedby="date-hint" value=""`) {
		t.Fatal("unknown date should leave the editable field blank even when existing tags are preserved")
	}
	if !strings.Contains(w.Body.String(), "Search Google for concert date (opens in new tab)") {
		t.Fatal("Google search should remain available after marking the date unknown")
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

func TestUnifiedReviewFormAndApproval(t *testing.T) {
	for _, writable := range []bool{false, true} {
		b := &fakeBackend{}
		h, err := New(Config{Writable: writable}, b)
		if err != nil {
			t.Fatal(err)
		}
		page := httptest.NewRecorder()
		h.ServeHTTP(page, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
		body := page.Body.String()
		if strings.Count(body, "<form ") != 1 || strings.Contains(body, `action="/apply" method=`) {
			t.Fatal("expected one review form")
		}
		if strings.Contains(body, `id="apply-review"`) != writable {
			t.Fatal("apply visible in wrong mode")
		}
		if strings.Contains(body, `name="approve" value="yes" checked`) || !strings.Contains(body, `id="save-review" type="submit" disabled`) {
			t.Fatal("approval must begin unchecked and save disabled")
		}
		v := url.Values{"csrf": {csrfFrom(t, h)}, "id": {"one"}, "date": {"1970"}, "fingerprint": {"source-version-1"}}
		post(h, "/review", v, "http://localhost")
		if b.saved != 0 {
			t.Fatal("unapproved review reached backend")
		}
		post(h, "/apply", url.Values{"csrf": {v.Get("csrf")}, "id": {"one"}}, "http://localhost")
		if b.applied != 0 {
			t.Fatal("bare apply reached backend")
		}
	}
}
