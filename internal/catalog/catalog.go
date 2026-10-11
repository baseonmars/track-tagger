// Package catalog discovers recordings and builds conservative, evidence-backed plans.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"track-tagger/internal/media"
)

// ReservedFilePrefix identifies application staging files, including orphaned
// stages left by an interrupted process. It does not reserve directory names.
const ReservedFilePrefix = ".track-tagger-"

type Evidence struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}
type Track struct {
	Path         string            `json:"path"`
	RelativePath string            `json:"relative_path"`
	Title        string            `json:"title"`
	Disc         int               `json:"disc"`
	Number       int               `json:"number"`
	DiscTotal    int               `json:"disc_total"`
	TrackTotal   int               `json:"track_total"`
	Tags         map[string]string `json:"tags"`
}
type Recording struct {
	Fingerprint       string     `json:"fingerprint"`
	ID                string     `json:"id"`
	Path              string     `json:"path"`
	RelativePath      string     `json:"relative_path"`
	Status            string     `json:"status"`
	Artist            string     `json:"artist"`
	Album             string     `json:"album"`
	Date              string     `json:"date"`
	Venue             string     `json:"venue"`
	City              string     `json:"city"`
	Tracks            []Track    `json:"tracks"`
	ArtworkCandidates []string   `json:"artwork_candidates"`
	Artwork           string     `json:"artwork"`
	Notes             []string   `json:"notes"`
	Evidence          []Evidence `json:"evidence"`
	ReviewReasons     []string   `json:"review_reasons"`
}

