package ui

import (
	"fmt"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"pckz/internal/audio"
)

// devicePage is the Output or Input tab: the current device with its volume,
// the list of devices to switch to, and the card configuration.
type devicePage struct {
	app *App
	dir audio.Direction

	root *gtk.ScrolledWindow

	heroIcon  *gtk.Image
	heroTitle *gtk.Label
	heroSub   *gtk.Label
	mute      *gtk.Button
	scale     *gtk.Scale
	pct       *gtk.Label
	endpoint  *audio.Endpoint
	lastUser  time.Time // last time the user moved the slider

	list    *adw.PreferencesGroup
	rows    []*deviceRow
	listSig string

	profGroup *adw.PreferencesGroup
	profRow   *adw.ExpanderRow
	profRows  []*adw.ActionRow
	profSig   string
}

type deviceRow struct {
	dev     audio.Device
	row     *adw.ActionRow
	tile    *gtk.Box
	check   *gtk.Image
	spinner *gtk.Spinner
}

func newDevicePage(app *App, dir audio.Direction) *devicePage {
	p := &devicePage{app: app, dir: dir}

	box := gtk.NewBox(gtk.OrientationVerticalValue, 24)
	box.SetMarginTop(18)
	box.SetMarginBottom(24)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)

	box.Append(&p.buildHero().Widget)

	p.list = adw.NewPreferencesGroup()
	if dir == audio.Output {
		p.list.SetTitle("Play sound through")
	} else {
		p.list.SetTitle("Record sound from")
	}
	box.Append(&p.list.Widget)

	p.profGroup = adw.NewPreferencesGroup()
	p.profRow = adw.NewExpanderRow()
	p.profRow.SetTitle("Configuration")
	p.profGroup.Add(&p.profRow.Widget)
	p.profGroup.SetVisible(false)
	box.Append(&p.profGroup.Widget)

	clamp := adw.NewClamp()
	clamp.SetMaximumSize(520)
	clamp.SetChild(&box.Widget)

	p.root = gtk.NewScrolledWindow()
	p.root.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	p.root.SetChild(&clamp.Widget)
	return p
}

func (p *devicePage) buildHero() *gtk.Box {
	hero := gtk.NewBox(gtk.OrientationVerticalValue, 14)
	hero.AddCssClass("card")
	hero.AddCssClass("hero")

	top := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
	tile := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	tile.AddCssClass("hero-icon")
	tile.SetValign(gtk.AlignCenterValue)
	tile.SetHomogeneous(true)
	p.heroIcon = gtk.NewImage()
	p.heroIcon.SetPixelSize(30)
	p.heroIcon.SetHalign(gtk.AlignCenterValue)
	tile.Append(&p.heroIcon.Widget)
	top.Append(&tile.Widget)

	labels := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	labels.SetValign(gtk.AlignCenterValue)
	labels.SetHexpand(true)
	caption := gtk.NewLabel("NOW PLAYING ON")
	if p.dir == audio.Input {
		caption.SetLabel("LISTENING THROUGH")
	}
	caption.AddCssClass("caption-heading")
	caption.AddCssClass("accent")
	caption.SetXalign(0)
	p.heroTitle = gtk.NewLabel("")
	p.heroTitle.AddCssClass("title-3")
	p.heroTitle.SetXalign(0)
	p.heroTitle.SetEllipsize(pango.EllipsizeEndValue)
	p.heroSub = gtk.NewLabel("")
	p.heroSub.AddCssClass("dim-label")
	p.heroSub.SetXalign(0)
	p.heroSub.SetEllipsize(pango.EllipsizeEndValue)
	labels.Append(&caption.Widget)
	labels.Append(&p.heroTitle.Widget)
	labels.Append(&p.heroSub.Widget)
	top.Append(&labels.Widget)
	hero.Append(&top.Widget)

	vol := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	p.mute = gtk.NewButtonFromIconName("audio-volume-high-symbolic")
	p.mute.AddCssClass("flat")
	p.mute.AddCssClass("circular")
	p.mute.SetValign(gtk.AlignCenterValue)
	onClick(p.mute, func() {
		if ep := p.endpoint; ep != nil {
			muted := !ep.Muted
			ep.Muted = muted
			p.updateVolumeWidgets()
			go p.app.do(func() error { return audio.SetMute(p.dir, ep.Name, muted) })
		}
	})
	p.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontalValue, 0, 100, 1)
	p.scale.SetDrawValue(false)
	p.scale.SetHexpand(true)
	p.scale.AddCssClass("volume")
	onValue(p.scale, func(v float64) {
		ep := p.endpoint
		if ep == nil {
			return
		}
		p.lastUser = time.Now()
		ep.Volume = int(v + 0.5)
		if ep.Muted && ep.Volume > 0 { // like GNOME: moving the slider unmutes
			ep.Muted = false
			go p.app.do(func() error { return audio.SetMute(p.dir, ep.Name, false) })
		}
		p.updateVolumeWidgets()
		p.app.volume.set(p.dir.String()+":"+ep.Name, func() error {
			return audio.SetVolume(p.dir, ep.Name, ep.Volume)
		})
	})
	p.pct = gtk.NewLabel("")
	p.pct.AddCssClass("numeric")
	p.pct.AddCssClass("volume-pct")
	p.pct.SetXalign(1)
	vol.Append(&p.mute.Widget)
	vol.Append(&p.scale.Widget)
	vol.Append(&p.pct.Widget)
	hero.Append(&vol.Widget)
	return hero
}

