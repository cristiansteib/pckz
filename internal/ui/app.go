// Package ui is the GTK4 / libadwaita window and the tray icon. Both live in
// one process: the tray keeps it running in the background, and opening the
// app from the launcher just presents the existing window.
package ui

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"pckz/assets"
	"pckz/internal/audio"
	"pckz/internal/install"
	"pckz/internal/tray"
)

const (
	AppID   = assets.AppID
	AppIcon = "pckz-audio-switcher"
)

var Version = "dev"

//go:embed style.css
var css string

type App struct {
	app       *adw.Application
	win       *adw.ApplicationWindow
	toasts    *adw.ToastOverlay
	iconTheme *gtk.IconTheme
	tray      *tray.Tray

	out, in *devicePage
	apps    *appsPage

	snap        *audio.Snapshot
	busy        string // key of the device being switched to
	autoFixed   map[string]time.Time
	seenCards   map[string]bool
	startHidden bool

	refreshCh chan struct{}
	volume    coalescer
}

// Running reports whether another instance already owns the app id, so a
// second `--tray` at login doesn't start a duplicate tray.
func Running() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	_ = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, AppID).Store(&has)
	return has
}

// Run starts the application. With background set, it starts in the tray
// without showing the window.
func Run(args []string, background bool) int {
	a := &App{startHidden: background, refreshCh: make(chan struct{}, 1)}
	id := AppID
	if audio.Demo { // never hand off to (or replace) a real running instance
		id += ".Demo"
	}
	a.app = adw.NewApplication(id, gio.GApplicationFlagsNoneValue)

	startup := func(gio.Application) { a.startup() }
	activate := func(gio.Application) { a.activate_() }
	a.app.ConnectStartup(&startup)
	a.app.ConnectActivate(&activate)
	return int(a.app.Run(int32(len(args)), args))
}

func (a *App) startup() {
	gtk.WindowSetDefaultIconName(AppIcon)
	display := gdk.DisplayGetDefault()
	a.iconTheme = gtk.IconThemeGetForDisplay(display)
	for _, dir := range iconDirs() {
		a.iconTheme.AddSearchPath(dir)
	}
	provider := gtk.NewCssProvider()
	provider.LoadFromString(css)
	gtk.StyleContextAddProviderForDisplay(display, provider, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))

	a.addActions()
	a.buildWindow()

	var t *tray.Tray
	var err error
	if !audio.Demo {
		t, err = tray.New("audio-switcher", "Audio Switcher", audio.IconApp, firstOr(iconDirs(), ""))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tray unavailable:", err)
	} else if t != nil {
		a.tray = t
		a.setupTray()
		a.app.Hold() // keep running in the tray when the window is closed
	}
	a.win.SetHideOnClose(a.tray != nil)

	go a.refreshLoop()
	audio.Watch(a.refresh)
	a.refresh()
}

func (a *App) activate_() {
	if a.startHidden {
		a.startHidden = false
		if a.tray != nil {
			return
		}
	}
	a.win.Present()
}

// iconDirs returns extra icon search paths. Installed copies (the .deb, or
// --install) live in the hicolor theme; a bare binary uses the embedded set
// unpacked to the cache directory.
func iconDirs() []string {
	if dir := install.CachedIcons(); dir != "" {
		return []string{dir}
	}
	return nil
}

func firstOr(s []string, def string) string {
	if len(s) > 0 {
		return s[0]
	}
	return def
}

// ── Window ──────────────────────────────────────────────────────────────────

