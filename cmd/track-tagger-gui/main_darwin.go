//go:build cgo

package main

import (
	"runtime"

	"track-tagger/internal/launcher"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	launcher.RunMac()
}
