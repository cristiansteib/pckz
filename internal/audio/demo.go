package audio

import (
	"os"
	"sync"
	"time"

	"pckz/internal/bt"
)

// Demo mode (AUDIO_SWITCHER_DEMO=1) replaces the sound server with a fixed,
// made-up set of devices. It is used for screenshots and for trying the UI
// without touching real audio; switching, volume and profiles all work on
// the in-memory state.

var Demo = os.Getenv("AUDIO_SWITCHER_DEMO") == "1"

var (
	demoMu     sync.Mutex
	demoInfo   rawInfo
	demoList   rawList
	demoBT     []bt.Device
	demoNotify func()
	demoOnce   sync.Once
)

func vol(pct int) rawVolume {
	v := pct * 65536 / 100
	return rawVolume{"front-left": {Value: v}, "front-right": {Value: v}}
}

func demoInit() {
	demoOnce.Do(func() {
		type port struct {
			name, desc, typ, avail, product string
		}
		var cards []rawCard
		var sinks, sources []rawNode
		id := 100
		// card adds a card with one profile per port and a live node for each.
		card := func(name, desc, bus, ff, vendor, active string, out, in []port, extra map[string]string, profiles map[string]rawProfile) {
			id++
			c := rawCard{Name: name, ActiveProfile: active, Profiles: map[string]rawProfile{"off": {Description: "Off", Available: true}},
				Ports: map[string]rawCardPort{},
				Properties: map[string]string{"device.description": desc, "device.bus": bus, "device.form_factor": ff,
					"device.vendor.name": vendor, "object.id": itoa(id)}}
			for k, v := range extra {
				c.Properties[k] = v
			}
			for k, v := range profiles {
				c.Profiles[k] = v
			}
			for i, p := range out {
				prof := active
				if len(profiles) == 0 {
					prof = "output:" + p.name
					c.Profiles[prof] = rawProfile{Description: p.desc + " Output", Sinks: 1, Available: true, Priority: 100 - i}
				}
				c.Ports[p.name] = rawCardPort{Description: p.desc, Type: p.typ, Availability: p.avail, Priority: 100 - i,
					Properties: map[string]string{"device.product.name": p.product}, Profiles: []string{prof}}
				if prof == active || len(out) == 1 {
					sinks = append(sinks, rawNode{Name: "demo_output." + name + "." + p.name, Description: desc, ActivePort: p.name,
						Volume: vol(62), Ports: []rawPort{{Name: p.name}}, Properties: map[string]string{"device.id": itoa(id), "api.bluez5.codec": extra["demo.codec"]}})
				}
			}
			for _, p := range in {
				prof := "input:" + p.name
				c.Profiles[prof] = rawProfile{Description: p.desc + " Input", Sources: 1, Available: true, Priority: 50}
				c.Ports[p.name] = rawCardPort{Description: p.desc, Type: p.typ, Availability: p.avail, Profiles: []string{prof}}
				sources = append(sources, rawNode{Name: "demo_input." + name + "." + p.name, Description: desc, ActivePort: p.name,
					Volume: vol(80), Ports: []rawPort{{Name: p.name}}, Properties: map[string]string{"device.id": itoa(id)}})
			}
			if c.ActiveProfile == "" {
				for n := range c.Profiles {
					if n != "off" {
						c.ActiveProfile = n
					}
				}
			}
			cards = append(cards, c)
		}

		card("bluez_card.00_1B_66_00_00_01", "Sony WH-1000XM5", "bluetooth", "headphone", "", "a2dp-sink-ldac",
			[]port{{"headphone-output", "Headphone", "Headphones", "available", ""}}, nil,
			map[string]string{"api.bluez5.address": "00:1B:66:00:00:01", "demo.codec": "ldac"},
			map[string]rawProfile{
				"a2dp-sink":         {Description: "High Fidelity Playback (A2DP Sink, codec SBC)", Sinks: 1, Available: true, Priority: 40},
				"a2dp-sink-aac":     {Description: "High Fidelity Playback (A2DP Sink, codec AAC)", Sinks: 1, Available: true, Priority: 41},
				"a2dp-sink-ldac":    {Description: "High Fidelity Playback (A2DP Sink, codec LDAC)", Sinks: 1, Available: true, Priority: 42},
				"headset-head-unit": {Description: "Headset Head Unit (HSP/HFP)", Sinks: 1, Sources: 1, Available: true, Priority: 30},
			})
		// Make every port of the headphones' profiles point at all of them.
		hp := cards[len(cards)-1]
		p := hp.Ports["headphone-output"]
		p.Profiles = []string{"a2dp-sink", "a2dp-sink-aac", "a2dp-sink-ldac", "headset-head-unit"}
		hp.Ports["headphone-output"] = p

		card("alsa_card.pci-0000_01_00.1", "GPU HDMI Audio", "pci", "", "NVIDIA Corporation", "",
			[]port{{"hdmi-output-0", "DisplayPort 1", "HDMI", "available", "Dell U2723QE"}}, nil, nil, nil)
		card("alsa_card.pci-0000_05_00.1", "GPU HDMI Audio", "pci", "", "AMD", "",
			[]port{{"hdmi-output-1", "HDMI 2", "HDMI", "available", "LG OLED TV"}}, nil, nil, nil)
		card("alsa_card.usb-Jabra_Evolve2_65-00", "Jabra Evolve2 65", "usb", "headset", "GN Audio", "",
			[]port{{"analog-output", "Analog Output", "Analog", "availability unknown", ""}},
			[]port{{"analog-input-mic", "Microphone", "Mic", "availability unknown", ""}}, nil, nil)
		card("alsa_card.pci-0000_00_1f.3", "Built-in Audio", "pci", "internal", "Intel Corporation", "",
			[]port{{"analog-output-speaker", "Speakers", "Speaker", "availability unknown", ""}},
			[]port{{"analog-input-internal-mic", "Internal Microphone", "Mic", "availability unknown", ""}}, nil, nil)
		card("alsa_card.usb-Logitech_BRIO-02", "Logitech BRIO", "usb", "webcam", "Logitech", "",
			nil, []port{{"analog-input-mic", "Microphone", "Mic", "availability unknown", ""}}, nil, nil)

		sinks = append(sinks, rawNode{Name: "easyeffects_sink", Description: "Easy Effects Sink", Volume: vol(100),
			Properties: map[string]string{"node.virtual": "true"}})

		streams := []rawStream{
			{Index: 1, Volume: vol(100), Properties: map[string]string{"application.name": "Firefox", "application.icon_name": "firefox", "media.name": "Lo-fi radio — YouTube"}},
			{Index: 2, Volume: vol(70), Properties: map[string]string{"application.name": "Rhythmbox", "application.icon_name": "org.gnome.Rhythmbox3", "media.name": "Evening Jazz"}},
			{Index: 3, Volume: vol(85), Properties: map[string]string{"application.name": "VLC media player", "application.icon_name": "vlc", "media.name": "Big Buck Bunny.mkv"}},
		}

		demoList = rawList{Cards: cards, Sinks: sinks, Sources: sources, SinkInputs: streams}
		demoInfo = rawInfo{ServerName: "PulseAudio (on PipeWire 1.6.2)",
			DefaultSinkName:   sinks[0].Name,
			DefaultSourceName: "demo_input.alsa_card.usb-Logitech_BRIO-02.analog-input-mic"}
		demoBT = []bt.Device{
			{Path: "/org/bluez/hci0/dev_00_1B_66_00_00_01", Address: "00:1B:66:00:00:01", Name: "Sony WH-1000XM5", Icon: "audio-headphones", Class: 0x240418, Connected: true, CanSink: true},
			{Path: "/org/bluez/hci0/dev_00_1B_66_00_00_02", Address: "00:1B:66:00:00:02", Name: "Echo Dot", Icon: "audio-card", Class: 0x2c0414, CanSink: true},
			{Path: "/org/bluez/hci0/dev_00_1B_66_00_00_03", Address: "00:1B:66:00:00:03", Name: "JBL Flip 6", Icon: "audio-card", Class: 0x240414, CanSink: true},
			{Path: "/org/bluez/hci0/dev_00_1B_66_00_00_04", Address: "00:1B:66:00:00:04", Name: "Galaxy Buds2 Pro", Icon: "audio-headset", Class: 0x240404, CanSink: true},
		}
	})
}

