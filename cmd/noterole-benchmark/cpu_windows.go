//go:build windows

package main

import "golang.org/x/sys/windows"

func cpuSeconds() (float64, error) {
	h, err := windows.GetCurrentProcess()
	if err != nil {
		return 0, err
	}
	var creation, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	ticks := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
	ticks += uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
	return float64(ticks) / 1e7, nil
}
