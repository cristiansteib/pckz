// Package audio models the sound devices of a PulseAudio / PipeWire server
// the way GNOME Settings presents them, and switches between them.
//
// Outputs and inputs are enumerated per *card port*, not per live sink: a card
// (e.g. a GPU with several HDMI connectors) exposes only one sink at a time via
// its active profile, so listing live sinks would hide the other connectors.
// Each port maps to the profile that activates it; selecting a port switches
// the card profile when needed, then makes the resulting sink the default.
package audio

import (
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"

	"pckz/internal/bt"
)

type Direction int

const (
	Output Direction = iota
	Input
)

func (d Direction) String() string {
	if d == Input {
		return "input"
	}
	return "output"
}

// Device is one selectable output or input, as the user thinks of it.
type Device struct {
	Key       string // stable identity across snapshots
	Dir       Direction
	Title     string
	Subtitle  string
	Kind      Kind
	Active    bool // it is the current default
	Connected bool // false for a paired Bluetooth device that is not connected

	Card    string // card name, empty for card-less (virtual/network) nodes
	Port    string
	Profile string // card profile that activates Port
	Node    string // sink/source name when it currently exists

	BTPath    dbus.ObjectPath
	BTAddress string
	// Reversed: a speaker that connected as an audio *source* (an Echo after
	// "Alexa, connect"); activating it flips it into speaker mode.
	Reversed bool
	// SourceOnly: a connected device that only sends audio to this PC (it was
	// paired as "use this computer as my speaker"); it must be re-paired.
	SourceOnly bool
}

// Endpoint is the current default sink or source.
type Endpoint struct {
	Name   string
	Volume int // percent
	Muted  bool
	Device Device
}

// Profile is a card configuration ("Digital Stereo (HDMI 2) Output", "A2DP"…).
type Profile struct {
	Name        string
	Description string
	Active      bool
}

// Stream is an application playing audio.
type Stream struct {
	Index  int
	App    string
	Media  string
	Icon   string
	Volume int
	Muted  bool
	Corked bool
}

type Snapshot struct {
	Outputs []Device
	Inputs  []Device
	Sink    *Endpoint
	Source  *Endpoint

	OutputCard     string
	OutputProfiles []Profile
	InputCard      string
	InputProfiles  []Profile

	Streams  []Stream
	Cards    []Card
	PipeWire bool
}

// Load queries the sound server (and BlueZ) for the current state.
func Load() (*Snapshot, error) {
	if Demo {
		return demoLoad(), nil
	}
	var info rawInfo
	if err := pactlJSON(&info, "info"); err != nil {
		return nil, err
	}
	var l rawList
	if err := pactlJSON(&l, "list"); err != nil {
		return nil, err
	}
	return build(info, l, bt.Devices()), nil
}