func demoLoad() *Snapshot {
	demoInit()
	demoMu.Lock()
	defer demoMu.Unlock()
	return build(demoInfo, demoList, demoBT)
}

// demoChange applies f to the demo state and tells the UI to refresh.
func demoChange(f func()) error {
	demoInit()
	demoMu.Lock()
	f()
	notify := demoNotify
	demoMu.Unlock()
	if notify != nil {
		go notify()
	}
	return nil
}

func demoNode(dir Direction, name string, f func(*rawNode)) {
	nodes := demoList.Sinks
	if dir == Input {
		nodes = demoList.Sources
	}
	for i := range nodes {
		if nodes[i].Name == name {
			f(&nodes[i])
		}
	}
}

func demoActivate(d Device) error {
	if !d.Connected {
		time.Sleep(1200 * time.Millisecond) // pretend to connect
		return demoChange(func() {})
	}
	return demoChange(func() {
		if d.Dir == Input {
			demoInfo.DefaultSourceName = d.Node
		} else {
			demoInfo.DefaultSinkName = d.Node
		}
	})
}

func demoStream(index int, f func(*rawStream)) {
	for i := range demoList.SinkInputs {
		if demoList.SinkInputs[i].Index == index {
			f(&demoList.SinkInputs[i])
		}
	}
}
