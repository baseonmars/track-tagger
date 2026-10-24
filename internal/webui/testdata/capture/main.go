// Capture serves the production web UI with synthetic, read-only data.
// Run from the repository root: go run ./internal/webui/testdata/capture
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"

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
	flag.Parse()
	b := library{recordings: []webui.Recording{
		{ID: "maple-hall", Path: "examples/maple-hall", Status: "needs-review", Fingerprint: "synthetic-fixture", Metadata: map[string]string{"ARTIST": "The Lanterns", "ALBUM": "Maple Hall", "DATE": "1977-05-14"}, Artwork: []string{"cover.jpg"}, Reasons: []string{"Check the second track title against the concert notes.", "Review the proposed track mapping."}, Evidence: []string{"Files are matched by track identifiers."}, Notes: []webui.Note{{Path: "show-notes.txt", Text: "Maple Hall, May 14, 1977\n01 Opening Theme\n02 Northbound Train\n03 Final Light"}}, Tracks: []webui.Track{{File: "01-opening-theme.flac", Title: "Opening Theme", Track: 1, Disc: 1, PlaybackID: "one"}, {File: "02-northbound-train.flac", Title: "Northbound Train", Track: 2, Disc: 1, PlaybackID: "two"}, {File: "03-final-light.flac", Title: "Final Light", Track: 3, Disc: 1, PlaybackID: "three"}}},
		{ID: "river-session", Path: "examples/river-session", Status: "ready", Fingerprint: "synthetic-fixture-2", Metadata: map[string]string{"ARTIST": "Mara Vale", "ALBUM": "River Session", "DATE": "1982"}, Tracks: []webui.Track{{File: "01-riverside.flac", Title: "Riverside", Track: 1, Disc: 1}}},
		{ID: "archive-tape", Path: "examples/archive-tape", Status: "ready", PlaybackOnly: true, Metadata: map[string]string{"ARTIST": "The Lanterns", "ALBUM": "Archive Tape"}, Tracks: []webui.Track{{File: "side-a.mp3", Title: "Side A", PlaybackID: "side-a"}}},
	}}
	h, err := webui.New(webui.Config{Addr: *addr}, b)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Capture UI: http://%s/ (review: /recording?id=maple-hall; playback: /recording?id=archive-tape; history: /history; manual: /help)", *addr)
	log.Fatal(http.ListenAndServe(*addr, h))
}