func build(info rawInfo, l rawList, btDevs []bt.Device) *Snapshot {
	s := &Snapshot{PipeWire: strings.Contains(info.ServerName, "PipeWire")}
	for _, c := range l.Cards {
		card := Card{Name: c.Name, ActiveProfile: c.ActiveProfile, Available: map[string]bool{}}
		for name, p := range c.Profiles {
			card.Available[name] = p.Available
		}
		s.Cards = append(s.Cards, card)
	}

	btByAddr := map[string]bt.Device{}
	for _, d := range btDevs {
		btByAddr[strings.ToUpper(d.Address)] = d
	}

	cards := map[string]*rawCard{}
	for i := range l.Cards {
		cards[l.Cards[i].Name] = &l.Cards[i]
	}

	for _, dir := range []Direction{Output, Input} {
		nodes := l.Sinks
		def := info.DefaultSinkName
		if dir == Input {
			nodes = l.Sources
			def = info.DefaultSourceName
		}

		var devs []Device
		for i := range l.Cards {
			devs = append(devs, cardDevices(&l.Cards[i], dir, btByAddr)...)
		}

		// Attach live nodes to their card ports, and add card-less nodes
		// (virtual sinks, network sinks, filters) as devices of their own.
		for _, n := range nodes {
			if dir == Input && isMonitor(n) {
				continue
			}
			c := cardOf(n, l.Cards)
			matched := false
			if c != nil {
				for i := range devs {
					if devs[i].Card == c.Name && devs[i].Port == n.ActivePort {
						devs[i].Node = n.Name
						if codec := n.Properties["api.bluez5.codec"]; codec != "" && devs[i].Subtitle == "Bluetooth" {
							devs[i].Subtitle = "Bluetooth · " + strings.ToUpper(codec)
						}
						matched = true
					}
				}
			}
			if !matched && (c == nil || n.Name == def) {
				devs = append(devs, nodeDevice(n, dir, c))
			}
		}

		for i := range devs {
			devs[i].Active = devs[i].Node != "" && devs[i].Node == def
		}

		if dir == Output {
			devs = append(devs, disconnectedBT(btDevs, devs, l.Cards)...)
		}
		disambiguate(devs)

		var ep *Endpoint
		for _, n := range nodes {
			if n.Name == def {
				ep = &Endpoint{Name: n.Name, Volume: n.Volume.percent(), Muted: n.Mute}
				for _, d := range devs {
					if d.Active {
						ep.Device = d
					}
				}
				if ep.Device.Key == "" {
					ep.Device = nodeDevice(n, dir, cardOf(n, l.Cards))
				}
			}
		}

		var profiles []Profile
		cardName := ""
		if ep != nil && ep.Device.Card != "" {
			cardName = ep.Device.Card
			profiles = cardProfiles(cards[cardName])
		}

		if dir == Output {
			s.Outputs, s.Sink, s.OutputCard, s.OutputProfiles = devs, ep, cardName, profiles
		} else {
			s.Inputs, s.Source, s.InputCard, s.InputProfiles = devs, ep, cardName, profiles
		}
	}

	for _, si := range l.SinkInputs {
		p := si.Properties
		if p["media.role"] == "event" || p["application.id"] == "org.PulseAudio.pavucontrol" ||
			strings.HasPrefix(p["application.name"], "speech-dispatcher") {
			continue
		}
		st := Stream{
			Index:  si.Index,
			App:    first(p["application.name"], p["application.process.binary"], "Unknown application"),
			Media:  p["media.name"],
			Icon:   first(p["application.icon_name"], p["application.process.binary"]),
			Volume: si.Volume.percent(),
			Muted:  si.Mute,
			Corked: si.Corked,
		}
		if strings.EqualFold(st.Media, st.App) || strings.EqualFold(st.Media, "playback") {
			st.Media = ""
		}
		s.Streams = append(s.Streams, st)
	}
	return s
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func isMonitor(n rawNode) bool {
	return strings.HasSuffix(n.Name, ".monitor") || n.Properties["device.class"] == "monitor"
}

// cardOf finds the card a sink/source belongs to.
func cardOf(n rawNode, cards []rawCard) *rawCard {
	if id := n.Properties["device.id"]; id != "" {
		for i := range cards {
			if cards[i].Properties["object.id"] == id {
				return &cards[i]
			}
		}
	}
	// Plain PulseAudio: alsa_output.<stem>.<profile> ↔ alsa_card.<stem>
	_, nstem, ok := strings.Cut(n.Name, ".")
	if !ok {
		return nil
	}
	for i := range cards {
		if _, cstem, ok := strings.Cut(cards[i].Name, "."); ok && strings.HasPrefix(nstem, cstem) {
			return &cards[i]
		}
	}
	return nil
}

func portDirection(name string, p rawCardPort, c *rawCard) (Direction, bool) {
	ln := strings.ToLower(name)
	switch {
	case strings.Contains(ln, "output"):
		return Output, true
	case strings.Contains(ln, "input"):
		return Input, true
	}
	out, in := true, true
	for _, pn := range p.Profiles {
		pr := c.Profiles[pn]
		if pr.Sinks == 0 {
			out = false
		}
		if pr.Sources == 0 {
			in = false
		}
	}
	switch {
	case out && !in:
		return Output, true
	case in && !out:
		return Input, true
	}
	return Output, false
}

// splitProfile splits "output:hdmi-stereo+input:analog-stereo" into its
// output and input halves.
func splitProfile(name string) (out, in string) {
	for _, part := range strings.Split(name, "+") {
		switch {
		case strings.HasPrefix(part, "output:"):
			out = part
		case strings.HasPrefix(part, "input:"):
			in = part
		}
	}
	return
}

// chooseProfile picks the profile that enables a port. Like GNOME Settings it
// keeps the active profile if it already includes the port, and otherwise
// prefers a profile that leaves the other direction of the card untouched
// (so picking the front mic doesn't silence the S/PDIF output).
func chooseProfile(c *rawCard, port rawCardPort, dir Direction) string {
	ok := func(name string) bool {
		pr, exists := c.Profiles[name]
		if !exists || !pr.Available || name == "off" || name == "pro-audio" {
			return false
		}
		if dir == Output {
			return pr.Sinks > 0
		}
		return pr.Sources > 0
	}
	for _, pn := range port.Profiles {
		if pn == c.ActiveProfile && ok(pn) {
			return pn
		}
	}
	// The configuration the user picked for this card last time.
	if want := PreferredProfile(c.Name); want != "" {
		for _, pn := range port.Profiles {
			if pn == want && ok(pn) {
				return pn
			}
		}
	}
	activeOut, activeIn := splitProfile(c.ActiveProfile)
	best, bestScore := "", -1
	for _, pn := range port.Profiles {
		if !ok(pn) {
			continue
		}
		score := c.Profiles[pn].Priority
		o, i := splitProfile(pn)
		if (dir == Output && i == activeIn) || (dir == Input && o == activeOut) {
			score += 1_000_000
		}
		if score > bestScore {
			best, bestScore = pn, score
		}
	}
	return best
}

func cardDevices(c *rawCard, dir Direction, btByAddr map[string]bt.Device) []Device {
	type cand struct {
		name string
		port rawCardPort
		prof string
	}
	var detected, unknown []cand
	for name, p := range c.Ports {
		d, ok := portDirection(name, p, c)
		if !ok || d != dir || p.Availability == "not available" {
			continue
		}
		prof := chooseProfile(c, p, dir)
		if prof == "" {
			continue
		}
		if p.Availability == "available" {
			detected = append(detected, cand{name, p, prof})
		} else {
			unknown = append(unknown, cand{name, p, prof})
		}
	}
	// Show every port with something plugged in (each monitor, headphones…),
	// plus the best port whose presence can't be detected (built-in speakers,
	// S/PDIF), so the same card doesn't appear once per encoding.
	chosen := detected
	if len(unknown) > 0 {
		sort.Slice(unknown, func(i, j int) bool { return unknown[i].port.Priority > unknown[j].port.Priority })
		chosen = append(chosen, unknown[0])
	}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].port.Priority > chosen[j].port.Priority })

	props := c.Properties
	cardDesc := first(props["device.description"], props["device.product.name"], c.Name)
	bus := props["device.bus"]
	addr := strings.ToUpper(props["api.bluez5.address"])
	btDev, isBT := btByAddr[addr]
	if bus == "" && strings.HasPrefix(c.Name, "bluez") {
		bus = "bluetooth"
	}

	duplex := false
	for _, pr := range c.Profiles {
		if pr.Sinks > 0 && pr.Sources > 0 {
			duplex = true
		}
	}

	var out []Device
	for _, ch := range chosen {
		product := ch.port.Properties["device.product.name"]
		h := hints{
			dir:        dir,
			portType:   ch.port.Type,
			portName:   ch.name,
			bus:        bus,
			formFactor: props["device.form_factor"],
			iconName:   first(props["device.icon_name"], ch.port.Properties["device.icon_name"]),
			text:       strings.ToLower(strings.Join([]string{cardDesc, product, props["device.product.name"], ch.port.Description}, " ")),
			duplex:     duplex,
		}
		if isBT {
			h.btClass = btDev.Class
			h.text += " " + strings.ToLower(btDev.Name)
			if btDev.Icon != "" && h.iconName == "" {
				h.iconName = btDev.Icon
			}
		}

		d := Device{
			Key:       "card:" + c.Name + ":" + ch.name,
			Dir:       dir,
			Kind:      classify(h),
			Connected: true,
			Card:      c.Name,
			Port:      ch.name,
			Profile:   ch.prof,
			BTAddress: addr,
		}
		switch {
		case product != "":
			vendor, _, _ := strings.Cut(props["device.vendor.name"], " ")
			d.Title, d.Subtitle = product, ch.port.Description+" · "+first(vendor, cardDesc)
		case bus == "bluetooth":
			d.Title, d.Subtitle = cardDesc, "Bluetooth"
			if pr, ok := c.Profiles[c.ActiveProfile]; ok && strings.Contains(c.ActiveProfile, "head") {
				d.Subtitle = "Bluetooth · " + pr.Description
			}
		case len(chosen) == 1:
			d.Title, d.Subtitle = cardDesc, connectionLabel(ch.port, bus, dir)
		default:
			d.Title, d.Subtitle = ch.port.Description, cardDesc
		}
		out = append(out, d)
	}
	return out
}