var discDir = regexp.MustCompile(`(?i)^(?:disc|disk|cd|set)[ _-]*(\d+)$`)
var fileDT = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:d|disc|cd|s|set)[ _-]*(\d+)[ _.-]*t?(\d+)(?:[^0-9]|$)`)
var fileNumericDT = regexp.MustCompile(`^(\d{1,2})[-_.](\d{2,3})(?:[^0-9]|$)`)
var fileT = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])t(\d+)(?:[^0-9]|$)`)
var leadingT = regexp.MustCompile(`^(\d+)[ ._-]`)
var noteT = regexp.MustCompile(`^\s*(\d{1,3})\s*[.):_-]\s*(.+)$`)
var noteDT = regexp.MustCompile(`(?i)^\s*(?:d|s)(\d+)t(\d+)\s*[ .):_-]*\s*(.+)$`)
var section = regexp.MustCompile(`(?i)^\s*(?:disc|disk|cd|set)\s*(\d+)\s*[:.-]?\s*$`)
var isoDate = regexp.MustCompile(`\b(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
var numericDate = regexp.MustCompile(`\b(\d{1,2})[/-](\d{1,2})[/-](\d{4})\b`)
var namedDate = regexp.MustCompile(`(?i)\b(?:\d{1,2}\s+(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{4}|(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2},?\s+\d{4})\b`)

func Scan(source string) ([]Recording, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("source must be a directory")
	}
	groups := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ReservedFilePrefix) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".flac") {
			dir := filepath.Dir(path)
			if discDir.MatchString(filepath.Base(dir)) && dir != root {
				dir = filepath.Dir(dir)
			}
			groups[dir] = append(groups[dir], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(groups))
	for dir := range groups {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var out []Recording
	for _, dir := range dirs {
		r, err := build(root, dir, groups[dir])
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func reason(r *Recording, s string) {
	for _, v := range r.ReviewReasons {
		if v == s {
			return
		}
	}
	r.ReviewReasons = append(r.ReviewReasons, s)
}
func build(root, dir string, files []string) (Recording, error) {
	rel, _ := filepath.Rel(root, dir)
	id := sha256.Sum256([]byte(dir))
	r := Recording{ID: hex.EncodeToString(id[:12]), Path: dir, RelativePath: rel, Status: "ready"}
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ReservedFilePrefix) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			reason(&r, "symbolic links require review")
			return nil
		}
		if d.IsDir() {
			if p != dir && !discDir.MatchString(filepath.Base(p)) {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return r, err
	}
	titles := map[string]string{}
	dates := map[string]bool{}
	artists := map[string]bool{}
	for _, p := range paths {
		if err := media.CheckLocal(p); err != nil {
			r.Status = "waiting"
			reason(&r, "file is not locally available: "+filepath.Base(p))
			continue
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".txt", ".nfo":
			r.Notes = append(r.Notes, p)
			st, err := os.Stat(p)
			if err != nil {
				return r, err
			}
			if st.Size() > 4<<20 {
				reason(&r, "notes exceed 4 MiB")
				break
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return r, err
			}
			if len(b) > 4<<20 {
				reason(&r, "notes exceed 4 MiB")
				break
			}
			s := string(b)
			if !utf8.Valid(b) {
				s, _ = charmap.Windows1252.NewDecoder().String(s)
			}
			parseNotes(&r, s, titles, dates, artists)
		case ".jpg", ".jpeg", ".png":
			r.ArtworkCandidates = append(r.ArtworkCandidates, p)
		case ".part", ".partial", ".tmp", ".download", ".icloud":
			r.Status = "waiting"
			reason(&r, "incomplete transfer or cloud placeholder: "+filepath.Base(p))
		}
		st, err := os.Stat(p)
		if err != nil {
			return r, err
		}
		if st.Size() == 0 {
			r.Status = "waiting"
			reason(&r, "empty file: "+filepath.Base(p))
		}
		e, err := Fingerprint(p)
		if err != nil {
			return r, err
		}
		r.Evidence = append(r.Evidence, e)
		if ext == ".md5" || ext == ".ffp" || ext == ".sfv" {
			checkManifest(&r, p)
		}
	}
	if len(dates) == 1 {
		for d := range dates {
			r.Date = d
		}
	} else if len(dates) > 1 {
		reason(&r, "conflicting concert dates or multi-date compilation")
	}
	if r.Date == "" {
		reason(&r, "concert date is missing or ambiguous")
	}
	if len(artists) == 1 {
		for a := range artists {
			r.Artist = a
		}
	} else if len(artists) > 1 {
		reason(&r, "mixed performers or conflicting artist names")
	}
	if r.Artist == "" {
		reason(&r, "artist is missing")
	}
	if len(r.Notes) == 0 {
		reason(&r, "no local concert notes")
	}
	seen := map[string]bool{}
	for _, p := range files {
		d, n := fileNumber(p)
		key := fmt.Sprintf("%d:%d", d, n)
		rp, _ := filepath.Rel(root, p)
		t := Track{Path: p, RelativePath: rp, Disc: d, Number: n, Title: titles[key]}
		if n == 0 {
			reason(&r, "unmatched file without explicit track number: "+filepath.Base(p))
		} else if seen[key] {
			reason(&r, "duplicate file numbering: "+key)
		}
		seen[key] = true
		if t.Title == "" {
			reason(&r, "unmatched audio file: "+filepath.Base(p))
		}
		r.Tracks = append(r.Tracks, t)
	}
	for key := range titles {
		if !seen[key] {
			reason(&r, "listed track has no audio file: "+key)
		}
	}
	if len(titles) != len(files) {
		reason(&r, fmt.Sprintf("track count mismatch: %d listed titles, %d audio files", len(titles), len(files)))
	}
	sort.Slice(r.Tracks, func(i, j int) bool {
		a, b := r.Tracks[i], r.Tracks[j]
		if a.Disc != b.Disc {
			return a.Disc < b.Disc
		}
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		return a.Path < b.Path
	})
	chooseArt(&r)
	r.Album = r.Date
	if r.Venue != "" {
		r.Album += " - " + r.Venue
		if r.City != "" {
			r.Album += ", " + r.City
		}
	} else {
		reason(&r, "venue is missing")
	}
	h := sha256.New()
	for _, e := range r.Evidence {
		rel, _ := filepath.Rel(dir, e.Path)
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\n", filepath.ToSlash(rel), e.SHA256, e.Size, e.ModTime)
	}
	r.Fingerprint = hex.EncodeToString(h.Sum(nil))
	RefreshPlan(&r)
	if len(r.ReviewReasons) > 0 && r.Status != "waiting" {
		r.Status = "needs-review"
	}
	return r, nil
}

func fileNumber(path string) (int, int) {
	base := filepath.Base(path)
	d := 1
	if m := discDir.FindStringSubmatch(filepath.Base(filepath.Dir(path))); m != nil {
		d, _ = strconv.Atoi(m[1])
	}
	if m := fileDT.FindStringSubmatch(base); m != nil {
		dd, _ := strconv.Atoi(m[1])
		n, _ := strconv.Atoi(m[2])
		return dd, n
	}
	if m := fileNumericDT.FindStringSubmatch(base); m != nil {
		dd, _ := strconv.Atoi(m[1])
		n, _ := strconv.Atoi(m[2])
		return dd, n
	}
	if m := fileT.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return d, n
	}
	if m := leadingT.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return d, n
	}
	return d, 0
}
func parseNotes(r *Recording, s string, titles map[string]string, dates, artists map[string]bool) {
	disc := 1
	header := true
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if m := section.FindStringSubmatch(line); m != nil {
			disc, _ = strconv.Atoi(m[1])
			header = false
			continue
		}
		if strings.EqualFold(strings.TrimSuffix(line, ":"), "encore") {
			continue
		}
		if m := noteDT.FindStringSubmatch(line); m != nil {
			d, _ := strconv.Atoi(m[1])
			n, _ := strconv.Atoi(m[2])
			addTitle(r, titles, d, n, m[3])
			header = false
			continue
		}
		if m := noteT.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			addTitle(r, titles, disc, n, m[2])
			header = false
			continue
		}
		if technical(lower) {
			continue
		}
		for _, label := range []string{"artist:", "performer:", "band:"} {
			if strings.HasPrefix(lower, label) {
				a := strings.TrimSpace(line[len(label):])
				if a != "" {
					artists[a] = true
				}
			}
		}
		if strings.HasPrefix(lower, "venue:") {
			r.Venue = strings.TrimSpace(line[6:])
		}
		if strings.HasPrefix(lower, "city:") {
			r.City = strings.TrimSpace(line[5:])
		}
		if header || strings.HasPrefix(lower, "date:") || strings.HasPrefix(lower, "concert date:") {
			parseDates(r, line, dates)
		}
		// Unlabelled headings can be useful evidence, but cannot safely identify an artist.
	}
}
func addTitle(r *Recording, titles map[string]string, d, n int, title string) {
	key := fmt.Sprintf("%d:%d", d, n)
	if _, ok := titles[key]; ok {
		reason(r, "duplicate numbering in notes: "+key)
	}
	titles[key] = strings.TrimSpace(title)
}
func technical(s string) bool {
	for _, prefix := range []string{"transfer", "transferred", "seed", "upload", "remaster", "mastered", "encoded", "extracted", "checksum", "source:", "lineage:", "eac ", "exact audio", "log ", "personnel:", "musicians:", "recording equipment"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
func parseDates(r *Recording, line string, dates map[string]bool) {
	for _, m := range isoDate.FindAllStringSubmatch(line, -1) {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		addDate(dates, y, mo, d)
	}
	for _, m := range numericDate.FindAllStringSubmatch(line, -1) {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		if a > 12 {
			addDate(dates, y, b, a)
		} else if b > 12 || a == b {
			addDate(dates, y, a, b)
		} else {
			reason(r, "ambiguous numeric concert date: "+m[0])
		}
	}
	for _, v := range namedDate.FindAllString(line, -1) {
		for _, layout := range []string{"2 January 2006", "2 Jan 2006", "January 2, 2006", "Jan 2, 2006", "January 2 2006", "Jan 2 2006"} {
			if t, e := time.Parse(layout, v); e == nil {
				dates[t.Format("2006-01-02")] = true
				break
			}
		}
	}
}
func addDate(dates map[string]bool, y, m, d int) {
	s := fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	if _, err := time.Parse("2006-01-02", s); err == nil {
		dates[s] = true
	}
}
func chooseArt(r *Recording) {
	var preferred []string
	for _, p := range r.ArtworkCandidates {
		n := strings.ToLower(strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))
		if n == "cover" || n == "front" || n == "folder" {
			preferred = append(preferred, p)
		}
	}
	if len(preferred) == 1 {
		r.Artwork = preferred[0]
	} else if len(preferred) > 1 {
		reason(r, "multiple preferred artwork candidates")
	} else if len(r.ArtworkCandidates) == 1 {
		r.Artwork = r.ArtworkCandidates[0]
	} else if len(r.ArtworkCandidates) > 1 {
		reason(r, "artwork selection requires review")
	}
}
func checkManifest(r *Recording, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		reason(r, "cannot read checksum manifest")
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		var name string
		if i := strings.Index(strings.ToLower(line), ".flac"); i >= 0 {
			part := strings.TrimSpace(line[:i+5])
			fields := strings.Fields(part)
			if len(fields) > 1 && len(fields[0]) == 32 {
				part = strings.TrimSpace(strings.TrimPrefix(part, fields[0]))
				part = strings.TrimPrefix(part, "*")
			}
			name = part
		}
		if name != "" {
			p := filepath.Join(filepath.Dir(path), filepath.FromSlash(strings.ReplaceAll(name, "\\", "/")))
			if _, err := os.Stat(p); err != nil {
				reason(r, "manifest references missing file: "+name)
			}
		}
	}
}

func RefreshPlan(r *Recording) {
	counts := map[int]int{}
	maxDisc := 0
	for _, t := range r.Tracks {
		counts[t.Disc]++
		if t.Disc > maxDisc {
			maxDisc = t.Disc
		}
	}
	for i := range r.Tracks {
		t := &r.Tracks[i]
		t.TrackTotal = counts[t.Disc]
		t.DiscTotal = maxDisc
		t.Tags = map[string]string{"ARTIST": r.Artist, "ALBUMARTIST": r.Artist, "ALBUM": r.Album, "DATE": r.Date, "TITLE": t.Title, "TRACKNUMBER": strconv.Itoa(t.Number), "TRACKTOTAL": strconv.Itoa(t.TrackTotal), "DISCNUMBER": strconv.Itoa(t.Disc), "DISCTOTAL": strconv.Itoa(t.DiscTotal)}
	}
}
func Fingerprint(path string) (Evidence, error) {
	if err := media.CheckLocal(path); err != nil {
		return Evidence{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Evidence{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Evidence{}, err
	}
	if !before.Mode().IsRegular() {
		return Evidence{}, fmt.Errorf("not a regular local file: %s", path)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return Evidence{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return Evidence{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Evidence{}, fmt.Errorf("file changed while reading: %s", path)
	}
	return Evidence{Path: path, SHA256: hex.EncodeToString(h.Sum(nil)), Size: after.Size(), ModTime: after.ModTime().UnixNano()}, nil
}
func ValidateEvidence(evidence []Evidence) error {
	for _, want := range evidence {
		got, err := Fingerprint(want.Path)
		if err != nil {
			return err
		}
		if got.SHA256 != want.SHA256 || got.Size != want.Size || got.ModTime != want.ModTime {
			return fmt.Errorf("input changed; rescan required: %s", want.Path)
		}
	}
	return nil
}
