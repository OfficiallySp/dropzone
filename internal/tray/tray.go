// Package tray renders the system-tray menu and exposes live status updates.
package tray

import (
	"sync"

	"fyne.io/systray"
)

// Callbacks wire the tray menu to the rest of the app.
type Callbacks struct {
	Tooltip string
	Icon    []byte

	// OnReady is called (in a goroutine) once the menu exists; start the
	// pipelines/watchers here.
	OnReady func()
	// OnPauseToggle is called with the new paused state.
	OnPauseToggle func(paused bool)
	// OnOpenConfig opens the config folder.
	OnOpenConfig func()
	// OnToggleAutostart requests the given state and returns the actual result.
	OnToggleAutostart func(enable bool) (bool, error)
	// AutostartEnabled reports the current autostart state (for the initial checkbox).
	AutostartEnabled func() bool
	// OnQuit is called after the user chooses Quit (before the process exits).
	OnQuit func()
}

var (
	statusMu   sync.Mutex
	statusItem *systray.MenuItem
)

// Run starts the tray event loop. It blocks until the user quits and MUST be
// called from the main goroutine.
func Run(cb Callbacks) {
	systray.Run(func() { onReady(cb) }, func() {
		if cb.OnQuit != nil {
			cb.OnQuit()
		}
	})
}

// Quit tears down the tray (causes Run to return).
func Quit() { systray.Quit() }

// SetStatus updates the (disabled) status menu item.
func SetStatus(s string) {
	statusMu.Lock()
	it := statusItem
	statusMu.Unlock()
	if it != nil {
		it.SetTitle("Status: " + s)
	}
}

func onReady(cb Callbacks) {
	if len(cb.Icon) > 0 {
		systray.SetIcon(cb.Icon)
	}
	systray.SetTitle("")
	systray.SetTooltip(cb.Tooltip)

	statusMu.Lock()
	statusItem = systray.AddMenuItem("Status: starting…", "")
	statusItem.Disable()
	statusMu.Unlock()

	mPause := systray.AddMenuItem("Pause", "Pause uploading")
	systray.AddSeparator()
	mConfig := systray.AddMenuItem("Open config folder", "Reveal dropzone's config")

	autoOn := false
	if cb.AutostartEnabled != nil {
		autoOn = cb.AutostartEnabled()
	}
	mAuto := systray.AddMenuItemCheckbox("Start at login", "Launch dropzone when you log in", autoOn)

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit dropzone")

	if cb.OnReady != nil {
		go cb.OnReady()
	}

	go func() {
		paused := false
		for {
			select {
			case <-mPause.ClickedCh:
				paused = !paused
				if paused {
					mPause.SetTitle("Resume")
				} else {
					mPause.SetTitle("Pause")
				}
				if cb.OnPauseToggle != nil {
					cb.OnPauseToggle(paused)
				}
			case <-mConfig.ClickedCh:
				if cb.OnOpenConfig != nil {
					cb.OnOpenConfig()
				}
			case <-mAuto.ClickedCh:
				want := !mAuto.Checked()
				if cb.OnToggleAutostart != nil {
					state, err := cb.OnToggleAutostart(want)
					if err != nil {
						SetStatus("autostart error: " + err.Error())
					}
					if state {
						mAuto.Check()
					} else {
						mAuto.Uncheck()
					}
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}
