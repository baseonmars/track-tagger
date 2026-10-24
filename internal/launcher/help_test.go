package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenHelpRefreshesStandaloneManual(t *testing.T) {
	root := t.TempDir()
	cache := func() (string, error) { return root, nil }
	var opened []string
	opener := func(path string) error {
		opened = append(opened, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(strings.ToLower(string(b)), "<html") || !strings.Contains(string(b), "</html>") {
			t.Fatal("opened help is not a complete HTML document")
		}
		return nil
	}
	if err := openHelp(cache, opener); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "track-tagger", "help", "manual.html")
	if len(opened) != 1 || opened[0] != path {
		t.Fatalf("opened %v, want %q", opened, path)
	}
	if err := os.WriteFile(path, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := openHelp(cache, opener); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 2 || opened[1] != path {
		t.Fatalf("opened %v", opened)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "manual.html" {
		t.Fatalf("cache entries: %v, %v", entries, err)
	}
}

func TestOpenHelpReportsCacheAndOpenerFailures(t *testing.T) {
	root := t.TempDir()
	if err := openHelp(func() (string, error) { return "", errors.New("cache unavailable") }, func(string) error { t.Fatal("opened"); return nil }); err == nil || !strings.Contains(err.Error(), "cache unavailable") {
		t.Fatalf("cache error: %v", err)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := openHelp(func() (string, error) { return blocked, nil }, func(string) error { t.Fatal("opened"); return nil }); err == nil || !strings.Contains(err.Error(), "prepare help cache") {
		t.Fatalf("directory error: %v", err)
	}
	if err := openHelp(func() (string, error) { return root, nil }, func(string) error { return errors.New("shell unavailable") }); err == nil || !strings.Contains(err.Error(), "shell unavailable") {
		t.Fatalf("open error: %v", err)
	}
}
