package app

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"track-tagger/internal/catalog"
	"track-tagger/internal/media"
)

var gnuMD5 = regexp.MustCompile(`(?i)^([0-9a-f]{32})[ \t]+[* ]?(.+)$`)
var bsdMD5 = regexp.MustCompile(`(?i)^MD5 \((.+)\) = ([0-9a-f]{32})$`)

type manifestDecision struct {
	Path   string
	Remove bool
	Reason string
}

type trackChecksum struct {
	md5     string
	changes bool
}

// classifyMD5 accepts only complete, valid whole-file manifests. Paths in
// manifests are matched against discovered FLACs and never opened directly.
func classifyMD5(path string, lookup func(string) (trackChecksum, error)) (manifestDecision, error) {
	d := manifestDecision{Path: path}
	file, err := os.Open(path)
	if err != nil {
		return d, err
	}
	defer file.Close()
	const limit = 4 << 20
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return d, err
	}
	if len(b) > limit {
		d.Reason = "Preserve oversized manifest."
		return d, nil
	}
	changed, other := 0, 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		var name, sum string
		if m := gnuMD5.FindStringSubmatch(line); m != nil {
			sum, name = m[1], m[2]
		} else if m := bsdMD5.FindStringSubmatch(line); m != nil {
			name, sum = m[1], m[2]
		} else {
			other++
			continue
		}
		name = filepath.FromSlash(strings.ReplaceAll(name, "\\", "/"))
		if !filepath.IsLocal(name) {
			other++
			continue
		}
		c, err := lookup(filepath.Join(filepath.Dir(path), name))
		if err != nil {
			return d, err
		}
		if c.changes && strings.EqualFold(sum, c.md5) {
			changed++
		} else {
			other++
		}
	}
	switch {
	case changed > 0 && other == 0:
		d.Remove = true
		d.Reason = "Tagging changes the whole-file checksums of all listed FLACs."
	case changed > 0:
		d.Reason = "Preserve mixed manifest containing affected and unaffected or unrecognized entries."
	default:
		d.Reason = "Preserve manifest. No matching checksum will become stale."
	}
	return d, nil
}

func assessMD5(item Item) ([]manifestDecision, error) {
	tracks := make(map[string]catalog.Track, len(item.Tracks))
	for _, t := range item.Tracks {
		tracks[t.Path] = t
	}
	cache := map[string]trackChecksum{}
	lookup := func(path string) (trackChecksum, error) {
		t, ok := tracks[path]
		if !ok {
			return trackChecksum{}, nil
		}
		if c, ok := cache[path]; ok {
			return c, nil
		}
		info, err := media.InspectMetadata(path)
		if err != nil {
			return trackChecksum{}, err
		}
		changed, err := media.MetadataWillChange(info, t.Tags, item.Artwork, item.Reviewed)
		if err != nil {
			return trackChecksum{}, err
		}
		c := trackChecksum{changes: changed}
		if changed {
			f, err := os.Open(path)
			if err != nil {
				return c, err
			}
			h := md5.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return c, err
			}
			c.md5 = hex.EncodeToString(h.Sum(nil))
		}
		cache[path] = c
		return c, nil
	}
	var results []manifestDecision
	for _, ev := range item.Evidence {
		if !strings.EqualFold(filepath.Ext(ev.Path), ".md5") {
			continue
		}
		d, err := classifyMD5(ev.Path, lookup)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", ev.Path, err)
		}
		results = append(results, d)
	}
	return results, nil
}
