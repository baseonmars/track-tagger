//go:build review_evaluation

// The review evaluation command replays the real WebBackend on disposable fixtures.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"track-tagger/internal/app"
	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
	"track-tagger/internal/notesmodel"
)

type input struct {
	ID    string
	Notes []struct{ Name, Text string }
	Files []string
}

type mapping struct {
	Disc   int    `json:"disc"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type result struct {
	SourcePrefix       string                   `json:"source_prefix,omitempty"`
	ScopedNoteCount    int                      `json:"scoped_note_count"`
	ScopedAudioCount   int                      `json:"scoped_audio_count"`
	ID                 string                   `json:"id"`
	Excluded           string                   `json:"excluded,omitempty"`
	Error              string                   `json:"error,omitempty"`
	Metadata           map[string]string        `json:"metadata,omitempty"`
	Mappings           map[string]mapping       `json:"mappings,omitempty"`
	Status             string                   `json:"status,omitempty"`
	ReviewReasons      []string                 `json:"review_reasons"`
	Eligible           bool                     `json:"eligible"`
	Ineligible         string                   `json:"ineligible,omitempty"`
	Proposals          []catalog.NoteSuggestion `json:"proposals"`
	MetadataPlanEqual  bool                     `json:"metadata_plan_equal"`
	BrowserRecordEqual bool                     `json:"browser_record_equal"`
	FixtureBytesEqual  bool                     `json:"fixture_bytes_equal"`
	RequestWallNS      int64                    `json:"request_wall_ns,omitempty"`
}

func writeFixture(root, name string, data []byte) error {
	// Match the earlier research adapter's portable path policy.
	path := filepath.FromSlash(name)
	if strings.ContainsAny(name, "\\:") || !filepath.IsLocal(path) || filepath.Clean(path) == "." {
		return fmt.Errorf("unsafe fixture path %q", name)
	}
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Close())
}

func snapshotBytes(root string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular fixture file %q", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(raw)
		return nil
	})
	return out, err
}

func replay(caseInput input, fixture []byte, scratch string, model *notesmodel.Model, reviewAmbiguous bool) (result, error) {
	out := result{ID: caseInput.ID, Proposals: []catalog.NoteSuggestion{}}
	work, err := os.MkdirTemp(scratch, "recording-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(work)
	source := filepath.Join(work, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		return out, err
	}
	for _, note := range caseInput.Notes {
		if err := writeFixture(source, note.Name, []byte(note.Text)); err != nil {
			out.Excluded = "portable fixture path"
			return out, err
		}
	}
	for _, name := range caseInput.Files {
		if err := writeFixture(source, name, fixture); err != nil {
			out.Excluded = "portable fixture path"
			return out, err
		}
	}
	before, err := snapshotBytes(source)
	if err != nil {
		return out, err
	}
	engine, err := app.New(app.Options{Source: source, State: filepath.Join(work, "state"), Mode: app.DryRun, ReviewAmbiguousDates: reviewAmbiguous})
	if err != nil {
		return out, err
	}
	defer engine.Close()
	backend := app.WebBackend{E: engine, NoteModel: model}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		status, err := backend.Status(ctx)
		if err != nil {
			return out, err
		}
		if status.Phase == "error" {
			return out, errors.New(status.Error)
		}
		if status.Validated {
			break
		}
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	plan, err := engine.Scan()
	if err != nil {
		return out, err
	}
	if len(plan.Recordings) != 1 {
		out.Excluded = fmt.Sprintf("expected one recording, got %d", len(plan.Recordings))
		return out, nil
	}
	base := plan.Recordings[0]
	out.SourcePrefix = filepath.ToSlash(base.RelativePath)
	out.ScopedNoteCount = len(base.Notes)
	out.ScopedAudioCount = len(base.Tracks)
	view, err := backend.Get(ctx, base.ID)
	if err != nil {
		return out, err
	}
	if view.Fingerprint != base.Fingerprint {
		return out, errors.New("browser and scanner fingerprints differ")
	}
	out.Metadata = map[string]string{"artist": base.Artist, "date": base.Date, "venue": base.Venue, "city": base.City}
	out.Mappings = make(map[string]mapping, len(base.Tracks))
	for _, t := range base.Tracks {
		out.Mappings[filepath.ToSlash(t.RelativePath)] = mapping{Disc: t.Disc, Number: t.Number, Title: t.Title}
	}
	out.Status, out.ReviewReasons = view.Status, view.Reasons
	out.Eligible = view.Status == "needs-review" && !view.PlaybackOnly && !view.Reviewed
	if out.Eligible {
		started := time.Now()
		out.Proposals, err = backend.Suggestions(ctx, view.ID, view.Fingerprint)
		out.RequestWallNS = time.Since(started).Nanoseconds()
		if err != nil {
			out.Error = err.Error()
		}
	} else {
		out.Ineligible = view.Status
	}

	afterPlan, err := engine.Scan()
	if err != nil {
		return out, err
	}
	afterView, err := backend.Get(ctx, base.ID)
	if err != nil {
		return out, err
	}
	after, err := snapshotBytes(source)
	if err != nil {
		return out, err
	}
	// Some reason producers iterate maps. Compare their normalized content.
	for i := range plan.Recordings {
		sort.Strings(plan.Recordings[i].ReviewReasons)
	}
	for i := range afterPlan.Recordings {
		sort.Strings(afterPlan.Recordings[i].ReviewReasons)
	}
	out.MetadataPlanEqual = reflect.DeepEqual(plan, afterPlan)
	out.BrowserRecordEqual = reflect.DeepEqual(view, afterView)
	out.FixtureBytesEqual = reflect.DeepEqual(before, after)
	return out, nil
}

func run() error {
	var pack, audio, scratch string
	var reviewAmbiguous bool
	flag.StringVar(&pack, "pack", "", "approved notes model pack")
	flag.StringVar(&audio, "synthetic-flac", "", "synthetic FLAC generated for this replay")
	flag.StringVar(&scratch, "scratch", "", "external disposable fixture parent")
	flag.BoolVar(&reviewAmbiguous, "review-ambiguous", false, "use review mode for ambiguous numeric dates")
	flag.Parse()
	if scratch == "" || pack == "" || audio == "" {
		return errors.New("pack, synthetic-flac and external scratch are required")
	}
	base, err := filepath.Abs(scratch)
	if err != nil {
		return err
	}
	checkout, err := os.Getwd()
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(checkout, base); err == nil && filepath.IsLocal(rel) {
		return errors.New("keep research fixtures outside the repository")
	}
	if _, err := media.InspectMetadata(audio); err != nil {
		return fmt.Errorf("synthetic FLAC fixture: %w", err)
	}
	fixture, err := os.ReadFile(audio)
	if err != nil {
		return err
	}
	model, err := notesmodel.Load(pack)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var c input
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			return err
		}
		row, err := replay(c, fixture, base, model, reviewAmbiguous)
		if err != nil {
			row.Error = err.Error()
		}
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
