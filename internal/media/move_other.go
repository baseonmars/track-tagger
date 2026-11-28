//go:build !linux && !darwin && !windows

package media

import "errors"

func MoveExclusive(_, _ string) error {
	return errors.New("exclusive file moves are unavailable on this platform")
}
