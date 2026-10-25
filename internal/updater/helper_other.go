//go:build !windows

package updater

import "errors"

func CleanupStale() error { return nil }

func Installed() (bool, error) { return false, nil }
func Install(s *Staged, resumeServer bool) error {
	return errors.New("built-in installation is available on Windows only")
}
func RunHelper(args []string) (bool, error) {
	if len(args) > 0 && args[0] == helperFlag {
		return true, errors.New("Windows update helper is unavailable")
	}
	return false, nil
}