func (a *App) buildWindow() {
	a.win = adw.NewApplicationWindow(&a.app.Application)
	a.win.SetTitle("Audio Switcher")
	a.win.SetDefaultSize(440, 720)
	a.win.SetSizeRequest(360, 420)

	a.out = newDevicePage(a, audio.Output)
	a.in = newDevicePage(a, audio.Input)
	a.apps = newAppsPage(a)

	stack := adw.NewViewStack()
	stack.AddTitledWithIcon(&a.out.root.Widget, "output", "Output", audio.KindSpeaker.IconName())
	stack.AddTitledWithIcon(&a.in.root.Widget, "input", "Input", audio.KindMicrophone.IconName())
	stack.AddTitledWithIcon(&a.apps.root.Widget, "apps", "Apps", audio.IconApps)

	switcher := adw.NewInlineViewSwitcher()
	switcher.SetStack(stack)
	switcher.SetDisplayMode(adw.InlineViewSwitcherLabelsValue)
	switcher.SetHomogeneous(true)
	switcher.AddCssClass("flat")

	header := adw.NewHeaderBar()
	header.SetTitleWidget(&switcher.Widget)

	// A flat menu: puregotk can't pass the NULL label that unlabeled
	// sections need, and "" renders as an empty header.
	menu := gio.NewMenu()
	menu.Append("Start in Tray at Login", "app.autostart")
	menu.Append("Sound Settings", "app.settings")
	menu.Append("About Audio Switcher", "app.about")
	menu.Append("Quit", "app.quit")
	menuBtn := gtk.NewMenuButton()
	menuBtn.SetIconName("open-menu-symbolic")
	menuBtn.SetTooltipText("Main Menu")
	menuBtn.SetMenuModel(&menu.MenuModel)
	menuBtn.SetPrimary(true)
	header.PackEnd(&menuBtn.Widget)

	view := adw.NewToolbarView()
	view.AddTopBar(&header.Widget)
	view.SetContent(&stack.Widget)

	a.toasts = adw.NewToastOverlay()
	a.toasts.SetChild(&view.Widget)
	a.win.SetContent(&a.toasts.Widget)
}

// toast reports a message in the window, or as a desktop notification when
// the window is hidden (e.g. the action came from the tray menu).
func (a *App) toast(msg string) {
	if !a.win.GetVisible() {
		go notify("Audio Switcher", msg)
		return
	}
	t := adw.NewToast(escape(msg))
	t.SetTimeout(4)
	a.toasts.AddToast(t)
}

// ── Actions ─────────────────────────────────────────────────────────────────

var actionHandlers = map[uintptr]func(*gio.SimpleAction){}

var onAction = func(s gio.SimpleAction, _ uintptr) {
	if f := actionHandlers[s.GoPointer()]; f != nil {
		f(&s)
	}
}

func (a *App) addAction(name string, state *glib.Variant, f func(*gio.SimpleAction)) {
	var act *gio.SimpleAction
	if state != nil {
		act = gio.NewSimpleActionStateful(name, nil, state)
	} else {
		act = gio.NewSimpleAction(name, nil)
	}
	actionHandlers[act.GoPointer()] = f
	act.ConnectActivate(&onAction)
	a.app.AddAction(act)
}

func (a *App) addActions() {
	a.addAction("quit", nil, func(*gio.SimpleAction) { a.app.Quit() })
	a.addAction("settings", nil, func(*gio.SimpleAction) { openSoundSettings() })
	a.addAction("about", nil, func(*gio.SimpleAction) { a.showAbout() })
	a.addAction("autostart", glib.NewVariantBoolean(autostartEnabled()), func(s *gio.SimpleAction) {
		on := !autostartEnabled()
		if err := setAutostart(on); err != nil {
			a.toast(err.Error())
			return
		}
		s.SetState(glib.NewVariantBoolean(on))
	})
	a.app.SetAccelsForAction("app.quit", []string{"<Control>q"})
	a.app.SetAccelsForAction("window.close", []string{"<Control>w", "Escape"})
}

func (a *App) showAbout() {
	d := adw.NewAboutDialog()
	d.SetApplicationName("Audio Switcher")
	d.SetApplicationIcon(AppIcon)
	d.SetVersion(Version)
	d.SetDeveloperName("pckz contributors")
	d.SetComments("Switch sound outputs and inputs from the tray — HDMI monitors, Bluetooth speakers, headsets and more.")
	d.SetLicenseType(gtk.LicenseMitX11Value)
	d.Present(&a.win.Widget)
}

