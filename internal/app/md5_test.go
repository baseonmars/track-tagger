package app

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssessMD5ChecksExactTagChangeAndOriginalChecksum(t *testing.T) {
	e, audio := engineFixture(t, Write)
	original, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", md5.Sum(original))
	for name, contents := range map[string]string{
		"all.md5":           "; recording\r\n" + sum + " *t01.flac\r\n",
		"bsd.md5":           "MD5 (t01.flac) = " + sum,
		"mixed.md5":         sum + "  t01.flac\n" + sum + "  notes.txt",
		"already-stale.md5": "12345678901234567890123456789012 *t01.flac",
		"unknown.md5":       sum + "  t01.flac\nnot a checksum",
		"audio.ffp":         "t01.flac:" + sum,
	} {
		if err = os.WriteFile(filepath.Join(e.Source, name), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := fixturePlan(t, e)
	verify := func(item Item, wantAffected bool) {
		t.Helper()
		decisions, err := assessMD5(item)
		if err != nil {
			t.Fatal(err)
		}
		if len(decisions) != 5 {
			t.Fatalf("expected five whole-file manifests, got %+v", decisions)
		}
		for _, d := range decisions {
			name := filepath.Base(d.Path)
			want := wantAffected && (name == "all.md5" || name == "bsd.md5")
			if d.Remove != want || d.Reason == "" {
				t.Fatalf("%s: %+v", name, d)
			}
		}
	}
	verify(p.Recordings[0], true)
	alreadyTagged(t, audio, p.Recordings[0].Tracks[0].Tags, "")
	current, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(e.Source, "all.md5"), []byte(fmt.Sprintf("%x *t01.flac", md5.Sum(current))), 0644); err != nil {
		t.Fatal(err)
	}
	verify(fixturePlan(t, e).Recordings[0], false)
}

func TestMD5CleanupApplyAndUndo(t *testing.T) {
	for _, mode := range []Mode{Write, Copy} {
		t.Run(string(mode), func(t *testing.T) {
			e, audio := engineFixture(t, mode)
			source, err := os.ReadFile(audio)
			if err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(e.Source, "show.md5")
			checksum := []byte(fmt.Sprintf("%x *t01.flac\r\n", md5.Sum(source)))
			if err = os.WriteFile(manifest, checksum, 0640); err != nil {
				t.Fatal(err)
			}
			notes := filepath.Join(e.Source, "notes.txt")
			prior, err := os.ReadFile(notes)
			if err != nil {
				t.Fatal(err)
			}
			ffp := filepath.Join(e.Source, "show.ffp")
			fingerprint := []byte("t01.flac:00000000000000000000000000000000")
			if err = os.WriteFile(ffp, fingerprint, 0644); err != nil {
				t.Fatal(err)
			}
			p := fixturePlan(t, e)
			item := &p.Recordings[0]
			item.CleanupMD5 = true
			if err = e.SaveReview(item.ID, *item); err != nil {
				t.Fatal(err)
			}
			// Saving a review must leave every source checksum valid.
			assertBytes := func(path string, want []byte) {
				t.Helper()
				got, x := os.ReadFile(path)
				if x != nil || !bytes.Equal(got, want) {
					t.Fatalf("%s: unexpected contents or error %v", path, x)
				}
			}
			assertBytes(manifest, checksum)
			assertBytes(notes, prior)
			assertBytes(audio, source)
			jobs, err := e.Apply(fixturePlan(t, e))
			if err != nil || len(jobs) != 1 {
				t.Fatalf("apply: %v, %v", jobs, err)
			}
			output := func(src string) string {
				t.Helper()
				d, x := e.destination(src)
				if x != nil {
					t.Fatal(x)
				}
				return d
			}
			if _, err = os.Lstat(output(manifest)); !os.IsNotExist(err) {
				t.Fatalf("removed output: %v", err)
			}
			assertBytes(output(ffp), fingerprint)
			want := append(bytes.Clone(prior), []byte("Removed show.md5 after tagging changed the file checksums.\n")...)
			assertBytes(output(notes), want)
			history, err := e.store.History()
			if err != nil || !strings.Contains(history[0].Summary, "Removed show.md5") {
				t.Fatalf("history: %+v, %v", history, err)
			}
			if mode == Copy {
				assertBytes(manifest, checksum)
				assertBytes(audio, source)
				assertBytes(notes, prior)
			}
			scan, err := e.Scan()
			if err != nil || scan.Recordings[0].Status != "completed" {
				t.Fatalf("post-apply status %s: %v", scan.Recordings[0].Status, err)
			}
			if err = os.WriteFile(output(manifest), []byte("external replacement"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err = e.Undo(jobs[0], true); err == nil {
				t.Fatal("recreated manifest must block Undo")
			}
			assertBytes(output(notes), want)
			if err = os.Remove(output(manifest)); err != nil {
				t.Fatal(err)
			}
			if _, err = e.Undo(jobs[0], true); err != nil {
				t.Fatal(err)
			}
			assertBytes(output(notes), prior)
			assertBytes(output(manifest), checksum)
			assertBytes(output(audio), source)
		})
	}
}

func TestInterruptedMD5RemovalRestoresJournaledManifest(t *testing.T) {
	e, _ := engineFixture(t, Write)
	path := filepath.Join(e.Source, "checksums.md5")
	data := []byte("; original manifest bytes\r\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenStorage(); err != nil {
		t.Fatal(err)
	}
	job, err := e.store.newJob("recording", "Approved MD5 cleanup for checksums.md5.")
	if err != nil {
		t.Fatal(err)
	}
	hash := hashBytes(data)
	_, err = e.store.journal(FileJournal{JobID: job, Source: path, Target: path, BeforeHash: hash, SourceHash: hash, Metadata: data, Mode: 0600, Companion: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := NewHistory(e.State, Write)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err = restored.Undo(job, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("interrupted manifest restoration: %q %v", got, err)
	}
}

func TestCleanupRequiresSafeNotesBeforeTagging(t *testing.T) {
	e, audio := engineFixture(t, Write)
	prior, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(e.Source, "show.md5")
	sum := []byte(fmt.Sprintf("%x *t01.flac", md5.Sum(prior)))
	if err = os.WriteFile(manifest, sum, 0644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(e.Source, "credits.txt")
	if err = os.WriteFile(extra, []byte("Credits: synthetic tape\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := e.Scan()
	if err != nil {
		t.Fatal(err)
	}
	item := &plan.Recordings[0]
	item.Reviewed, item.CleanupMD5, item.Status = true, true, "ready"
	if _, err = e.Apply(plan); err == nil || !strings.Contains(err.Error(), "Choose which show-notes file") {
		t.Fatalf("expected notes target error, got %v", err)
	}
	for path, original := range map[string][]byte{audio: prior, manifest: sum} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, original) {
			t.Fatal(path, err)
		}
	}
	item.NotesTarget = extra
	if _, err = e.Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(extra)
	if err != nil || !strings.Contains(string(got), "Removed show.md5") {
		t.Fatal(string(got), err)
	}
}
