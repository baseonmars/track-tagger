//go:build cgo

package main

import (
	"fmt"
	"os"
	"runtime"

	"track-tagger/internal/launcher"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	path, _, err := launcher.ParseNoteSuggestions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "track-tagger-gui:", err)
		os.Exit(1)
	}
	launcher.RunMac(path)
}
