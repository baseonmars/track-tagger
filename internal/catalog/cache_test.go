package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowseCacheDetectsReplacementAndPrunesRemovedPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t01.mp3")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache BrowseCache
	first, err := ScanCached(dir, &cache)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement")
	if err = os.WriteFile(replacement, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(replacement, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	// Windows may not permit rename over the destination, so remove explicitly.
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	next, err := ScanCached(dir, &cache)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Fingerprint == next[0].Fingerprint {
		t.Fatal("same-stat replacement reused old evidence")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	next, err = ScanCached(dir, &cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 0 || len(cache.entries) != 0 {
		t.Fatal("removed files retained")
	}
}
