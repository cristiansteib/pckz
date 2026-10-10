package audio

import "strings"

// Kind is what a device physically is; it drives the icon shown for it.
type Kind string

const (
	KindSpeaker         Kind = "speaker"
	KindHeadphones      Kind = "headphones"
	KindHeadset         Kind = "headset"
	KindEarbuds         Kind = "earbuds"
	KindDisplay         Kind = "display"
	KindTV              Kind = "tv"
	KindSmartSpeaker    Kind = "smart-speaker"
	KindPortableSpeaker Kind = "portable-speaker"
	KindMicrophone      Kind = "microphone"
	KindWebcam          Kind = "webcam"
	KindOptical         Kind = "optical"
	KindJack            Kind = "jack"
	KindSoundCard       Kind = "sound-card"
	KindComputer        Kind = "computer"
	KindPhone           Kind = "phone"
	KindVirtual         Kind = "virtual"
	KindNetwork         Kind = "network"
)

// IconName is the themed icon installed for this kind (see icons/).
func (k Kind) IconName() string { return "pckz-audio-" + string(k) + "-symbolic" }

const (
	IconMuted = "pckz-audio-muted-symbolic"
	IconApp   = "pckz-audio-switcher-symbolic"
	IconApps  = "pckz-audio-apps-symbolic"
	IconBT    = "pckz-audio-bluetooth-symbolic"
)

// hints is everything we know about a device that helps tell what it is.
type hints struct {
	dir        Direction
	portType   string // pactl port type: HDMI, Headphones, Speaker, SPDIF, Line, Mic, Headset, Analog…
	portName   string
	bus        string // device.bus: pci, usb, bluetooth…
	formFactor string // device.form_factor: headset, headphone, speaker, tv, webcam, internal…
	iconName   string // device.icon_name / port icon / BlueZ icon
	text       string // lower-cased names and descriptions, for keyword matching
	btClass    uint32 // Bluetooth class of device, 0 if unknown
	virtual    bool
	network    bool
	duplex     bool // the card has both playback and capture (typical of headsets)
}

var (
	smartSpeakerWords = []string{"echo", "alexa", "google home", "nest audio", "nest mini", "homepod", "sonos"}
	earbudWords       = []string{"buds", "pods", "beam", "earbud", "tws", "wf-", "liberty", "freebuds"}
	tvWords           = []string{" tv", "tv ", "television", "bravia"}
	webcamWords       = []string{"webcam", "streamcam", "camera", " cam ", "brio", "c920", "c922", "c930", "c270", "kiyo", "facecam"}
	interfaceWords    = []string{"scarlett", "focusrite", "audient", "motu", "behringer", "umc", "presonus", "steinberg", "dac", "interface", "mixer"}
)

func hasAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// btMinor decodes the Audio/Video minor class of a Bluetooth class-of-device.
func btMinor(class uint32) (uint32, bool) {
	if (class>>8)&0x1f != 0x04 { // major class Audio/Video
		return 0, false
	}
	return (class >> 2) & 0x3f, true
}

func classify(h hints) Kind {
	pt := strings.ToLower(h.portType)
	pn := strings.ToLower(h.portName)
	ff := strings.ToLower(h.formFactor)
	icon := strings.ToLower(h.iconName)
	text := " " + h.text + " "

	switch {
	case h.network:
		return KindNetwork
	case h.virtual:
		return KindVirtual
	}

	if hasAny(text, smartSpeakerWords) {
		return KindSmartSpeaker
	}

	if h.dir == Input {
		switch {
		case ff == "webcam" || strings.Contains(icon, "camera") || hasAny(text, webcamWords):
			return KindWebcam
		case ff == "headset" || ff == "hands-free" || pt == "headset" || strings.Contains(pn, "headset"):
			return KindHeadset
		case h.bus == "bluetooth":
			return KindHeadset
		case h.bus == "usb" && h.duplex && !hasAny(text, interfaceWords):
			return KindHeadset
		case pt == "spdif" || strings.Contains(pn, "iec958"):
			return KindOptical
		case pt == "line":
			return KindJack
		}
		return KindMicrophone
	}

	// Output.
	if pt == "hdmi" || strings.Contains(pn, "hdmi") || strings.Contains(pn, "displayport") {
		if ff == "tv" || hasAny(text, tvWords) {
			return KindTV
		}
		return KindDisplay
	}

	if h.bus == "bluetooth" {
		if hasAny(text, earbudWords) {
			return KindEarbuds
		}
		if minor, ok := btMinor(h.btClass); ok {
			switch minor {
			case 1, 2: // wearable headset, hands-free
				return KindHeadset
			case 5: // loudspeaker
				return KindPortableSpeaker
			case 6: // headphones
				return KindHeadphones
			case 7, 10: // portable audio, HiFi
				return KindPortableSpeaker
			case 14, 15: // video monitor, display & loudspeaker
				return KindTV
			}
		}
	}

	switch ff {
	case "headset", "hands-free":
		return KindHeadset
	case "headphone":
		if hasAny(text, earbudWords) {
			return KindEarbuds
		}
		return KindHeadphones
	case "tv":
		return KindTV
	case "portable":
		return KindPortableSpeaker
	case "speaker", "hifi", "car":
		if h.bus == "bluetooth" {
			return KindPortableSpeaker
		}
		return KindSpeaker
	case "computer":
		return KindComputer
	case "handset":
		return KindPhone
	}

	switch pt {
	case "headphones":
		return KindHeadphones
	case "headset":
		return KindHeadset
	case "speaker":
		return KindSpeaker
	case "spdif":
		return KindOptical
	case "line":
		return KindJack
	}
	if strings.Contains(pn, "iec958") || strings.Contains(pn, "spdif") {
		return KindOptical
	}

	switch {
	case strings.Contains(icon, "headset"):
		return KindHeadset
	case strings.Contains(icon, "headphone"):
		return KindHeadphones
	case strings.Contains(icon, "phone"):
		return KindPhone
	case strings.Contains(icon, "video-display"):
		return KindDisplay
	}

	switch h.bus {
	case "bluetooth":
		return KindHeadphones
	case "usb":
		if h.duplex && !hasAny(text, interfaceWords) {
			return KindHeadset
		}
		return KindSoundCard
	}
	return KindSpeaker
}