func openSoundSettings() {
	for _, cmd := range [][]string{
		{"gnome-control-center", "sound"},
		{"systemsettings", "kcm_pulseaudio"},
		{"pavucontrol"},
	} {
		if _, err := exec.LookPath(cmd[0]); err == nil {
			c := exec.Command(cmd[0], cmd[1:]...)
			if c.Start() == nil {
				go c.Wait()
				return
			}
		}
	}
}

// ── State ───────────────────────────────────────────────────────────────────

// refresh requests a new snapshot; requests made while one is loading are
// merged into a single follow-up load.
func (a *App) refresh() {
	select {
	case a.refreshCh <- struct{}{}:
	default:
	}
}

func (a *App) refreshLoop() {
	for range a.refreshCh {
		s, err := audio.Load()
		post(func() {
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return
			}
			a.apply(s)
		})
	}
}

func (a *App) apply(s *audio.Snapshot) {
	a.snap = s
	a.out.update(s)
	a.in.update(s)
	a.apps.update(s.Streams)
	a.updateTray()
	a.autoFix(s)
	a.restoreProfiles(s)
}

// restoreProfiles puts a card back on the configuration the user chose for it
// (e.g. SBC-XQ for a Bluetooth speaker) when the card appears — once per
// appearance, so a later manual change is not fought.
func (a *App) restoreProfiles(s *audio.Snapshot) {
	seen := map[string]bool{}
	for _, c := range s.Cards {
		seen[c.Name] = true
		if a.seenCards[c.Name] {
			continue
		}
		if want := c.RestoreTarget(); want != "" {
			name := c.Name
			go a.do(func() error { return audio.SetProfile(name, want) })
		}
	}
	a.seenCards = seen
}

// autoFix flips speakers that connected the wrong way round. An Echo only
// accepts connections it starts itself ("Alexa, connect"), and then connects
// as an audio source, as if this PC were its speaker; turning it into an
// output right away makes "Alexa, connect" just work. One attempt per device
// every 30 s, so a device that keeps refusing can't cause a loop.
func (a *App) autoFix(s *audio.Snapshot) {
	if a.busy != "" {
		return
	}
	if a.autoFixed == nil {
		a.autoFixed = map[string]time.Time{}
	}
	for _, d := range s.Outputs {
		if d.Reversed && time.Since(a.autoFixed[d.Key]) > 30*time.Second {
			a.autoFixed[d.Key] = time.Now()
			a.activate(d)
			return
		}
	}
}

// do runs a sound-server action off the main thread and reports failures.
func (a *App) do(f func() error) {
	if err := f(); err != nil {
		post(func() { a.toast(err.Error()) })
	}
	a.refresh()
}

// activate switches to a device (connecting it first if it is a Bluetooth
// device that is not connected).
func (a *App) activate(d audio.Device) {
	a.busy = d.Key
	if a.snap != nil {
		a.apply(a.snap)
	}
	go func() {
		err := audio.Activate(d)
		post(func() {
			a.busy = ""
			if err != nil {
				a.toast(err.Error())
			}
			if a.snap != nil {
				a.apply(a.snap)
			}
		})
		a.refresh()
	}()
}

// coalescer runs at most one pactl volume call per target at a time, always
// with the latest value, so dragging a slider never queues up stale calls.
type coalescer struct {
	mu      sync.Mutex
	pending map[string]func() error
	running map[string]bool
}

func (c *coalescer) set(key string, f func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending, c.running = map[string]func() error{}, map[string]bool{}
	}
	c.pending[key] = f
	if c.running[key] {
		return
	}
	c.running[key] = true
	go func() {
		for {
			c.mu.Lock()
			f, ok := c.pending[key]
			delete(c.pending, key)
			if !ok {
				c.running[key] = false
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
			_ = f()
		}
	}()
}

