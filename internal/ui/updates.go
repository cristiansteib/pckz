package ui

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"

	"pckz/internal/tray"
	"pckz/internal/update"
)

// Update checks: shortly after start and then daily. A newer release shows a
// banner in the window, an entry at the top of the tray menu and, once per
// version, a desktop notification. "Update" upgrades the way the app was
// installed (mise or .deb) and relaunches it.

var onBannerClicked = func(adw.Banner) {
	if appInstance != nil {
		appInstance.applyUpdate()
	}
}

var appInstance *App

func (a *App) buildUpdateBanner() *adw.Banner {
	appInstance = a
	a.banner = adw.NewBanner("")
	a.banner.SetRevealed(false)
	a.banner.ConnectButtonClicked(&onBannerClicked)
	return a.banner
}

func (a *App) startUpdateChecks() {
	if !update.Enabled() {
		return
	}
	go func() {
		time.Sleep(30 * time.Second)
		for {
			a.checkForUpdate(false)
			time.Sleep(24 * time.Hour)
		}
	}()
}

// checkForUpdate runs off the main thread. With manual, the user asked, so
// "up to date" and errors are reported too.
func (a *App) checkForUpdate(manual bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := update.Check(ctx)
	post(func() {
		switch {
		case err != nil && manual:
			a.toast("Couldn't check for updates: " + err.Error())
		case r == nil && manual:
			a.toast(fmt.Sprintf("Audio Switcher %s is up to date", update.Current))
		case r != nil:
			a.offerUpdate(r)
		}
	})
}

func (a *App) offerUpdate(r *update.Release) {
	a.pending = r
	a.banner.SetTitle(fmt.Sprintf("Audio Switcher %s is available", r.Version))
	if update.CanApply() {
		a.banner.SetButtonLabel("Update")
	} else {
		a.banner.SetButtonLabel("Download")
	}
	a.banner.SetRevealed(true)
	a.updateTray()
	if update.Notified() != r.Version {
		update.SetNotified(r.Version)
		go notify(fmt.Sprintf("Audio Switcher %s is available", r.Version),
			"Open Audio Switcher and click Update, or run: audio-switcher --update")
	}
}

func (a *App) applyUpdate() {
	r := a.pending
	if r == nil || a.updating {
		return
	}
	if !update.CanApply() {
		_ = exec.Command("xdg-open", r.URL).Start()
		return
	}
	a.updating = true
	a.banner.SetTitle(fmt.Sprintf("Updating to %s…", r.Version))
	a.banner.SetButtonLabel("")
	a.updateTray()
	go func() {
		bin, err := r.Apply(context.Background(), io.Discard)
		post(func() {
			a.updating = false
			if err != nil {
				a.offerUpdate(r)
				a.toast(err.Error())
				go notify("Audio Switcher update failed", err.Error()+"\n\nTo update by hand:\n"+r.Command())
				return
			}
			if bin == "" { // the package scripts restart the app
				return
			}
			// Hand over to the new version: it starts once this process
			// has released the app id.
			args := "--tray"
			if a.win.GetVisible() {
				args = ""
			}
			_ = exec.Command("sh", "-c", "sleep 1; exec \"$0\" $1", bin, args).Start()
			a.app.Quit()
		})
	}()
}

// updateItems is the tray menu entry for a pending update.
func (a *App) updateItems() []*tray.Item {
	r := a.pending
	if r == nil {
		return nil
	}
	label := fmt.Sprintf("Update to %s…", r.Version)
	if a.updating {
		label = fmt.Sprintf("Updating to %s…", r.Version)
	}
	return []*tray.Item{
		{Label: label, Icon: "software-update-available-symbolic", Disabled: a.updating,
			OnClick: func() { post(a.applyUpdate) }},
		{Separator: true},
	}
}
