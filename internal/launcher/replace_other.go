//go:build !windows

package launcher

import "os"

func replaceSettings(src, dst string) error { return os.Rename(src, dst) }
