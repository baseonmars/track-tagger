package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// Corpus expectations are authored from synthetic derivatives of inspected
// notes, independently of parser output. Original recordings never enter tests.
type corpusCase struct {
	ID, Split, Group string
	SourceURL        string `json:"source_url"`
	SourceSHA256     string `json:"source_sha256"`
	Features         []string
	Notes            []struct{ Name, Text, Encoding string }
	Files            []string
	Titles           map[string]string
	Mappings         map[string]corpusMapping
	Unresolved       []string
	Review           bool
	Explanation      string
}

type corpusMapping struct {
	Disc   int
	Number int
	Title  string
}

type corpusMetrics struct {
	Cases                     int      `json:"cases"`
	ExactCases                int      `json:"exact_cases"`
	CorrectTitles             int      `json:"correct_titles"`
	ExpectedTitles            int      `json:"expected_titles"`
	CorrectUnresolvedTitles   int      `json:"correct_unresolved_titles"`
	ExpectedUnresolvedTitles  int      `json:"expected_unresolved_titles"`
	CorrectMappings           int      `json:"correct_mappings"`
	ExpectedMappings          int      `json:"expected_mappings"`
	IncorrectAcceptedMappings int      `json:"incorrect_accepted_mappings"`
	FalseReady                int      `json:"false_ready"`
	ReviewCases               int      `json:"review_cases"`
	Failures                  []string `json:"failures"`
}

func loadCorpus(tb testing.TB) []corpusCase {
	tb.Helper()
	b, err := os.ReadFile("testdata/notes-corpus.json")
	if err != nil {
		tb.Fatal(err)
	}
	var cases []corpusCase
	if err := json.Unmarshal(b, &cases); err != nil {
		tb.Fatal(err)
	}
	if len(cases) < 200 {
		tb.Fatalf("corpus has %d cases; need at least 200", len(cases))
	}
	seen := map[string]bool{}
	groups, sources := map[string]string{}, map[string]string{}
	for _, tc := range cases {
		if tc.ID == "" || seen[tc.ID] {
			tb.Fatalf("missing or duplicate corpus ID %q", tc.ID)
		}
		seen[tc.ID] = true
		if tc.Split != "development" && tc.Split != "held-out" {
			tb.Fatalf("%s: invalid split %q", tc.ID, tc.Split)
		}
		if tc.SourceURL == "" || tc.SourceSHA256 == "" {
			tb.Fatalf("%s: missing source provenance", tc.ID)
		}
		if tc.Group == "" {
			tb.Fatalf("%s: missing recording group", tc.ID)
		}
		if split := groups[tc.Group]; split != "" && split != tc.Split {
			tb.Fatalf("%s: recording group spans splits", tc.ID)
		}
		if split := sources[tc.SourceURL]; split != "" && split != tc.Split {
			tb.Fatalf("%s: source URL spans splits", tc.ID)
		}
		groups[tc.Group], sources[tc.SourceURL] = tc.Split, tc.Split
		if len(tc.Mappings) != len(tc.Files) {
			tb.Fatalf("%s: need one independent mapping per audio file", tc.ID)
		}
		for _, path := range tc.Files {
			if _, ok := tc.Mappings[path]; !ok {
				tb.Fatalf("%s: missing mapping for %s", tc.ID, path)
			}
		}
	}
	return cases
}

func corpusNote(tb testing.TB, text, encoding string) []byte {
	tb.Helper()
	if encoding == "windows-1252" {
		b, err := charmap.Windows1252.NewEncoder().Bytes([]byte(text))
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}
	return []byte(text)
}

