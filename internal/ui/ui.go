// Package ui is the Fyne desktop GUI: a dashboard window (live status + recent
// uploads + settings) plus a system-tray icon for background operation.
package ui

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"dropzone/internal/autostart"
	"dropzone/internal/clipboardwatch"
	"dropzone/internal/config"
	"dropzone/internal/r2"
	"dropzone/internal/service"
)

//go:embed icon.png
var iconPNG []byte

type uploadRec struct{ name, url string }

// UI holds the Fyne app and its widgets.
type UI struct {
	app fyne.App
	win fyne.Window
	cfg *config.Config
	svc *service.Service

	tabs        *container.AppTabs
	statusLabel *widget.Label
	pauseBtn    *widget.Button
	paused      bool

	recentMu sync.Mutex
	recent   []uploadRec
	list     *widget.List

	// settings widgets
	accountID *widget.Entry
	accessKey *widget.Entry
	secret    *widget.Entry
	bucket    *widget.Entry
	publicURL *widget.Entry
	folders   *widget.Entry
	watchClip *widget.Check
	copyLink  *widget.Check
	codec     *widget.Select

	// tray
	desk   desktop.App
	menu   *fyne.Menu
	mPause *fyne.MenuItem
	mAuto  *fyne.MenuItem
}

// Run builds and runs the GUI. It blocks until the app exits, then stops the
// service. Must be called from the main goroutine.
func Run(cfg *config.Config, svc *service.Service) {
	u := &UI{cfg: cfg, svc: svc}
	u.app = app.NewWithID("com.dropzone.app")
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	u.app.SetIcon(icon)

	u.win = u.app.NewWindow("dropzone")
	u.win.SetIcon(icon)
	u.win.Resize(fyne.NewSize(580, 540))
	u.win.SetCloseIntercept(func() { u.win.Hide() }) // close = hide to tray

	// Service callbacks marshalled onto the UI thread.
	svc.OnStatus = func(s string) { u.setStatus(s) }
	svc.OnUpload = func(name, url string) { u.addRecent(name, url) }

	u.buildUI()
	u.setupTray(icon)

	if cfg.Configured() {
		u.startService()
	} else {
		u.statusLabel.SetText("Welcome! Fill in Settings and click Save & Apply to begin.")
		u.tabs.SelectIndex(1)
		u.win.Show()
	}

	u.app.Run()
	svc.Stop()
}

