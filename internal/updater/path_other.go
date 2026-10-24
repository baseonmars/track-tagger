//go:build !windows

package updater

import "path/filepath"

func resolvePath(path string) (string, error) { return filepath.EvalSymlinks(path) }
