// Package catalog discovers recordings and builds conservative, evidence-backed plans.
package catalog

import (
	"context"
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
	Artist       string            `json:"artist,omitempty"`
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
	PlaybackOnly      bool       `json:"playback_only"`
	Artist            string     `json:"artist"`
	Album             string     `json:"album"`
	Date              string     `json:"date"`
	DateUnknown       bool       `json:"date_unknown,omitempty"`
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
var datedFileT = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{4}-\d{2}-\d{2})t(\d{1,3})$`)
var datedFileDT = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{4}-\d{2}-\d{2})(?:s|d)(\d{1,2})t(\d{1,3})$`)
var weekdayPrefix = regexp.MustCompile(`(?i)^(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday),?\s+`)
var weekdaySuffix = regexp.MustCompile(`(?i)^(?:,?\s*(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday)|\s*\((?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday)\))$`)
var otherPerformer = regexp.MustCompile(`(?i)\b(?:opening|support)\s+set\b`)
var leadingT = regexp.MustCompile(`^(\d+)[ ._-]`)
var suffixedT = regexp.MustCompile(`^[a-zA-Z]+[-_](\d{2,3})$`)
var noteT = regexp.MustCompile(`^\s*(\d{1,3})\s*[.):_-]\s*(.+)$`)
var noteSpaceT = regexp.MustCompile(`^\s*(\d{1,3})[ \t]+(\S.*)$`)
var reportDuration = regexp.MustCompile(`^\d+:\d{2}(?:[.:]\d+)?\s`)
var noteDT = regexp.MustCompile(`(?i)^\s*(?:d|s)(\d+)t(\d+)\s*[ .):_-]*\s*(.+)$`)
var isoDate = regexp.MustCompile(`\b(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
var numericDate = regexp.MustCompile(`\b(\d{1,2})[/-](\d{1,2})[/-](\d{4})\b`)
var namedDate = regexp.MustCompile(`(?i)\b(?:\d{1,2}(?:st|nd|rd|th)?\s+(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{4}|(?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4})\b`)
var ordinalDay = regexp.MustCompile(`(?i)(\d{1,2})(?:st|nd|rd|th)([,\s])`)

// IsAudio reports whether a path has a supported playback extension. The browser
// determines whether the actual codec in the file can be decoded.
func IsAudio(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac", ".wav", ".mp3", ".ogg", ".oga", ".opus":
		return true
	default:
		return false
	}
}

// CanTag reports whether a path uses the only currently writable format, FLAC.
func CanTag(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".flac")
}

func Scan(source string) ([]Recording, error) { return scan(source, Fingerprint) }

// Discovery describes a recording without opening its file contents.
type Discovery struct {
	ID, Path, RelativePath string
	Files                  []string
}

func Discover(source string) ([]Discovery, error) {
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
		if IsAudio(path) {
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
	var out []Discovery
	for _, dir := range dirs {
		rel, _ := filepath.Rel(root, dir)
		id := sha256.Sum256([]byte(dir))
		out = append(out, Discovery{ID: hex.EncodeToString(id[:12]), Path: dir, RelativePath: rel, Files: groups[dir]})
	}
	return out, nil
}

func scan(source string, fingerprint func(string) (Evidence, error)) ([]Recording, error) {
	discovered, err := Discover(source)
	if err != nil {
		return nil, err
	}
	var out []Recording
	for _, d := range discovered {
		r, err := Validate(source, d, fingerprint)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Validate reads the contents of one discovered recording. Callers must not use
// cached fingerprints for review approval or media changes.
func Validate(source string, d Discovery, fingerprint func(string) (Evidence, error)) (Recording, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return Recording{}, err
	}
	return scanRecording(root, d.Path, fingerprint)
}

// ScanRecording freshly discovers and validates one recording, retaining paths
// relative to the original collection. Independent nested recordings are skipped.
func ScanRecording(source, dir string) (Recording, error) {
	return scanRecording(source, dir, Fingerprint)
}
func scanRecording(source, dir string, fingerprint func(string) (Evidence, error)) (Recording, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return Recording{}, err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return Recording{}, err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Recording{}, fmt.Errorf("recording is outside source")
	}
	var files []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if path != dir && !discDir.MatchString(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasPrefix(d.Name(), ReservedFilePrefix) && IsAudio(path) {
			owner := filepath.Dir(path)
			if owner != root && discDir.MatchString(filepath.Base(owner)) {
				owner = filepath.Dir(owner)
			}
			if owner == dir {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		return Recording{}, err
	}
	if len(files) == 0 {
		return Recording{}, fmt.Errorf("recording disappeared")
	}
	return build(root, dir, files, fingerprint)
}
func reason(r *Recording, s string) {
	for _, v := range r.ReviewReasons {
		if v == s {
			return
		}
	}
	r.ReviewReasons = append(r.ReviewReasons, s)
}
func build(root, dir string, files []string, fingerprint func(string) (Evidence, error)) (Recording, error) {
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
	listedTitles := 0
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
			listedTitles += parseNotes(&r, s, titles, dates, artists)
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
		e, err := fingerprint(p)
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
		if !CanTag(p) {
			r.PlaybackOnly = true
		}
		d, n := fileNumber(p)
		key := fmt.Sprintf("%d:%d", d, n)
		rp, _ := filepath.Rel(root, p)
		t := Track{Path: p, RelativePath: rp, Disc: d, Number: n}
		// Zero means the filename has no recognized identifier, never track zero.
		if n > 0 {
			t.Title = titles[key]
		}
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
	if listedTitles != len(files) {
		reason(&r, fmt.Sprintf("track count mismatch: %d listed titles, %d audio files", listedTitles, len(files)))
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
		if r.Album != "" {
			r.Album += " - "
		}
		r.Album += r.Venue
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
	// A date immediately followed by tNN is a common taper convention. Only
	// accept a valid complete date and a terminal track token, not arbitrary
	// embedded tNN fragments in descriptive names.
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if m := datedFileDT.FindStringSubmatch(stem); m != nil {
		if _, err := time.Parse("2006-01-02", m[1]); err == nil {
			disc, _ := strconv.Atoi(m[2])
			n, _ := strconv.Atoi(m[3])
			return disc, n
		}
	}
	if m := datedFileT.FindStringSubmatch(stem); m != nil {
		if _, err := time.Parse("2006-01-02", m[1]); err == nil {
			n, _ := strconv.Atoi(m[2])
			return d, n
		}
	}
	if m := leadingT.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return d, n
	}
	// A letter-only recording prefix with a terminal, padded number is an
	// identifier such as ericburdon-01. Exclude dates and descriptive names
	// with additional numeric tokens rather than interpreting their last number.
	if m := suffixedT.FindStringSubmatch(stem); m != nil {
		n, _ := strconv.Atoi(m[1])
		return d, n
	}
	return d, 0
}

// Recognize complete artist/date/city @ venue or artist/venue/city/date
// headers. A lone first line could instead be a recording or transfer label.
func parseUnlabelledHeader(r *Recording, source []noteLine, artists map[string]bool) {
	var lines []string
	for _, item := range source {
		raw := item.text
		line := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if line != "" {
			lines = append(lines, line)
			if len(lines) == 4 {
				break
			}
		}
	}
	if len(lines) < 3 || technical(strings.ToLower(lines[0])) || strings.ContainsAny(lines[0], ":@>") || noteT.MatchString(lines[0]) || noteSpaceT.MatchString(lines[0]) || noteDT.MatchString(lines[0]) || notesSection.MatchString(lines[0]) {
		return
	}
	// A second common taper header has separate artist, venue, city and
	// date lines. An ambiguous numeric date still identifies the structure;
	// parseDates keeps that ambiguity for review without discarding the artist.
	if len(lines) == 4 && (isoDate.FindString(lines[3]) == lines[3] || numericDate.FindString(lines[3]) == lines[3] || namedDate.FindString(lines[3]) == lines[3]) && strings.Contains(lines[2], ",") {
		venue := strings.TrimSpace(strings.TrimSuffix(lines[1], ","))
		city := lines[2]
		if venue != "" && !strings.ContainsAny(venue+city, ":@>") && !technical(strings.ToLower(venue)) && !noteT.MatchString(venue) && !noteSpaceT.MatchString(venue) {
			artists[lines[0]] = true
			setLocation(r, "venue", &r.Venue, venue)
			setLocation(r, "city", &r.City, city)
			return
		}
	}
	date := weekdayPrefix.ReplaceAllString(lines[1], "")
	if isoDate.FindString(date) != date && numericDate.FindString(date) != date && namedDate.FindString(date) != date {
		return
	}
	parsed := map[string]bool{}
	probe := Recording{}
	parseDates(&probe, date, parsed)
	if len(parsed) != 1 || len(probe.ReviewReasons) != 0 || strings.Count(lines[2], " @ ") != 1 {
		return
	}
	location := strings.SplitN(lines[2], " @ ", 2)
	city, venue := strings.TrimSpace(location[0]), strings.TrimSpace(location[1])
	if city == "" || venue == "" || strings.ContainsAny(city+venue, ":>") {
		return
	}
	artists[lines[0]] = true
	setLocation(r, "city", &r.City, city)
	setLocation(r, "venue", &r.Venue, venue)
}

func setLocation(r *Recording, field string, target *string, value string) {
	if *target != "" && value != "" && *target != value {
		reason(r, "conflicting "+field+" in concert notes")
	}
	if value != "" {
		*target = value
	}
}

func addTitle(r *Recording, titles map[string]string, d, n int, title string) {
	if otherPerformer.MatchString(title) {
		reason(r, "possible other performer in track "+fmt.Sprintf("%d:%d", d, n)+": "+strings.TrimSpace(title))
	}
	key := fmt.Sprintf("%d:%d", d, n)
	if _, ok := titles[key]; ok {
		reason(r, "duplicate numbering in notes: "+key)
		// Neither title has stronger evidence. Leave the mapping unresolved.
		titles[key] = ""
		return
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
		v = ordinalDay.ReplaceAllString(v, "${1}${2}")
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
	f, err := os.Open(path)
	if err != nil {
		reason(r, "cannot read checksum manifest")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		reason(r, "cannot read checksum manifest")
		return
	}
	if len(b) > 4<<20 {
		reason(r, "checksum manifest exceeds 4 MiB")
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
		artist := t.Artist
		if artist == "" {
			artist = r.Artist
		}
		t.Tags = map[string]string{"ARTIST": artist, "ALBUMARTIST": r.Artist, "ALBUM": r.Album, "DATE": r.Date, "TITLE": t.Title, "TRACKNUMBER": strconv.Itoa(t.Number), "TRACKTOTAL": strconv.Itoa(t.TrackTotal), "DISCNUMBER": strconv.Itoa(t.Disc), "DISCTOTAL": strconv.Itoa(t.DiscTotal)}
		if r.DateUnknown {
			delete(t.Tags, "DATE")
		}
	}
}
func Fingerprint(path string) (Evidence, error) {
	return FingerprintContext(context.Background(), path)
}

type contextReader struct {
	context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func FingerprintContext(ctx context.Context, path string) (Evidence, error) {
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
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
	if _, err = io.Copy(h, contextReader{ctx, f}); err != nil {
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
