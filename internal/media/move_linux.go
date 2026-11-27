package media

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func MoveExclusive(from, to string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return SyncDirectory(filepath.Dir(to))
}
