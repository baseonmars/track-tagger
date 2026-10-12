package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func registeredAudioURL(t *testing.T, e *Engine) string {
	t.Helper()
	rs, err := (WebBackend{E: e}).List(context.Background())
	if err != nil || len(rs) != 1 || len(rs[0].Tracks) == 0 || rs[0].Tracks[0].PlaybackID == "" {
		t.Fatalf("playback registration: %+v, %v", rs, err)
	}
	return "/audio?" + url.Values{"recording": {rs[0].ID}, "track": {rs[0].Tracks[0].PlaybackID}}.Encode()
}

func TestHTTPRegisteredAudioRangesAndHead(t *testing.T) {
	e, path := engineFixture(t, DryRun)
	before := checkedHash(t, path)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := registeredAudioURL(t, e)
	s := realWeb(t, e, false)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequest(method, s.server.URL+endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", "bytes=4-17")
		resp, err := s.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "audio/flac" || resp.Header.Get("Content-Range") != "bytes 4-17/"+strconv.Itoa(len(original)) {
			t.Fatalf("%s response: %d %v", method, resp.StatusCode, resp.Header)
		}
		if method == http.MethodGet && !bytes.Equal(body, original[4:18]) {
			t.Fatalf("wrong range: %x", body)
		}
		if method == http.MethodHead && len(body) != 0 {
			t.Fatal("HEAD returned body")
		}
	}
	if checkedHash(t, path) != before {
		t.Fatal("playback changed source")
	}
	for _, values := range []url.Values{{"recording": {"unknown"}, "track": {"unknown"}}, {"recording": {"unknown"}, "track": {path}}, {"recording": {e.Source}, "track": {path}}, {"recording": {"unknown"}, "track": {"../../etc/passwd"}}} {
		s.request(http.MethodGet, "/audio?"+values.Encode(), nil, http.StatusNotFound)
	}
}

func TestHTTPAudioRejectsChangedAndSymlinkSources(t *testing.T) {
	for _, kind := range []string{"edited", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			e, path := engineFixture(t, DryRun)
			endpoint := registeredAudioURL(t, e)
			s := realWeb(t, e, false)
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "secret.flac")
				if err := os.WriteFile(target, []byte("private data"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte("edited"))
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			s.request(http.MethodGet, endpoint, nil, http.StatusNotFound)
			if kind == "edited" {
				refreshed := registeredAudioURL(t, e)
				s.request(http.MethodGet, refreshed, nil, http.StatusOK)
			}
		})
	}
}

func TestHTTPPlaybackMIMEsAndWriteGuards(t *testing.T) {
	for _, tc := range []struct{ ext, mime string }{{"wav", "audio/wav"}, {"mp3", "audio/mpeg"}, {"ogg", "audio/ogg"}, {"oga", "audio/ogg"}, {"opus", "audio/ogg"}} {
		for _, mixed := range []bool{false, true} {
			name := tc.ext
			if mixed {
				name += "-mixed"
			}
			t.Run(name, func(t *testing.T) {
				e, flacPath := engineFixture(t, Write)
				before := checkedHash(t, flacPath)
				if !mixed {
					if err := os.Remove(flacPath); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(e.Source, "t01."+tc.ext)
				payload := []byte("synthetic playback bytes; decoder compatibility is browser-tested separately")
				if err := os.WriteFile(path, payload, 0644); err != nil {
					t.Fatal(err)
				}
				plan, err := e.Scan()
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.Recordings) != 1 || plan.Recordings[0].Status != "playback-only" || !plan.Recordings[0].PlaybackOnly {
					t.Fatalf("unsafe playback plan: %+v", plan)
				}
				rs, err := (WebBackend{E: e}).List(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				var id string
				for _, tr := range rs[0].Tracks {
					if tr.File == path {
						id = tr.PlaybackID
					}
				}
				if id == "" {
					t.Fatal("non-FLAC playback missing")
				}
				s := realWeb(t, e, true)
				endpoint := "/audio?" + url.Values{"recording": {rs[0].ID}, "track": {id}}.Encode()
				resp, err := s.client.Get(s.server.URL + endpoint)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != tc.mime || !bytes.Equal(body, payload) {
					t.Fatalf("playback response: %d %v %q", resp.StatusCode, resp.Header, body)
				}
				item := &plan.Recordings[0]
				item.Status = "ready"
				item.Reviewed = true
				item.PlaybackOnly = false
				if _, err := e.Apply(plan); err == nil || !strings.Contains(err.Error(), "playback-only") {
					t.Fatalf("forged apply accepted: %v", err)
				}
				if err := e.SaveReview(item.ID, *item); err == nil || !strings.Contains(err.Error(), "playback-only") {
					t.Fatalf("forged review accepted: %v", err)
				}
				actual, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, payload) {
					t.Fatal("playback-only source changed")
				}
				if mixed && checkedHash(t, flacPath) != before {
					t.Fatal("mixed FLAC changed")
				}
			})
		}
	}
}

func TestNewAudioDiscoveryInvalidatesLegacyReview(t *testing.T) {
	e, _ := engineFixture(t, DryRun)
	if err := os.WriteFile(filepath.Join(e.Source, "t02.mp3"), []byte("ID3audio"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	r := p.Recordings[0]
	if len(r.Tracks) != 2 {
		t.Fatal("missing audio")
	}
	originalEvidence := evidence(r.Recording)
	r.Tracks = r.Tracks[:1]
	r.PlaybackOnly = false
	r.Reviewed = true
	if err = e.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	if err = e.store.saveReview(r.ID, originalEvidence, r); err != nil {
		t.Fatal(err)
	}
	p, err = e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Recordings[0].Tracks) != 2 || p.Recordings[0].Reviewed || !p.Recordings[0].PlaybackOnly {
		t.Fatal("legacy review hid newly supported audio")
	}
}
