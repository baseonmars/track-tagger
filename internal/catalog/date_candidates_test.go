package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func dateFixture(t *testing.T, dirName string, noteFiles map[string]string, audioFiles ...string) Recording {
	t.Helper()
	dir := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, text := range noteFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range audioFiles {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("synthetic audio"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return only(t, dir)
}

func dateNotes(date string) string {
	return "Artist: Example Band\nVenue: Example Hall\nDate: " + date + "\n01. Song\n"
}

func TestReedCollegeNumericDateSuggestion(t *testing.T) {
	// The concert header is four unlabelled lines, without a Date field.
	notes := "Grateful Dead\nReed College\nPortland, OR\n10/09/1969\n01. Song\n"
	r := dateFixture(t, "show", map[string]string{"notes.txt": notes}, "t01.flac")
	if r.Date != "1969-10-09" || !strings.HasPrefix(r.Album, "1969-10-09") || r.Status != "needs-review" || !hasReason(r, "ambiguous numeric concert date: 10/09/1969") {
		t.Fatalf("Reed College date was not proposed for review: date=%q, album=%q, status=%s, reasons=%v", r.Date, r.Album, r.Status, r.ReviewReasons)
	}
}

func TestCorroboratedNumericConcertDates(t *testing.T) {
	for _, tc := range []struct {
		date, want, audio string
	}{
		{"09/05/1989", "1989-09-05", "milesdavis1989-09-05[t01].flac"},
		{"06/07/1992", "1992-06-07", "example1992-06-07s1t01.flac"},
		{"02/11/1990", "1990-02-11", "example1990-02-11d1t01.flac"},
		{"10/09/1969", "1969-10-09", "example1969-10-09t01.flac"},
		{"09/05/1989", "1989-05-09", "example1989-05-09t01.flac"},
		{"09-05-1989", "1989-09-05", "example1989-09-05t01.flac"},
	} {
		t.Run(tc.date+tc.want, func(t *testing.T) {
			r := dateFixture(t, "show", map[string]string{"notes.txt": dateNotes(tc.date)}, tc.audio)
			if r.Date != tc.want || r.Status != "ready" || r.Tracks[0].Title != "Song" {
				t.Fatalf("date = %q, status = %s, reasons = %v", r.Date, r.Status, r.ReviewReasons)
			}
		})
	}
}

func TestNumericDateCorroborationSourcesAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, dir, notesName, audio, want string
		ambiguous, conflict               bool
	}{
		{"folder only", "show1989-09-05", "notes.txt", "t01.flac", "1989-09-05", false, false},
		{"notes only", "show", "show1989-09-05.nfo", "t01.flac", "1989-09-05", false, false},
		{"all agree", "show1989-09-05", "show1989-09-05.txt", "example1989-09-05t01.flac", "1989-09-05", false, false},
		{"no evidence", "show", "notes.txt", "t01.flac", "1989-09-05", true, false},
		{"wrong date", "show1989-01-01", "notes.txt", "t01.flac", "1989-09-05", true, true},
		{"folder audio conflict", "show1989-05-09", "notes.txt", "example1989-09-05t01.flac", "1989-09-05", true, true},
		{"notes audio conflict", "show", "show1989-05-09.txt", "example1989-09-05t01.flac", "1989-09-05", true, true},
		{"two dates in name", "show1989-05-09_and_1989-09-05", "notes.txt", "t01.flac", "1989-09-05", true, true},
		{"numeric filename excluded", "show09-05-1989", "notes.txt", "t01.flac", "1989-09-05", true, false},
		{"invalid filename excluded", "show1989-02-29", "notes.txt", "t01.flac", "1989-09-05", true, false},
		{"invalid and valid", "show1989-02-29", "notes.txt", "example1989-09-05t01.flac", "1989-09-05", false, false},
		{"ancestor excluded", "1989-09-05/show", "notes.txt", "t01.flac", "1989-09-05", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dateFixture(t, tc.dir, map[string]string{tc.notesName: dateNotes("09/05/1989")}, tc.audio)
			if r.Date != tc.want || hasReason(r, "ambiguous numeric concert date") != tc.ambiguous || hasReason(r, "conflicting filename evidence") != tc.conflict || (tc.ambiguous || tc.conflict) != (r.Status == "needs-review") || !strings.HasPrefix(r.Album, tc.want) {
				t.Fatalf("date = %q, album = %q, status = %s, want %q; reasons = %v", r.Date, r.Album, r.Status, tc.want, r.ReviewReasons)
			}
		})
	}
	r := dateFixture(t, "show", map[string]string{"notes.txt": dateNotes("09/05/1989") + "02. Last\n"}, "example1989-09-05t01.flac", "example1989-05-09t02.flac")
	if r.Date != "1989-09-05" || !hasReason(r, "ambiguous numeric concert date") || !hasReason(r, "conflicting filename evidence") || r.Status != "needs-review" {
		t.Fatalf("conflicting audio dates lost suggestion or review: %q, %v", r.Date, r.ReviewReasons)
	}
	r = dateFixture(t, "jack1905-08-11", map[string]string{"jack1905-08-11.txt": dateNotes("08/11/1995")}, "jack1995-11-08t01.flac")
	if r.Date != "1995-11-08" || r.Status != "needs-review" || !hasReason(r, "conflicting filename evidence") || hasReason(r, "ambiguous numeric concert date") {
		t.Fatalf("unique day-first evidence was not selected despite unrelated year: date=%q, status=%s, reasons=%v", r.Date, r.Status, r.ReviewReasons)
	}
}

func TestFilenameDateBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"milesdavis1989-09-05[t01].flac", true},
		{"band1989-09-05t01.flac", true},
		{"1989-09-05.txt", true},
		{"11989-09-05t01.flac", false},
		{"1989-09-050t01.flac", false},
		{"1989-9-05t01.flac", false},
		{"1989/09/05t01.flac", false},
		{"1989-02-29t01.flac", false},
		{"0000-09-05t01.flac", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := filenameDates("show", []string{tc.name}, nil)
			if (len(got) == 1) != tc.want {
				t.Fatalf("filename dates = %v, want one = %v", got, tc.want)
			}
		})
	}
	got := filenameDates(filepath.Join("1989-09-05", "show"), []string{filepath.Join("1989-09-05", "notes.txt")}, []string{filepath.Join("1989-09-05", "t01.flac")})
	if len(got) != 0 {
		t.Fatalf("read ancestor date: %v", got)
	}
}

func TestDateEvidenceNeverCreatesOrOverridesNotes(t *testing.T) {
	for _, tc := range []struct {
		name, date, want string
	}{
		{"no notes date", "", ""},
		{"invalid numeric", "02/30/1989", ""},
		{"invalid ISO", "1989-02-30", ""},
		{"zero month", "00/05/1989", ""},
		{"invalid year", "09/05/0000", ""},
		{"unambiguous day first", "20/01/1989", "1989-01-20"},
		{"unambiguous month first", "01/20/1989", "1989-01-20"},
		{"equal day month", "05/05/1989", "1989-05-05"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dateFixture(t, "show1989-09-05", map[string]string{"notes.txt": dateNotes(tc.date)}, "t01.flac")
			if r.Date != tc.want || (tc.want == "" && !hasReason(r, "concert date is missing")) {
				t.Fatalf("date = %q, want %q; reasons = %v", r.Date, tc.want, r.ReviewReasons)
			}
		})
	}
	r := dateFixture(t, "jack1905-08-11", map[string]string{"jack1905-08-11.txt": dateNotes("08/11/1995")}, "jack1995-08-11t01.flac")
	if r.Date != "1995-08-11" || !hasReason(r, "conflicting filename evidence") || r.Status != "needs-review" || !strings.HasPrefix(r.Album, "1995-08-11") {
		t.Fatalf("conflicting year evidence lost suggestion or review: %q, %v", r.Date, r.ReviewReasons)
	}
	r = dateFixture(t, "show1989-09-05", nil, "t01.flac")
	if r.Date != "" || !hasReason(r, "no local concert notes") {
		t.Fatalf("filename-only date accepted: %q, %v", r.Date, r.ReviewReasons)
	}
}