func (u *UI) buildUI() {
	// --- Status tab ---
	u.statusLabel = widget.NewLabel("Starting…")
	u.statusLabel.Wrapping = fyne.TextWrapWord
	u.pauseBtn = widget.NewButton("Pause", func() { u.togglePause() })

	u.list = widget.NewList(
		func() int { u.recentMu.Lock(); defer u.recentMu.Unlock(); return len(u.recent) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			u.recentMu.Lock()
			rec := u.recent[i]
			u.recentMu.Unlock()
			o.(*widget.Label).SetText(rec.name + "  →  " + rec.url)
		},
	)
	u.list.OnSelected = func(i widget.ListItemID) {
		u.recentMu.Lock()
		rec := u.recent[i]
		u.recentMu.Unlock()
		clipboardwatch.WriteText(rec.url)
		u.list.UnselectAll()
		u.setStatus("link copied: " + rec.url)
	}

	statusHeader := container.NewVBox(
		u.statusLabel,
		u.pauseBtn,
		widget.NewLabelWithStyle("Recent uploads (click to copy link)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	statusTab := container.NewBorder(statusHeader, nil, nil, nil, u.list)

	// --- Settings tab ---
	u.accountID = widget.NewEntry()
	u.accountID.SetPlaceHolder("Cloudflare account ID")
	u.accessKey = widget.NewEntry()
	u.accessKey.SetPlaceHolder("R2 access key ID")
	u.secret = widget.NewPasswordEntry()
	u.secret.SetPlaceHolder("R2 secret access key")
	u.bucket = widget.NewEntry()
	u.bucket.SetPlaceHolder("bucket name")
	u.publicURL = widget.NewEntry()
	u.publicURL.SetPlaceHolder("https://cdn.example.com")
	u.folders = widget.NewMultiLineEntry()
	u.folders.SetPlaceHolder("One folder per line. Leave empty to auto-detect OS capture folders.")
	u.folders.SetMinRowsVisible(3)
	u.watchClip = widget.NewCheck("Watch clipboard for images (e.g. Win+Shift+S)", nil)
	u.copyLink = widget.NewCheck("Copy public link to clipboard after upload", nil)
	u.codec = widget.NewSelect([]string{"h265", "h264", "av1"}, nil)

	u.loadIntoForm()

	form := widget.NewForm(
		widget.NewFormItem("Account ID", u.accountID),
		widget.NewFormItem("Access Key ID", u.accessKey),
		widget.NewFormItem("Secret Access Key", u.secret),
		widget.NewFormItem("Bucket", u.bucket),
		widget.NewFormItem("Public base URL", u.publicURL),
		widget.NewFormItem("Video codec", u.codec),
		widget.NewFormItem("Watch folders", u.folders),
	)

	buttons := container.NewHBox(
		widget.NewButton("Test connection", func() { u.testConnection() }),
		widget.NewButton("Save & Apply", func() { u.save() }),
	)
	settingsTab := container.NewBorder(
		nil,
		container.NewVBox(u.watchClip, u.copyLink, buttons),
		nil, nil,
		container.NewVScroll(form),
	)

	u.tabs = container.NewAppTabs(
		container.NewTabItem("Status", statusTab),
		container.NewTabItem("Settings", settingsTab),
	)
	u.win.SetContent(u.tabs)
}

func (u *UI) loadIntoForm() {
	u.accountID.SetText(u.cfg.R2.AccountID)
	u.accessKey.SetText(u.cfg.R2.AccessKeyID)
	if s, err := u.cfg.Secret(); err == nil {
		u.secret.SetText(s)
	}
	u.bucket.SetText(u.cfg.R2.Bucket)
	u.publicURL.SetText(u.cfg.R2.PublicBaseURL)
	u.folders.SetText(strings.Join(u.cfg.WatchFolders, "\n"))
	u.watchClip.SetChecked(u.cfg.WatchClipboard)
	u.copyLink.SetChecked(u.cfg.CopyLinkToClipboard)
	codec := u.cfg.Video.Codec
	if codec == "" {
		codec = "h265"
	}
	u.codec.SetSelected(codec)
}

func (u *UI) collectForm() {
	u.cfg.R2.AccountID = strings.TrimSpace(u.accountID.Text)
	u.cfg.R2.AccessKeyID = strings.TrimSpace(u.accessKey.Text)
	u.cfg.R2.Bucket = strings.TrimSpace(u.bucket.Text)
	u.cfg.R2.PublicBaseURL = strings.TrimSpace(u.publicURL.Text)
	u.cfg.WatchClipboard = u.watchClip.Checked
	u.cfg.CopyLinkToClipboard = u.copyLink.Checked
	if u.codec.Selected != "" {
		u.cfg.Video.Codec = u.codec.Selected
	}
	var fs []string
	for _, line := range strings.Split(u.folders.Text, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			fs = append(fs, t)
		}
	}
	u.cfg.WatchFolders = fs
}

func (u *UI) save() {
	u.collectForm()
	if secret := strings.TrimSpace(u.secret.Text); secret != "" {
		_ = u.cfg.SetSecret(secret) // falls back to config file on keychain failure
	}
	u.cfg.ApplyDefaults()
	if err := u.cfg.Save(); err != nil {
		dialog.ShowError(fmt.Errorf("save failed: %w", err), u.win)
		return
	}
	if !u.cfg.Configured() {
		dialog.ShowInformation("Saved", "Settings saved, but some required R2 fields are still missing.", u.win)
		return
	}
	if err := u.svc.Restart(u.cfg); err != nil {
		u.setStatus("error: " + err.Error())
		dialog.ShowError(fmt.Errorf("could not start: %w", err), u.win)
		return
	}
	folders, clip := u.svc.Summary()
	u.setStatus(summaryText(folders, clip))
	dialog.ShowInformation("Saved", "Settings saved and applied.", u.win)
}

func (u *UI) testConnection() {
	u.collectForm()
	secret := strings.TrimSpace(u.secret.Text)
	u.setStatus("testing connection…")
	go func() {
		err := verify(u.cfg, secret)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("connection failed: %w", err), u.win)
				u.setStatus("connection failed")
			} else {
				dialog.ShowInformation("Success", "Bucket reachable — credentials look good.", u.win)
				u.setStatus("connection OK")
			}
		})
	}()
}

