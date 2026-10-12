package webui

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Audio is a locally available, validated media file. The handler owns Content
// and closes it after serving. Backends must resolve identifiers against their
// recording inventory.
type Audio struct {
	Content     io.ReadSeekCloser
	Name        string
	ModTime     time.Time
	ContentType string
}

// AudioBackend optionally enables read-only playback in all operating modes.
type AudioBackend interface {
	OpenAudio(context.Context, string, string) (Audio, error)
}

func (s *server) audio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	backend, ok := s.backend.(AudioBackend)
	if !ok {
		http.Error(w, "playback unavailable", http.StatusServiceUnavailable)
		return
	}
	recording, track := r.URL.Query().Get("recording"), r.URL.Query().Get("track")
	if recording == "" || track == "" {
		http.NotFound(w, r)
		return
	}
	audio, err := backend.OpenAudio(r.Context(), recording, track)
	if audio.Content != nil {
		defer func() { _ = audio.Content.Close() }()
	}
	if err != nil || audio.Content == nil {
		// Avoid disclosing source paths or filesystem errors to clients.
		http.NotFound(w, r)
		return
	}
	switch audio.ContentType {
	case "audio/flac", "audio/wav", "audio/mpeg", "audio/ogg":
		w.Header().Set("Content-Type", audio.ContentType)
	default:
		http.NotFound(w, r)
		return
	}
	// Browsers may consume long recordings slowly. Keep page timeouts while
	// allowing this read-only stream to remain open until the client closes it.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	http.ServeContent(w, r, audio.Name, audio.ModTime, audio.Content)
}
