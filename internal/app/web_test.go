package app

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"track-tagger/internal/media"
	"track-tagger/internal/webui"
)

// webSession follows rendered HTML and posts the same form fields as a browser.
// Its backend is the real catalog, SQLite store, and FLAC writer.
type webSession struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
}

func realWeb(t *testing.T, e *Engine, writable bool) *webSession {
	t.Helper()
	h, err := webui.New(webui.Config{Writable: writable}, WebBackend{E: e})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c := s.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &webSession{t: t, server: s, client: c}
}

func (s *webSession) request(method, path string, v url.Values, want int) string {
	s.t.Helper()
	var body io.Reader
	if v != nil {
		body = strings.NewReader(v.Encode())
	}
	r, err := http.NewRequest(method, s.server.URL+path, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if v != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", s.server.URL)
	}
	resp, err := s.client.Do(r)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	if resp.StatusCode != want {
		s.t.Fatalf("%s %s: got %d, want %d\n%s", method, path, resp.StatusCode, want, b)
	}
	return string(b)
}

func (s *webSession) recordingPage() (string, string) {
	s.t.Helper()
	list := s.request(http.MethodGet, "/", nil, http.StatusOK)
	m := regexp.MustCompile(`href="(/recording\?id=[^"]+)"`).FindStringSubmatch(list)
	if len(m) != 2 {
		s.t.Fatalf("recording link absent: %s", list)
	}
	path := html.UnescapeString(m[1])
	return path, s.request(http.MethodGet, path, nil, http.StatusOK)
}

func renderedForm(t *testing.T, page, action string) url.Values {
	t.Helper()
	m := regexp.MustCompile(`(?s)<form action="` + regexp.QuoteMeta(action) + `"[^>]*>(.*?)</form>`).FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatalf("form %s absent: %s", action, page)
	}
	v := url.Values{}
	for _, tag := range regexp.MustCompile(`<input\b[^>]*>`).FindAllString(m[1], -1) {
		attrs := map[string]string{}
		for _, a := range regexp.MustCompile(`([a-z_-]+)="([^"]*)"`).FindAllStringSubmatch(tag, -1) {
			attrs[a[1]] = html.UnescapeString(a[2])
		}
		if attrs["type"] == "checkbox" {
			continue
		}
		if name := attrs["name"]; name != "" {
			v.Add(name, attrs["value"])
		}
	}
	if action == "/review" {
		v.Set("artwork", "")
		v.Set("approve", "yes")
	}
	return v
}

