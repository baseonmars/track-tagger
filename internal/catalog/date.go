package catalog

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var concertDate = regexp.MustCompile(`^[0-9]{4}(?:-[0-9]{2}(?:-[0-9]{2})?)?$`)
var unknownDayDate = regexp.MustCompile(`(?i)\b(\d{1,2})[/-]xx[/-](\d{4})\b`)
var basenameISODate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

type noteDateCandidate struct {
	text    string
	options []string
}

// Keep alternatives until every notes file has been read. Filename evidence
// can select an interpretation, but never supplies a concert date by itself.
type noteDateCandidates []noteDateCandidate

func (c *noteDateCandidates) collect(line string) {
	add := func(text string, options ...string) {
		candidate := noteDateCandidate{text: text}
		for _, date := range options {
			if ValidateDate(date) == nil && (len(candidate.options) == 0 || candidate.options[0] != date) {
				candidate.options = append(candidate.options, date)
			}
		}
		*c = append(*c, candidate)
	}
	for _, m := range isoDate.FindAllStringSubmatch(line, -1) {
		y, _ := strconv.Atoi(m[1])
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		add(m[0], fmt.Sprintf("%04d-%02d-%02d", y, month, day))
	}
	for _, m := range numericDate.FindAllStringSubmatch(line, -1) {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		add(m[0], fmt.Sprintf("%04d-%02d-%02d", y, a, b), fmt.Sprintf("%04d-%02d-%02d", y, b, a))
	}
	for _, m := range unknownDayDate.FindAllStringSubmatch(line, -1) {
		month, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		add(m[0], fmt.Sprintf("%04d-%02d", y, month))
	}
	for _, text := range namedDate.FindAllString(line, -1) {
		v := ordinalDay.ReplaceAllString(text, "${1}${2}")
		date := ""
		for _, layout := range []string{"2 January 2006", "2 Jan 2006", "January 2, 2006", "Jan 2, 2006", "January 2 2006", "Jan 2 2006"} {
			if t, err := time.Parse(layout, v); err == nil {
				date = t.Format("2006-01-02")
				break
			}
		}
		add(text, date)
	}
}

func (c noteDateCandidates) resolve(r *Recording, dates, filenames map[string]bool) bool {
	resolved := true
	for _, candidate := range c {
		switch len(candidate.options) {
		case 0:
			reason(r, "invalid concert date: "+candidate.text)
			resolved = false
		case 1:
			dates[candidate.options[0]] = true
		default:
			match := ""
			matches := 0
			for _, date := range candidate.options {
				if filenames[date] {
					match = date
					matches++
				}
			}
			if matches == 1 {
				dates[match] = true
				if len(filenames) > 1 {
					reason(r, "conflicting filename evidence for concert date")
				}
				continue
			}
			// Keep the month-first interpretation as a reviewable suggestion.
			// Filename evidence alone cannot settle a tie or supply a date.
			dates[candidate.options[0]] = true
			reason(r, "ambiguous numeric concert date: "+candidate.text)
			if len(filenames) > 0 {
				reason(r, "conflicting filename evidence for concert date")
			}
		}
	}
	return resolved
}

func filenameDates(dir string, notes, audio []string) map[string]bool {
	dates := map[string]bool{}
	names := []string{filepath.Base(dir)}
	for _, paths := range [][]string{notes, audio} {
		for _, path := range paths {
			names = append(names, filepath.Base(path))
		}
	}
	for _, name := range names {
		for _, span := range basenameISODate.FindAllStringIndex(name, -1) {
			// Taper names often join the artist/date/track identifier without
			// punctuation. Only adjacent digits invalidate an ISO date token.
			prev := span[0] > 0 && name[span[0]-1] >= '0' && name[span[0]-1] <= '9'
			next := span[1] < len(name) && name[span[1]] >= '0' && name[span[1]] <= '9'
			if prev || next {
				continue
			}
			date := name[span[0]:span[1]]
			if ValidateDate(date) == nil {
				dates[date] = true
			}
		}
	}
	return dates
}

// ValidateDate accepts a concert date without inventing missing months or days.
func ValidateDate(date string) error {
	if !concertDate.MatchString(date) || strings.HasPrefix(date, "0000") {
		return errors.New("date must be YYYY, YYYY-MM, or YYYY-MM-DD (year 0001–9999)")
	}
	layout := map[int]string{4: "2006", 7: "2006-01", 10: "2006-01-02"}[len(date)]
	if _, err := time.Parse(layout, date); err != nil {
		return errors.New("date must be a valid YYYY, YYYY-MM, or YYYY-MM-DD")
	}
	return nil
}