func volumeIcon(dir audio.Direction, vol int, muted bool) string {
	base := "audio-volume-"
	if dir == audio.Input {
		base = "microphone-sensitivity-"
	}
	switch {
	case muted || vol == 0:
		return base + "muted-symbolic"
	case vol < 34:
		return base + "low-symbolic"
	case vol < 67:
		return base + "medium-symbolic"
	}
	return base + "high-symbolic"
}

func (p *devicePage) updateVolumeWidgets() {
	ep := p.endpoint
	if ep == nil {
		return
	}
	p.mute.SetIconName(volumeIcon(p.dir, ep.Volume, ep.Muted))
	if ep.Muted {
		p.mute.SetTooltipText("Unmute")
		p.pct.SetLabel("Muted")
		p.scale.AddCssClass("muted")
	} else {
		p.mute.SetTooltipText("Mute")
		p.pct.SetLabel(fmt.Sprintf("%d%%", ep.Volume))
		p.scale.RemoveCssClass("muted")
	}
}

func (p *devicePage) update(s *audio.Snapshot) {
	ep, devs, profiles, card := s.Sink, s.Outputs, s.OutputProfiles, s.OutputCard
	if p.dir == audio.Input {
		ep, devs, profiles, card = s.Source, s.Inputs, s.InputProfiles, s.InputCard
	}

	// Hero.
	if ep != nil {
		p.heroIcon.SetFromIconName(ep.Device.Kind.IconName())
		p.heroTitle.SetLabel(ep.Device.Title)
		p.heroSub.SetLabel(ep.Device.Subtitle)
		dragging := time.Since(p.lastUser) < 600*time.Millisecond
		if p.endpoint != nil && p.endpoint.Name == ep.Name && dragging {
			ep.Volume = p.endpoint.Volume // don't fight the user's drag
		} else {
			setValue(p.scale, float64(min(ep.Volume, 100)))
		}
		p.endpoint = ep
		p.scale.SetSensitive(true)
		p.mute.SetSensitive(true)
		p.updateVolumeWidgets()
	} else {
		p.endpoint = nil
		p.heroIcon.SetFromIconName(audio.KindSpeaker.IconName())
		p.heroTitle.SetLabel("No device")
		p.heroSub.SetLabel("")
		p.pct.SetLabel("")
		p.scale.SetSensitive(false)
		p.mute.SetSensitive(false)
	}

	p.updateList(devs)
	p.updateProfiles(card, profiles)
}

