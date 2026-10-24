package updater

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This child only checks inherited handles and acknowledges its parent. It does
// not dispatch RunHelper, touch registry settings, or execute an installer.
func TestReadinessHelperChild(t *testing.T) {
	if os.Getenv("TRACK_TAGGER_TEST_READY_CHILD") == "" {
		return
	}
	parentValue, _ := strconv.ParseUint(os.Getenv("TRACK_TAGGER_TEST_PARENT"), 10, 64)
	readyValue, _ := strconv.ParseUint(os.Getenv("TRACK_TAGGER_TEST_READY"), 10, 64)
	parent := windows.Handle(parentValue)
	ready := windows.Handle(readyValue)
	pid, e := windows.GetProcessId(parent)
	if e != nil || strconv.FormatUint(uint64(pid), 10) != os.Getenv("TRACK_TAGGER_TEST_PID") {
		os.Exit(2)
	}
	if os.Getenv("TRACK_TAGGER_TEST_READY_CHILD") == "signal" {
		if windows.SetEvent(ready) != nil {
			os.Exit(3)
		}
	}
	os.Exit(0)
}

func TestInheritedHelperReadiness(t *testing.T) {
	for _, mode := range []string{"signal", "exit"} {
		t.Run(mode, func(t *testing.T) {
			attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
			ready, e := windows.CreateEvent(&attrs, 1, 0, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer windows.CloseHandle(ready)
			parent, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, true, uint32(os.Getpid()))
			if e != nil {
				t.Fatal(e)
			}
			defer windows.CloseHandle(parent)
			cmd := exec.Command(os.Args[0], "-test.run=^TestReadinessHelperChild$")
			cmd.Env = append(os.Environ(), "TRACK_TAGGER_TEST_READY_CHILD="+mode, "TRACK_TAGGER_TEST_PARENT="+strconv.FormatUint(uint64(parent), 10), "TRACK_TAGGER_TEST_READY="+strconv.FormatUint(uint64(ready), 10), "TRACK_TAGGER_TEST_PID="+strconv.Itoa(os.Getpid()))
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(parent), syscall.Handle(ready)}}
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			child, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
			if e != nil {
				_ = cmd.Wait()
				t.Fatal(e)
			}
			defer windows.CloseHandle(child)
			e = waitHelperReady(ready, child, 5000)
			if (e == nil) != (mode == "signal") {
				t.Errorf("readiness %s: %v", mode, e)
			}
			if e = cmd.Wait(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestCompletedStageCleanupHonorsActiveGuard(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "update-completed")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(dir, "update-helper.exe")
	if e := os.WriteFile(helper, []byte("synthetic helper bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, e := fileHash(helper, maxInstaller)
	if e != nil {
		t.Fatal(e)
	}
	h := handoff{ParentPID: 1, ReadyEvent: 1, InstallDir: t.TempDir(), HelperHash: hash, InstallerHash: hash}
	b, _ := json.Marshal(h)
	if e = os.WriteFile(filepath.Join(dir, "cleanup.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	guard, e := stageGuard(dir)
	if e != nil {
		t.Fatal(e)
	}
	lock := func(p string) (io.Closer, error) { return openStageGuard(p, false) }
	if e = cleanupStages(root, lock); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(helper); e != nil {
		t.Fatal("active helper was removed")
	}
	if e = guard.Close(); e != nil {
		t.Fatal(e)
	}
	if e = cleanupStages(root, lock); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(dir); !os.IsNotExist(e) {
		t.Fatalf("completed stage retained: %v", e)
	}
}

func TestStageAllowsAncestorJunctionButRejectsChildJunction(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real-cache")
	if e := os.Mkdir(realRoot, 0700); e != nil {
		t.Fatal(e)
	}
	junction := func(link, target string) {
		t.Helper()
		cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("create disposable directory junction: %v: %s", e, b)
		}
	}
	alias := filepath.Join(base, "cache-alias")
	junction(alias, realRoot)
	aliasInfo, e := os.Stat(alias)
	if e != nil {
		t.Fatal(e)
	}
	realInfo, e := os.Stat(realRoot)
	if e != nil {
		t.Fatal(e)
	}
	if !os.SameFile(aliasInfo, realInfo) {
		t.Fatal("fixture junction does not refer to real cache directory")
	}
	dir := filepath.Join(alias, "update-safe")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "helper.exe")
	if e := os.WriteFile(file, []byte("synthetic bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(realRoot, "update-safe", "helper.exe")); e != nil || string(b) != "synthetic bytes" {
		t.Fatalf("fixture junction did not write into expected target: %q %v", b, e)
	}
	if e := safeStage(dir, alias); e != nil {
		t.Fatalf("ancestor junction rejected: %v", e)
	}
	if _, e := fileHash(file, maxInstaller); e != nil {
		t.Fatalf("file below ancestor junction rejected: %v", e)
	}
	outside := t.TempDir()
	child := filepath.Join(alias, "update-redirected")
	junction(child, outside)
	if e := safeStage(child, alias); e == nil {
		t.Fatal("owned child junction accepted")
	}
}
