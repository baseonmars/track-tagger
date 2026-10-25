package main

import (
	"os"

	"track-tagger/internal/launcher"
	"track-tagger/internal/updater"
)

var version = "dev"

func main() {
	if handled, _ := updater.RunHelper(os.Args[1:]); handled {
		return
	}
	launcher.Run(version, updater.ResumeRequested(os.Args[1:]))
}
