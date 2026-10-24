package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopedDiscoveryAndValidation(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "show")
	other := filepath.Join(root, "other")
	for _, dir := range []string{filepath.Join(show, "Disc 1"), filepath.Join(show, "Disc 2"), other} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string]string{
		filepath.Join(show, "Disc 1", "t01.mp3"): "first audio",
		filepath.Join(show, "Disc 2", "t02.mp3"): "second audio",
		filepath.Join(show, "notes.txt"):         "Artist: Band\nDate: 1970-01-20\nVenue: Hall\nd1t01 First\nd2t02 Second\n",
		filepath.Join(other, "t01.mp3"):          "unrelated audio",
		filepath.Join(other, "notes.txt"):        "Artist: Other\nDate: 1970-01-21\nVenue: Hall\n01. Other\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	discovered, err := Discover(root)
	if err != nil || len(discovered) != 2 {
		t.Fatalf("discovery = %+v, %v", discovered, err)
	}
	var selected Discovery
	for _, d := range discovered {
		if d.Path == show {
			selected = d
		}
	}
	if selected.Path != show || selected.RelativePath != "show" || len(selected.Files) != 2 {
		t.Fatalf("disc files were not grouped under recording: %+v", selected)
	}
	var fingerprints []string
	stub := func(path string) (Evidence, error) {
		fingerprints = append(fingerprints, path)
		st, err := os.Stat(path)
		if err != nil {
			return Evidence{}, err
		}
		return Evidence{Path: path, SHA256: "synthetic", Size: st.Size(), ModTime: st.ModTime().UnixNano()}, nil
	}
	r, err := Validate(root, selected, stub)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != selected.ID || r.RelativePath != "show" || len(r.Tracks) != 2 || len(fingerprints) != 3 {
		t.Fatalf("scoped validation = %+v, fingerprinted %v", r, fingerprints)
	}
	for _, path := range fingerprints {
		if path == filepath.Join(other, "t01.mp3") || path == filepath.Join(other, "notes.txt") {
			t.Fatalf("fingerprinted unrelated recording: %s", path)
		}
	}
	if r.Tracks[0].Disc != 1 || r.Tracks[1].Disc != 2 || r.Tracks[0].RelativePath != filepath.Join("show", "Disc 1", "t01.mp3") {
		t.Fatalf("disc or root relative paths lost: %+v", r.Tracks)
	}
}

func TestScanRecordingSeesInputChangesAndSkipsNestedRecording(t *testing.T) {
	root := t.TempDir()
	show := filepath.Join(root, "show")
	nested := filepath.Join(show, "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(show, "t01.mp3"):   "first audio",
		filepath.Join(show, "notes.txt"): "Artist: Band\nDate: 1970-01-20\nVenue: Hall\n01. First\n02. Second\n",
		filepath.Join(nested, "t01.mp3"): "nested audio",
	} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ScanRecording(root, show)
	if err != nil {
		t.Fatal(err)
	}
	if first.RelativePath != "show" || len(first.Tracks) != 1 || len(first.Evidence) != 2 {
		t.Fatalf("nested recording included: %+v", first)
	}
	secondPath := filepath.Join(show, "t02.mp3")
	if err := os.WriteFile(secondPath, []byte("second audio"), 0644); err != nil {
		t.Fatal(err)
	}
	added, err := ScanRecording(root, show)
	if err != nil || len(added.Tracks) != 2 || added.Fingerprint == first.Fingerprint {
		t.Fatalf("added input missed: %+v, %v", added, err)
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	removed, err := ScanRecording(root, show)
	if err != nil || len(removed.Tracks) != 1 || removed.Fingerprint != first.Fingerprint {
		t.Fatalf("removed input missed: %+v, %v", removed, err)
	}
	if _, err := ScanRecording(root, filepath.Join(root, "outside")); err == nil {
		t.Fatal("accepted missing recording")
	}
}

func TestScanRecordingUsesDiscoveryOwnershipForNestedDiscDirs(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "Disc 1")
	inner := filepath.Join(outer, "Disc 2")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		filepath.Join(outer, "t01.mp3"): "outer audio",
		filepath.Join(inner, "t02.mp3"): "inner audio",
	} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	discovered, err := Discover(root)
	if err != nil || len(discovered) != 2 || discovered[0].Path != root || discovered[1].Path != outer || len(discovered[0].Files) != 1 || len(discovered[1].Files) != 1 {
		t.Fatalf("nested disc discovery = %+v, %v", discovered, err)
	}
	for _, d := range discovered {
		r, err := ScanRecording(root, d.Path)
		if err != nil || r.ID != d.ID || len(r.Tracks) != len(d.Files) {
			t.Fatalf("nested disc scan disagrees with discovery: %+v, %v", r, err)
		}
	}
	standalone, err := ScanRecording(outer, outer)
	if err != nil || standalone.RelativePath != "." || len(standalone.Tracks) != 2 {
		t.Fatalf("explicit disc source = %+v, %v", standalone, err)
	}
}
