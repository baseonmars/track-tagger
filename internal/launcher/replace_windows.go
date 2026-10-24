package launcher

import "golang.org/x/sys/windows"

func replaceSettings(src, dst string) error {
	s, e := windows.UTF16PtrFromString(src)
	if e != nil {
		return e
	}
	d, e := windows.UTF16PtrFromString(dst)
	if e != nil {
		return e
	}
	return windows.MoveFileEx(s, d, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
