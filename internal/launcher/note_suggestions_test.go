package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseNoteSuggestionsPreservesOtherLauncherArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--resume-server", "--note-suggestions", "local path/package"},
		{"--note-suggestions=local path/package", "--resume-server"},
	} {
		path, rest, err := ParseNoteSuggestions(args)
		if err != nil || path != "local path/package" || !reflect.DeepEqual(rest, []string{"--resume-server"}) {
			t.Fatalf("got %q, %v, %v", path, rest, err)
		}
	}
	for _, args := range [][]string{{"--note-suggestions"}, {"--note-suggestions="}, {"--note-suggestions=a", "--note-suggestions=b"}} {
		if _, _, err := ParseNoteSuggestions(args); err == nil {
			t.Errorf("accepted invalid arguments: %v", args)
		}
	}
}

func TestFindNoteSuggestionsPath(t *testing.T) {
	settings := t.TempDir()
	launcher := filepath.Join(t.TempDir(), "launcher")
	configured := filepath.Join(settings, "note-suggestions", "bert-agreement.bin.gz")
	beside := filepath.Join(filepath.Dir(launcher), "note-suggestions", "bert-agreement.bin.gz")
	if got := findNoteSuggestionsPath(settings, launcher); got != configured {
		t.Fatalf("missing pack path = %q, want %q", got, configured)
	}
	for _, pack := range []string{beside, configured} {
		if err := os.MkdirAll(filepath.Dir(pack), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pack, []byte("pack placeholder"), 0600); err != nil {
			t.Fatal(err)
		}
		if got := findNoteSuggestionsPath(settings, launcher); got != pack {
			t.Fatalf("selected %q, want %q", got, pack)
		}
	}
	if got := resolveNoteSuggestionsPath("", false); got != "" {
		t.Fatalf("default-off launcher resolved %q", got)
	}
	if got := resolveNoteSuggestionsPath("custom.pack", false); got != "custom.pack" {
		t.Fatalf("explicit path overridden: %q", got)
	}
}

func TestNoteSuggestionsPreferenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil || s.NoteSuggestions {
		t.Fatalf("unexpected default: %+v, %v", s, err)
	}
	s.NoteSuggestions = true
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil || !got.NoteSuggestions {
		t.Fatalf("preference not restored: %+v, %v", got, err)
	}
}

func TestFindNoteSuggestionsPathInAppBundle(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "Track Tagger.app", "Contents")
	pack := filepath.Join(bundle, "Resources", "note-suggestions", "bert-agreement.bin.gz")
	if err := os.MkdirAll(filepath.Dir(pack), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pack, []byte("pack placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := findNoteSuggestionsPath(t.TempDir(), filepath.Join(bundle, "MacOS", "track-tagger-gui")); got != pack {
		t.Fatalf("got %q, want %q", got, pack)
	}
}