func connectionLabel(p rawCardPort, bus string, dir Direction) string {
	switch strings.ToLower(p.Type) {
	case "hdmi":
		return p.Description
	case "spdif":
		return "Optical · S/PDIF"
	case "headphones":
		return "Headphone jack"
	case "speaker":
		return "Speakers"
	case "line":
		if dir == Input {
			return "Line in"
		}
		return "Line out"
	}
	switch bus {
	case "usb":
		return "USB"
	case "pci", "":
		return "Built-in · " + p.Description
	}
	return p.Description
}

func nodeDevice(n rawNode, dir Direction, c *rawCard) Device {
	p := n.Properties
	lname := strings.ToLower(n.Name + " " + p["factory.name"] + " " + p["media.class"])
	h := hints{
		dir:        dir,
		bus:        p["device.bus"],
		formFactor: p["device.form_factor"],
		iconName:   p["device.icon_name"],
		text:       strings.ToLower(n.Description),
		network:    hasAny(lname, []string{"tunnel", "raop", "rtp", "roc", "snapcast", "airplay"}),
		virtual: p["node.virtual"] == "true" || hasAny(lname, []string{"null", "virtual", "filter", "easyeffects", "loopback", "combine", "echo-cancel"}) ||
			(c == nil && p["device.api"] == ""),
	}
	sub := "Virtual device"
	switch {
	case h.network:
		sub = "Network"
	case c != nil:
		sub = first(c.Properties["device.description"], c.Name)
		h.virtual = false
	}
	return Device{
		Key:       "node:" + n.Name,
		Dir:       dir,
		Title:     n.Description,
		Subtitle:  sub,
		Kind:      classify(h),
		Connected: true,
		Node:      n.Name,
	}
}