func (p *devicePage) updateList(devs []audio.Device) {
	var sig strings.Builder
	for _, d := range devs {
		fmt.Fprintf(&sig, "%s|%s|%s|%s|%v\n", d.Key, d.Title, d.Subtitle, d.Kind, d.Connected)
	}
	if sig.String() != p.listSig {
		p.listSig = sig.String()
		for _, r := range p.rows {
			forget(r.row.GoPointer())
			p.list.Remove(&r.row.Widget)
		}
		p.rows = nil
		for _, d := range devs {
			p.rows = append(p.rows, p.newRow(d))
		}
		if len(devs) == 0 {
			p.list.SetDescription("No devices found")
		} else {
			p.list.SetDescription("")
		}
	}
	for i, r := range p.rows {
		r.dev = devs[i]
		busy := p.app.busy == r.dev.Key
		r.spinner.SetVisible(busy)
		r.spinner.SetSpinning(busy)
		r.check.SetVisible(r.dev.Active && !busy)
		if r.dev.Active {
			r.tile.AddCssClass("active")
		} else {
			r.tile.RemoveCssClass("active")
		}
	}
}

func (p *devicePage) newRow(d audio.Device) *deviceRow {
	r := &deviceRow{dev: d}
	r.row = adw.NewActionRow()
	r.row.SetTitle(escape(d.Title))
	r.row.SetSubtitle(escape(d.Subtitle))
	r.row.SetTitleLines(1)
	r.row.SetSubtitleLines(1)
	r.row.SetActivatable(true)

	r.tile = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	r.tile.AddCssClass("device-icon")
	r.tile.SetValign(gtk.AlignCenterValue)
	r.tile.SetHomogeneous(true)
	img := gtk.NewImageFromIconName(d.Kind.IconName())
	img.SetPixelSize(20)
	img.SetHalign(gtk.AlignCenterValue)
	r.tile.Append(&img.Widget)
	r.row.AddPrefix(&r.tile.Widget)

	if !d.Connected {
		r.row.AddCssClass("disconnected")
		bt := gtk.NewImageFromIconName(audio.IconBT)
		bt.AddCssClass("dim-label")
		bt.SetTooltipText("Click to connect")
		r.row.AddSuffix(&bt.Widget)
	}
	r.spinner = gtk.NewSpinner()
	r.spinner.SetVisible(false)
	r.row.AddSuffix(&r.spinner.Widget)
	r.check = gtk.NewImageFromIconName("object-select-symbolic")
	r.check.AddCssClass("accent")
	r.check.SetVisible(false)
	r.row.AddSuffix(&r.check.Widget)

	onRow(r.row, func() {
		if r.dev.Active || p.app.busy != "" {
			return
		}
		p.app.activate(r.dev)
	})
	p.list.Add(&r.row.Widget)
	return r
}

func (p *devicePage) updateProfiles(card string, profiles []audio.Profile) {
	if len(profiles) < 2 {
		p.profGroup.SetVisible(false)
		return
	}
	p.profGroup.SetVisible(true)

	var sig strings.Builder
	sig.WriteString(card)
	for _, pr := range profiles {
		fmt.Fprintf(&sig, "|%s:%v", pr.Name, pr.Active)
	}
	if sig.String() == p.profSig {
		return
	}
	p.profSig = sig.String()

	for _, r := range p.profRows {
		forget(r.GoPointer())
		p.profRow.Remove(&r.Widget)
	}
	p.profRows = nil
	for _, pr := range profiles {
		pr := pr
		r := adw.NewActionRow()
		r.SetTitle(escape(pr.Description))
		r.SetActivatable(true)
		check := gtk.NewImageFromIconName("object-select-symbolic")
		check.AddCssClass("accent")
		check.SetVisible(pr.Active)
		r.AddSuffix(&check.Widget)
		if pr.Active {
			p.profRow.SetSubtitle(escape(pr.Description))
		}
		onRow(r, func() {
			if pr.Active {
				return
			}
			go p.app.do(func() error { return audio.ChooseProfile(card, pr.Name) })
		})
		p.profRow.AddRow(&r.Widget)
		p.profRows = append(p.profRows, r)
	}
}

// escape makes text safe for widgets that interpret Pango markup.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
