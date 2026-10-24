// Capture serves the production web UI with synthetic, read-only data.
// Run from the repository root: go run ./internal/webui/testdata/capture
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	"track-tagger/internal/webui"
)

type library struct{ recordings []webui.Recording }

func (b library) List(context.Context) ([]webui.Recording, error) { return b.recordings, nil }
func (b library) Get(_ context.Context, id string) (webui.Recording, error) {
	for _, recording := range b.recordings {
		if recording.ID == id {
			return recording, nil
		}
	}
	return webui.Recording{}, fmt.Errorf("recording not found")
}
func (library) SaveReview(context.Context, string, webui.Review) error {
	return fmt.Errorf("capture fixture is read-only")
}
func (library) Apply(context.Context, string) error {
	return fmt.Errorf("capture fixture is read-only")
}
func (library) ApplyReview(context.Context, string, webui.Review) error {
	return fmt.Errorf("capture fixture is read-only")
}
func (library) Undo(context.Context, string) error { return fmt.Errorf("capture fixture is read-only") }
func (library) History(context.Context) ([]webui.Job, error) {
	return []webui.Job{{ID: "sample-job", RecordingID: "maple-hall", Status: "completed", Summary: "Updated metadata for 3 tracks (synthetic example)"}}, nil
}
func (b library) Status(context.Context) (webui.Status, error) {
	return webui.Status{Phase: "ready", Validated: true, Scanned: len(b.recordings), Total: len(b.recordings), Generation: 1, Recordings: b.recordings}, nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8788", "local listen address")
	recordings := flag.Int("recordings", 3, "number of synthetic recordings (at least 3)")
	tracks := flag.Int("tracks", 0, "tracks per recording; zero keeps the small screenshot examples")
	flag.Parse()
	if *recordings < 3 || *tracks < 0 {
		log.Fatal("recordings must be at least 3 and tracks must not be negative")
	}
	b := library{recordings: []webui.Recording{
		{ID: "maple-hall", Path: "examples/maple-hall", Status: "needs-review", Fingerprint: "synthetic-fixture", Metadata: map[string]string{"ARTIST": "The Lanterns", "ALBUM": "Maple Hall", "DATE": "1977-05-14"}, Artwork: []string{"cover.jpg"}, Reasons: []string{"Check the second track title against the concert notes.", "Review the proposed track mapping."}, Evidence: []string{"Files are matched by track identifiers."}, Notes: []webui.Note{{Path: "show-notes.txt", Text: "Maple Hall, May 14, 1977\n01 Opening Theme\n02 Northbound Train\n03 Final Light"}}, Tracks: []webui.Track{{File: "01-opening-theme.flac", Title: "Opening Theme", Track: 1, Disc: 1, PlaybackID: "one"}, {File: "02-northbound-train.flac", Title: "Northbound Train", Track: 2, Disc: 1, PlaybackID: "two"}, {File: "03-final-light.flac", Title: "Final Light", Track: 3, Disc: 1, PlaybackID: "three"}}},
		{ID: "river-session", Path: "examples/river-session", Status: "ready", Fingerprint: "synthetic-fixture-2", Metadata: map[string]string{"ARTIST": "Mara Vale", "ALBUM": "River Session", "DATE": "1982"}, Tracks: []webui.Track{{File: "01-riverside.flac", Title: "Riverside", Track: 1, Disc: 1}}},
		{ID: "archive-tape", Path: "examples/archive-tape", Status: "playback-only", PlaybackOnly: true, Metadata: map[string]string{"ARTIST": "The Lanterns", "ALBUM": "Archive Tape"}, Tracks: []webui.Track{{File: "side-a.mp3", Title: "Side A", PlaybackID: "side-a"}}},
	}}
	statuses := []string{"needs-review", "ready", "completed", "waiting", "playback-only"}
	for i := len(b.recordings); i < *recordings; i++ {
		status := statuses[i%len(statuses)]
		date := []string{"1977-05-14", "1982", "1991-06", ""}[i%4]
		r := webui.Recording{
			ID: fmt.Sprintf("synthetic-%05d", i+1), Path: fmt.Sprintf("examples/%05d-夜の音楽 — Montréal — an unusually long concert recording folder", i+1),
			Status: status, PlaybackOnly: status == "playback-only", DateUnknown: date == "", LastModified: int64(1700000000+i) * 1000000000,
			Fingerprint: fmt.Sprintf("synthetic-fixture-%d", i+1),
			Metadata:    map[string]string{"ARTIST": []string{"The Lanterns", "Élodie & the Satellites", "東京カルテット"}[i%3], "ALBUM": fmt.Sprintf("%05d — A very long evening at Théâtre des Étoiles / 夜の音楽", i+1), "DATE": date},
			Notes:       []webui.Note{{Path: "concert-notes.txt", Text: "Synthetic concert notes — no music files are read.\nArtist: Élodie & the Satellites\nVenue: Théâtre des Étoiles\nCheck uncertain dates and guest performers against these notes.\n" + strings.Repeat("Source notes: audience recording, encore and segue annotations.\n", 12)}},
		}
		if status == "needs-review" {
			r.Reasons = []string{"Check the concert date against the source notes.", "Review the proposed track mapping."}
		}
		b.recordings = append(b.recordings, r)
	}
	for i := range b.recordings {
		r := &b.recordings[i]
		r.RelativePath = strings.TrimPrefix(r.Path, "examples/")
		n := *tracks
		if n == 0 && i >= 3 {
			n = 3
		}
		if n == 0 {
			continue
		}
		r.Tracks = nil
		for t := 1; t <= n; t++ {
			ext := "flac"
			if r.PlaybackOnly {
				ext = "mp3"
			}
			track := webui.Track{File: fmt.Sprintf("d%dt%02d — Étoiles, improvisation et retour à la maison — 長い曲名.%s", 1+(t-1)/15, 1+(t-1)%15, ext), Title: fmt.Sprintf("%02d — Étoiles / 夜の音楽 → reprise", t), Track: 1 + (t-1)%15, Disc: 1 + (t-1)/15, PlaybackID: fmt.Sprintf("synthetic-%d-%d", i, t)}
			if t%7 == 0 {
				track.Artist = "Guest performer — Zoë"
			}
			r.Tracks = append(r.Tracks, track)
		}
	}
	h, err := webui.New(webui.Config{Addr: *addr}, b)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Capture UI: http://%s/ (review: /recording?id=maple-hall; playback: /recording?id=archive-tape; history: /history; manual: /help)", *addr)
	log.Fatal(http.ListenAndServe(*addr, h))
}
