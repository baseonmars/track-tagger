//go:build windows

package media

import (
	"golang.org/x/sys/windows"
	"os"
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
