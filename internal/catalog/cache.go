package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"track-tagger/internal/media"
)

// BrowseCache avoids reading unchanged payloads repeatedly while browsing. It is
// not an integrity check: Scan and ValidateEvidence always read current bytes.
// Callers serialize access. Entries for removed paths are pruned after each scan.
type BrowseCache struct{ entries map[string]cachedEvidence }
type cachedEvidence struct {
	info     os.FileInfo
	evidence Evidence
}

func (c *BrowseCache) Fingerprint(path string) (Evidence, error) {
	return c.FingerprintContext(context.Background(), path)
}
func (c *BrowseCache) FingerprintContext(ctx context.Context, path string) (Evidence, error) {
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
	if err := media.CheckLocal(path); err != nil {
		return Evidence{}, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return Evidence{}, err
	}
	if old, ok := c.entries[path]; ok && os.SameFile(old.info, st) && old.info.Size() == st.Size() && old.info.ModTime().Equal(st.ModTime()) {
		return old.evidence, nil
	}
	ev, err := FingerprintContext(ctx, path)
	if err != nil {
		return Evidence{}, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return Evidence{}, err
	}
	if os.SameFile(st, after) && after.Size() == ev.Size && after.ModTime().UnixNano() == ev.ModTime {
		if c.entries == nil {
			c.entries = map[string]cachedEvidence{}
		}
		c.entries[path] = cachedEvidence{after, ev}
	}
	return ev, nil
}

func ScanCached(source string, cache *BrowseCache) ([]Recording, error) {
	recordings, err := scan(source, cache.Fingerprint)
	if err != nil {
		return nil, err
	}
	cache.Retain(recordings)
	return recordings, nil
}

// Retain drops removed inputs after reconciliation, keeping failed recordings'
// entries until they can be inspected successfully.
func (c *BrowseCache) Retain(recordings []Recording) {
	live := map[string]bool{}
	var failed []string
	for _, r := range recordings {
		if r.Status == "failed" {
			failed = append(failed, r.Path)
		}
		for _, ev := range r.Evidence {
			live[ev.Path] = true
		}
	}
	for path := range c.entries {
		for _, dir := range failed {
			rel, err := filepath.Rel(dir, path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				live[path] = true
				break
			}
		}
		if !live[path] {
			delete(c.entries, path)
		}
	}
}
