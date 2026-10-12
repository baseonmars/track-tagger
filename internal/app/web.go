package app

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/text/encoding/charmap"
	"io"
	"os"
	"track-tagger/internal/media"
	"unicode/utf8"

	"track-tagger/internal/webui"
)

type WebBackend struct{ E *Engine }

func (w WebBackend) List(ctx context.Context) ([]webui.Recording, error) {
	p, err := w.E.browse()
	if err != nil {
		return nil, err
	}
	var rs []webui.Recording
	latest := map[string]string{}
	w.E.mu.Lock()
	if w.E.store != nil {
		jobs, x := w.E.store.History()
		if x == nil {
			for _, j := range jobs {
				if _, ok := latest[j.RecordingID]; !ok {
					latest[j.RecordingID] = j.Status
				}
			}
		}
	}
	w.E.mu.Unlock()
	for _, r := range p.Recordings {
		wr := webRecord(r)
		if wr.Status == "ready" && latest[r.ID] == "failed" {
			wr.Status = "failed"
		}
		w.E.registerPlayback(&wr)
		rs = append(rs, wr)
	}
	return rs, nil
}
func webRecord(r Item) webui.Recording {
	out := webui.Recording{ID: r.ID, Fingerprint: r.Fingerprint, Path: r.Path, PlaybackOnly: r.PlaybackOnly, DateUnknown: r.DateUnknown, Status: r.Status, Metadata: map[string]string{"ARTIST": r.Artist, "ALBUM": r.Album, "DATE": r.Date}, Artwork: r.ArtworkCandidates, SelectedArtwork: r.Artwork, Reasons: r.ReviewReasons}
	for _, t := range r.Tracks {
		out.Tracks = append(out.Tracks, webui.Track{File: t.Path, Title: t.Title, Artist: t.Artist, Track: t.Number, Disc: t.Disc})
	}
	for _, ev := range r.Evidence {
		out.Evidence = append(out.Evidence, fmt.Sprintf("%s · SHA256 %s", ev.Path, ev.SHA256))
	}
	for _, path := range r.Notes {
		if media.CheckLocal(path) != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(f, 4<<20))
		f.Close()
		if err != nil {
			continue
		}
		text := string(b)
		if !utf8.Valid(b) {
			text, _ = charmap.Windows1252.NewDecoder().String(text)
		}
		out.Notes = append(out.Notes, webui.Note{Path: path, Text: text})
	}
	return out
}
func (w WebBackend) Get(ctx context.Context, id string) (webui.Recording, error) {
	p, err := w.E.browse()
	if err != nil {
		return webui.Recording{}, err
	}
	for _, item := range p.Recordings {
		if item.ID != id {
			continue
		}
		r := webRecord(item)
		w.E.mu.Lock()
		if r.Status == "ready" && w.E.store != nil {
			jobs, err := w.E.store.History()
			if err == nil {
				for _, job := range jobs {
					if job.RecordingID == id {
						if job.Status == "failed" {
							r.Status = "failed"
						}
						break
					}
				}
			}
		}
		w.E.mu.Unlock()
		w.E.registerPlayback(&r)
		return r, nil
	}
	return webui.Recording{}, errors.New("recording not found")
}
func (w WebBackend) SaveReview(ctx context.Context, id string, v webui.Review) error {
	if !v.Approve {
		return errors.New("explicit review acknowledgment required")
	}
	p, err := w.E.Scan()
	if err != nil {
		return err
	}
	for _, r := range p.Recordings {
		if r.ID != id {
			continue
		}
		if v.Fingerprint != r.Fingerprint {
			return errors.New("inputs changed since this page was loaded; refresh and review again")
		}
		for k := range v.Metadata {
			if k != "ARTIST" && k != "ALBUM" && k != "DATE" {
				return fmt.Errorf("unsupported album field %s", k)
			}
		}
		r.Artist = v.Metadata["ARTIST"]
		r.Album = v.Metadata["ALBUM"]
		r.Date = v.Metadata["DATE"]
		r.DateUnknown = v.DateUnknown
		r.Artwork = v.Artwork
		if len(v.Tracks) != len(r.Tracks) {
			return errors.New("map every file")
		}
		for i, t := range v.Tracks {
			r.Tracks[i].Path = t.File
			r.Tracks[i].Title = t.Title
			r.Tracks[i].Artist = t.Artist
			r.Tracks[i].Disc = t.Disc
			r.Tracks[i].Number = t.Track
		}
		return w.E.SaveReview(id, r)
	}
	return errors.New("recording not found")
}
func (w WebBackend) Apply(ctx context.Context, id string) error {
	p, err := w.E.Scan()
	if err != nil {
		return err
	}
	for _, r := range p.Recordings {
		if r.ID == id {
			if r.Status != "ready" {
				return fmt.Errorf("recording is %s", r.Status)
			}
			p.Recordings = []Item{r}
			_, err = w.E.Apply(p)
			return err
		}
	}
	return errors.New("recording not found")
}
func (w WebBackend) Undo(ctx context.Context, id string) error {
	_, err := w.E.Undo(id, true)
	return err
}
func (w WebBackend) History(ctx context.Context) ([]webui.Job, error) {
	w.E.mu.Lock()
	defer w.E.mu.Unlock()
	if err := w.E.OpenStorage(); err != nil {
		return nil, err
	}
	jobs, err := w.E.store.History()
	var out []webui.Job
	for _, j := range jobs {
		out = append(out, webui.Job{ID: j.ID, RecordingID: j.RecordingID, Status: j.Status, Summary: j.Summary})
	}
	return out, err
}