// ── Tray ────────────────────────────────────────────────────────────────────

func (a *App) setupTray() {
	a.tray.OnActivate = func() { post(func() { a.win.Present() }) }
	a.tray.OnSecondaryActivate = func() {
		post(func() {
			if a.snap != nil && a.snap.Sink != nil {
				ep := *a.snap.Sink
				go a.do(func() error { return audio.SetMute(audio.Output, ep.Name, !ep.Muted) })
			}
		})
	}
	a.tray.OnScroll = func(delta int) {
		post(func() {
			if a.snap == nil || a.snap.Sink == nil {
				return
			}
			step := 5
			if delta > 0 { // GNOME sends positive deltas for scrolling down
				step = -5
			}
			if strings.Contains(os.Getenv("XDG_CURRENT_DESKTOP"), "KDE") {
				step = -step
			}
			ep := a.snap.Sink
			ep.Volume = max(0, min(100, ep.Volume+step))
			name, vol := ep.Name, ep.Volume
			a.volume.set("output:"+name, func() error { return audio.SetVolume(audio.Output, name, vol) })
		})
	}
}

func (a *App) updateTray() {
	if a.tray == nil || a.snap == nil {
		return
	}
	s := a.snap
	icon := audio.IconApp
	tip := "No output device"
	if s.Sink != nil {
		icon = s.Sink.Device.Kind.IconName()
		tip = fmt.Sprintf("%s · %d%%", s.Sink.Device.Title, s.Sink.Volume)
		if s.Sink.Muted {
			icon = audio.IconMuted
			tip = s.Sink.Device.Title + " · muted"
		}
	}
	a.tray.SetIcon(icon)
	a.tray.SetTooltip("Audio Switcher", tip)

	items := []*tray.Item{
		{Label: "Open Audio Switcher", Icon: audio.IconApp, OnClick: func() { post(func() { a.win.Present() }) }},
		{Separator: true},
		{Label: "Output", Header: true},
	}
	items = append(items, a.deviceItems(s.Outputs)...)
	items = append(items, &tray.Item{Separator: true}, &tray.Item{Label: "Input", Header: true})
	items = append(items, a.deviceItems(s.Inputs)...)
	items = append(items, &tray.Item{Separator: true})
	if s.Sink != nil {
		ep := *s.Sink
		items = append(items, &tray.Item{
			Label: "Mute Output", Toggle: "checkmark", Checked: ep.Muted,
			OnClick: func() { a.do(func() error { return audio.SetMute(audio.Output, ep.Name, !ep.Muted) }) },
		})
	}
	if s.Source != nil {
		ep := *s.Source
		items = append(items, &tray.Item{
			Label: "Mute Microphone", Toggle: "checkmark", Checked: ep.Muted,
			OnClick: func() { a.do(func() error { return audio.SetMute(audio.Input, ep.Name, !ep.Muted) }) },
		})
	}
	items = append(items,
		&tray.Item{Separator: true},
		&tray.Item{Label: "Sound Settings…", OnClick: openSoundSettings},
		&tray.Item{Label: "Quit", OnClick: func() { post(func() { a.app.Quit() }) }},
	)
	a.tray.SetMenu(items)
}

func (a *App) deviceItems(devs []audio.Device) []*tray.Item {
	var items []*tray.Item
	for _, d := range devs {
		d := d
		label := d.Title
		if !d.Connected {
			label = "Connect " + d.Title
		}
		if a.busy == d.Key {
			label += " …"
		}
		items = append(items, &tray.Item{
			Label:   label,
			Icon:    d.Kind.IconName(),
			Toggle:  "radio",
			Checked: d.Active,
			OnClick: func() {
				post(func() {
					if !d.Active && a.busy == "" {
						a.activate(d)
					}
				})
			},
		})
	}
	if len(items) == 0 {
		items = append(items, &tray.Item{Label: "No devices", Disabled: true})
	}
	return items
}