func TestAllNotesDateCandidatesParticipate(t *testing.T) {
	for _, tc := range []struct {
		name, extra, want string
	}{
		{"matching ambiguous", "Date: 09/05/1989\n", "1989-09-05"},
		{"matching ISO", "Date: 1989-09-05\n", "1989-09-05"},
		{"conflicting ISO", "Date: 1989-05-09\n", ""},
		{"unresolved other year", "Date: 09/05/1990\n", ""},
		{"invalid second date", "Date: 1989-02-30\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := dateFixture(t, "show", map[string]string{"a.txt": dateNotes("09/05/1989"), "z.nfo": tc.extra}, "example1989-09-05t01.flac")
			if r.Date != tc.want || (tc.want == "" && !hasReason(r, "concert date")) {
				t.Fatalf("date = %q, want %q; reasons = %v", r.Date, tc.want, r.ReviewReasons)
			}
		})
	}
	// A separate unambiguous note must not hide an unresolved numeric note.
	r := dateFixture(t, "show", map[string]string{"a.txt": dateNotes("1989-09-05"), "z.txt": "Date: 09/05/1989\n"}, "t01.flac")
	if r.Date != "1989-09-05" || !hasReason(r, "ambiguous numeric concert date") || r.Status != "needs-review" {
		t.Fatalf("ambiguous candidate hidden: %q, %v", r.Date, r.ReviewReasons)
	}
}

func TestLaterConcertSectionDates(t *testing.T) {
	for _, date := range []string{"01/xx/1968", "January 20, 1968", "20 January 1968", "1968-01-20", "20/01/1968"} {
		t.Run(date, func(t *testing.T) {
			s := "Artist: Example Band\nVenue: Hall\nJanuary 20, 1967\n01. First\n\n-------------------\nBerlin bietet Beat\nBerlin House of Broadcasting\nGermany\n" + date + "\n\n01. Second\n"
			r := dateFixture(t, "show", map[string]string{"notes.txt": s}, "t01.flac")
			if r.Date != "" || !hasReason(r, "conflicting concert dates or multi-date compilation") || !hasReason(r, "duplicate numbering in notes") || r.Tracks[0].Title != "" {
				t.Fatalf("compilation not held: date=%q, title=%q, reasons=%v", r.Date, r.Tracks[0].Title, r.ReviewReasons)
			}
			if strings.HasPrefix(r.Album, "1967") || strings.HasPrefix(r.Album, "1968") {
				t.Fatalf("compilation retained one album date: %q", r.Album)
			}
		})
	}
	for _, date := range []string{"01/xx/1968", "1/XX/1968", "01-xx-1968"} {
		r := dateFixture(t, "show1968-01-20", map[string]string{"notes.txt": dateNotes(date)}, "t01.flac")
		if r.Date != "1968-01" || r.Status != "ready" {
			t.Fatalf("partial date invented a day: %q, %v", r.Date, r.ReviewReasons)
		}
	}
	for _, date := range []string{"00/xx/1968", "13/xx/1968", "01/xx/0000"} {
		r := dateFixture(t, "show", map[string]string{"notes.txt": dateNotes(date)}, "t01.flac")
		if r.Date != "" || !hasReason(r, "invalid concert date") {
			t.Fatalf("invalid partial date accepted: %q, %v", r.Date, r.ReviewReasons)
		}
	}
}

