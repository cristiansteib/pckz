package audio

import (
	"strings"
	"testing"

	"pckz/internal/bt"
)

const echoAddr = "00:1B:66:00:00:02"

func echoCard(active string, profiles map[string]rawProfile, ports map[string]rawCardPort) rawCard {
	return rawCard{
		Name:          "bluez_card.00_1B_66_00_00_02",
		ActiveProfile: active,
		Properties: map[string]string{
			"device.description": "Echo Dot",
			"device.bus":         "bluetooth",
			"device.form_factor": "speaker",
			"api.bluez5.address": echoAddr,
			"object.id":          "90",
		},
		Profiles: profiles,
		Ports:    ports,
	}
}

var echoBT = bt.Device{Path: "/org/bluez/hci0/dev_00_1B_66_00_00_02", Address: echoAddr,
	Name: "Echo Dot", Icon: "audio-card", Class: 0x2c0414, Connected: true, CanSink: true}

func find(devs []Device, title string) *Device {
	for i := range devs {
		if devs[i].Title == title {
			return &devs[i]
		}
	}
	return nil
}

// An Echo that connected to the PC as an audio *source* exposes only the
// "audio-gateway" profile, so it has no output port. It must still be offered
// as an output, routed through the Bluetooth reconnect path.
func TestEchoConnectedAsInputIsOffered(t *testing.T) {
	card := echoCard("audio-gateway", map[string]rawProfile{
		"off":           {Description: "Off", Available: true},
		"audio-gateway": {Description: "Audio Gateway (A2DP Source & HSP/HFP AG)", Available: true, Priority: 256},
	}, nil)
	s := build(rawInfo{ServerName: "PulseAudio (on PipeWire 1.6.2)"}, rawList{Cards: []rawCard{card}}, []bt.Device{echoBT})

	d := find(s.Outputs, "Echo Dot")
	if d == nil {
		t.Fatalf("Echo missing from outputs: %+v", s.Outputs)
	}
	if d.Connected || d.BTPath == "" {
		t.Errorf("want a Bluetooth reconnect action, got Connected=%v BTPath=%q", d.Connected, d.BTPath)
	}
	if !strings.Contains(d.Subtitle, "Connected as input") {
		t.Errorf("subtitle = %q", d.Subtitle)
	}
	if d.Kind != KindSmartSpeaker {
		t.Errorf("kind = %s", d.Kind)
	}
	if !d.Reversed {
		t.Error("want Reversed so the app flips it automatically")
	}
}

// Another user's session owns the connection: our sound server has no card
// for it, so this session must not list it or try to take it over.
func TestEchoInOtherSessionIsIgnored(t *testing.T) {
	s := build(rawInfo{ServerName: "PulseAudio (on PipeWire 1.6.2)"}, rawList{}, []bt.Device{echoBT})
	if d := find(s.Outputs, "Echo Dot"); d != nil {
		t.Errorf("unexpected %+v", d)
	}
}

// Once the speaker profile is available the Echo is a regular card output and
// must not also appear as a reconnect entry.
func TestEchoWithSinkProfileIsNormalOutput(t *testing.T) {
	card := echoCard("off", map[string]rawProfile{
		"off":       {Description: "Off", Available: true},
		"a2dp-sink": {Description: "High Fidelity Playback (A2DP Sink)", Sinks: 1, Available: true, Priority: 40},
	}, map[string]rawCardPort{
		"speaker-output": {Description: "Speaker", Type: "Speaker", Availability: "availability unknown", Profiles: []string{"a2dp-sink"}},
	})
	s := build(rawInfo{ServerName: "PulseAudio (on PipeWire 1.6.2)"}, rawList{Cards: []rawCard{card}}, []bt.Device{echoBT})

	n := 0
	for _, d := range s.Outputs {
		if strings.HasPrefix(d.Title, "Echo Dot") {
			n++
			if !d.Connected || d.Profile != "a2dp-sink" {
				t.Errorf("want a card output on a2dp-sink, got %+v", d)
			}
		}
	}
	if n != 1 {
		t.Errorf("Echo listed %d times: %+v", n, s.Outputs)
	}
}

// Paired from the Echo's side ("use this computer as a speaker"): it
// advertises only A2DP source. It is listed, flagged for re-pairing, and
// never auto-fixed.
func TestEchoSourceOnlyIsExplained(t *testing.T) {
	card := echoCard("audio-gateway", map[string]rawProfile{
		"audio-gateway": {Description: "Audio Gateway", Available: true, Priority: 256},
	}, nil)
	b := echoBT
	b.CanSink = false
	s := build(rawInfo{}, rawList{Cards: []rawCard{card}}, []bt.Device{b})
	d := find(s.Outputs, "Echo Dot")
	if d == nil || !d.SourceOnly || d.Reversed {
		t.Fatalf("got %+v", d)
	}
	if err := Activate(*d); err == nil || !strings.Contains(err.Error(), "Alexa, pair") {
		t.Errorf("Activate error = %v", err)
	}
}

func TestRememberedProfileIsPreferred(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prefsLoaded = false
	card := echoCard("off", map[string]rawProfile{
		"off":              {Description: "Off", Available: true},
		"a2dp-sink":        {Description: "SBC", Sinks: 1, Available: true, Priority: 40},
		"a2dp-sink-sbc_xq": {Description: "SBC-XQ", Sinks: 1, Available: true, Priority: 39},
	}, map[string]rawCardPort{
		"speaker-output": {Type: "Speaker", Availability: "available", Profiles: []string{"a2dp-sink", "a2dp-sink-sbc_xq"}},
	})
	build1 := func() *Snapshot {
		return build(rawInfo{}, rawList{Cards: []rawCard{card}}, []bt.Device{echoBT})
	}
	if d := find(build1().Outputs, "Echo Dot"); d == nil || d.Profile != "a2dp-sink" {
		t.Fatalf("default choice: %+v", d)
	}
	if err := RememberProfile(card.Name, "a2dp-sink-sbc_xq"); err != nil {
		t.Fatal(err)
	}
	prefsLoaded = false // reload from disk, as a new process would
	s := build1()
	if d := find(s.Outputs, "Echo Dot"); d == nil || d.Profile != "a2dp-sink-sbc_xq" {
		t.Errorf("remembered choice not used: %+v", d)
	}
	if got := s.Cards[0].RestoreTarget(); got != "a2dp-sink-sbc_xq" {
		t.Errorf("RestoreTarget = %q", got)
	}
}
