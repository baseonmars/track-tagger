package catalog

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"track-tagger/internal/notesmodel"
)

// ErrStaleNoteSuggestion asks the review client to reload its source snapshot.
var ErrStaleNoteSuggestion = errors.New("inputs changed since this page was loaded; refresh and review again")

type NotePredictor interface {
	Predict(notes map[string][]string) ([]notesmodel.Span, error)
}

type NoteSuggestion struct {
	Field      string `json:"field"`
	Value      string `json:"value"`
	Current    string `json:"current"`
	Note       string `json:"note"`
	Line       int    `json:"line"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Quote      string `json:"quote"`
	Context    string `json:"context"`
	Album      string `json:"album,omitempty"`
	AlbumVenue string `json:"albumVenue,omitempty"`
	AlbumCity  string `json:"albumCity,omitempty"`
	AlbumSafe  bool   `json:"albumSafe,omitempty"`
}

var secondaryNoteScope = regexp.MustCompile(`(?i)\b(?:histor(?:y|ical)|previous(?:ly)?|recreation|re-?creat(?:e|ed)|opening for|opened for|opening act|support act|headliner|guest(?:s)?|featur(?:e|es|ing)|seem(?:s|ed)|suspect(?:ed)?|likely|maybe|unknown|unconfirmed|conflicting|not sure|front of house|soundboard|recorded by|taped by|mastering)\b`)

func scopedSuggestionLine(line string) bool {
	return !secondaryNoteScope.MatchString(line) && !locationUncertainty.MatchString(line)
}

func openingNoteEnd(lines []string) int {
	started := false
	count := 0
	for index, raw := range lines {
		line := classifyNoteLine(raw, index)
		if line.kind == noteBlank && !started {
			continue
		}
		if line.kind == noteBlank || count == 8 {
			return index
		}
		if line.kind != noteText && line.kind != noteHeading && !standaloneNoteDate(line.text) {
			return index
		}
		if strings.HasSuffix(line.text, ":") || !scopedSuggestionLine(line.text) {
			return index
		}
		started = true
		count++
	}
	return len(lines)
}

var suggestionFieldLabels = map[string][]string{
	"ARTIST": {"artist:", "performer:", "band:"},
	"DATE":   {"date:", "concert date:"},
	"VENUE":  {"venue:"},
	"CITY":   {"city:"},
}

func explicitSuggestionFields(line string) (string, string) {
	lower := strings.ToLower(line)
	for field, labels := range suggestionFieldLabels {
		for _, label := range labels {
			if strings.HasPrefix(lower, label) {
				return field, strings.TrimSpace(line[len(label):])
			}
		}
	}
	return "", ""
}

func suggestionDate(text string, filenames map[string]bool, policy DatePolicy, exact bool) (string, bool) {
	if len(filenames) > 1 || !scopedSuggestionLine(text) {
		return "", false
	}
	var candidates noteDateCandidates
	candidates.collect(text)
	if len(candidates) != 1 || (exact && candidates[0].text != text) {
		return "", false
	}
	var check Recording
	resolved := map[string]bool{}
	if !policy.resolve(candidates, &check, resolved, filenames) || len(check.ReviewReasons) > 0 || len(resolved) != 1 {
		return "", false
	}
	for date := range resolved {
		return date, len(filenames) == 0 || filenames[date] ||
			(len(date) < 10 && len(filenames) == 1 && matchingFilenamePrefix(date, filenames))
	}
	return "", false
}

func matchingFilenamePrefix(date string, filenames map[string]bool) bool {
	for candidate := range filenames {
		return strings.HasPrefix(candidate, date+"-")
	}
	return false
}

// GuardNoteSuggestions validates model spans against the full authorized source
// snapshot and returns only suggestions with current-recording evidence.
func GuardNoteSuggestions(recording Recording, notes map[string][]string, spans []notesmodel.Span, policy DatePolicy) []NoteSuggestion {
	audio := make([]string, 0, len(recording.Tracks))
	for _, track := range recording.Tracks {
		audio = append(audio, track.Path)
	}
	filenames := filenameDates(recording.Path, recording.Notes, audio)
	openings := make(map[string]int, len(notes))
	explicit := map[string][]string{}
	for note, lines := range notes {
		openings[note] = openingNoteEnd(lines)
		for index, raw := range lines {
			text := strings.TrimSpace(strings.TrimPrefix(raw, "\uFEFF"))
			field, value := explicitSuggestionFields(text)
			if field != "" && value != "" {
				explicit[field] = append(explicit[field], value)
			} else if index < openings[note] && standaloneNoteDate(text) {
				var candidates noteDateCandidates
				candidates.collect(text)
				for _, candidate := range candidates {
					explicit["DATE"] = append(explicit["DATE"], candidate.text)
				}
			}
		}
	}

	result := map[string]NoteSuggestion{}
	conflict := map[string]bool{}
	for _, span := range spans {
		field := strings.ToUpper(span.Field)
		lines, exists := notes[span.Note]
		if _, known := suggestionFieldLabels[field]; !known || !exists ||
			span.Line < 0 || span.Line >= openings[span.Note] {
			continue
		}
		raw := lines[span.Line]
		runes := []rune(raw)
		if span.Start < 0 || span.End <= span.Start || span.End > len(runes) {
			continue
		}
		quote := string(runes[span.Start:span.End])
		text := strings.TrimSpace(strings.TrimPrefix(raw, "\uFEFF"))
		if strings.TrimSpace(quote) != quote || len(runes) > 512 || !scopedSuggestionLine(text) ||
			!suggestionTokenBoundary(runes, span.Start) || !suggestionTokenBoundary(runes, span.End) {
			continue
		}
		if explicitField, _ := explicitSuggestionFields(text); explicitField != "" && explicitField != field {
			continue
		}
		value := quote
		if field == "DATE" {
			var ok bool
			value, ok = suggestionDate(value, filenames, policy, true)
			if !ok {
				continue
			}
		} else if strings.IndexFunc(value, unicode.IsLetter) < 0 || containsNoteDate(value) || strings.ContainsAny(value, ":@<>") ||
			(field == "VENUE" && explicitCityRegion.MatchString(value)) {
			continue
		}
		for _, other := range explicit[field] {
			if field == "DATE" {
				var ok bool
				other, ok = suggestionDate(other, filenames, policy, false)
				if !ok {
					conflict[field] = true
					continue
				}
			}
			if !strings.EqualFold(value, other) {
				conflict[field] = true
			}
		}
		candidate := NoteSuggestion{Field: field, Value: value, Note: span.Note, Line: span.Line,
			Start: span.Start, End: span.End, Quote: quote, Context: raw}
		switch field {
		case "ARTIST":
			candidate.Current = recording.Artist
		case "DATE":
			candidate.Current = recording.Date
			candidate.AlbumVenue, candidate.AlbumCity = recording.Venue, recording.City
			candidate.AlbumSafe = recording.Album == concertAlbum(recording.Date, recording.Venue, recording.City)
			candidate.Album = concertAlbum(value, recording.Venue, recording.City)
		case "VENUE", "CITY":
			candidate.Current = recording.Album
			candidate.AlbumVenue, candidate.AlbumCity = recording.Venue, recording.City
			candidate.AlbumSafe = recording.Album == concertAlbum(recording.Date, recording.Venue, recording.City)
			venue, city := recording.Venue, recording.City
			if field == "VENUE" {
				venue = value
			} else {
				city = value
			}
			candidate.Album = concertAlbum(recording.Date, venue, city)
		}
		if previous, ok := result[field]; ok {
			if !strings.EqualFold(previous.Value, candidate.Value) {
				conflict[field] = true
			}
			continue
		}
		result[field] = candidate
	}

	for field, candidate := range result {
		for otherField, other := range result {
			if field != otherField && candidate.Note == other.Note && candidate.Line == other.Line &&
				candidate.Start < other.End && other.Start < candidate.End {
				conflict[field], conflict[otherField] = true, true
			}
		}
	}

	out := make([]NoteSuggestion, 0, len(result))
	for field, candidate := range result {
		if conflict[field] || guardedReviewReason(recording.ReviewReasons, field) {
			continue
		}
		if field == "VENUE" && strings.EqualFold(recording.Venue, candidate.Value) ||
			field == "CITY" && strings.EqualFold(recording.City, candidate.Value) ||
			(field == "ARTIST" || field == "DATE") && strings.EqualFold(candidate.Current, candidate.Value) {
			continue
		}
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

func suggestionTokenBoundary(runes []rune, index int) bool {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }
	return index == 0 || index == len(runes) || !word(runes[index-1]) || !word(runes[index])
}

func guardedReviewReason(reasons []string, field string) bool {
	for _, reason := range reasons {
		switch field {
		case "ARTIST":
			if strings.HasPrefix(reason, "mixed performers or conflicting artist names") {
				return true
			}
		case "DATE":
			if strings.HasPrefix(reason, "conflicting concert dates") ||
				strings.HasPrefix(reason, "conflicting filename evidence for concert date") {
				return true
			}
		case "VENUE", "CITY":
			location := strings.ToLower(field)
			if strings.HasPrefix(reason, "conflicting "+location+" in concert notes") ||
				strings.HasPrefix(reason, "uncertain "+location+" in concert notes") {
				return true
			}
		}
	}
	return false
}

func concertAlbum(date, venue, city string) string {
	if venue == "" {
		return date
	}
	if date != "" {
		date += " - "
	}
	date += venue
	if city != "" {
		date += ", " + city
	}
	return date
}

// CheckSuggestionMembership verifies the scoped notes and audio filename
// inventory without opening the audio files. Approval still revalidates all content.
func CheckSuggestionMembership(ctx context.Context, root string, recording Recording) error {
	evidence := make(map[string]Evidence, len(recording.Evidence))
	for _, item := range recording.Evidence {
		evidence[item.Path] = item
	}
	expected := make(map[string]bool, len(recording.Notes)+len(recording.Tracks))
	for _, path := range recording.Notes {
		expected[path] = true
	}
	for _, track := range recording.Tracks {
		expected[track.Path] = true
	}
	err := filepath.WalkDir(recording.Path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != recording.Path && !discDir.MatchString(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ReservedFilePrefix) || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		isNote := ext == ".txt" || ext == ".nfo"
		owner := filepath.Dir(path)
		if owner != root && discDir.MatchString(filepath.Base(owner)) {
			owner = filepath.Dir(owner)
		}
		if !isNote && (!IsAudio(path) || owner != recording.Path) {
			return nil
		}
		cached, known := evidence[path]
		if !expected[path] || !known || !entry.Type().IsRegular() {
			return ErrStaleNoteSuggestion
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != cached.Size || info.ModTime().UnixNano() != cached.ModTime {
			return ErrStaleNoteSuggestion
		}
		delete(expected, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(expected) != 0 {
		return ErrStaleNoteSuggestion
	}
	return nil
}

// SuggestionNoteName converts a scoped path to the note name used by the model.
func SuggestionNoteName(recording, note string) (string, error) {
	name, err := filepath.Rel(recording, note)
	if err != nil || name == "." || !filepath.IsLocal(name) {
		return "", errors.New("note is outside the recording")
	}
	return filepath.ToSlash(name), nil
}
