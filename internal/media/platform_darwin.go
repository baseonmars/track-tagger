//go:build darwin

package media

import (
	"os"
	"syscall"
)

func checkPlatformLocal(path string, st os.FileInfo) error {
	if s, ok := st.Sys().(*syscall.Stat_t); ok && s.Flags&0x40000000 != 0 {
		return ErrWaiting
	}
	return nil
}
