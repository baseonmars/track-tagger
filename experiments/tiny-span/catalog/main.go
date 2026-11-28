//go:build parser_experiment

// Command catalog runs the production scanner over disposable note and filename fixtures.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"track-tagger/internal/catalog"
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
	ID            string             `json:"id"`
	Error         string             `json:"error,omitempty"`
	Metadata      map[string]string  `json:"metadata,omitempty"`
	Mappings      map[string]mapping `json:"mappings,omitempty"`
	Ready         bool               `json:"ready"`
	Status        string             `json:"status,omitempty"`
	ReviewReasons []string           `json:"review_reasons"`
}

func scan(c input) (result, error) {
	out := result{ID: c.ID}
	dir, err := os.MkdirTemp("", "track-tagger-catalog-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	write := func(name, content string) error {
		if strings.ContainsAny(name, "\\:") || !filepath.IsLocal(filepath.FromSlash(name)) || filepath.Clean(filepath.FromSlash(name)) == "." {
			return fmt.Errorf("unsafe fixture path %q", name)
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(content)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	for _, n := range c.Notes {
		if err := write(n.Name, n.Text); err != nil {
			return out, err
		}
	}
	for _, name := range c.Files {
		if err := write(name, "disposable audio placeholder"); err != nil {
			return out, err
		}
	}

	recordings, err := catalog.Scan(dir)
	if err != nil {
		return out, err
	}
	if len(recordings) != 1 {
		return out, fmt.Errorf("expected one recording, got %d", len(recordings))
	}
	r := recordings[0]
	out.Metadata = map[string]string{"artist": r.Artist, "date": r.Date, "venue": r.Venue, "city": r.City}
	out.Mappings = make(map[string]mapping, len(r.Tracks))
	for _, t := range r.Tracks {
		out.Mappings[filepath.ToSlash(t.RelativePath)] = mapping{t.Disc, t.Number, t.Title}
	}
	out.Ready = r.Status == "ready"
	out.Status = r.Status
	out.ReviewReasons = append([]string{}, r.ReviewReasons...)
	slices.Sort(out.ReviewReasons)
	return out, nil
}

func run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var c input
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			return err
		}
		out, err := scan(c)
		if err != nil {
			out.Error = strings.ReplaceAll(err.Error(), os.TempDir(), "[temp]/")
		}
		if err := encoder.Encode(out); err != nil {
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