func TestHTTPPreviewSavesReviewWithoutMediaChanges(t *testing.T) {
	e, audio := engineFixture(t, DryRun)
	before := checkedHash(t, audio)
	notes := filepath.Join(e.Source, "notes.txt")
	notesBefore := checkedHash(t, notes)
	s := realWeb(t, e, false)
	path, page := s.recordingPage()
	if !strings.Contains(page, "Artist: Band") || !strings.Contains(page, "01. First Song") {
		t.Fatal("original source notes not visible")
	}
	v := renderedForm(t, page, "/review")
	if v.Get("fingerprint") == "" || v.Get("csrf") == "" {
		t.Fatal("missing review binding")
	}
	v.Set("track_title", "Corrected title [annotation]")
	s.request(http.MethodPost, "/review", v, http.StatusSeeOther)
	saved := s.request(http.MethodGet, path, nil, http.StatusOK)
	if !strings.Contains(saved, `value="Corrected title [annotation]"`) {
		t.Fatal("saved review not reflected in UI")
	}
	s.request(http.MethodPost, "/apply", url.Values{"csrf": {v.Get("csrf")}, "id": {v.Get("id")}}, http.StatusForbidden)
	if checkedHash(t, audio) != before || checkedHash(t, notes) != notesBefore {
		t.Fatal("preview changed source files")
	}
	if _, err := os.Stat(e.Destination); !os.IsNotExist(err) {
		t.Fatalf("preview created output: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(e.Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err = reopened.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	p, err := reopened.Scan()
	if err != nil || len(p.Recordings) != 1 || !p.Recordings[0].Reviewed || p.Recordings[0].Tracks[0].Title != "Corrected title [annotation]" {
		t.Fatalf("review did not persist across restart: %+v %v", p, err)
	}
}

func TestHTTPStaleReviewRejected(t *testing.T) {
	e, audio := engineFixture(t, DryRun)
	before := checkedHash(t, audio)
	s := realWeb(t, e, false)
	_, page := s.recordingPage()
	v := renderedForm(t, page, "/review")
	notes := filepath.Join(e.Source, "notes.txt")
	f, err := os.OpenFile(notes, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nTransfer annotation arrived after page load.\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	body := s.request(http.MethodPost, "/review", v, http.StatusConflict)
	if !strings.Contains(body, "inputs changed") {
		t.Fatalf("unexpected stale review error: %s", body)
	}
	if checkedHash(t, audio) != before {
		t.Fatal("stale review modified audio")
	}
	p, err := e.Scan()
	if err != nil || len(p.Recordings) != 1 || p.Recordings[0].Reviewed {
		t.Fatalf("stale review was stored: %+v %v", p, err)
	}
}

func TestHTTPReviewApplyHistoryAndUndo(t *testing.T) {
	e, audio := engineFixture(t, Write)
	before, err := media.Inspect(audio)
	if err != nil {
		t.Fatal(err)
	}
	s := realWeb(t, e, true)
	path, page := s.recordingPage()
	v := renderedForm(t, page, "/review")
	v.Set("artist", "Reviewed Band")
	v.Set("album", "1970-01-20 - Reviewed Hall, London")
	v.Set("track_title", "Reviewed song [encore]")
	v.Set("track_artist", "Guest Artist")
	s.request(http.MethodPost, "/review", v, http.StatusSeeOther)
	saved := s.request(http.MethodGet, path, nil, http.StatusOK)
	s.request(http.MethodPost, "/apply", renderedForm(t, saved, "/apply"), http.StatusSeeOther)
	after, err := media.Inspect(audio)
	if err != nil {
		t.Fatal(err)
	}
	if after.Tags["ARTIST"] != "Guest Artist" || after.Tags["ALBUMARTIST"] != "Reviewed Band" || after.Tags["TITLE"] != "Reviewed song [encore]" || after.AudioHash != before.AudioHash {
		t.Fatalf("applied wrong metadata or altered audio: %+v", after)
	}
	history := s.request(http.MethodGet, "/history", nil, http.StatusOK)
	if !strings.Contains(strings.ToLower(history), "completed") {
		t.Fatal("completed job absent from history")
	}
	undo := renderedForm(t, history, "/undo")
	if undo.Get("id") == "" {
		t.Fatal("undo job ID missing")
	}
	s.request(http.MethodPost, "/undo", undo, http.StatusSeeOther)
	if checkedHash(t, audio) != before.FileHash {
		t.Fatal("HTTP undo did not restore original bytes")
	}
	history = s.request(http.MethodGet, "/history", nil, http.StatusOK)
	if !strings.Contains(history, "undone") {
		t.Fatal("undo not recorded in history")
	}
}

func TestHTTPUnknownDatePreservesExistingMetadata(t *testing.T) {
	e, audio := engineFixture(t, Write)
	stage := filepath.Join(t.TempDir(), "existing.flac")
	if _, err := media.Stage(audio, stage, map[string]string{"DATE": "1969-12-31"}, "", true); err != nil {
		t.Fatal(err)
	}
	if err := media.Replace(stage, audio); err != nil {
		t.Fatal(err)
	}
	before, err := media.Inspect(audio)
	if err != nil {
		t.Fatal(err)
	}
	s := realWeb(t, e, true)
	path, page := s.recordingPage()
	v := renderedForm(t, page, "/review")
	v.Set("date", "")
	s.request(http.MethodPost, "/review", v, http.StatusBadRequest)
	v.Set("date_unknown", "yes")
	s.request(http.MethodPost, "/review", v, http.StatusSeeOther)
	saved := s.request(http.MethodGet, path, nil, http.StatusOK)
	if !regexp.MustCompile(`<input[^>]*name="date_unknown"[^>]*checked`).MatchString(saved) {
		t.Fatal("unknown acknowledgment not rendered as checked")
	}
	p, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Recordings[0].Reviewed || !p.Recordings[0].DateUnknown || p.Recordings[0].Date != "" {
		t.Fatal("HTTP review lost unknown state")
	}
	s.request(http.MethodPost, "/apply", renderedForm(t, saved, "/apply"), http.StatusSeeOther)
	after, err := media.Inspect(audio)
	if err != nil {
		t.Fatal(err)
	}
	if after.Tags["DATE"] != "1969-12-31" || after.AudioHash != before.AudioHash {
		t.Fatalf("unknown review altered date/audio: %+v", after)
	}
}
