package launcher

import (
	"context"
	"errors"
	"fmt"

	"track-tagger/internal/updater"
)

type nativeUpdateBackend struct{}

func (nativeUpdateBackend) Check(ctx context.Context, v string) (*updateRelease, error) {
	r, e := updater.Check(ctx, v)
	if errors.Is(e, updater.ErrDevelopmentBuild) {
		return nil, errDevelopmentUpdate
	}
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, nil
	}
	return &updateRelease{Version: r.Version, PageURL: r.PageURL, data: r}, nil
}
func (nativeUpdateBackend) Download(ctx context.Context, r *updateRelease) (updateDownload, error) {
	d, e := updater.Download(ctx, r.data.(*updater.Release), nil)
	if e != nil {
		return nil, e
	}
	return d, nil
}
func (nativeUpdateBackend) Install(d updateDownload) error {
	return updater.Install(d.(*updater.Staged))
}
func (nativeUpdateBackend) Installed() (bool, error) { return updater.Installed() }

func initializeUpdates() {
	go func() { _ = updater.CleanupStale() }()
	updates = &updateController{backend: nativeUpdateBackend{}, dispatch: post, changed: refresh, version: appVersion, message: "Updates install only when you choose Install update.", stop: func(done func()) {
		if session == nil {
			done()
			return
		}
		busy = true
		status = "Stopping"
		refresh()
		s := session
		go func() { s.Stop(); post(done) }()
	}, exit: func() { call(user, "DestroyWindow", win) }}
	refreshUpdates()
}
func refreshUpdates() {
	if updates == nil {
		return
	}
	set(43, updates.message)
	for _, id := range []int{41, 42, 44, 45} {
		enabled := !updates.busy
		if id == 44 {
			enabled = !updates.installing
		}
		if id == 45 {
			enabled = enabled && updates.release != nil && !busy
		}
		v := 0
		if enabled {
			v = 1
		}
		call(user, "EnableWindow", controls[id], v)
	}
}
func openReleaseNotes() {
	url := updater.ReleasesURL
	if updates != nil && updates.release != nil {
		url = updates.release.PageURL
	}
	if call(shell, "ShellExecuteW", win, "open", url, 0, 0, 1) <= 32 {
		set(43, "Could not open release notes: "+url)
	}
}
func confirmInstallUpdate() {
	if updates == nil || updates.busy || updates.release == nil || busy {
		return
	}
	installed, e := updates.backend.Installed()
	if e != nil {
		set(43, "Cannot determine installation: "+e.Error())
		return
	}
	if !installed {
		set(43, "Portable copy: download and install the new version from Release notes.")
		openReleaseNotes()
		return
	}
	prompt := fmt.Sprintf("Download and install Track Tagger %s?\n\nTrack Tagger will finish active work, stop safely, and restart with processing stopped. Your preferences, history, and music will be preserved.", updates.release.Version)
	if call(user, "MessageBoxW", win, prompt, "Install Track Tagger update", 0x124) != 6 {
		return
	}
	if session == nil {
		readSettings()
	}
	if e := Save(directory, config); e != nil {
		set(43, "Cannot save preferences before update: "+e.Error())
		return
	}
	updates.BeginInstall()
	refresh()
}
