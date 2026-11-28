package launcher

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "gui_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/cgo"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"track-tagger/internal/app"
	"track-tagger/internal/service"
)

// The AppKit main thread owns the launcher state.
var macLauncher struct {
	state    int
	cancel   context.CancelFunc
	session  *service.Session
	quitting bool
}

// RunMac runs AppKit on the process's initial OS thread.
func RunMac() {
	C.ttRun()
}

func macPost(f func()) {
	C.ttPost(C.uintptr_t(cgo.NewHandle(f)))
}

//export ttDispatch
func ttDispatch(handle C.uintptr_t) {
	h := cgo.Handle(handle)
	f := h.Value().(func())
	h.Delete()
	f()
}

func macUpdate(state int, url, message string) {
	macLauncher.state = state
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	C.ttUpdate(C.int(state), cURL, cMessage)
}

//export ttStart
func ttStart(src, dst *C.char, selectedMode C.int) {
	if macLauncher.state != C.TTStopped || macLauncher.quitting {
		return
	}
	s := Settings{Source: C.GoString(src), Mode: app.DryRun}
	switch selectedMode {
	case C.TTWrite:
		s.Mode = app.Write
	case C.TTCopy:
		s.Mode = app.Copy
		s.Destination = C.GoString(dst)
	}
	if err := s.Validate(); err != nil {
		macUpdate(C.TTStopped, "", err.Error()+", then click Start.")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	macLauncher.cancel = cancel
	macUpdate(C.TTStarting, "", "")
	go func() {
		session, err := startMacSession(ctx, s)
		if err != nil {
			macPost(func() { macFinished(err, ctx.Err() != nil) })
			return
		}
		macPost(func() {
			macLauncher.session = session
			if ctx.Err() == nil {
				macUpdate(C.TTRunning, session.URL(), "")
				ttOpenReview()
			}
		})
		<-session.Done()
		macPost(func() { macFinished(session.Err(), false) })
	}()
}

func startMacSession(ctx context.Context, s Settings) (*service.Session, error) {
	if err := checkMacFolder(s.Source, s.Mode == app.Write); err != nil {
		if s.Mode == app.Write {
			return nil, fmt.Errorf("cannot use music folder: %w. Choose a readable, writable music folder", err)
		}
		return nil, fmt.Errorf("cannot read music folder: %w. Choose an accessible music folder", err)
	}
	if s.Mode == app.Copy {
		if err := checkMacFolder(s.Destination, true); err != nil {
			return nil, fmt.Errorf("cannot use copy destination: %w. Choose a writable folder", err)
		}
	}
	session, err := service.Start(ctx, service.Options{
		Options:      app.Options{Source: s.Source, Destination: s.Destination, Mode: s.Mode, Settle: 2 * time.Minute},
		FallbackPort: true,
	}, func(line string) { fmt.Fprintln(os.Stderr, line) })
	if err != nil {
		return nil, fmt.Errorf("could not start: %w. Check folders and permissions", err)
	}
	return session, nil
}

// Check the selected directory without creating a probe file in either tree.
func checkMacFolder(path string, writable bool) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	dir := os.NewFile(uintptr(fd), path)
	defer dir.Close()
	if _, err = dir.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	access := uint32(unix.X_OK)
	if writable {
		access |= unix.W_OK
	}
	if err := unix.Access(path, access); err != nil {
		return &os.PathError{Op: "access", Path: path, Err: err}
	}
	return nil
}

func macFinished(err error, canceled bool) {
	macLauncher.cancel()
	macLauncher.cancel = nil
	macLauncher.session = nil
	var message string
	if err != nil && !canceled {
		message = err.Error() + ", then click Start to retry."
	}
	macUpdate(C.TTStopped, "", message)
	if macLauncher.quitting {
		C.ttFinishQuit()
	}
}

//export ttOpenReview
func ttOpenReview() {
	if macLauncher.state != C.TTRunning {
		return
	}
	url := macLauncher.session.URL()
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	var message string
	if C.ttOpenURL(cURL) == 0 {
		message = "Could not open your default browser. Copy the local URL into your browser."
	}
	macUpdate(C.TTRunning, url, message)
}

//export ttStop
func ttStop() {
	if macLauncher.state == C.TTStarting || macLauncher.state == C.TTRunning {
		macUpdate(C.TTStopping, "", "Waiting for active work to finish.")
		macLauncher.cancel()
	}
}

//export ttRequestQuit
func ttRequestQuit() C.int {
	if macLauncher.state == C.TTStopped {
		return 0
	}
	macLauncher.quitting = true
	ttStop()
	return 1
}
