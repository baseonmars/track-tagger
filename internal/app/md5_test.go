package app

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
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