func (u *UI) startService() {
	if err := u.svc.Start(u.cfg); err != nil {
		u.setStatus("error: " + err.Error())
		return
	}
	folders, clip := u.svc.Summary()
	u.setStatus(summaryText(folders, clip))
}

func (u *UI) togglePause() {
	u.paused = !u.paused
	u.svc.SetPaused(u.paused)
	if u.paused {
		u.pauseBtn.SetText("Resume")
		u.setStatus("paused")
	} else {
		u.pauseBtn.SetText("Pause")
		u.setStatus("watching")
	}
	u.refreshTray()
}

func (u *UI) addRecent(name, url string) {
	u.recentMu.Lock()
	u.recent = append([]uploadRec{{name: name, url: url}}, u.recent...)
	if len(u.recent) > 50 {
		u.recent = u.recent[:50]
	}
	u.recentMu.Unlock()
	fyne.Do(func() { u.list.Refresh() })
}

func (u *UI) setStatus(s string) {
	fyne.Do(func() { u.statusLabel.SetText(s) })
}

// --- system tray ---

func (u *UI) setupTray(icon fyne.Resource) {
	desk, ok := u.app.(desktop.App)
	if !ok {
		return
	}
	u.desk = desk
	mShow := fyne.NewMenuItem("Open dropzone", func() {
		fyne.Do(func() { u.win.Show(); u.win.RequestFocus() })
	})
	u.mPause = fyne.NewMenuItem("Pause", func() { u.togglePause() })
	u.mAuto = fyne.NewMenuItem("Start at login", func() { u.toggleAutostart() })
	u.mAuto.Checked = autostart.IsEnabled()

	u.menu = fyne.NewMenu("dropzone", mShow, u.mPause, fyne.NewMenuItemSeparator(), u.mAuto)
	desk.SetSystemTrayMenu(u.menu)
	desk.SetSystemTrayIcon(icon)
	desk.SetSystemTrayWindow(u.win) // left-click opens the window
}

func (u *UI) refreshTray() {
	if u.mPause == nil {
		return
	}
	if u.paused {
		u.mPause.Label = "Resume"
	} else {
		u.mPause.Label = "Pause"
	}
	u.mAuto.Checked = autostart.IsEnabled()
	if u.menu != nil {
		u.menu.Refresh()
	}
}

func (u *UI) toggleAutostart() {
	state, err := autostart.Set(!autostart.IsEnabled())
	if err != nil {
		u.setStatus("autostart error: " + err.Error())
	}
	u.mAuto.Checked = state
	u.refreshTray()
}

// --- helpers ---

func summaryText(folders []string, clip bool) string {
	s := fmt.Sprintf("Watching %d folder(s)", len(folders))
	if clip {
		s += " + clipboard"
	}
	if len(folders) > 0 {
		s += ":\n" + strings.Join(folders, "\n")
	}
	return s
}

func verify(cfg *config.Config, secret string) error {
	if secret == "" {
		var err error
		if secret, err = cfg.Secret(); err != nil {
			return err
		}
	}
	client, err := r2.New(context.Background(), cfg.R2.AccountID, cfg.R2.AccessKeyID, secret, cfg.R2.Bucket, cfg.R2.PublicBaseURL)
	if err != nil {
		return err
	}
	return client.Verify(context.Background())
}
