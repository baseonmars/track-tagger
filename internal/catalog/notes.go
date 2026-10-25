package catalog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type noteKind uint8

const (
	noteText noteKind = iota
	noteBlank
	noteSection
	noteTracklist
	noteTechnicalSection
	noteTechnicalRow
	noteEntry
	noteExplicitEntry
	noteEncore
	noteAmbiguousSection
	noteHeading
)

type noteLine struct {
	text         string
	line, indent int
	kind         noteKind
	disc, number int
	title        string
}

var notesSection = regexp.MustCompile(`(?i)^-*(?:disc|disk|cd|set)\s*(\d+|one|two|three|four|five|six|seven|eight|nine|ten|I|II|III|IV|V|VI|VII|VIII|IX|X)\s*(?:[:.-]|\(\d+:\d{2}\)|-\s*\d+:\d{2})?\s*-*$`)
var notesSectionStart = regexp.MustCompile(`(?i)^-*(?:disc|disk|cd|set)\b`)
var notesTrailingTime = regexp.MustCompile(`\s+\(?\d+:\d{2}(?:\.\d+)?\)?$`)
var notesChecksum = regexp.MustCompile(`(?i)^(?:.+\.[a-z0-9]{1,8}:\s*(?:[a-f0-9]{32}|[a-f0-9]{40}|[a-f0-9]{64})|(?:[a-f0-9]{32}|[a-f0-9]{40}|[a-f0-9]{64})\s+\*?.+\.[a-z0-9]{1,8}|.+\.[a-z0-9]{1,8}\s+[a-f0-9]{8})$`)

func classifyNoteLine(raw string, number int) noteLine {
	line := noteLine{line: number}
	raw = strings.TrimPrefix(raw, "\ufeff")
	line.indent = len(raw) - len(strings.TrimLeft(raw, " \t"))
	line.text = strings.TrimSpace(raw)
	lower := strings.ToLower(line.text)
	switch {
	case line.text == "":
		line.kind = noteBlank
	case notesSection.MatchString(line.text):
		line.kind = noteSection
		token := notesSection.FindStringSubmatch(line.text)[1]
		line.disc, _ = strconv.Atoi(token)
		if line.disc == 0 {
			line.disc = noteDiscToken(token)
		}
	case notesSectionStart.MatchString(line.text):
		line.kind = noteAmbiguousSection
	case lower == "tracklist" || lower == "tracklist:" || lower == "track list:" || lower == "setlist:" || lower == "setlist":
		line.kind = noteTracklist
	case lower == "encore" || lower == "encore:":
		line.kind = noteEncore
	case technicalSection(lower):
		line.kind = noteTechnicalSection
	case technical(lower) || reportDuration.MatchString(line.text) || notesChecksum.MatchString(line.text):
		line.kind = noteTechnicalRow
	case strings.HasPrefix(lower, "artist:") || strings.HasPrefix(lower, "performer:") || strings.HasPrefix(lower, "band:") || strings.HasPrefix(lower, "venue:") || strings.HasPrefix(lower, "city:") || strings.HasPrefix(lower, "date:") || strings.HasPrefix(lower, "concert date:"):
		line.kind = noteHeading
	default:
		if m := noteDT.FindStringSubmatch(line.text); m != nil {
			line.kind = noteExplicitEntry
			line.disc, _ = strconv.Atoi(m[1])
			line.number, _ = strconv.Atoi(m[2])
			line.title = m[3]
			return line
		}
		m := noteT.FindStringSubmatch(line.text)
		if m == nil {
			m = noteSpaceT.FindStringSubmatch(line.text)
		}
		if m != nil {
			line.kind = noteEntry
			line.number, _ = strconv.Atoi(m[1])
			line.title = m[2]
		} else if strings.HasSuffix(lower, ":") {
			line.kind = noteHeading
		}
	}
	return line
}

// noteDiscToken maps word and Roman-numeral disc labels such as "three" or
// "IV" to their ordinal. Zero means the label is not recognized.
func noteDiscToken(token string) int {
	for i, v := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"} {
		if strings.EqualFold(token, v) {
			return i + 1
		}
	}
	for i, v := range []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"} {
		if strings.EqualFold(token, v) {
			return i + 1
		}
	}
	return 0
}

func technicalSection(s string) bool {
	s = strings.TrimSpace(strings.TrimSuffix(s, ":"))
	if s != "" && labelledTransfer.FindString(s) == s {
		return true
	}
	for _, heading := range []string{"source", "transfer", "transfer notes", "production notes", "mastering notes", "editing notes", "lineage", "recording lineage", "checksums", "checksum", "md5", "ffp", "sha1", "sha-1", "sha256", "sha-256", "sfv", "flac fingerprints", "shntool output", "shntool", "duration report", "durations", "track durations"} {
		if s == heading {
			return true
		}
	}
	return strings.HasPrefix(s, "length expanded size") || strings.HasPrefix(s, "length     expanded size")
}

