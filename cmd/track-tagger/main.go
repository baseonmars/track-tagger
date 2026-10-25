package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"track-tagger/internal/app"
	"track-tagger/internal/service"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "track-tagger:", err)
		os.Exit(1)
	}
}
func usage() {
	fmt.Println(`Track Tagger — offline FLAC metadata review

  track-tagger scan SOURCE [--dry-run | --write | --copy DEST] [--json] [--plan FILE]
  track-tagger watch SOURCE [--dry-run | --write | --copy DEST] [--poll]
  track-tagger serve SOURCE [--dry-run | --write | --copy DEST] [--addr 127.0.0.1:8787]
  track-tagger apply PLAN [--dry-run | --write | --copy DEST]
  track-tagger undo JOB_ID [--write] [--state DIR]

Common: --state DIR --settle 2m --reconcile 5m
Remote UI: --remote --addr 0.0.0.0:8787 and TRACK_TAGGER_TOKEN (16+ characters).
Preview is the default. Select --write or --copy DEST to apply tags.`)
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		usage()
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	cmd := args[0]
	if cmd != "scan" && cmd != "watch" && cmd != "serve" && cmd != "apply" && cmd != "undo" {
		return fmt.Errorf("unknown command %q", cmd)
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	write := f.Bool("write", false, "change media")
	f.Bool("dry-run", false, "preview only")
	dest := f.String("copy", "", "copy destination")
	state := f.String("state", "", "application storage directory")
	js := f.Bool("json", false, "JSON output")
	planFile := f.String("plan", "", "save preview plan")
	settle := f.Duration("settle", 2*time.Minute, "quiet interval")
	reconcile := f.Duration("reconcile", 5*time.Minute, "full reconciliation interval")
	poll := f.Bool("poll", false, "use polling for network shares")
	addr := f.String("addr", "127.0.0.1:8787", "web listen address")
	remote := f.Bool("remote", false, "enable authenticated remote access")
	// Standard flag stops at the first positional argument; accept both documented orders.
	var positional, flags []string
	valueFlags := map[string]bool{"copy": true, "state": true, "plan": true, "settle": true, "reconcile": true, "addr": true}
	for i := 1; i < len(args); i++ {
		v := args[i]
		if strings.HasPrefix(v, "-") {
			flags = append(flags, v)
			name := strings.TrimLeft(strings.SplitN(v, "=", 2)[0], "-")
			if valueFlags[name] && !strings.Contains(v, "=") {
				i++
				if i == len(args) {
					return fmt.Errorf("missing value for %s", v)
				}
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, v)
		}
	}
	if err := f.Parse(flags); err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("exactly one source, plan, or job ID is required")
	}
	modes := 0
	mode := app.DryRun
	f.Visit(func(v *flag.Flag) {
		switch v.Name {
		case "write":
			modes++
			if *write {
				mode = app.Write
			}
		case "dry-run":
			modes++
		case "copy":
			modes++
			mode = app.Copy
		}
	})
	if modes > 1 {
		return errors.New("--dry-run, --write, and --copy are mutually exclusive")
	}
	if mode == app.Copy && *dest == "" {
		return errors.New("--copy requires a destination")
	}
	if *settle < 0 || *reconcile <= 0 {
		return errors.New("invalid time interval")
	}
	if cmd == "undo" && mode == app.Copy {
		return errors.New("undo accepts --dry-run or --write")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if cmd == "undo" {
		if *state == "" {
			base, err := os.UserConfigDir()
			if err != nil {
				return err
			}
			*state = filepath.Join(base, "track-tagger")
		}
		e, err := app.NewHistory(*state, mode)
		if err != nil {
			return err
		}
		defer e.Close()
		files, err := e.Undo(positional[0], mode == app.Write)
		if err != nil {
			return err
		}
		if *js {
			return json.NewEncoder(os.Stdout).Encode(files)
		}
		for _, j := range files {
			if len(j.Metadata) > 0 {
				fmt.Printf("%s: restore metadata %s\n", j.Status, j.Target)
			}
		}
		return nil
	}
	var p app.Plan
	source := positional[0]
	if cmd == "apply" {
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		defer in.Close()
		dec := json.NewDecoder(io.LimitReader(in, 16<<20))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&p); err != nil {
			return err
		}
		source = p.Source
	}
	opts := app.Options{Source: source, Destination: *dest, State: *state, Mode: mode, Settle: *settle}
	if cmd == "serve" {
		s, err := service.Start(ctx, service.Options{Options: opts, Addr: *addr, Remote: *remote, Token: os.Getenv("TRACK_TAGGER_TOKEN"), Poll: *poll, Reconcile: *reconcile}, func(s string) { fmt.Fprintln(os.Stderr, s) })
		if err != nil {
			return err
		}
		defer s.Stop()
		fmt.Fprintln(os.Stderr, "web interface", s.URL(), "mode:", mode)
		<-s.Done()
		return s.Err()
	}
	e, err := app.New(opts)
	if err != nil {
		return err
	}
	defer e.Close()
	if cmd == "watch" || mode != app.DryRun {
		if err = e.OpenStorage(); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Join(e.State, "history.sqlite")); err == nil {
		if err = e.OpenStorage(); err != nil {
			return err
		}
	}
	switch cmd {
	case "scan", "apply":
		if cmd == "scan" {
			p, err = e.Scan()
			if err != nil {
				return err
			}
		}
		if *planFile != "" {
			if err = app.SavePlan(*planFile, p, e.Source, e.Destination); err != nil {
				return err
			}
		}
		if mode != app.DryRun {
			ids, err := e.Apply(p)
			if err != nil {
				return err
			}
			for _, id := range ids {
				fmt.Fprintln(os.Stderr, "job", id)
			}
		}
		if *js {
			return json.NewEncoder(os.Stdout).Encode(p)
		}
		printPlan(p)
		return nil
	case "watch":
		return e.Watch(ctx, *poll, *reconcile, func(s string) { fmt.Fprintln(os.Stderr, s) })
	}
	return nil
}
func printPlan(p app.Plan) {
	fmt.Println("Whole-file checksums change when metadata changes; audio payload fingerprints are preserved.")
	for _, r := range p.Recordings {
		fmt.Printf("\n[%s] %s\n  %s — %s\n", r.Status, r.Path, r.Artist, r.Album)
		for _, t := range r.Tracks {
			fmt.Printf("  disc %d track %d: %s → %s\n", t.Disc, t.Number, filepath.Base(t.Path), t.Title)
		}
		if r.Artwork != "" {
			fmt.Println("  artwork:", r.Artwork)
		}
		for _, reason := range r.ReviewReasons {
			fmt.Println("  review:", reason)
		}
	}
}
