//go:build !windows && !darwin

package media

import "os"

func checkPlatformLocal(path string, st os.FileInfo) error { return nil }
