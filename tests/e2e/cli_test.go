// Package e2e exercises the shipped executable through its public CLI.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	flac "github.com/go-flac/go-flac/v2"
	"track-tagger/internal/app"
	"track-tagger/internal/media"
)

var executable string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "track-tagger-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	executable = filepath.Join(dir, "track-tagger")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", executable, "../../cmd/track-tagger")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI: %v\n%s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type fixture struct{ source, state, dest string }

func setup(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{filepath.Join(root, "source"), filepath.Join(root, "state"), filepath.Join(root, "copies")}
	show := filepath.Join(f.source, "show")
	if err := os.MkdirAll(show, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		file, err := os.Create(filepath.Join(show, fmt.Sprintf("t%02d.flac", i)))
		if err != nil {
			t.Fatal(err)
		}
		// A structural FLAC fixture is enough to exercise lossless metadata editing;
		// frames are opaque and must be preserved exactly, not decoded by the app.
		ff := flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}, {Type: flac.Application, Data: []byte("TESTpreserved")}}, Frames: bytes.NewReader([]byte{255, 248, byte(i), 1, 2, 3, 4})}
		if _, err = ff.WriteTo(file); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(show, "notes.txt"), []byte("Artist: Test Artist\nDate: 1970-01-20\nVenue: Test Hall\nCity: London\n01. Opening [intro]\n02. Finale > Encore\n"))
	write(t, filepath.Join(show, "checksums.md5"), []byte("original checksums retained verbatim\n"))
	return f
}
func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}
func run(t *testing.T, wantSuccess bool, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(executable, args...)
	out, err := cmd.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("CLI %q: %v\n%s", args, err, out)
	}
	if !wantSuccess && err == nil {
		t.Fatalf("CLI unexpectedly accepted %q\n%s", args, out)
	}
	return out
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[rel] = "directory"
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(h[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func unchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	after := snapshot(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("tree changed: before=%v after=%v", before, after)
	}
}
func missing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path must not exist: %s (%v)", path, err)
	}
}
func jobs(t *testing.T, state string) []app.Job {
	t.Helper()
	s, err := app.OpenStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j, err := s.History()
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestPreviewAndModeExclusivity(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	out := run(t, true, "scan", f.source, "--json", "--state", f.state, "--settle", "0s")
	var p app.Plan
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("JSON plan: %v\n%s", err, out)
	}
	if len(p.Recordings) != 1 || len(p.Recordings[0].Tracks) != 2 || p.Recordings[0].Status != "ready" {
		t.Fatalf("unexpected plan: %+v", p)
	}
	unchanged(t, f.source, before)
	missing(t, f.dest)
	for _, flags := range [][]string{{"--write", "--dry-run"}, {"--write", "--copy", f.dest}, {"--copy", f.dest, "--dry-run"}} {
		args := append([]string{"scan", f.source, "--state", f.state, "--settle", "0s"}, flags...)
		run(t, false, args...)
	}
	unchanged(t, f.source, before)
	missing(t, f.dest)
}

