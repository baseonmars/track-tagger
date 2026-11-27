//go:build windows

package media

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func checkPlatformLocal(path string, st os.FileInfo) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attr, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attr&(0x1000|0x40000|0x400000) != 0 {
		return ErrWaiting
	}
	return nil
}

// Replace uses write-through replacement; if Windows refuses a sharing violation,
// the original and staged file remain for journal-backed recovery.
func Replace(temp, dest string) error {
	from, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Remove moves the journaled file out of the recording with write-through.
func Remove(path string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".track-tagger-removed-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Close(); err != nil {
		return err
	}
	return Replace(path, name)
}
