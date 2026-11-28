package catalog

import "testing"

func TestSourceEvidenceReview(t *testing.T) {
	for _, tc := range []struct {
		name, header, title, venue, city string
		review                           bool
	}{
		{"speculative venue", "Band\nHall (probably Other Hall)\nTown, NY\n2020-03-14\n", "Song", "Hall (probably Other Hall)", "Town, NY", true},
		{"alias", "Band\nAKA Combined Band\nHall\nTown, NY\n2020-03-14\n", "Song", "Hall", "Town, NY", false},
		{"event at venue", "Band\n2020-03-14\nFestival @ Farm\nTown, NY\n", "Song", "Festival, Farm", "Town, NY", true},
		{"festival ambiguous city", "Band\nMusic Festival\nGreat Hall, Town\n2020-03-14\n", "Song", "Music Festival", "", true},
		{"uncertain title", "Artist: Band\nVenue: Hall\nCity: Town\nDate: 2020-03-14\n", "Song (New Song, unsure on title)", "Hall", "Town", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := only(t, fixture(t, "show", 1, tc.header+"01. "+tc.title+"\n"))
			if r.Venue != tc.venue || r.City != tc.city {
				t.Fatalf("location: %+v", r)
			}
			if (r.Status == "needs-review") != tc.review {
				t.Fatalf("review: %+v", r)
			}
			if tc.name == "uncertain title" && r.Tracks[0].Title != tc.title {
				t.Fatal("provisional suggestion lost")
			}
		})
	}
}
func TestTitleAnnotationBoundaries(t *testing.T) {
	for _, tc := range []struct{ in, out string }{
		{"Song [08:17]", "Song"}, {"Song > [03:53]", "Song >"},
		{"Song [1:19:03]", "Song"}, {"Song (Live)", "Song (Live)"},
		{"Who Are You?", "Who Are You?"}, {"Song (Unsure)", "Song (Unsure)"}, {"Probably", "Probably"}, {"Song (Probably)", "Song (Probably)"}, {"Song (New Song)", "Song (New Song)"},
		{"Song [03:99]", "Song [03:99]"}, {"Song [Part 2]", "Song [Part 2]"},
	} {
		r := Recording{}
		titles := map[string]string{}
		addTitle(&r, titles, 1, 1, tc.in)
		if len(r.ReviewReasons) != 0 {
			t.Errorf("literal title %q unexpectedly requires review: %v", tc.in, r.ReviewReasons)
		}
		if titles["1:1"] != tc.out {
			t.Errorf("%q => %q want %q", tc.in, titles["1:1"], tc.out)
		}
	}
}
