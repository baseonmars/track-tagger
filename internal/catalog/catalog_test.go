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

func TestPlaybackFormatsDiscovered(t *testing.T) {
	for _, ext := range []string{"flac", "WAV", "mp3", "ogg", "oga", "opus"} {
		t.Run(ext, func(t *testing.T) {
			dir := fixture(t, "show", 0, notes("Band", 1))
			path := filepath.Join(dir, "t01."+ext)
			if err := os.WriteFile(path, []byte("fixture audio"), 0644); err != nil {
				t.Fatal(err)
			}
			r := only(t, dir)
			if len(r.Tracks) != 1 || r.Tracks[0].Title != "Title 1 [annotation]" || r.Tracks[0].Path != path {
				t.Fatalf("lost playback track or mapping: %+v", r)
			}
			if r.PlaybackOnly != (ext != "flac") || len(r.Evidence) != 2 || r.Fingerprint == "" {
				t.Fatalf("wrong playback capability or missing evidence: %+v", r)
			}
			if err := ValidateEvidence(r.Evidence); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMixedPlaybackFormatsKeepExplicitDiscGrouping(t *testing.T) {
	dir := fixture(t, "show", 0, "Artist: Band\nDate: 1970-01-20\nVenue: Hall\nSet 1\n01. First\nSet 2\n01. Second\n")
	for _, p := range []string{"disc 1/t01.FLAC", "disc 2/t01.mp3"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("fixture audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := only(t, dir)
	if !r.PlaybackOnly || len(r.Tracks) != 2 || r.Tracks[0].Disc != 1 || r.Tracks[1].Disc != 2 || r.Tracks[1].Title != "Second" {
		t.Fatalf("mixed recording was split or made writable: %+v", r)
	}
}

func TestAudioExtensions(t *testing.T) {
	for _, path := range []string{"cover.jpg", "notes.txt", "song.flac.part", "song.mp4", "song"} {
		if IsAudio(path) || CanTag(path) {
			t.Errorf("accepted unsupported audio %q", path)
		}
	}
	if !CanTag("song.FLAC") || CanTag("song.wav") {
		t.Fatal("incorrect writable format classification")
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

func TestDateSuffixedTrackNumbers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disc, track int
	}{
		{"SnarkyPuppy2023-04-22t01.flac", 1, 1},
		{"SnarkyPuppy2023-04-22T13.FLAC", 1, 13},
		{"disc 2/SnarkyPuppy2023-04-22t03.flac", 2, 3},
		{"Band2023-02-30t01.flac", 1, 0},
		{"Band2023-04-22t01extra.flac", 1, 0},
		{"part01.flac", 1, 0},
		{"concert01.flac", 1, 0},
		{"s1t02.flac", 1, 2},
		{"d2t03.flac", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, n := fileNumber(filepath.FromSlash(tc.name))
			if d != tc.disc || n != tc.track {
				t.Fatalf("got %d:%d, want %d:%d", d, n, tc.disc, tc.track)
			}
		})
	}
}

func TestSnarkyPuppyNotesAndMappings(t *testing.T) {
	titles := []string{"Keep It on Your Mind", "Coney Bear", "Cliroy", "Bet", "East Bay", "Trinity", "Honiara", "Pineapple", "Belmont", "RL's", "Shofukan", "What About Me?", "two songs from Nate Wood's opening set"}
	s := "Snarky Puppy\nSaturday, April 22, 2023\nNewport, KY @ MegaCorp Pavilion (inside)\n\nShure MV88 > iPhone 7 > Motiv Audio App > WAV (24/48) > CD Wave Editor (tracking) > FLAC (24/48)\nRecorded by Funk It Blog\n\n"
	for i, title := range titles {
		if i == 11 {
			s += "Encore:\n"
		}
		if i == 12 {
			s += "BONUS:\n"
		}
		s += fmt.Sprintf("%02d. %s\n", i+1, title)
	}
	s += "\nMichael League - bass\nJustin Stanton - keyboards & trumpet\n"
	dir := fixture(t, "SnarkyPuppy2023-04-22.ShureMV88.flac24", 0, s)
	for i := range titles {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("SnarkyPuppy2023-04-22t%02d.flac", i+1)), []byte("audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := only(t, dir)
	if r.Artist != "Snarky Puppy" || r.Date != "2023-04-22" || r.Album != "2023-04-22 - MegaCorp Pavilion (inside), Newport, KY" {
		t.Fatalf("header: %+v", r)
	}
	if r.Status != "needs-review" || len(r.ReviewReasons) != 1 || !hasReason(r, "possible other performer in track 1:13") {
		t.Fatalf("reasons: %v", r.ReviewReasons)
	}
	for i, tr := range r.Tracks {
		if tr.Number != i+1 || tr.Title != titles[i] {
			t.Fatalf("mapping: %+v", tr)
		}
	}
	r.Tracks[12].Artist = "Nate Wood"
	RefreshPlan(&r)
	if r.Tracks[12].Tags["ARTIST"] != "Nate Wood" || r.Tracks[12].Tags["ALBUMARTIST"] != "Snarky Puppy" || r.Tracks[0].Tags["ARTIST"] != "Snarky Puppy" {
		t.Fatal("per-track artist override lost")
	}
}

func TestUnlabelledHeaderRequiresStructureAndPreservesConflicts(t *testing.T) {
	for _, prefix := range []string{
		"Random Heading\nDate: 2023-04-22\nNewport, KY @ Hall\n",
		"Source: Transfer\nApril 22, 2023\nNewport, KY @ Hall\n",
		"Random Heading\n04/05/2023\nNewport, KY @ Hall\n",
		"Random Heading\nApril 22, 2023\nSome location\n",
	} {
		r := only(t, fixture(t, "show", 1, prefix+"01. Song\n"))
		if r.Artist != "" || !hasReason(r, "artist is missing") {
			t.Fatalf("guessed artist: %+v", r)
		}
	}
	r := only(t, fixture(t, "conflicts", 1, "Band\nApril 22, 2023\nCity @ Hall\nArtist: Other Band\nDate: 2023-04-23\nVenue: Other Hall\nCity: Other City\n01. Song\n"))
	for _, reason := range []string{"conflicting artist", "conflicting concert dates", "conflicting venue", "conflicting city"} {
		if !hasReason(r, reason) {
			t.Fatalf("missing %q: %v", reason, r.ReviewReasons)
		}
	}
}

func TestMilesStyleWhitespaceNotesKeepDuplicateUnresolved(t *testing.T) {
	// Minimal shape of the reported taper notes, including the duplicated 02.
	s := "Example Jazz Ensemble\r\nOutdoor Hall,\r\nVienna, VA\r\n09/05/1989\r\n\r\nLate show\r\nSource: microphone > DAT\r\nTransfer: DAT > wav\r\n\r\n01   crowd\r\n02   First Tune\r\n02   Second Tune (annotation)\r\n03   Third Tune\r\n04   Fourth / Fifth\r\n05   Sixth\r\n06   Seventh\r\n07   Eighth\r\n08   Ninth\r\n09   Last Tune//\r\n[MISSING] Another Tune\r\n\r\nMusician - trumpet\r\nshntool output:\r\n  0:38.053 7306284 B cxx -- ---xx flac 0.4895 band1989-09-05t01.flac\r\n  10:32.027 121349164 B cxx -- ---xx flac 0.4905 band1989-09-05t03.flac\r\n  84:40.853 975524236 B 0.5375 (9 files)\r\n"
	dir := fixture(t, "show", 9, s)
	if err := os.WriteFile(filepath.Join(dir, "shntool_len.txt"), []byte("length expanded size cdr WAVE problems fmt ratio filename\n  0:38.053 7306284 B cxx -- ---xx flac 0.4895 t01.flac\n  10:32.027 121349164 B cxx -- ---xx flac 0.4905 t03.flac\n  13:55.653 160445484 B cxx -- ---xx flac 0.5395 t07.flac\n  84:40.853 975524236 B 0.5375 (9 files)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r := only(t, dir)
	if r.Artist != "Example Jazz Ensemble" || r.Venue != "Outdoor Hall" || r.City != "Vienna, VA" {
		t.Fatalf("header lost: %+v", r)
	}
	if r.Date != "" || r.Status != "needs-review" {
		t.Fatalf("ambiguous date silently accepted: %+v", r)
	}
	for _, want := range []string{"ambiguous numeric concert date", "duplicate numbering in notes: 1:2", "track count mismatch: 10 listed titles, 9 audio files"} {
		if !hasReason(r, want) {
			t.Fatalf("missing %q: %v", want, r.ReviewReasons)
		}
	}
	if r.Tracks[0].Title != "crowd" || r.Tracks[1].Title != "" || r.Tracks[2].Title != "Third Tune" || r.Tracks[8].Title != "Last Tune//" {
		t.Fatalf("shifted or invented mappings: %+v", r.Tracks)
	}
}

func TestWhitespaceTrackListsAndTechnicalReports(t *testing.T) {
	r := only(t, fixture(t, "show", 2, "Artist: Band\nDate: 1989-09-05\nVenue: Hall\n01 Song one\n02\tSong two [annotation]\nshntool output:\n  1:23.456 12345 B flac 0.5 t01.flac\n"))
	if r.Status != "ready" || r.Tracks[0].Title != "Song one" || r.Tracks[1].Title != "Song two [annotation]" {
		t.Fatalf("whitespace list or report parsed incorrectly: %+v", r)
	}
}

func TestDuplicateTitlesNeverChooseLastCandidate(t *testing.T) {
	r := only(t, fixture(t, "show", 1, notes("Band", 1)+"01. Other title\n01. Third title\n"))
	if r.Tracks[0].Title != "" || !hasReason(r, "duplicate numbering") {
		t.Fatalf("duplicate title chosen: %+v", r)
	}
}

func TestDayFirstDateIsNotWhitespaceTrack(t *testing.T) {
	r := only(t, fixture(t, "show", 1, "Artist: Band\n5 September 1989\nVenue: Hall\n01 Song\n"))
	if r.Date != "1989-09-05" || r.Status != "ready" {
		t.Fatalf("date parsed as title: %+v", r)
	}
}

func TestDayFirstDateWithWeekdayIsNotWhitespaceTrack(t *testing.T) {
	for _, date := range []string{"7 June 1992 (Sunday)", "7 June 1992, Sunday", "7 June 1992 Sunday"} {
		t.Run(date, func(t *testing.T) {
			r := only(t, fixture(t, "show", 1, "Artist: Band\n"+date+"\nVenue: Hall\n01 Song\n"))
			if r.Date != "1992-06-07" || r.Status != "ready" || r.Tracks[0].Title != "Song" {
				t.Fatalf("annotated date parsed as title: %+v", r)
			}
		})
	}
	// A date embedded in a numbered title is still a song, including when it
	// is the first numbered line rather than following a set heading.
	r := only(t, fixture(t, "show", 1, "Artist: Band\nDate: 1992-06-07\nVenue: Hall\n01 June 1992 Memories\n"))
	if r.Status != "ready" || r.Tracks[0].Title != "June 1992 Memories" {
		t.Fatalf("date-like song title lost: %+v", r)
	}
}

func TestRingoStyleRomanSetsAndTwoTextFiles(t *testing.T) {
	s := "Example All Star Band\nOutdoor Amphitheater,\nAtlanta, GA\n06/07/1992\nSource: microphone > DAT\n\nSet I\n"
	for i := 1; i <= 10; i++ {
		s += fmt.Sprintf("%02d   First set title %d\n", i, i)
	}
	s += "\nSet II\n"
	for i := 1; i <= 15; i++ {
		if i == 14 {
			s += "encore\n"
		}
		s += fmt.Sprintf("%02d   Second set title %d\n", i, i)
	}
	report := "length expanded size cdr WAVE problems fmt ratio filename\n 0:00.013 2604 B cxx -- ---xx flac 0.5111 example1992-06-07s1t03.flac\n 111:21.653 1282878584 B 0.5387 (26 files)\n"
	dir := fixture(t, "show", 0, s+"\nshntool output:\n"+report)
	if err := os.WriteFile(filepath.Join(dir, "shntool_len.txt"), []byte(report), 0644); err != nil {
		t.Fatal(err)
	}
	for disc, count := range map[int]int{1: 11, 2: 15} {
		for n := 1; n <= count; n++ {
			p := filepath.Join(dir, fmt.Sprintf("example1992-06-07s%dt%02d.flac", disc, n))
			if err := os.WriteFile(p, []byte("fixture audio"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	r := only(t, dir)
	if r.Artist != "Example All Star Band" || r.Venue != "Outdoor Amphitheater" || r.City != "Atlanta, GA" || r.Date != "" || r.Status != "needs-review" {
		t.Fatalf("header or review state lost: %+v", r)
	}
	if !hasReason(r, "track count mismatch: 25 listed titles, 26 audio files") || hasReason(r, "duplicate numbering") || hasReason(r, "listed track has no audio file") {
		t.Fatalf("set list confused by reports: %v", r.ReviewReasons)
	}
	for _, tr := range r.Tracks {
		want := fmt.Sprintf("First set title %d", tr.Number)
		if tr.Disc == 2 {
			want = fmt.Sprintf("Second set title %d", tr.Number)
		} else if tr.Number == 11 {
			want = ""
		}
		if tr.Title != want || tr.Disc < 1 || tr.Disc > 2 || tr.Number == 0 {
			t.Fatalf("title shifted across unmatched file: %+v want %q", tr, want)
		}
	}
}

func TestDateSuffixedDiscTrackNumbers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disc, track int
	}{
		{"Band1992-06-07s1t01.flac", 1, 1},
		{"Band1992-06-07S2T15.FLAC", 2, 15},
		{"Band1992-06-07d2t03.flac", 2, 3},
		{"Band1992-02-30s1t01.flac", 1, 0},
		{"Band1992-06-07s1t01extra.flac", 1, 0},
		{"Bands1t01.flac", 1, 0},
	} {
		d, n := fileNumber(tc.name)
		if d != tc.disc || n != tc.track {
			t.Errorf("%s: got %d:%d want %d:%d", tc.name, d, n, tc.disc, tc.track)
		}
	}
}

func TestUnknownFilenameNeverMatchesZeroTitle(t *testing.T) {
	dir := fixture(t, "show", 0, "Artist: Band\nDate: 1989-09-05\nVenue: Hall\n00. Intro\n")
	if err := os.WriteFile(filepath.Join(dir, "unknown.flac"), []byte("fixture audio"), 0644); err != nil {
		t.Fatal(err)
	}
	r := only(t, dir)
	if len(r.Tracks) != 1 || r.Tracks[0].Number != 0 || r.Tracks[0].Title != "" || !hasReason(r, "unmatched file without explicit track number") {
		t.Fatalf("unknown filename matched zero title: %+v", r)
	}
}

func TestLetterPrefixTrackNumbers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disc, track int
	}{
		{"ericburdon-01.flac", 1, 1},
		{"ERICBURDON-07.FLAC", 1, 7},
		{"disc 2/ericburdon_03.flac", 2, 3},
		{"band-123.flac", 1, 123},
		{"band-00.flac", 1, 0},
		{"band-1.flac", 1, 0},
		{"band-1967.flac", 1, 0},
		{"band1967-01.flac", 1, 0},
		{"band-1967-01-20.flac", 1, 0},
		{"band-01extra.flac", 1, 0},
		{"band-title-01.flac", 1, 0},
		{"band title-01.flac", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, n := fileNumber(filepath.FromSlash(tc.name))
			if d != tc.disc || n != tc.track {
				t.Fatalf("got %d:%d, want %d:%d", d, n, tc.disc, tc.track)
			}
		})
	}
}

func TestLetterPrefixMappingDoesNotShiftMissingTracks(t *testing.T) {
	dir := fixture(t, "show", 0, notes("Eric Burdon", 3))
	for _, n := range []int{1, 3} {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ericburdon-%02d.flac", n)), []byte("fixture audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	r := only(t, dir)
	if len(r.Tracks) != 2 || r.Status != "needs-review" || !hasReason(r, "listed track has no audio file: 1:2") {
		t.Fatalf("missing track lost review: %+v", r)
	}
	for _, tr := range r.Tracks {
		if tr.Number == 0 || tr.Title != fmt.Sprintf("Title %d [annotation]", tr.Number) {
			t.Fatalf("filename identifier lost or title shifted: %+v", tr)
		}
	}
}

func TestOrdinalConcertHeader(t *testing.T) {
	for _, date := range []string{"August 27th, 1992", "27th August 1992", "Aug 27TH 1992"} {
		t.Run(date, func(t *testing.T) {
			r := only(t, fixture(t, "csn", 1, "Crosby, Stills and Nash\r\nOrpheum Theatre\r\nMinneapolis, MN\r\n"+date+"\r\n\r\nSource: SBD > DAT\r\nTransfer: January 1st, 2019\r\n01   //Wasted On The Way\r\n"))
			if r.Artist != "Crosby, Stills and Nash" || r.Date != "1992-08-27" || r.Album != "1992-08-27 - Orpheum Theatre, Minneapolis, MN" {
				t.Fatalf("header: %+v", r)
			}
			if len(r.Tracks) != 1 || r.Tracks[0].Title != "//Wasted On The Way" || hasReason(r, "concert date") || hasReason(r, "artist is missing") {
				t.Fatalf("mapping or metadata: %+v", r)
			}
		})
	}
}

func TestOrdinalConcertDatesRetainValidation(t *testing.T) {
	for _, date := range []string{"February 30th, 1992", "August 327th, 1992"} {
		r := only(t, fixture(t, "invalid", 1, "Artist: Band\nVenue: Hall\nDate: "+date+"\n01 Song\n"))
		if r.Date != "" || !hasReason(r, "concert date is missing") {
			t.Fatalf("accepted invalid date %q: %+v", date, r)
		}
	}
	r := only(t, fixture(t, "conflicts", 1, "Crosby, Stills and Nash\nOrpheum Theatre\nMinneapolis, MN\nAugust 27th, 1992\nArtist: Other Band\nDate: August 28th, 1992\n01 Song\n"))
	if !hasReason(r, "conflicting concert dates") || !hasReason(r, "conflicting artist") {
		t.Fatalf("lost conflicts: %+v", r)
	}
}
