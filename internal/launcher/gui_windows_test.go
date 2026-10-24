//go:build windows

package launcher

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"track-tagger/internal/app"
)

// Exercise the actual native controls and asynchronous error path without
// launching a browser or opening a music library.
func TestNativeStartFailureReturnsToStoppedControls(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	directory = t.TempDir()
	config = Settings{Mode: app.DryRun}
	busy = false
	quitting = false
	session = nil
	status = "Stopped"
	win = call(user, "CreateWindowExW", 0, ptr("STATIC"), ptr("Launcher test"), 0, 0, 0, 600, 600, 0, 0, 0, 0)
	if win == 0 {
		t.Fatal("create native test window")
	}
	defer call(user, "DestroyWindow", win)
	controls = map[int]uintptr{}
	for _, id := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 15, 20, 21, 22} {
		kind := "BUTTON"
		if id == 1 || id == 2 || id >= 20 {
			kind = "EDIT"
		}
		controls[id] = call(user, "CreateWindowExW", 0, ptr(kind), ptr(""), 0x40000000, 0, 0, 300, 25, win, id, 0, 0)
	}
	set(1, filepath.Join(directory, "missing-music"))
	check(4, true)
	start()
	if !busy || status != "Starting" {
		t.Fatalf("start transition: %s %v", status, busy)
	}
	deadline := time.Now().Add(5 * time.Second)
	for busy && time.Now().Before(deadline) {
		queueMu.Lock()
		q := queue
		queue = nil
		queueMu.Unlock()
		for _, f := range q {
			f()
		}
		time.Sleep(time.Millisecond)
	}
	if busy || status != "Error" || session != nil {
		t.Fatalf("failed start did not finish: %s busy=%v", status, busy)
	}
	if call(user, "IsWindowEnabled", controls[1]) == 0 {
		t.Fatal("source remains disabled after error")
	}
	if get(21) == "" {
		t.Fatal("missing actionable error")
	}
	diagnostics := diagnosticDetails()
	for _, want := range []string{"Track Tagger dev", "Status: Error", "Mode: dry-run", get(1), get(21)} {
		if !strings.Contains(diagnostics, want) {
			t.Fatalf("diagnostics missing %q: %s", want, diagnostics)
		}
	}
	start()
	stop(false)
	deadline = time.Now().Add(5 * time.Second)
	for busy && time.Now().Before(deadline) {
		queueMu.Lock()
		q := queue
		queue = nil
		queueMu.Unlock()
		for _, f := range q {
			f()
		}
		time.Sleep(time.Millisecond)
	}
	if busy || status != "Stopped" {
		t.Fatalf("canceling startup: status=%s busy=%v", status, busy)
	}
	details = false
	toggleDetails()
	var expanded rect
	call(user, "GetWindowRect", win, unsafe.Pointer(&expanded))
	if get(15) != "Hide details" {
		t.Fatal("details toggle label")
	}
	toggleDetails()
	var collapsed rect
	call(user, "GetWindowRect", win, unsafe.Pointer(&collapsed))
	if expanded.bottom-expanded.top < collapsed.bottom-collapsed.top {
		t.Fatal("details window shrank on expansion")
	}
}
