package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectOverlapOnCaseInsensitiveVolume(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "Music")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "music")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("test volume distinguishes directory name case")
	} else if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{alias, filepath.Join(alias, "copies")} {
		if err := rejectOverlap(source, destination); err == nil {
			t.Errorf("accepted overlapping destination %s", destination)
		}
		if err := rejectOverlap(destination, source); err == nil {
			t.Errorf("accepted overlapping source %s", destination)
		}
	}
}
