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

var gnuMD5 = regexp.MustCompile(`(?i)^([0-9a-f]{32})[ \t][ *]?(.+)$`)
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
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
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
		name = filepath.FromSlash(name)
		if !filepath.IsLocal(name) || strings.Contains("/"+filepath.ToSlash(name)+"/", "/../") {
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
		if !t.Skip {
			tracks[t.Path] = t
		}
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

// planMD5 selects manifests against pre-tag evidence. A later source edit is
// rejected by the same full-recording validation used for audio replacements.
func planMD5(item Item) ([]string, error) {
	if !item.CleanupMD5 {
		return nil, nil
	}
	if !item.Reviewed {
		return nil, fmt.Errorf("MD5 cleanup requires an approved review")
	}
	decisions, err := assessMD5(item)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, d := range decisions {
		if d.Remove {
			paths = append(paths, d.Path)
		}
	}
	return paths, nil
}

func cleanupLine(root, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil {
		name = filepath.Base(path)
	}
	return "Removed " + filepath.ToSlash(name) + " after tagging changed the file checksums."
}

func (e *Engine) removeMD5(job string, item *Item, paths []string, skipped map[string]string) error {
	for _, src := range paths {
		dest, err := e.destination(src)
		if err != nil {
			return err
		}
		var expected string
		for _, ev := range item.Evidence {
			if ev.Path == src {
				expected = ev.SHA256
				break
			}
		}
		if err = e.commitCompanion(job, item, src, dest, expected, nil, true); err != nil {
			return err
		}
		delete(skipped, dest)
		// Make partial work visible in History even if a later note or file fails.
		if _, err = e.store.db.Exec("UPDATE jobs SET summary=summary || ' ' || ? WHERE id=?", cleanupLine(item.Path, src), job); err != nil {
			return err
		}
	}
	return nil
}
