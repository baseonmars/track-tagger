package webui

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
)

// Format display paths portably; form submissions retain the original paths.
func displayBase(p string) string {
	return path.Base(strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/"))
}
func recordingTitle(r Recording) string {
	artist, album := r.Metadata["ARTIST"], r.Metadata["ALBUM"]
	if artist != "" && album != "" {
		return artist + " · " + album
	}
	if album != "" {
		return album
	}
	if artist != "" && r.Metadata["DATE"] != "" {
		return artist + " · " + r.Metadata["DATE"]
	}
	return displayBase(r.Path)
}

// googleSearchURL includes only bounded descriptive metadata. The recording
// path is reduced to its basename and used only when artist and album are both
// absent; no request is made unless the user follows the returned link.
func googleSearchURL(r Recording) string {
	artist := searchTerm(r.Metadata["ARTIST"])
	album := searchTerm(r.Metadata["ALBUM"])
	query := strings.TrimSpace(strings.Join([]string{artist, album}, " "))
	if query == "" {
		query = searchTerm(displayBase(r.Path))
	}
	query = truncateRunes(query, 240)
	if query == "" {
		return ""
	}
	return "https://www.google.com/search?q=" + url.QueryEscape(query)
}

func searchTerm(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func statusLabel(s string) string {
	switch s {
	case "needs-review":
		return "Needs review"
	case "ready":
		return "Ready"
	case "waiting":
		return "Waiting for files"
	case "loading", "validating":
		return "Checking files"
	case "completed":
		return "Completed"
	case "failed":
		return "Could not finish"
	}
	return s
}
func recordingAction(r Recording) string {
	if r.PlaybackOnly {
		return "Listen to recording"
	}
	if r.Status == "needs-review" || r.Status == "failed" {
		return "Review recording"
	}
	return "View preview"
}

// Summarize known actionable categories, deduplicating overlapping diagnostics
// for the same file. Raw evidence remains available on the recording page.
func issueSummary(r Recording) []string {
	if r.PlaybackOnly {
		return []string{"Listening available. Tag editing currently supports FLAC recordings only."}
	}
	var out []string
	unmatched := map[string]bool{}
	others := map[string]bool{}
	for _, reason := range r.Reasons {
		found := false
		for _, prefix := range []string{"unmatched file without explicit track number: ", "unmatched audio file: "} {
			if strings.HasPrefix(reason, prefix) {
				unmatched[strings.TrimPrefix(reason, prefix)] = true
				found = true
				break
			}
		}
		if found {
			continue
		}
		var label string
		switch {
		case strings.HasPrefix(reason, "possible other performer in track "):
			identifier, _, _ := strings.Cut(strings.TrimPrefix(reason, "possible other performer in track "), ": ")
			disc, track, ok := strings.Cut(identifier, ":")
			if ok {
				label = "Track " + track + " (disc " + disc + "): check the performer; enter a track artist if different."
			} else {
				label = "Check the performer on track " + identifier + "; enter a track artist if different."
			}
		case strings.Contains(reason, "mixed performers"):
			label = "Check performers against the notes; enter a track artist where different."
		case reason == "artist is missing":
			label = "Add the artist name."
		case reason == "venue is missing":
			label = "Check the venue and album name."
		case strings.HasPrefix(reason, "ambiguous numeric concert date: "):
			if r.Metadata["DATE"] != "" {
				label = "The proposed concert date uses month/day order. Check the notes and correct the date if needed."
			} else {
				label = "The numeric concert date could use month/day or day/month order. Check the notes and enter the date."
			}
		case reason == "conflicting filename evidence for concert date":
			if r.Metadata["DATE"] != "" {
				label = "Filename dates disagree. Check the proposed concert date against the notes and audio filenames."
			} else {
				label = "Filename dates disagree. Check the notes and audio filenames, then enter the concert date."
			}
		case strings.Contains(reason, "date"):
			label = "Confirm the concert date against the notes."
		case strings.Contains(reason, "existing") && strings.Contains(reason, "conflicts"):
			label = "Review conflicts with existing tags."
		case strings.Contains(reason, "artwork"):
			label = "Choose the artwork to use."
		case strings.Contains(reason, "track count mismatch"), strings.Contains(reason, "listed track has no audio file"):
			label = "Check the track list against the available audio files."
		case strings.Contains(reason, "duplicate"):
			label = "Resolve duplicate track numbers."
		case reason == "no local concert notes":
			label = "Add local concert notes or enter the details below."
		case strings.Contains(reason, "stale"):
			label = "Files have changed; check your saved review again."
		default:
			label = "Check the source diagnostics before approving."
		}
		if !others[label] {
			out = append(out, label)
			others[label] = true
		}
	}
	if len(unmatched) > 0 {
		noun := "files need"
		if len(unmatched) == 1 {
			noun = "file needs"
		}
		out = append(out, fmt.Sprintf("%d audio %s a track title or number confirmed.", len(unmatched), noun))
	}
	if len(out) == 0 {
		switch r.Status {
		case "ready":
			out = append(out, "Tags are ready to preview.")
		case "waiting":
			out = append(out, "Waiting for local files and two minutes without changes.")
		case "completed":
			out = append(out, "Metadata has been applied.")
		case "failed":
			out = append(out, "Open the recording to check what went wrong.")
		}
	}
	return out
}

func performerReview(r Recording, t Track) bool {
	prefix := "possible other performer in track " + strconv.Itoa(t.Disc) + ":" + strconv.Itoa(t.Track) + ": "
	for _, reason := range r.Reasons {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}
