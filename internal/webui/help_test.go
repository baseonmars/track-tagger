package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type noHelpBackend struct{ calls int }

func (b *noHelpBackend) List(context.Context) ([]Recording, error) { b.calls++; return nil, nil }
func (b *noHelpBackend) Get(context.Context, string) (Recording, error) {
	b.calls++
	return Recording{}, nil
}
func (b *noHelpBackend) SaveReview(context.Context, string, Review) error  { b.calls++; return nil }
func (b *noHelpBackend) Apply(context.Context, string) error               { b.calls++; return nil }
func (b *noHelpBackend) ApplyReview(context.Context, string, Review) error { b.calls++; return nil }
func (b *noHelpBackend) Undo(context.Context, string) error                { b.calls++; return nil }
func (b *noHelpBackend) History(context.Context) ([]Job, error)            { b.calls++; return nil, nil }
func (b *noHelpBackend) Status(context.Context) (Status, error)            { b.calls++; return Status{}, nil }
func (b *noHelpBackend) RefreshBrowsing()                                  { b.calls++ }

func TestHelpUsesHostAndAuthenticationChecksWithoutBackend(t *testing.T) {
	b := &noHelpBackend{}
	h, err := New(Config{Token: "long-authentication-token"}, b)
	if err != nil {
		t.Fatal(err)
	}
	request := func(host string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/help", nil)
		if authenticated {
			r.SetBasicAuth("reviewer", "long-authentication-token")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("evil.example", true); w.Code != http.StatusForbidden {
		t.Fatalf("host: %d", w.Code)
	}
	if w := request("localhost", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("auth: %d", w.Code)
	}
	w := request("localhost", true)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "</html>") {
		t.Fatalf("help: %d %s", w.Code, w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "style-src 'unsafe-inline'") || !strings.Contains(csp, "img-src data:") {
		t.Fatalf("manual assets blocked by CSP: %s", csp)
	}
	if b.calls != 0 {
		t.Fatalf("help called backend %d times", b.calls)
	}
}

func TestHelpLinkOpensNewTab(t *testing.T) {
	h, err := New(Config{}, &noHelpBackend{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/", nil))
	if !strings.Contains(w.Body.String(), `href="/help" target="_blank" rel="noopener" aria-label="Help (opens in a new tab)"`) {
		t.Fatal("recordings navigation lacks accessible Help link")
	}
}