func noteSegue(s string) bool {
	s = strings.TrimSpace(notesTrailingTime.ReplaceAllString(s, ""))
	return strings.HasSuffix(s, ">") || strings.HasSuffix(s, "→")
}

func parseNotes(r *Recording, s string, titles map[string]string, dates, artists map[string]bool) int {
	var candidates noteDateCandidates
	count := parseNotesCandidates(r, s, titles, &candidates, artists)
	candidates.resolve(r, dates, nil)
	return count
}

func parseNotesCandidates(r *Recording, s string, titles map[string]string, dates *noteDateCandidates, artists map[string]bool) int {
	// Preserve line boundaries and indentation; header inference looks at
	// the first five nonempty lines of this same classified representation.
	lines := make([]noteLine, 0, strings.Count(s, "\n")+1)
	for number, raw := range strings.Split(s, "\n") {
		lines = append(lines, classifyNoteLine(raw, number+1))
	}
	parseUnlabelledHeader(r, lines, artists)
	disc, count := 1, 0
	header, blocked := true, false
	continuationReview, sectionReview := false, false
	var pending *noteLine
	var parts []string
	flush := func() {
		if pending != nil {
			count++
			addTitle(r, titles, pending.disc, pending.number, strings.Join(parts, " "))
			pending = nil
			parts = nil
		}
	}
	for i := range lines {
		line := &lines[i]
		lower := strings.ToLower(line.text)
		// Date-only lines can resemble whitespace-numbered tracks. After the
		// opening header, require another track block so trailing production
		// dates cannot become concert metadata. Numbered song titles never qualify.
		if !blocked && standaloneNoteDate(line.text) {
			flush()
			if header || concertTracksFollow(lines[i+1:]) {
				dates.collect(line.text)
			}
			continue
		}
		switch line.kind {
		case noteBlank:
			flush()
			continue
		case noteSection:
			flush()
			disc = line.disc
			header = false
			blocked = false
			continue
		case noteTracklist:
			flush()
			header = false
			blocked = false
			continue
		case noteTechnicalSection:
			flush()
			blocked = true
			continue
		case noteTechnicalRow:
			flush()
			continue
		case noteEncore:
			flush()
			continue
		case noteAmbiguousSection:
			flush()
			if !sectionReview {
				reason(r, fmt.Sprintf("ambiguous notes section at line %d: %s", line.line, line.text))
				sectionReview = true
			}
			continue
		case noteHeading:
			flush()
		case noteExplicitEntry:
			blocked = false
			disc = line.disc
		case noteEntry:
			if blocked {
				continue
			}
		}
		if line.kind == noteEntry || line.kind == noteExplicitEntry {
			flush()
			copy := *line
			copy.disc = disc
			pending = &copy
			parts = []string{line.title}
			header = false
			continue
		}
		if !blocked && pending != nil && line.indent > 0 {
			if line.indent > pending.indent && noteSegue(parts[len(parts)-1]) {
				parts = append(parts, line.text)
				continue
			}
			if !continuationReview {
				reason(r, fmt.Sprintf("ambiguous title continuation at line %d: %s", line.line, line.text))
				continuationReview = true
			}
		}
		flush()
		if blocked {
			continue
		}
		for _, label := range []string{"artist:", "performer:", "band:"} {
			if strings.HasPrefix(lower, label) {
				a := strings.TrimSpace(line.text[len(label):])
				if a != "" {
					artists[a] = true
				}
			}
		}
		if strings.HasPrefix(lower, "venue:") {
			setLocation(r, "venue", &r.Venue, strings.TrimSpace(line.text[6:]))
		}
		if strings.HasPrefix(lower, "city:") {
			setLocation(r, "city", &r.City, strings.TrimSpace(line.text[5:]))
		}
		if header || strings.HasPrefix(lower, "date:") || strings.HasPrefix(lower, "concert date:") {
			dates.collect(line.text)
		}
	}
	flush()
	return count
}

func standaloneNoteDate(text string) bool {
	text = weekdayPrefix.ReplaceAllString(text, "")
	for _, pattern := range []*regexp.Regexp{isoDate, numericDate, namedDate, unknownDayDate} {
		date := pattern.FindString(text)
		if date != "" && strings.HasPrefix(text, date) && (date == text || weekdaySuffix.MatchString(strings.TrimPrefix(text, date))) {
			return true
		}
	}
	return false
}

func concertTracksFollow(lines []noteLine) bool {
	for _, line := range lines {
		switch line.kind {
		case noteBlank:
			continue
		case noteSection, noteTracklist, noteEncore:
			continue
		case noteEntry, noteExplicitEntry:
			return !standaloneNoteDate(line.text)
		default:
			// Decorative separators do not turn production prose into a header.
			if strings.Trim(line.text, "-=*_ ") == "" {
				continue
			}
			return false
		}
	}
	return false
}
