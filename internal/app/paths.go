package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// canonical resolves existing ancestors as well as a not-yet-created leaf.
func canonical(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(p)
	if err == nil {
		return real, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", err
	}
	real, err = canonical(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(real, filepath.Base(p)), nil
}
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func rejectOverlap(a, b string) error {
	if inside(a, b) || inside(b, a) {
		return fmt.Errorf("paths must not overlap: %s and %s", a, b)
	}
	return nil
}
func safePath(root, p string) error {
	if !inside(root, p) {
		return fmt.Errorf("path escapes root: %s", p)
	}
	cur := p
	for {
		st, err := os.Lstat(cur)
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink refused: %s", cur)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if cur == root {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return fmt.Errorf("invalid root")
		}
		cur = parent
	}
	return nil
}