func TestNotesCorpus(t *testing.T) {
	cases := loadCorpus(t)
	metrics := map[string]*corpusMetrics{}
	for _, tc := range cases {
		if split := os.Getenv("TRACK_TAGGER_CORPUS_SPLIT"); split != "" && tc.Split != split {
			continue
		}
		m := metrics[tc.Split]
		if m == nil {
			m = &corpusMetrics{}
			metrics[tc.Split] = m
		}
		m.Cases++
		unsafeBefore := m.FalseReady + m.IncorrectAcceptedMappings
		dir := t.TempDir()
		for _, name := range tc.Files {
			p := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("synthetic disposable audio"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		titles := map[string]string{}
		dates, artists := map[string]bool{}, map[string]bool{}
		probe := Recording{}
		for _, note := range tc.Notes {
			if err := os.WriteFile(filepath.Join(dir, note.Name), corpusNote(t, note.Text, note.Encoding), 0600); err != nil {
				t.Fatal(err)
			}
			parseNotes(&probe, note.Text, titles, dates, artists)
		}
		r := only(t, dir)
		exact := true
		unresolved := map[string]bool{}
		for key, want := range tc.Titles {
			if want == "" {
				unresolved[key] = true
			} else {
				m.ExpectedTitles++
				if titles[key] == want {
					m.CorrectTitles++
				}
			}
			if titles[key] != want {
				exact = false
			}
		}
		for key, got := range titles {
			if got != "" && got != tc.Titles[key] {
				exact = false
			}
		}
		for _, key := range tc.Unresolved {
			unresolved[key] = true
			if titles[key] != "" {
				exact = false
			}
		}
		for key := range unresolved {
			m.ExpectedUnresolvedTitles++
			if titles[key] == "" {
				m.CorrectUnresolvedTitles++
			}
		}
		m.ExpectedMappings += len(tc.Mappings)
		actual := map[string]bool{}
		for _, track := range r.Tracks {
			path := filepath.ToSlash(track.RelativePath)
			want, exists := tc.Mappings[path]
			got := corpusMapping{Disc: track.Disc, Number: track.Number, Title: track.Title}
			if exists && !actual[path] && got == want {
				m.CorrectMappings++
			} else {
				exact = false
				if r.Status == "ready" {
					m.IncorrectAcceptedMappings++
				}
			}
			actual[path] = true
		}
		for path := range tc.Mappings {
			if !actual[path] {
				exact = false
				if r.Status == "ready" {
					m.IncorrectAcceptedMappings++
				}
			}
		}
		if r.Status != "ready" {
			m.ReviewCases++
		}
		if tc.Review && r.Status == "ready" {
			m.FalseReady++
			exact = false
		}
		if !tc.Review && r.Status != "ready" {
			exact = false
		}
		if exact {
			m.ExactCases++
		} else {
			m.Failures = append(m.Failures, tc.ID)
			// Default acceptance checks safety; conservative unresolved cases remain
			// visible in metrics. Exact acceptance is available for explicit audits.
			unsafe := m.FalseReady+m.IncorrectAcceptedMappings > unsafeBefore
			if os.Getenv("TRACK_TAGGER_CORPUS_REPORT_ONLY") == "" && (unsafe || os.Getenv("TRACK_TAGGER_CORPUS_REQUIRE_EXACT") != "") {
				t.Errorf("%s: titles=%v want=%v unresolved=%v status=%s review=%v reasons=%v", tc.ID, titles, tc.Titles, tc.Unresolved, r.Status, tc.Review, r.ReviewReasons)
			}
		}
	}

	if len(metrics) == 0 {
		t.Fatal("selected corpus split is empty")
	}
	b, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(b))
	if path := os.Getenv("TRACK_TAGGER_CORPUS_REPORT"); path != "" {
		if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkNotesCorpus(b *testing.B) {
	cases := loadCorpus(b)
	for _, split := range []string{"development", "held-out"} {
		var notes []string
		for _, tc := range cases {
			if tc.Split == split {
				for _, n := range tc.Notes {
					notes = append(notes, n.Text)
				}
			}
		}
		b.Run(split, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(strings.Join(notes, ""))))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, note := range notes {
					parseNotes(&Recording{}, note, map[string]string{}, map[string]bool{}, map[string]bool{})
				}
			}
		})
	}
}
