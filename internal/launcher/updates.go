package launcher

import (
	"context"
	"errors"
	"fmt"
)

var errDevelopmentUpdate = errors.New("development builds use manual releases")

type updateRelease struct {
	Version, PageURL string
	data             any
}
type updateDownload interface{ Cleanup() }
type updateBackend interface {
	Check(context.Context, string) (*updateRelease, error)
	Download(context.Context, *updateRelease) (updateDownload, error)
	Installed() (bool, error)
	Install(updateDownload) error
}

// updateController is owned by the UI thread. Workers return exclusively through
// dispatch; no network or installer preparation blocks window message handling.
type updateController struct {
	backend          updateBackend
	dispatch         func(func())
	changed          func()
	stop             func(func())
	exit             func()
	version          string
	message          string
	release          *updateRelease
	busy, installing bool
	cancel           context.CancelFunc
	afterCancel      func()
}

func (u *updateController) notify() {
	if u.changed != nil {
		u.changed()
	}
}
func (u *updateController) Check() {
	if u.busy {
		return
	}
	u.busy = true
	u.message = "Checking for updates…"
	u.notify()
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	go func() {
		r, e := u.backend.Check(ctx, u.version)
		u.dispatch(func() {
			defer cancel()
			u.busy = false
			u.cancel = nil
			if ctx.Err() != nil {
				u.message = "Update check canceled."
			} else if errors.Is(e, errDevelopmentUpdate) {
				u.message = "Development build. Open Release notes for stable releases."
			} else if e != nil {
				u.message = "Could not check for updates. Try again when online."
			} else if r == nil {
				u.release = nil
				u.message = "You have the latest stable version."
			} else {
				u.release = r
				u.message = "Version " + r.Version + " is available."
			}
			u.notify()
			u.finishCancel()
		})
	}()
}
func (u *updateController) BeginInstall() {
	if u.busy || u.release == nil {
		return
	}
	u.busy = true
	u.installing = true
	u.message = "Downloading and verifying update…"
	u.notify()
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	r := u.release
	go func() {
		d, e := u.backend.Download(ctx, r)
		u.dispatch(func() {
			if ctx.Err() != nil || e != nil {
				canceled := ctx.Err() != nil
				cancel()
				u.cancel = nil
				u.busy = false
				u.installing = false
				if d != nil {
					d.Cleanup()
				}
				if canceled {
					u.message = "Update canceled."
				} else {
					u.message = "Update download failed: " + e.Error()
				}
				u.notify()
				u.finishCancel()
				return
			}
			u.message = "Stopping safely before installing…"
			u.notify()
			u.stop(func() {
				if ctx.Err() != nil {
					d.Cleanup()
					cancel()
					u.cancel = nil
					u.busy = false
					u.installing = false
					u.message = "Update canceled."
					u.notify()
					u.finishCancel()
					return
				}
				u.message = "Preparing installer…"
				u.notify()
				go func() {
					e := u.backend.Install(d)
					u.dispatch(func() {
						cancel()
						u.cancel = nil
						if e != nil {
							d.Cleanup()
							u.busy = false
							u.installing = false
							u.message = fmt.Sprintf("Could not start installer: %v. Try Install update again.", e)
							u.notify()
							u.finishCancel()
							return
						}
						u.exit()
					})
				}()
			})
		})
	}()
}
func (u *updateController) CancelThen(f func()) {
	if !u.busy {
		f()
		return
	}
	u.afterCancel = f
	if u.cancel != nil {
		u.cancel()
	}
}
func (u *updateController) finishCancel() {
	if u.afterCancel != nil {
		f := u.afterCancel
		u.afterCancel = nil
		f()
	}
}
