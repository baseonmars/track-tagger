package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string, n int, notes string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("t%02d.flac", i)), []byte("fixture audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(notes), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}
func notes(artist string, n int) string {
	s := "Artist: " + artist + "\nDate: 1970-01-20\nVenue: Test Hall\nCity: London\n"
	for i := 1; i <= n; i++ {
		s += fmt.Sprintf("%02d. Title %d [annotation]\n", i, i)
	}
	return s
}
func only(t *testing.T, dir string) Recording {
	t.Helper()
	rs, e := Scan(dir)
	if e != nil || len(rs) != 1 {
		t.Fatalf("scan = %v %v", rs, e)
	}
	return rs[0]
}
func hasReason(r Recording, s string) bool {
	for _, v := range r.ReviewReasons {
		if strings.Contains(v, s) {
			return true
		}
	}
	return false
}
func TestConcertFixtures(t *testing.T) {
	for _, tc := range []struct {
		name          string
		files, titles int
	}{{"Miles Davis", 9, 10}, {"Ringo Starr", 26, 25}} {
		t.Run(tc.name, func(t *testing.T) {
			r := only(t, fixture(t, tc.name, tc.files, notes(tc.name, tc.titles)))
			if r.Status != "needs-review" || !hasReason(r, "track count mismatch") {
				t.Fatalf("unsafe plan: %+v", r)
			}
			for _, tr := range r.Tracks {
				if tr.Number <= tc.titles && tr.Title != fmt.Sprintf("Title %d [annotation]", tr.Number) {
					t.Fatalf("shifted title: %+v", tr)
				}
			}
		})
	}
	t.Run("Ramblin Jack date conflict", func(t *testing.T) {
		r := only(t, fixture(t, "Ramblin Jack", 1, "Date: 1971-02-13\n"+notes("Ramblin Jack", 1)))
		if !hasReason(r, "conflicting concert dates") {
			t.Fatal(r)
		}
	})
	t.Run("Hendrix unnumbered partial setlist", func(t *testing.T) {
		r := only(t, fixture(t, "Hendrix", 3, "Artist: Jimi Hendrix\nDate: 1970-01-20\nVenue: Test Hall\nPurple Haze\nHey Joe\n"))
		if r.Status != "needs-review" {
			t.Fatal(r)
		}
		for _, tr := range r.Tracks {
			if tr.Title != "" {
				t.Fatal("invented mapping")
			}
		}
	})
}
func TestReadyEvidenceAndAnnotations(t *testing.T) {
	dir := fixture(t, "show", 2, notes("Band", 2))
	r := only(t, dir)
	if r.Status != "ready" || r.Tracks[0].Title != "Title 1 [annotation]" {
		t.Fatal(r)
	}
	if err := ValidateEvidence(r.Evidence); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(notes("Changed", 2)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEvidence(r.Evidence); err == nil {
		t.Fatal("accepted changed notes")
	}
	if r.Fingerprint == only(t, dir).Fingerprint {
		t.Fatal("fingerprint unchanged")
	}
}
func TestLateArtworkChangesFingerprint(t *testing.T) {
	dir := fixture(t, "show", 1, notes("Band", 1))
	before := only(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("picture"), 0644); err != nil {
		t.Fatal(err)
	}
	after := only(t, dir)
	if before.Fingerprint == after.Fingerprint || after.Artwork == "" {
		t.Fatal(after)
	}
}
func TestExplicitDiscsAndNoShift(t *testing.T) {
	dir := fixture(t, "show", 0, "Artist: Band\nDate: 1970-01-20\nVenue: Hall\nSet 1\n01. First\n03. Third\nSet 2\n01. Fourth\n")
	for _, p := range []string{"disc 1/t01.flac", "disc 1/t03.flac", "disc 2/t01.flac"} {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0755)
		os.WriteFile(full, []byte("audio"), 0644)
	}
	r := only(t, dir)
	if len(r.Tracks) != 3 || r.Tracks[1].Title != "Third" || r.Tracks[2].Disc != 2 || r.Tracks[2].Title != "Fourth" {
		t.Fatal(r)
	}
}
func TestCP1252AndTransferDate(t *testing.T) {
	dir := fixture(t, "show", 1, strings.Replace(notes("Andr\xe9", 1), "Date:", "Transferred: 2001-03-04\nDate:", 1))
	r := only(t, dir)
	if r.Artist != "André" || r.Date != "1970-01-20" || r.Status != "ready" {
		t.Fatal(r)
	}
}
func TestDuplicateAndMissingManifest(t *testing.T) {
	dir := fixture(t, "show", 1, notes("Band", 1)+"01. Duplicate\n")
	os.WriteFile(filepath.Join(dir, "checksums.ffp"), []byte("missing.flac:0123456789abcdef\n"), 0644)
	r := only(t, dir)
	if !hasReason(r, "duplicate numbering") || !hasReason(r, "manifest references missing") {
		t.Fatal(r)
	}
}
func TestIncompleteTransferWaiting(t *testing.T) {
	dir := fixture(t, "show", 1, notes("Band", 1))
	os.WriteFile(filepath.Join(dir, "t02.flac.part"), []byte("partial"), 0644)
	r := only(t, dir)
	if r.Status != "waiting" {
		t.Fatal(r)
	}
}
func FuzzNotes(f *testing.F) {
	f.Add("Artist: Band\n1970-01-20\n01. Song")
	f.Add("\xff\x00Set 2\n01. Song")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 65536 {
			return
		}
		r := Recording{}
		parseNotes(&r, s, map[string]string{}, map[string]bool{}, map[string]bool{})
	})
}

func TestOrphanStagesAreNotRecordingInputs(t *testing.T) {
	dir := fixture(t, "show", 1, notes("Band", 1))
	before := only(t, dir)
	for _, name := range []string{".track-tagger-orphan.flac", ".track-tagger-undo-old", ".track-tagger-incomplete.part", ".track-tagger-bad.txt", ".track-tagger-empty.jpg"} {
		content := []byte("malformed partial data")
		if strings.Contains(name, "empty") {
			content = nil
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	after := only(t, dir)
	if after.Fingerprint != before.Fingerprint || after.Status != "ready" || len(after.Tracks) != 1 || len(after.Evidence) != len(before.Evidence) {
		t.Fatalf("orphan stages affected recording: %+v", after)
	}
}

func TestReservedPrefixDoesNotHideDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".track-tagger-recording")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t01.flac"), []byte("audio"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(notes("Band", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	recordings, err := Scan(root)
	if err != nil || len(recordings) != 1 || recordings[0].Path != dir {
		t.Fatalf("directory was hidden: %v %v", recordings, err)
	}
}

func TestOrphanStageDoesNotDiscoverRecording(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".track-tagger-orphan.flac"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	recordings, err := Scan(root)
	if err != nil || len(recordings) != 0 {
		t.Fatalf("orphan discovered as recording: %v %v", recordings, err)
	}
}
