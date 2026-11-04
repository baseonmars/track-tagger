//go:build darwin || linux

package main

import "syscall"

func cpuSeconds() (float64, error) {
	var u syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
		return 0, err
	}
	return float64(u.Utime.Sec+u.Stime.Sec) + float64(u.Utime.Usec+u.Stime.Usec)/1e6, nil
}
