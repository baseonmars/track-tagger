package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\{A4D0D90B-4DB2-469D-A85B-2E2FE5311E67}_is1`

func openStageGuard(dir string, create bool) (io.Closer, error) {
	path := filepath.Join(dir, "active.lock")
	if _, e := os.Lstat(path); e == nil {
		if e = regularStageFile(path); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	disposition := uint32(windows.OPEN_EXISTING)
	share := uint32(0)
	if create {
		disposition = windows.OPEN_ALWAYS
		share = windows.FILE_SHARE_READ
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, share, nil, disposition, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
func stageGuard(dir string) (io.Closer, error) { return openStageGuard(dir, true) }

// CleanupStale reclaims only validated completed stages whose helper has exited.
func CleanupStale() error {
	root, e := updateRoot()
	if e != nil {
		return e
	}
	return cleanupStages(root, func(dir string) (io.Closer, error) { return openStageGuard(dir, false) })
}

func waitHelperReady(ready, child windows.Handle, milliseconds uint32) error {
	result, e := windows.WaitForMultipleObjects([]windows.Handle{ready, child}, false, milliseconds)
	if e != nil {
		return e
	}
	if result != windows.WAIT_OBJECT_0 {
		return errors.New("update helper did not confirm readiness; the launcher remains open")
	}
	return nil
}

func installedAt(exe string) (bool, error) {
	if !strings.EqualFold(filepath.Base(exe), "track-tagger-gui.exe") {
		return false, nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer k.Close()
	location, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return false, err
	}
	name, _, err := k.GetStringValue("DisplayName")
	if err != nil {
		return false, err
	}
	uninstall, _, err := k.GetStringValue("UninstallString")
	if err != nil {
		return false, err
	}
	if name != "Track Tagger" || !filepath.IsAbs(location) {
		return false, nil
	}
	args, err := windows.DecomposeCommandLine(uninstall)
	if err != nil || len(args) != 1 || !samePath(filepath.Dir(args[0]), location) || !strings.EqualFold(filepath.Base(args[0]), "unins000.exe") {
		return false, nil
	}
	for _, path := range []string{exe, args[0]} {
		st, err := os.Lstat(path)
		if err != nil {
			return false, err
		}
		if !st.Mode().IsRegular() {
			return false, nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false, err
		}
		if !samePath(resolved, path) {
			return false, nil
		}
	}
	return samePath(filepath.Dir(exe), location), nil
}

// Installed verifies that this executable is the registered per-user install.
func Installed() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return installedAt(exe)
}

// Install transfers ownership of staged files to a helper. The caller must have
// drained its service and must exit after success so the helper can proceed.
func Install(s *Staged) error {
	if s == nil || !validHash(s.hash) {
		return errors.New("missing verified update")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	installed, err := installedAt(exe)
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("portable copies must be updated manually from GitHub Releases")
	}
	root, err := updateRoot()
	if err != nil {
		return err
	}
	if err := safeStage(s.dir, root); err != nil {
		return err
	}
	got, err := fileHash(filepath.Join(s.dir, "installer.exe"), maxInstaller)
	if err != nil {
		return err
	}
	if got != s.hash {
		return errors.New("staged installer checksum changed")
	}
	helper := filepath.Join(s.dir, "update-helper.exe")
	if err := copyHelper(exe, helper); err != nil {
		return err
	}
	helperHash, err := fileHash(helper, maxInstaller)
	if err != nil {
		return err
	}
	attrs := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	ready, err := windows.CreateEvent(&attrs, 1, 0, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ready)
	h := handoff{ParentPID: uint32(os.Getpid()), InstallDir: filepath.Dir(exe), InstallerHash: s.hash, HelperHash: helperHash, ReadyEvent: uint64(ready)}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	manifest := filepath.Join(s.dir, "handoff.json")
	if err := os.WriteFile(manifest, b, 0600); err != nil {
		return err
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, true, uint32(os.Getpid()))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(parent)
	cmd := exec.Command(helper, helperFlag, strconv.FormatUint(uint64(parent), 10), manifest)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(parent), syscall.Handle(ready)}}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err == nil {
		defer windows.CloseHandle(child)
		err = waitHelperReady(ready, child, 20000)
	}
	if err != nil {
		// Before readiness the parent is still alive, so this helper cannot be
		// installing or touching application storage. Stop only that helper.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("prepare update helper: %w", err)
	}
	_ = cmd.Process.Release()
	s.dir = "" // The helper now owns the staging directory.
	return nil
}

func copyHelper(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, maxInstaller+1))
	if err != nil {
		return err
	}
	if n > maxInstaller {
		return errors.New("launcher exceeds copy size limit")
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return out.Close()
}

func processPath(handle windows.Handle) (string, error) {
	b := make([]uint16, 32768)
	n := uint32(len(b))
	if err := windows.QueryFullProcessImageName(handle, 0, &b[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(b[:n]), nil
}

func runHelper(args []string, acknowledged *bool) error {
	handle, manifest, err := helperArguments(args)
	if err != nil {
		return err
	}
	parent := windows.Handle(handle)
	defer windows.CloseHandle(parent)
	root, err := updateRoot()
	if err != nil {
		return err
	}
	dir := filepath.Dir(manifest)
	if err := safeStage(dir, root); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(exe, filepath.Join(dir, "update-helper.exe")) {
		return errors.New("helper is not running from its staging directory")
	}
	guard, err := stageGuard(dir)
	if err != nil {
		return err
	}
	defer guard.Close()
	h, err := readHandoff(manifest)
	if err != nil {
		return err
	}
	pid, err := windows.GetProcessId(parent)
	if err != nil || pid != h.ParentPID {
		return errors.New("update parent handle does not match handoff")
	}
	original := filepath.Join(h.InstallDir, "track-tagger-gui.exe")
	parentPath, err := processPath(parent)
	if err != nil {
		return err
	}
	if !samePath(parentPath, original) {
		return errors.New("update parent is not the installed launcher")
	}
	installed, err := installedAt(original)
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("registered installation changed before update")
	}
	for _, path := range []string{exe, original} {
		got, err := fileHash(path, maxInstaller)
		if err != nil {
			return err
		}
		if got != h.HelperHash {
			return errors.New("update helper does not match the installed launcher")
		}
	}
	installer := filepath.Join(dir, "installer.exe")
	got, err := fileHash(installer, maxInstaller)
	if err != nil {
		return err
	}
	if got != h.InstallerHash {
		return errors.New("installer checksum changed before handoff")
	}
	ready := windows.Handle(h.ReadyEvent)
	defer windows.CloseHandle(ready)
	if err := windows.SetEvent(ready); err != nil {
		return fmt.Errorf("confirm update helper readiness: %w", err)
	}
	*acknowledged = true
	defer func() {
		_ = os.Remove(installer)
		_ = os.Remove(manifest)
		b, _ := json.Marshal(h)
		_ = os.WriteFile(filepath.Join(dir, "cleanup.json"), b, 0600)
	}()
	log := filepath.Join(root, "last-install.log")
	return finishHandoff(func() error {
		result, err := windows.WaitForSingleObject(parent, 5*60*1000)
		if err != nil {
			return err
		}
		if result != windows.WAIT_OBJECT_0 {
			return errors.New("launcher has not exited; quit Track Tagger and retry the update")
		}
		return nil
	}, func() error {
		// Deny write/delete sharing from validation through installer completion.
		p, err := windows.UTF16PtrFromString(installer)
		if err != nil {
			return err
		}
		file, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			return err
		}
		f := os.NewFile(uintptr(file), installer)
		defer f.Close()
		got, err := readerHash(f, maxInstaller)
		if err != nil {
			return err
		}
		if got != h.InstallerHash {
			return errors.New("installer checksum changed; update refused")
		}
		cmd := exec.Command(installer, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CURRENTUSER", "/DIR="+h.InstallDir, "/LOG="+log)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w; see %s", err, log)
		}
		return nil
	}, func() error {
		cmd := exec.Command(original)
		cmd.Dir = h.InstallDir
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	})
}

// RunHelper must be dispatched before the normal GUI's single-instance lock.
func RunHelper(args []string) (bool, error) {
	if len(args) == 0 || args[0] != helperFlag {
		return false, nil
	}
	acknowledged := false
	err := runHelper(args, &acknowledged)
	if err != nil {
		root, rootErr := updateRoot()
		if rootErr == nil {
			_ = os.MkdirAll(root, 0700)
			_ = os.WriteFile(filepath.Join(root, "last-update-error.txt"), []byte(time.Now().Format(time.RFC3339)+"\n"+err.Error()+"\n"), 0600)
		}
		if acknowledged {
			message, _ := windows.UTF16PtrFromString("Track Tagger could not finish the update. Your music and application history are retained. Reopen Track Tagger if needed and try Check for updates again.\n\n" + err.Error())
			title, _ := windows.UTF16PtrFromString("Track Tagger update")
			_, _, _ = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
		}
	}
	return true, err
}
