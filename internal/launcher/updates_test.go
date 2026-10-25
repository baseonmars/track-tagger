package launcher

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUpdateDownload struct{ cleaned bool }

func (d *fakeUpdateDownload) Cleanup() { d.cleaned = true }

type fakeUpdateBackend struct {
	check    func(context.Context, string) (*updateRelease, error)
	download func(context.Context, *updateRelease) (updateDownload, error)
	install  func(updateDownload, bool) error
}

func (b fakeUpdateBackend) Check(c context.Context, v string) (*updateRelease, error) {
	return b.check(c, v)
}
func (b fakeUpdateBackend) Download(c context.Context, r *updateRelease) (updateDownload, error) {
	return b.download(c, r)
}
func (b fakeUpdateBackend) Install(d updateDownload, resume bool) error { return b.install(d, resume) }
func (b fakeUpdateBackend) Installed() (bool, error)                    { return true, nil }
func dispatchUpdate(t *testing.T, q <-chan func()) {
	t.Helper()
	select {
	case f := <-q:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("update worker did not complete")
	}
}

func TestUpdateRequiresInstallAndSafeStop(t *testing.T) {
	q := make(chan func(), 8)
	downloaded := &fakeUpdateDownload{}
	installed := make(chan bool, 1)
	exited := false
	var stopped func(bool)
	u := updateController{version: "1.0.0", dispatch: func(f func()) { q <- f }, stop: func(f func(bool)) { stopped = f }, exit: func() { exited = true }, backend: fakeUpdateBackend{
		check: func(_ context.Context, v string) (*updateRelease, error) {
			if v != "1.0.0" {
				t.Errorf("version %q", v)
			}
			return &updateRelease{Version: "1.1.0"}, nil
		},
		download: func(context.Context, *updateRelease) (updateDownload, error) { return downloaded, nil }, install: func(_ updateDownload, resume bool) error { installed <- resume; return nil },
	}}
	u.Check()
	dispatchUpdate(t, q)
	if u.release == nil || u.busy || u.installing {
		t.Fatal("check should only offer an update")
	}
	u.BeginInstall()
	dispatchUpdate(t, q)
	if stopped == nil || !u.installing {
		t.Fatal("download should wait for safe stop")
	}
	select {
	case <-installed:
		t.Fatal("installer ran before service stopped")
	default:
	}
	stopped(true)
	dispatchUpdate(t, q)
	if !<-installed {
		t.Fatal("running server must be carried into installer handoff")
	}
	if !exited || downloaded.cleaned {
		t.Fatal("successful handoff must exit and leave staged files to helper")
	}
}
func TestUpdateDownloadCancellationLeavesAppRunning(t *testing.T) {
	q := make(chan func(), 8)
	quit := false
	stopCalled := false
	u := updateController{release: &updateRelease{Version: "1.1.0"}, dispatch: func(f func()) { q <- f }, stop: func(func(bool)) { stopCalled = true }, backend: fakeUpdateBackend{
		download: func(ctx context.Context, _ *updateRelease) (updateDownload, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	u.BeginInstall()
	u.CancelThen(func() { quit = true })
	dispatchUpdate(t, q)
	if u.busy || u.installing || !quit || stopCalled {
		t.Fatal("canceled download must release UI and never stop service or install")
	}
}
func TestUpdateInstallFailureRetainsLauncher(t *testing.T) {
	q := make(chan func(), 8)
	d := &fakeUpdateDownload{}
	exited := false
	u := updateController{release: &updateRelease{Version: "1.1.0"}, dispatch: func(f func()) { q <- f }, stop: func(f func(bool)) { f(false) }, exit: func() { exited = true }, backend: fakeUpdateBackend{
		download: func(context.Context, *updateRelease) (updateDownload, error) { return d, nil }, install: func(_ updateDownload, resume bool) error {
			if resume {
				t.Error("stopped server requested resume")
			}
			return errors.New("helper unavailable")
		},
	}}
	u.BeginInstall()
	dispatchUpdate(t, q)
	dispatchUpdate(t, q)
	if exited || u.busy || u.installing || !d.cleaned {
		t.Fatal("handoff failure must preserve launcher and clean staging")
	}
}

func TestResumeSnapshotAtSafeStop(t *testing.T) {
	done := make(chan struct{})
	if !resumeAtSafeStop(done, false) {
		t.Fatal("running service should resume")
	}
	if resumeAtSafeStop(done, true) {
		t.Fatal("manual Stop in progress should stay stopped")
	}
	close(done)
	if resumeAtSafeStop(done, false) {
		t.Fatal("completed service should stay stopped")
	}
}
