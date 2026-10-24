package updater

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// Resolve through an opened handle so ancestor junctions and Windows short-path
// spellings are normalized consistently, including paths beneath junctions.
func resolvePath(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b := make([]uint16, 32768)
	n, e := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &b[0], uint32(len(b)), 0)
	if e != nil {
		return "", e
	}
	if n >= uint32(len(b)) {
		return "", errors.New("resolved update path exceeds Windows path limit")
	}
	s := windows.UTF16ToString(b[:n])
	if strings.HasPrefix(s, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(s, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(s, `\\?\`), nil
}
