package catalog

import (
	"os"
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
	ev, err := Fingerprint(path)
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
	live := map[string]bool{}
	for _, r := range recordings {
		for _, ev := range r.Evidence {
			live[ev.Path] = true
		}
	}
	for path := range cache.entries {
		if !live[path] {
			delete(cache.entries, path)
		}
	}
	return recordings, nil
}
