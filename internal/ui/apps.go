package ui

import (
	"fmt"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"pckz/internal/audio"
)

// appsPage lists applications that are playing sound, each with its own
// volume, like the "Volume Levels" section of GNOME Settings.
type appsPage struct {
	app  *App
	root *gtk.Stack

	empty *adw.StatusPage
	group *adw.PreferencesGroup
	rows  []*streamRow
	sig   string
}

type streamRow struct {
	st       audio.Stream
	row      *adw.ActionRow
	mute     *gtk.Button
	scale    *gtk.Scale
	lastUser time.Time
}

func newAppsPage(app *App) *appsPage {
	p := &appsPage{app: app}

	p.empty = adw.NewStatusPage()
	p.empty.SetIconName(audio.IconApps)
	p.empty.SetTitle("Nothing is playing")
	p.empty.SetDescription("Applications appear here while they play sound, each with its own volume.")
	p.empty.AddCssClass("compact")

	p.group = adw.NewPreferencesGroup()
	p.group.SetTitle("Playing now")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	box.SetMarginTop(18)
	box.SetMarginBottom(24)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)
	box.Append(&p.group.Widget)
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(520)
	clamp.SetChild(&box.Widget)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetChild(&clamp.Widget)

	p.root = gtk.NewStack()
	p.root.AddNamed(&p.empty.Widget, "empty")
	p.root.AddNamed(&scroll.Widget, "list")
	return p
}

func (p *appsPage) update(streams []audio.Stream) {
	if len(streams) == 0 {
		p.root.SetVisibleChildName("empty")
	} else {
		p.root.SetVisibleChildName("list")
	}

	var sig strings.Builder
	for _, s := range streams {
		fmt.Fprintf(&sig, "%d|%s|%s|%s\n", s.Index, s.App, s.Media, s.Icon)
	}
	if sig.String() != p.sig {
		p.sig = sig.String()
		for _, r := range p.rows {
			forget(r.row.GoPointer(), r.mute.GoPointer(), r.scale.GoPointer())
			p.group.Remove(&r.row.Widget)
		}
		p.rows = nil
		for _, s := range streams {
			p.rows = append(p.rows, p.newRow(s))
		}
	}
	for i, r := range p.rows {
		st := streams[i]
		if time.Since(r.lastUser) < 600*time.Millisecond {
			st.Volume = r.st.Volume
		} else {
			setValue(r.scale, float64(min(st.Volume, 100)))
		}
		r.st = st
		r.refresh()
	}
}

func (r *streamRow) refresh() {
	r.mute.SetIconName(volumeIcon(audio.Output, r.st.Volume, r.st.Muted))
	if r.st.Muted {
		r.scale.AddCssClass("muted")
	} else {
		r.scale.RemoveCssClass("muted")
	}
}

func (p *appsPage) newRow(st audio.Stream) *streamRow {
	r := &streamRow{st: st}
	r.row = adw.NewActionRow()
	r.row.SetTitle(escape(st.App))
	r.row.SetSubtitle(escape(st.Media))
	r.row.SetTitleLines(1)
	r.row.SetSubtitleLines(1)

	icon := st.Icon
	if icon == "" || !p.app.iconTheme.HasIcon(icon) {
		icon = "application-x-executable"
	}
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(32)
	img.AddCssClass("lowres-icon")
	r.row.AddPrefix(&img.Widget)

	r.mute = gtk.NewButtonFromIconName("audio-volume-high-symbolic")
	r.mute.AddCssClass("flat")
	r.mute.AddCssClass("circular")
	r.mute.SetValign(gtk.AlignCenterValue)
	onClick(r.mute, func() {
		r.st.Muted = !r.st.Muted
		r.refresh()
		idx, m := r.st.Index, r.st.Muted
		go p.app.do(func() error { return audio.SetStreamMute(idx, m) })
	})

	r.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontalValue, 0, 100, 1)
	r.scale.SetDrawValue(false)
	r.scale.SetSizeRequest(150, -1)
	r.scale.SetValign(gtk.AlignCenterValue)
	setValue(r.scale, float64(min(st.Volume, 100)))
	onValue(r.scale, func(v float64) {
		r.lastUser = time.Now()
		r.st.Volume = int(v + 0.5)
		r.refresh()
		idx, vol := r.st.Index, r.st.Volume
		p.app.volume.set(fmt.Sprint("stream:", idx), func() error { return audio.SetStreamVolume(idx, vol) })
	})

	r.row.AddSuffix(&r.scale.Widget)
	r.row.AddSuffix(&r.mute.Widget)
	p.group.Add(&r.row.Widget)
	r.refresh()
	return r
}
