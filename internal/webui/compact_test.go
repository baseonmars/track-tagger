package webui

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompactLibraryDataEscapesScriptAndKeepsRelativePaths(t *testing.T) {
	b := &progressBackend{status: Status{Validated: true, Recordings: []Recording{{ID: "one", Path: `C:\private\show`, RelativePath: "Band/show", Metadata: map[string]string{"ARTIST": "Band", "ALBUM": "</script><script>alert(1)</script>"}}}}}
	h, err := New(Config{}, b)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/", nil))
	body := w.Body.String()
	start := strings.Index(body, `id="library-data">`)
	if start < 0 {
		t.Fatal("missing data")
	}
	data := strings.SplitN(body[start+len(`id="library-data">`):], "</script>", 2)[0]
	var rows []statusCard
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		t.Fatalf("unsafe embedded data: %v", err)
	}
	if len(rows) != 1 || rows[0].Artist != "Band" || rows[0].RelativePath != "Band/show" {
		t.Fatalf("rows: %+v", rows)
	}
	if strings.Contains(data, "<script>") || strings.Contains(data, "private") {
		t.Fatalf("unsafe data: %s", data)
	}
	for _, value := range []string{`C:\private\show`, "/private/show", "../private/show", `\\server\share`} {
		if relativeDisplayPath(value) != "" {
			t.Fatalf("accepted absolute/traversing path %q", value)
		}
	}
}
func TestLayoutSettingsAndAssets(t *testing.T) {
	h, err := New(Config{}, &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/settings", "/layout.js", "/library.js", "/review-layout.js", "/review-layout.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost"+path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
}