// disconnectedBT lists paired audio devices that can't be played to right
// now, so the user can (re)connect and switch to them in one click: devices
// that are not connected, and speakers that connected only in the other
// direction (an Echo streaming *to* this PC exposes no output).
//
// Bluetooth is shared by every logged-in user but only the active session's
// sound server gets the device's card, so a connected device without a card
// here belongs to someone else's session and is left alone.
func disconnectedBT(devs []bt.Device, existing []Device, cards []rawCard) []Device {
	var out []Device
	for _, b := range devs {
		subtitle := "Bluetooth · Not connected"
		reversed, sourceOnly := false, false
		if b.Connected {
			if hasOutput(existing, b.Address) || !hasCard(cards, b.Address) {
				continue
			}
			if b.CanSink {
				subtitle = "Bluetooth · Connected as input — click to play through it"
				reversed = true
			} else {
				subtitle = "Bluetooth · Using this PC as its speaker — needs re-pairing"
				sourceOnly = true
			}
		} else if !b.CanSink && b.Class != 0 && !strings.HasPrefix(b.Icon, "audio-") {
			continue
		}
		h := hints{dir: Output, bus: "bluetooth", iconName: b.Icon, btClass: b.Class, text: strings.ToLower(b.Name)}
		if strings.Contains(b.Icon, "headset") {
			h.formFactor = "headset"
		} else if strings.Contains(b.Icon, "headphone") {
			h.formFactor = "headphone"
		}
		out = append(out, Device{
			Key:        "bt:" + b.Address,
			Dir:        Output,
			Title:      b.Name,
			Subtitle:   subtitle,
			Kind:       classify(h),
			BTPath:     b.Path,
			BTAddress:  strings.ToUpper(b.Address),
			Reversed:   reversed,
			SourceOnly: sourceOnly,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func hasCard(cards []rawCard, addr string) bool {
	for _, c := range cards {
		if strings.EqualFold(c.Properties["api.bluez5.address"], addr) {
			return true
		}
	}
	return false
}

func hasOutput(devs []Device, addr string) bool {
	for _, d := range devs {
		if strings.EqualFold(d.BTAddress, addr) {
			return true
		}
	}
	return false
}

// disambiguate appends the subtitle to titles that would otherwise repeat
// (two identical monitors, say), which matters in the tray menu where only
// the title is shown.
func disambiguate(devs []Device) {
	count := map[string]int{}
	for _, d := range devs {
		count[d.Title]++
	}
	for i := range devs {
		if count[devs[i].Title] > 1 {
			devs[i].Title += " (" + devs[i].Subtitle + ")"
		}
	}
}

func cardProfiles(c *rawCard) []Profile {
	if c == nil {
		return nil
	}
	type kv struct {
		name string
		p    rawProfile
	}
	var all []kv
	for n, p := range c.Profiles {
		if (p.Available || n == c.ActiveProfile) && n != "off" && n != "pro-audio" {
			all = append(all, kv{n, p})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].p.Priority != all[j].p.Priority {
			return all[i].p.Priority > all[j].p.Priority
		}
		return all[i].name < all[j].name
	})
	out := make([]Profile, 0, len(all))
	for _, e := range all {
		out = append(out, Profile{Name: e.name, Description: e.p.Description, Active: e.name == c.ActiveProfile})
	}
	return out
}
