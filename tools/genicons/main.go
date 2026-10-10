// Command genicons writes the device icon set to assets/icons/.
//
// Icons are 16×16 symbolic SVGs built from filled shapes only (no strokes):
// GTK and GNOME Shell recolor symbolic icons by overriding `fill`, so a
// stroke-based outline would render as a solid blob. Outlines are therefore
// expressed as even-odd rings.
//
// It also renders docs/icons.svg, the gallery shown in the README.
//
//	go run ./tools/genicons
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const t = 1.5 // standard line weight

type icon []string // each entry is one <path d="..."/>

func f(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

func rrect(x, y, w, h, r float64) string {
	r = math.Min(r, math.Min(w, h)/2)
	if r <= 0 {
		return fmt.Sprintf("M%s %sh%sv%sh%sz", f(x), f(y), f(w), f(h), f(-w))
	}
	return fmt.Sprintf("M%s %sh%sa%s %s 0 0 1 %s %sv%sa%s %s 0 0 1 %s %sh%sa%s %s 0 0 1 %s %sv%sa%s %s 0 0 1 %s %sz",
		f(x+r), f(y), f(w-2*r),
		f(r), f(r), f(r), f(r), f(h-2*r),
		f(r), f(r), f(-r), f(r), f(-(w - 2*r)),
		f(r), f(r), f(-r), f(-r), f(-(h - 2*r)),
		f(r), f(r), f(r), f(-r))
}

// rrectCCW is rrect wound counter-clockwise, so it punches a hole under both
// the nonzero and even-odd fill rules.
func rrectCCW(x, y, w, h, r float64) string {
	r = math.Min(r, math.Min(w, h)/2)
	if r <= 0 {
		return fmt.Sprintf("M%s %sv%sh%sv%sz", f(x), f(y), f(h), f(w), f(-h))
	}
	return fmt.Sprintf("M%s %sa%s %s 0 0 0 %s %sv%sa%s %s 0 0 0 %s %sh%sa%s %s 0 0 0 %s %sv%sa%s %s 0 0 0 %s %sz",
		f(x+r), f(y), f(r), f(r), f(-r), f(r), f(h-2*r),
		f(r), f(r), f(r), f(r), f(w-2*r),
		f(r), f(r), f(r), f(-r), f(-(h - 2*r)),
		f(r), f(r), f(-r), f(-r))
}

func ringRect(x, y, w, h, r, th float64) string {
	return rrect(x, y, w, h, r) + rrectCCW(x+th, y+th, w-2*th, h-2*th, math.Max(r-th, 0))
}

func circle(cx, cy, r float64) string {
	return fmt.Sprintf("M%s %sa%s %s 0 1 0 %s 0a%s %s 0 1 0 %s 0z",
		f(cx-r), f(cy), f(r), f(r), f(2*r), f(r), f(r), f(-2*r))
}

func circleCCW(cx, cy, r float64) string {
	return fmt.Sprintf("M%s %sa%s %s 0 1 1 %s 0a%s %s 0 1 1 %s 0z",
		f(cx-r), f(cy), f(r), f(r), f(2*r), f(r), f(r), f(-2*r))
}

func ring(cx, cy, r, th float64) string { return circle(cx, cy, r) + circleCCW(cx, cy, r-th) }

func pt(cx, cy, r, deg float64) (float64, float64) {
	a := deg * math.Pi / 180
	return cx + r*math.Cos(a), cy + r*math.Sin(a)
}

// arc is a band of thickness th whose outer radius is ro, swept clockwise
// (screen coordinates) from a0 to a1 degrees, with round caps.
func arc(cx, cy, ro, th, a0, a1 float64) string {
	ri := ro - th
	c := th / 2
	large := 0
	if a1-a0 > 180 {
		large = 1
	}
	x0, y0 := pt(cx, cy, ro, a0)
	x1, y1 := pt(cx, cy, ro, a1)
	x2, y2 := pt(cx, cy, ri, a1)
	x3, y3 := pt(cx, cy, ri, a0)
	return fmt.Sprintf("M%s %sA%s %s 0 %d 1 %s %sA%s %s 0 0 1 %s %sA%s %s 0 %d 0 %s %sA%s %s 0 0 1 %s %sz",
		f(x0), f(y0), f(ro), f(ro), large, f(x1), f(y1),
		f(c), f(c), f(x2), f(y2),
		f(ri), f(ri), large, f(x3), f(y3),
		f(c), f(c), f(x0), f(y0))
}

// line is a thick segment with round caps.
func line(x0, y0, x1, y1, th float64) string {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	nx, ny := -dy/l*th/2, dx/l*th/2
	c := th / 2
	return fmt.Sprintf("M%s %sL%s %sA%s %s 0 0 1 %s %sL%s %sA%s %s 0 0 1 %s %sz",
		f(x0+nx), f(y0+ny), f(x1+nx), f(y1+ny),
		f(c), f(c), f(x1-nx), f(y1-ny), f(x0-nx), f(y0-ny),
		f(c), f(c), f(x0+nx), f(y0+ny))
}

func poly(pts ...float64) string {
	var b strings.Builder
	for i := 0; i < len(pts); i += 2 {
		if i == 0 {
			b.WriteString("M")
		} else {
			b.WriteString("L")
		}
		b.WriteString(f(pts[i]) + " " + f(pts[i+1]))
	}
	b.WriteString("z")
	return b.String()
}

var speakerCone = poly(1.5, 5.5, 4.25, 5.5, 8, 2, 8, 14, 4.25, 10.5, 1.5, 10.5)

var icons = map[string]icon{
	"speaker": {
		speakerCone,
		arc(8, 8, 4.25, t, -48, 48),
		arc(8, 8, 7.25, t, -52, 52),
	},
	"muted": {
		speakerCone,
		line(9.75, 5.25, 14.75, 10.75, t),
		line(14.75, 5.25, 9.75, 10.75, t),
	},
	"headphones": {
		arc(8, 9, 6.75, t, 180, 360),
		rrect(1.25, 8.5, 4, 6.5, 1.5),
		rrect(10.75, 8.5, 4, 6.5, 1.5),
	},
	"headset": {
		arc(8, 8.5, 6.75, t, 180, 360),
		rrect(1.25, 8, 4, 5.5, 1.5),
		rrect(10.75, 8, 4, 5.5, 1.5),
		line(3.25, 12.5, 3.25, 14.5, 1.25),
		line(3.25, 14.5, 7.25, 14.5, 1.25),
		circle(8.5, 14.5, 1.5),
	},
	"earbuds": {
		circle(4.5, 5, 3.25),
		rrect(1.25, 6.5, 2.25, 8.5, 1.125),
		circle(11.5, 5, 3.25),
		rrect(12.5, 6.5, 2.25, 8.5, 1.125),
	},
	"display": {
		ringRect(0.75, 1.5, 14.5, 10, 1.75, t),
		rrect(7, 11, 2, 2.5, 0),
		rrect(4, 13.25, 8, 1.5, 0.75),
	},
	"tv": {
		ringRect(0.75, 4, 14.5, 9.25, 1.75, t),
		line(5, 0.9, 8, 3.75, 1.25),
		line(11, 0.9, 8, 3.75, 1.25),
		rrect(3.5, 14, 9, 1.25, 0.625),
	},
	"smart-speaker": {
		// tall cylinder with a lit top ring, like an Echo / Nest speaker
		ringRect(3.5, 0.75, 9, 14.5, 3, t),
		rrect(3.5, 0.75, 9, 4, 2.5),
		circle(6.25, 8, 0.85), circle(9.75, 8, 0.85),
		circle(8, 10, 0.85),
		circle(6.25, 12, 0.85), circle(9.75, 12, 0.85),
	},
	"portable-speaker": {
		ringRect(0.5, 3.75, 15, 8.5, 3.25, t),
		circle(5, 8, 2.1),
		circle(11, 8, 2.1),
	},
	"microphone": {
		rrect(5.5, 0.75, 5, 8.75, 2.5),
		arc(8, 6.75, 5, t, 0, 180),
		rrect(7.25, 11.5, 1.5, 2.25, 0),
		rrect(4.5, 13.5, 7, 1.5, 0.75),
	},
	"webcam": {
		ring(8, 6.5, 5.75, t),
		circle(8, 6.5, 2.25),
		rrect(7.25, 12, 1.5, 1.75, 0),
		rrect(3.5, 13.5, 9, 1.5, 0.75),
	},
	"optical": {
		poly(4, 1.5, 12, 1.5, 14.75, 4.25, 14.75, 14.5, 1.25, 14.5, 1.25, 4.25) +
			poly(4.6, 3, 2.75, 4.85, 2.75, 13, 13.25, 13, 13.25, 4.85, 11.4, 3),
		circle(8, 8.25, 2.5),
	},
	"jack": {
		rrect(6.75, 0.5, 2.5, 3.5, 1.25),
		rrect(6.75, 4.75, 2.5, 2.25, 0),
		rrect(6.75, 7.75, 2.5, 1.75, 0),
		rrect(5, 10.25, 6, 5.25, 1.25),
	},
	"sound-card": {
		ringRect(0.75, 2.5, 14.5, 9, 1.5, t),
		rrect(3.25, 5, 4.5, 4, 0.5),
		circle(11, 7, 1.75),
		rrect(3, 11, 1.5, 3, 0), rrect(5.5, 11, 1.5, 3, 0), rrect(8, 11, 1.5, 3, 0),
	},
	"computer": {
		ringRect(2.25, 2, 11.5, 8.75, 1.25, t),
		rrect(0.5, 11.75, 15, 2, 1),
	},
	"phone": {
		ringRect(4, 0.75, 8, 14.5, 2, t),
		rrect(6.5, 2.5, 3, 1, 0.5),
		circle(8, 12.75, 0.9),
	},
	"virtual": {
		rrect(2.75, 1, 1.5, 14, 0.75), circle(3.5, 10.5, 2),
		rrect(7.25, 1, 1.5, 14, 0.75), circle(8, 5, 2),
		rrect(11.75, 1, 1.5, 14, 0.75), circle(12.5, 9, 2),
	},
	"network": {
		circle(8, 12.75, 1.6),
		arc(8, 12.75, 5.5, t, 225, 315),
		arc(8, 12.75, 9, t, 228, 312),
	},
	"bluetooth": {
		poly(7.25, 0.5, 12.25, 4.75, 9.25, 8, 12.25, 11.25, 7.25, 15.5, 7.25, 9.75, 4.4, 12.6, 3.35, 11.55, 6.9, 8, 3.35, 4.45, 4.4, 3.4, 7.25, 6.25) +
			poly(8.75, 3.75, 10.1, 4.85, 8.75, 6.25) + poly(8.75, 9.75, 10.1, 11.15, 8.75, 12.25),
	},
	"apps": {
		rrect(1, 1, 6, 6, 1.5), rrect(9, 1, 6, 6, 1.5),
		rrect(1, 9, 6, 6, 1.5), rrect(9, 9, 6, 6, 1.5),
	},
	"switcher": {
		poly(0.75, 5.75, 3, 5.75, 6.25, 2.75, 6.25, 13.25, 3, 10.25, 0.75, 10.25),
		line(8.75, 5, 13, 5, t),
		poly(12, 2.25, 15.5, 5, 12, 7.75),
		line(11.5, 11, 15.25, 11, t),
		poly(12.5, 8.25, 9, 11, 12.5, 13.75),
	},
}

const appIcon = `<svg xmlns="http://www.w3.org/2000/svg" width="128" height="128" viewBox="0 0 128 128">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#62a0ea"/>
      <stop offset="1" stop-color="#813d9c"/>
    </linearGradient>
    <linearGradient id="shine" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#fff" stop-opacity=".22"/>
      <stop offset="1" stop-color="#fff" stop-opacity="0"/>
    </linearGradient>
  </defs>
  <rect x="8" y="10" width="112" height="112" rx="28" fill="#000" opacity=".18"/>
  <rect x="8" y="6" width="112" height="112" rx="28" fill="url(#bg)"/>
  <rect x="8" y="6" width="112" height="56" rx="28" fill="url(#shine)"/>
  <g transform="translate(22 20) scale(5.25)">
%s
  </g>
</svg>
`

func main() {
	dir := "assets/icons"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	must(os.MkdirAll(dir, 0o755))
	for name, paths := range icons {
		var b strings.Builder
		b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">` + "\n")
		for _, d := range paths {
			fmt.Fprintf(&b, "  <path fill=\"#2e3436\" fill-rule=\"evenodd\" d=\"%s\"/>\n", d)
		}
		b.WriteString("</svg>\n")
		file := "pckz-audio-" + name + "-symbolic.svg"
		if name == "switcher" {
			file = "pckz-audio-switcher-symbolic.svg"
		}
		must(os.WriteFile(filepath.Join(dir, file), []byte(b.String()), 0o644))
	}

	var g strings.Builder
	for _, d := range icons["switcher"] {
		fmt.Fprintf(&g, "    <path fill=\"#fff\" fill-rule=\"evenodd\" d=\"%s\"/>\n", d)
	}
	writeGallery("docs/icons.svg")
	must(os.WriteFile(filepath.Join(dir, "pckz-audio-switcher.svg"),
		[]byte(fmt.Sprintf(appIcon, strings.TrimRight(g.String(), "\n"))), 0o644))
}

// gallery lists the device kinds shown in docs/icons.svg, in order.
var gallery = []struct{ name, label string }{
	{"speaker", "Speakers"}, {"headphones", "Headphones"}, {"headset", "Headset"},
	{"earbuds", "Earbuds"}, {"display", "Monitor"}, {"tv", "TV"},
	{"smart-speaker", "Smart speaker"}, {"portable-speaker", "Portable speaker"}, {"microphone", "Microphone"},
	{"webcam", "Webcam"}, {"optical", "Optical"}, {"jack", "Line / jack"},
	{"sound-card", "Sound card"}, {"computer", "Computer"}, {"phone", "Phone"},
	{"virtual", "Virtual"}, {"network", "Network"}, {"muted", "Muted"},
}

// writeGallery renders the icon set as one SVG for the README.
func writeGallery(path string) {
	const cols, tw, th = 6, 112, 104
	rows := (len(gallery) + cols - 1) / cols
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="Cantarell, Inter, system-ui, sans-serif">`+"\n",
		cols*tw, rows*th, cols*tw, rows*th)
	for i, g := range gallery {
		x, y := (i%cols)*tw, (i/cols)*th
		fmt.Fprintf(&b, `  <g transform="translate(%d %d)">`+"\n", x, y)
		fmt.Fprintf(&b, `    <rect x="28" y="8" width="56" height="56" rx="14" fill="#3584e4"/>`+"\n")
		fmt.Fprintf(&b, `    <g transform="translate(40 20) scale(2)" fill="#ffffff">`+"\n")
		for _, d := range icons[g.name] {
			fmt.Fprintf(&b, "      <path fill-rule=\"evenodd\" d=\"%s\"/>\n", d)
		}
		fmt.Fprintf(&b, "    </g>\n")
		fmt.Fprintf(&b, `    <text x="56" y="88" text-anchor="middle" font-size="13" fill="#77767b">%s</text>`+"\n", g.label)
		b.WriteString("  </g>\n")
	}
	b.WriteString("</svg>\n")
	must(os.MkdirAll(filepath.Dir(path), 0o755))
	must(os.WriteFile(path, []byte(b.String()), 0o644))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
