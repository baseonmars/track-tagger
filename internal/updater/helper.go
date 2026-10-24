package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const helperFlag = "--track-tagger-update-helper"

type handoff struct {
	ParentPID     uint32
	InstallDir    string
	InstallerHash string
	HelperHash    string
	ReadyEvent    uint64
}

func helperArguments(args []string) (uintptr, string, error) {
	if len(args) != 3 || args[0] != helperFlag {
		return 0, "", errors.New("invalid internal update helper arguments")
	}
	n, err := strconv.ParseUint(args[1], 10, strconv.IntSize)
	if err != nil || n == 0 || !filepath.IsAbs(args[2]) || filepath.Base(args[2]) != "handoff.json" {
		return 0, "", errors.New("invalid update parent handle or handoff path")
	}
	return uintptr(n), filepath.Clean(args[2]), nil
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

func readHandoff(path string) (handoff, error) {
	var h handoff
	if err := regularStageFile(path); err != nil {
		return h, err
	}
	f, err := os.Open(path)
	if err != nil {
		return h, err
	}
	defer f.Close()
	b, err := boundedRead(f, 16<<10)
	if err != nil {
		return h, err
	}
	if err = json.Unmarshal(b, &h); err != nil {
		return h, err
	}
	if h.ParentPID == 0 || h.ReadyEvent == 0 || !filepath.IsAbs(h.InstallDir) || !validHash(h.InstallerHash) || !validHash(h.HelperHash) {
		return h, errors.New("invalid update handoff metadata")
	}
	return h, nil
}

func regularStageFile(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("update file is not a regular file")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	if !samePath(resolved, path) {
		return errors.New("update file contains a redirected path")
	}
	return nil
}

func fileHash(path string, max int64) (string, error) {
	if err := regularStageFile(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return readerHash(f, max)
}

func readerHash(r io.Reader, max int64) (string, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, max+1))
	if err != nil {
		return "", err
	}
	if n > max {
		return "", errors.New("update file exceeds size limit")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

// safeStage requires an immediate random child of the private update root and
// rejects junctions/symlinks before reading or removing any handoff files.
func safeStage(dir, root string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(dir), root) || !strings.HasPrefix(filepath.Base(dir), "update-") {
		return errors.New("update staging directory is outside application cache")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if !samePath(resolved, dir) {
		return errors.New("update staging directory contains a redirected path")
	}
	return nil
}

// finishHandoff keeps the ordering testable without executing installers in tests.
// A failed wait must never run an installer or start a duplicate parent process.
func finishHandoff(wait, install, restart func() error) error {
	if err := wait(); err != nil {
		return fmt.Errorf("wait for Track Tagger to exit: %w", err)
	}
	installErr := install()
	restartErr := restart()
	if installErr != nil {
		installErr = fmt.Errorf("installation failed: %w", installErr)
	}
	if restartErr != nil {
		restartErr = fmt.Errorf("reopen Track Tagger: %w", restartErr)
	}
	return errors.Join(installErr, restartErr)
}

// cleanupStages only reclaims completed helper stages. Active downloads have no
// completion marker, and an active helper keeps its exclusive guard unavailable.
func cleanupStages(root string, lock func(string) (io.Closer, error)) error {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if safeStage(dir, root) != nil {
			continue
		}
		h, e := readHandoff(filepath.Join(dir, "cleanup.json"))
		if e != nil {
			continue
		}
		files, e := os.ReadDir(dir)
		if e != nil {
			continue
		}
		valid := true
		for _, f := range files {
			switch f.Name() {
			case "cleanup.json", "update-helper.exe", "active.lock":
			default:
				valid = false
			}
			if regularStageFile(filepath.Join(dir, f.Name())) != nil {
				valid = false
			}
		}
		if !valid {
			continue
		}
		hash, e := fileHash(filepath.Join(dir, "update-helper.exe"), maxInstaller)
		if e != nil || hash != h.HelperHash {
			continue
		}
		guard, e := lock(dir)
		if e != nil {
			continue
		}
		// Remove known files individually. Never recursively delete a cache tree.
		e = os.Remove(filepath.Join(dir, "update-helper.exe"))
		if e == nil {
			e = os.Remove(filepath.Join(dir, "cleanup.json"))
		}
		_ = guard.Close()
		if e == nil {
			_ = os.Remove(filepath.Join(dir, "active.lock"))
			_ = os.Remove(dir)
		}
	}
	return nil
}