func TestNonConcertDatesDoNotAffectMetadataOrTitles(t *testing.T) {
	for _, tail := range []string{
		"\nJanuary 20, 1968\n",
		"\n20 January 1968\n",
		"\n01/xx/1968\n",
		"\nJanuary 20, 1968\n20 January 1969\n",
		"\nJanuary 20, 1968\nshntool output:\n01. Calibration\n",
		"\nJanuary 20, 1968\n 1:23.456 12345 B flac 0.5 t01.flac\n",
		"\nJanuary 20, 1968\nt01.flac ABCD1234\n",
		"\nTransfer notes:\nJanuary 20, 1968\n01. Calibration\n",
		"\nProduction notes:\nJanuary 20, 1968\n01. Calibration\n",
		"\nMastering notes:\n01/xx/1968\n01. Calibration\n",
	} {
		t.Run(tail, func(t *testing.T) {
			r := dateFixture(t, "show", map[string]string{"notes.txt": dateNotes("1967-01-20") + tail}, "t01.flac")
			if r.Date != "1967-01-20" || r.Status != "ready" || r.Tracks[0].Title != "Song" {
				t.Fatalf("production date leaked: %q, %v, %v", r.Date, r.ReviewReasons, r.Tracks)
			}
		})
	}
	// Technical sections can precede the first track while the header state
	// is still active; a date-only shortcut must respect that boundary.
	s := "Artist: Example Band\nDate: 1967-01-20\nVenue: Hall\nTransfer notes:\n20 January 1968\n01/xx/1968\nTracklist:\n01. Song\n"
	r := dateFixture(t, "show", map[string]string{"notes.txt": s}, "t01.flac")
	if r.Date != "1967-01-20" || r.Status != "ready" {
		t.Fatalf("technical section date leaked through header: %q, %v", r.Date, r.ReviewReasons)
	}
	for _, heading := range []string{"Original transfer:", "First transfer:", "Second transfer", "Third transfer :", "Final transfer:"} {
		s = "Artist: Example Band\nDate: 1967-01-20\nVenue: Hall\n" + heading + "\n20 January 1968\n01/xx/1968\n01. Calibration\nTracklist:\n01. Song\n"
		r = dateFixture(t, "show", map[string]string{"notes.txt": s}, "t01.flac")
		if r.Date != "1967-01-20" || r.Status != "ready" || r.Tracks[0].Title != "Song" {
			t.Fatalf("modified transfer heading %q leaked: %q, %v", heading, r.Date, r.ReviewReasons)
		}
	}
	s = "Artist: Example Band\nDate: 1973-07-12\nVenue: Hall\nOriginal transfer: Reel > Otari MX-55 > TC 2000 > DAT (44k) - by Example Person 08/04/1997\nSecond transfer: DAT > WAV 09/05/1998\n01. Song\n"
	r = dateFixture(t, "show", map[string]string{"notes.txt": s}, "t01.flac")
	if r.Date != "1973-07-12" || r.Status != "ready" {
		t.Fatalf("labelled transfer date leaked: %q, %v", r.Date, r.ReviewReasons)
	}
	for _, title := range []string{"01. January 20, 1968", "01 January 20, 1968", "01. 01/xx/1968", "01. 1968-01-20", "01 June 1992 Memories"} {
		s = "Artist: Example Band\nDate: 1967-01-20\nVenue: Hall\n" + title + "\n02. Last\n"
		r = dateFixture(t, "show", map[string]string{"notes.txt": s}, "t01.flac", "t02.flac")
		if r.Date != "1967-01-20" || r.Status != "ready" || r.Tracks[0].Title == "" {
			t.Fatalf("date-like song changed metadata or title: %q, %v", title, r.ReviewReasons)
		}
	}
}

func TestResolvedDateRetainsDuplicateReview(t *testing.T) {
	s := dateNotes("09/05/1989") + "01. Different Song\n"
	r := dateFixture(t, "show", map[string]string{"notes.txt": s}, "milesdavis1989-09-05[t01].flac")
	if r.Date != "1989-09-05" || !hasReason(r, "duplicate numbering in notes") || r.Tracks[0].Title != "" || r.Status != "needs-review" {
		t.Fatalf("date resolution changed duplicate handling: %q, %v", r.Date, r.ReviewReasons)
	}
}

func TestFilenameDatesUseOnlySuppliedBasenames(t *testing.T) {
	got := filenameDates("show1989-09-05", []string{"notes1989-09-05.txt"}, []string{"example1989-09-05t01.flac", "example1989-09-05t02.flac"})
	want := map[string]bool{"1989-09-05": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate corroboration = %v, want %v", got, want)
	}
}
