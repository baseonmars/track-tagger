package media

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func MoveExclusive(from, to string) error {
	if err := unix.RenamexNp(from, to, unix.RENAME_EXCL); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return SyncDirectory(filepath.Dir(to))
}
