package webui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type audioReader struct {
	*strings.Reader
	closed bool
}

func (r *audioReader) Close() error { r.closed = true; return nil }

type audioFake struct {
	fakeBackend
	reader           *audioReader
	err              error
	mime             string
	calls            int
	recording, track string
}

func (b *audioFake) OpenAudio(_ context.Context, recording, track string) (Audio, error) {
	b.calls++
	b.recording, b.track = recording, track
	b.reader = &audioReader{Reader: strings.NewReader("0123456789")}
	return Audio{Content: b.reader, Name: "track.flac", ContentType: b.mime}, b.err
}

func TestAudioStreaming(t *testing.T) {
	for _, writable := range []bool{false, true} {
		for _, tc := range []struct {
			name, method, byteRange, body string
			status                        int
		}{
			{"full", "GET", "", "0123456789", 200},
			{"partial", "GET", "bytes=2-5", "2345", 206},
			{"suffix", "GET", "bytes=-3", "789", 206},
			{"head", "HEAD", "", "", 200},
			{"unsatisfiable", "GET", "bytes=20-30", "", 416},
		} {
			t.Run(tc.name, func(t *testing.T) {
				b := &audioFake{mime: "audio/flac"}
				h, err := New(Config{Writable: writable}, b)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(tc.method, "http://localhost/audio?recording=album&track=track-id", nil)
				if tc.byteRange != "" {
					r.Header.Set("Range", tc.byteRange)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("status %d: %s", w.Code, w.Body.String())
				}
				if tc.status != 416 && w.Body.String() != tc.body {
					t.Fatalf("body %q", w.Body.String())
				}
				if tc.status != 416 && w.Header().Get("Content-Type") != "audio/flac" {
					t.Fatal("incorrect MIME")
				}
				if tc.status == 206 && w.Header().Get("Content-Range") == "" {
					t.Fatal("missing content range")
				}
				if b.reader == nil || !b.reader.closed {
					t.Fatal("reader not closed")
				}
				if b.recording != "album" || b.track != "track-id" {
					t.Fatal("identifiers not forwarded")
				}
				if b.applied != 0 || b.saved != 0 || b.undone != 0 {
					t.Fatal("playback mutated backend")
				}
			})
		}
	}
}

func TestAudioGuards(t *testing.T) {
	for _, tc := range []struct {
		name, method, url string
		cfg               Config
		status            int
	}{
		{"rebinding", "GET", "http://evil.example/audio?recording=a&track=b", Config{}, 403},
		{"auth", "GET", "http://localhost/audio?recording=a&track=b", Config{Remote: true, Token: "1234567890123456"}, 401},
		{"post", "POST", "http://localhost/audio?recording=a&track=b", Config{Writable: true}, 405},
		{"delete", "DELETE", "http://localhost/audio?recording=a&track=b", Config{}, 405},
		{"missing", "GET", "http://localhost/audio?recording=a", Config{}, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &audioFake{mime: "audio/flac"}
			h, err := New(tc.cfg, b)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.url, nil))
			if w.Code != tc.status || b.calls != 0 {
				t.Fatalf("status %d calls %d", w.Code, b.calls)
			}
		})
	}
}

func TestAudioUnavailableAndInvalid(t *testing.T) {
	for _, b := range []Backend{&fakeBackend{}, &audioFake{mime: "audio/flac", err: errors.New("secret/path")}, &audioFake{mime: "text/html"}} {
		h, err := New(Config{}, b)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/audio?recording=a&track=b", nil))
		if w.Code != http.StatusNotFound && w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("disclosed backend error")
		}
		if b, ok := b.(*audioFake); ok && !b.reader.closed {
			t.Fatal("error reader not closed")
		}
	}
}
