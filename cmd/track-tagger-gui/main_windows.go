package main

import (
	"fmt"
	"os"

	"track-tagger/internal/launcher"
	"track-tagger/internal/updater"
)

var version = "dev"

func main() {
	if handled, _ := updater.RunHelper(os.Args[1:]); handled {
		return
	}
	path, args, err := launcher.ParseNoteSuggestions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "track-tagger-gui:", err)
		os.Exit(1)
	}
	launcher.Run(version, updater.ResumeRequested(args), path)
}
