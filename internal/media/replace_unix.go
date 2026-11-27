//go:build !windows

package media

import (
	"os"
	"path/filepath"
)

// Replace atomically renames a staged file on the same filesystem, then flushes
// its directory. A returned sync error can occur after the rename succeeded;
// callers must reconcile journal hashes before retrying.
func Replace(temp, dest string) error {
	if err := os.Rename(temp, dest); err != nil {
		return err
	}
	return syncDirectory(dest)
}

// Remove flushes the parent directory after removing a journaled file.
func Remove(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(path)
}

func syncDirectory(dest string) error {
	d, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
