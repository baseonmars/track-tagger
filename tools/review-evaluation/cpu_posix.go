//go:build review_evaluation && (darwin || linux)

package main

import "syscall"

// processCPU measures user and system CPU consumed by all process threads.
func processCPU() (int64, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}
	return usage.Utime.Nano() + usage.Stime.Nano(), true
}
