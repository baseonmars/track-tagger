package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
	"track-tagger/internal/webui"
)

type playbackFile struct {
	path string
	info os.FileInfo
}

func playbackKey(recording, track string) string { return recording + ":" + track }

// Only files from an observed recording page can be streamed. A range request
// never rescans or hashes the collection; file identity/stat checks detect
// replaced or modified sources and ask the listener to refresh the page.
func (e *Engine) registerPlayback(r *webui.Recording) {
	e.streamMu.Lock()
	defer e.streamMu.Unlock()
	if e.streams == nil {
		e.streams = make(map[string]playbackFile)
	}
	prefix := r.ID + ":"
	for k := range e.streams {
		if strings.HasPrefix(k, prefix) {
			delete(e.streams, k)
		}
	}
	for i := range r.Tracks {
		t := &r.Tracks[i]
		if !catalog.IsAudio(t.File) || safePath(e.Source, t.File) != nil || media.CheckLocal(t.File) != nil {
			continue
		}
		st, err := os.Stat(t.File)
		if err != nil {
			continue
		}
		sum := sha256.Sum256([]byte(t.File))
		t.PlaybackID = hex.EncodeToString(sum[:])
		e.streams[playbackKey(r.ID, t.PlaybackID)] = playbackFile{t.File, st}
	}
}

func (w WebBackend) OpenAudio(ctx context.Context, recording, track string) (webui.Audio, error) {
	if err := ctx.Err(); err != nil {
		return webui.Audio{}, err
	}
	e := w.E
	e.streamMu.Lock()
	entry, ok := e.streams[playbackKey(recording, track)]
	e.streamMu.Unlock()
	if !ok {
		return webui.Audio{}, os.ErrNotExist
	}
	if err := safePath(e.Source, entry.path); err != nil {
		return webui.Audio{}, err
	}
	if err := media.CheckLocal(entry.path); err != nil {
		return webui.Audio{}, err
	}
	rel, err := filepath.Rel(e.Source, entry.path)
	if err != nil {
		return webui.Audio{}, err
	}
	root, err := os.OpenRoot(e.Source)
	if err != nil {
		return webui.Audio{}, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return webui.Audio{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return webui.Audio{}, err
	}
	if !st.Mode().IsRegular() || !os.SameFile(st, entry.info) || st.Size() != entry.info.Size() || !st.ModTime().Equal(entry.info.ModTime()) {
		f.Close()
		return webui.Audio{}, errors.New("audio changed; refresh the recording")
	}
	mime := ""
	switch strings.ToLower(filepath.Ext(entry.path)) {
	case ".flac":
		mime = "audio/flac"
	case ".wav":
		mime = "audio/wav"
	case ".mp3":
		mime = "audio/mpeg"
	case ".ogg", ".oga", ".opus":
		mime = "audio/ogg"
	default:
		f.Close()
		return webui.Audio{}, os.ErrNotExist
	}
	return webui.Audio{Content: f, Name: filepath.Base(entry.path), ModTime: st.ModTime(), ContentType: mime}, nil
}
