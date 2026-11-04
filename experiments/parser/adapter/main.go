//go:build parser_experiment

// Command adapter evaluates cases using the public catalog scanner in disposable trees.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"track-tagger/internal/catalog"
)

type note struct{ Name, Text, Encoding string }
type input struct {
	ID    string
	Notes []note
	Files []string
}
type mapping struct {
	Disc   int    `json:"disc"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

func run(c input) (map[string]any, error) {
	seen := map[string]bool{}
	dir, e := os.MkdirTemp("", "parser-eval-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	write := func(name string, b []byte) error {
		if strings.Contains(name, `\`) || regexp.MustCompile(`^[A-Za-z]:`).MatchString(name) {
			return fmt.Errorf("unsafe path %q", name)
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if seen[clean] {
			return fmt.Errorf("duplicate path %q", name)
		}
		seen[clean] = true
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path %q", name)
		}
		p := filepath.Join(dir, clean)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		return os.WriteFile(p, b, 0600)
	}
	for _, n := range c.Notes {
		b := []byte(n.Text)
		if n.Encoding == "windows-1252" {
			b, e = charmap.Windows1252.NewEncoder().Bytes(b)
			if e != nil {
				return nil, e
			}
		} else if n.Encoding != "" && n.Encoding != "utf-8" {
			return nil, fmt.Errorf("unsupported encoding %s", n.Encoding)
		}
		if e = write(n.Name, b); e != nil {
			return nil, e
		}
	}
	for _, f := range c.Files {
		if e = write(f, []byte("synthetic disposable audio")); e != nil {
			return nil, e
		}
	}
	rs, e := catalog.Scan(dir)
	if e != nil {
		return nil, e
	}
	if len(rs) != 1 {
		return nil, fmt.Errorf("expected one recording, got %d", len(rs))
	}
	r := rs[0]
	texts := make([]string, 0, len(c.Notes))
	for _, n := range c.Notes {
		ext := strings.ToLower(filepath.Ext(n.Name))
		if ext == ".txt" || ext == ".nfo" {
			texts = append(texts, n.Text)
		}
	}
	titles := catalog.ExperimentTitles(texts)
	mappings := map[string]mapping{}
	for _, t := range r.Tracks {
		mappings[filepath.ToSlash(t.RelativePath)] = mapping{t.Disc, t.Number, t.Title}

	}
	return map[string]any{"id": c.ID, "artist": r.Artist, "date": r.Date, "venue": r.Venue, "city": r.City, "titles": titles, "mappings": mappings, "review": r.Status != "ready", "status": r.Status, "review_reasons": r.ReviewReasons}, nil
}
func main() {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 16<<20)
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for s.Scan() {
		if len(os.Args) == 2 && os.Args[1] == "--identifiers" {
			var paths []string
			if e := json.Unmarshal(s.Bytes(), &paths); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			out := make([]map[string]int, 0, len(paths))
			for _, p := range paths {
				d, n := catalog.ExperimentFileNumber(filepath.FromSlash(p))
				out = append(out, map[string]int{"disc": d, "number": n})
			}
			if e := json.NewEncoder(w).Encode(out); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			continue
		}
		var c input
		if e := json.Unmarshal(s.Bytes(), &c); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		out, e := run(c)
		if e != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", c.ID, e)
			os.Exit(1)
		}
		if e = json.NewEncoder(w).Encode(out); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
	}
	if e := s.Err(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
