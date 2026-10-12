package webui

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type playbackRenderBackend struct {
	fakeBackend
	playbackOnly bool
}

func (b *playbackRenderBackend) Get(context.Context, string) (Recording, error) {
	return Recording{ID: "recording&one", PlaybackOnly: b.playbackOnly, Metadata: map[string]string{}, Tracks: []Track{{File: "track.flac", Title: "A <title>", PlaybackID: "track&one"}}}, nil
}

func TestPlayerRequiresExplicitPlay(t *testing.T) {
	for _, listenOnly := range []bool{false, true} {
		h, err := New(Config{Writable: true}, &playbackRenderBackend{playbackOnly: listenOnly})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/recording?id=one", nil))
		body := w.Body.String()
		if w.Code != 200 {
			t.Fatalf("render: %d %s", w.Code, body)
		}
		for _, expected := range []string{`<script src="/player.js" defer>`, `<audio id="recording-player" controls preload="none" aria-label="Track playback"></audio>`, `type="button" class="play-track"`, `recording=recording%26one&amp;track=track%26one`, `data-title="A &lt;title&gt;"`, `id="previous-track" disabled`, `id="next-track" disabled`, `<noscript>`} {
			if !strings.Contains(body, expected) {
				t.Errorf("listenOnly=%v missing %s", listenOnly, expected)
			}
		}
		if strings.Contains(body, "autoplay") {
			t.Error("player must not autoplay")
		}
		for _, form := range []string{`action="/review"`, `action="/apply"`} {
			if strings.Contains(body, form) == listenOnly {
				t.Errorf("listenOnly=%v unexpected form visibility: %s", listenOnly, form)
			}
		}
	}
}