func TestCopySourceIdentityCollisionsAndIdempotence(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	args := []string{"scan", f.source, "--copy", f.dest, "--state", f.state, "--settle", "0s"}
	run(t, true, args...)
	unchanged(t, f.source, before)
	for _, name := range []string{"notes.txt", "checksums.md5"} {
		src, _ := os.ReadFile(filepath.Join(f.source, "show", name))
		dst, err := os.ReadFile(filepath.Join(f.dest, "show", name))
		if err != nil || !bytes.Equal(src, dst) {
			t.Fatalf("companion file changed: %s %v", name, err)
		}
	}
	for i := 1; i <= 2; i++ {
		rel := filepath.Join("show", fmt.Sprintf("t%02d.flac", i))
		src, err := media.Inspect(filepath.Join(f.source, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst, err := media.Inspect(filepath.Join(f.dest, rel))
		if err != nil {
			t.Fatal(err)
		}
		if src.AudioHash != dst.AudioHash || dst.Tags["ARTIST"] != "Test Artist" || dst.Tags["TITLE"] == "" {
			t.Fatalf("bad tagged copy: %+v", dst)
		}
		if !bytes.Contains(dst.Metadata, []byte("TESTpreserved")) {
			t.Fatal("unknown metadata lost")
		}
	}
	first := snapshot(t, f.dest)
	run(t, true, args...)
	unchanged(t, f.dest, first)
	unchanged(t, f.source, before)
	write(t, filepath.Join(f.dest, "show", "notes.txt"), []byte("external edit"))
	edited := snapshot(t, f.dest)
	run(t, false, args...)
	unchanged(t, f.dest, edited)
	unchanged(t, f.source, before)
	t.Run("unrelated output", func(t *testing.T) {
		g := setup(t)
		if err := os.MkdirAll(filepath.Join(g.dest, "show"), 0755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(g.dest, "show", "t01.flac"), []byte("unrelated"))
		orig := snapshot(t, g.dest)
		run(t, false, "scan", g.source, "--copy", g.dest, "--state", g.state, "--settle", "0s")
		unchanged(t, g.dest, orig)
	})
}

func TestWriteUndoRestoresOriginalMetadata(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	run(t, true, "scan", f.source, "--write", "--state", f.state, "--settle", "0s")
	if reflect.DeepEqual(before, snapshot(t, f.source)) {
		t.Fatal("write did not tag files")
	}
	j := jobs(t, f.state)
	if len(j) != 1 {
		t.Fatalf("jobs: %+v", j)
	}
	tagged := snapshot(t, f.source)
	run(t, true, "undo", j[0].ID, "--state", f.state)
	unchanged(t, f.source, tagged)
	run(t, true, "undo", j[0].ID, "--write", "--state", f.state)
	unchanged(t, f.source, before)
}

func TestUndoRefusesExternalChanges(t *testing.T) {
	f := setup(t)
	run(t, true, "scan", f.source, "--write", "--state", f.state, "--settle", "0s")
	j := jobs(t, f.state)
	if len(j) != 1 {
		t.Fatalf("jobs: %+v", j)
	}
	path := filepath.Join(f.source, "show", "t01.flac")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte("external edit"))
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	edited := snapshot(t, f.source)
	run(t, false, "undo", j[0].ID, "--write", "--state", f.state)
	unchanged(t, f.source, edited)
}

func TestCopyUndoRestoresMetadataAndKeepsCopies(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	run(t, true, "scan", f.source, "--copy", f.dest, "--state", f.state, "--settle", "0s")
	j := jobs(t, f.state)
	if len(j) != 1 {
		t.Fatalf("jobs: %+v", j)
	}
	run(t, true, "undo", j[0].ID, "--write", "--state", f.state)
	unchanged(t, f.source, before)
	unchanged(t, f.dest, before)
}

func TestOverlappingTreesAreRejectedWithoutChanges(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	for _, dest := range []string{f.source, filepath.Join(f.source, "nested"), filepath.Dir(f.source)} {
		run(t, false, "scan", f.source, "--copy", dest, "--state", f.state, "--settle", "0s")
		unchanged(t, f.source, before)
	}
}

func TestSavedPlanRequiresModeAndRejectsStaleInputs(t *testing.T) {
	f := setup(t)
	before := snapshot(t, f.source)
	plan := filepath.Join(t.TempDir(), "plan.json")
	run(t, true, "scan", f.source, "--plan", plan, "--state", f.state, "--settle", "0s")
	run(t, true, "apply", plan, "--state", f.state, "--settle", "0s")
	unchanged(t, f.source, before)
	write(t, filepath.Join(f.source, "show", "late.txt"), []byte("late arrival"))
	changed := snapshot(t, f.source)
	run(t, false, "apply", plan, "--write", "--state", f.state, "--settle", "0s")
	unchanged(t, f.source, changed)
}
