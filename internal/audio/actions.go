package audio

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"pckz/internal/bt"
)

func kindArg(dir Direction) string {
	if dir == Input {
		return "source"
	}
	return "sink"
}

// Activate makes d the default output/input, switching the card profile and
// port first if needed — the same steps GNOME Settings performs. A
// disconnected Bluetooth device is connected first.
func Activate(d Device) error {
	if Demo {
		return demoActivate(d)
	}
	if d.SourceOnly {
		hint := "put it in pairing mode"
		if d.Kind == KindSmartSpeaker {
			hint = "say \"Alexa, pair\""
		}
		return fmt.Errorf("%s is paired to use this PC as its speaker, so it can't play from it. "+
			"Remove it in Bluetooth settings, %s, and pair it again from this PC", d.Title, hint)
	}
	if !d.Connected && d.BTPath != "" {
		if err := bt.Connect(d.BTPath); err != nil {
			if errors.Is(err, bt.ErrRefused) && d.Kind == KindSmartSpeaker {
				return fmt.Errorf("%s refused the connection. Say \"Alexa, connect\" to it (Echo keeps one device at a time), or remove and re-pair it", d.Title)
			}
			return fmt.Errorf("could not connect %s: %w", d.Title, err)
		}
		return activateBT(d)
	}
	if d.Card == "" {
		if d.Node == "" {
			return errors.New("device has no sink or source")
		}
		return setDefault(d.Dir, d.Node)
	}

	var cards []rawCard
	if err := pactlJSON(&cards, "list", "cards"); err != nil {
		return err
	}
	var card *rawCard
	for i := range cards {
		if cards[i].Name == d.Card {
			card = &cards[i]
		}
	}
	if card == nil {
		return fmt.Errorf("%s is gone", d.Title)
	}
	if d.Profile != "" && card.ActiveProfile != d.Profile {
		if _, err := pactl("set-card-profile", d.Card, d.Profile); err != nil {
			return err
		}
	}

	// The sink/source shows up asynchronously after a profile switch.
	var node *rawNode
	for i := 0; i < 40 && node == nil; i++ {
		node = findNode(d, card)
		if node == nil {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if node == nil {
		return fmt.Errorf("%s did not appear after switching profile", d.Title)
	}
	if node.ActivePort != d.Port {
		for _, p := range node.Ports {
			if p.Name == d.Port {
				_, _ = pactl("set-"+kindArg(d.Dir)+"-port", node.Name, d.Port)
			}
		}
	}
	return setDefault(d.Dir, node.Name)
}

func findNode(d Device, card *rawCard) *rawNode {
	var nodes []rawNode
	if err := pactlJSON(&nodes, "list", kindArg(d.Dir)+"s"); err != nil {
		return nil
	}
	var fallback *rawNode
	for i := range nodes {
		n := &nodes[i]
		if d.Dir == Input && isMonitor(*n) {
			continue
		}
		if c := cardOf(*n, []rawCard{*card}); c == nil {
			continue
		}
		for _, p := range n.Ports {
			if p.Name == d.Port {
				return n
			}
		}
		if len(n.Ports) == 0 && fallback == nil { // drivers that expose no ports
			fallback = n
		}
	}
	return fallback
}

func activateBT(d Device) error {
	for i := 0; i < 50; i++ { // the card takes a moment to appear after connecting
		var cards []rawCard
		if pactlJSON(&cards, "list", "cards") == nil {
			for _, c := range cards {
				if strings.EqualFold(c.Properties["api.bluez5.address"], d.BTAddress) {
					ensurePlayback(c)
				}
			}
		}
		var sinks []rawNode
		if err := pactlJSON(&sinks, "list", "sinks"); err == nil {
			for _, s := range sinks {
				if strings.EqualFold(s.Properties["api.bluez5.address"], d.BTAddress) {
					return setDefault(Output, s.Name)
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil // connected; the session manager picks its own default
}

// ensurePlayback switches a Bluetooth card that is "off" (or in a profile
// without playback) to its best playback profile.
func ensurePlayback(c rawCard) {
	if c.Profiles[c.ActiveProfile].Sinks > 0 {
		return
	}
	if want := PreferredProfile(c.Name); c.Profiles[want].Available && c.Profiles[want].Sinks > 0 {
		_, _ = pactl("set-card-profile", c.Name, want)
		return
	}
	best, prio := "", -1
	for name, p := range c.Profiles {
		if p.Available && p.Sinks > 0 && p.Priority > prio {
			best, prio = name, p.Priority
		}
	}
	if best != "" {
		_, _ = pactl("set-card-profile", c.Name, best)
	}
}

func setDefault(dir Direction, name string) error {
	if _, err := pactl("set-default-"+kindArg(dir), name); err != nil {
		return err
	}
	// PipeWire moves streams that follow the default on its own; moving them
	// explicitly would pin them to this device. Plain PulseAudio needs it.
	var info rawInfo
	if pactlJSON(&info, "info") == nil && strings.Contains(info.ServerName, "PipeWire") {
		return nil
	}
	listKind, moveCmd := "sink-inputs", "move-sink-input"
	if dir == Input {
		listKind, moveCmd = "source-outputs", "move-source-output"
	}
	var streams []rawStream
	if pactlJSON(&streams, "list", listKind) == nil {
		for _, s := range streams {
			_, _ = pactl(moveCmd, itoa(s.Index), name)
		}
	}
	return nil
}

// SetProfile switches a card to another configuration.
func SetProfile(card, profile string) error {
	if Demo {
		return demoChange(func() {
			for i := range demoList.Cards {
				if demoList.Cards[i].Name == card {
					demoList.Cards[i].ActiveProfile = profile
				}
			}
		})
	}
	_, err := pactl("set-card-profile", card, profile)
	return err
}

func clamp(pct int) int { return max(0, min(pct, 150)) }

// SetVolume sets the volume of a sink or source, in percent.
func SetVolume(dir Direction, name string, pct int) error {
	if Demo {
		return demoChange(func() { demoNode(dir, name, func(n *rawNode) { n.Volume = vol(clamp(pct)) }) })
	}
	_, err := pactl("set-"+kindArg(dir)+"-volume", name, itoa(clamp(pct))+"%")
	return err
}

func SetMute(dir Direction, name string, mute bool) error {
	if Demo {
		return demoChange(func() { demoNode(dir, name, func(n *rawNode) { n.Mute = mute }) })
	}
	_, err := pactl("set-"+kindArg(dir)+"-mute", name, boolArg(mute))
	return err
}

func SetStreamVolume(index, pct int) error {
	if Demo {
		return demoChange(func() { demoStream(index, func(s *rawStream) { s.Volume = vol(clamp(pct)) }) })
	}
	_, err := pactl("set-sink-input-volume", itoa(index), itoa(clamp(pct))+"%")
	return err
}

func SetStreamMute(index int, mute bool) error {
	if Demo {
		return demoChange(func() { demoStream(index, func(s *rawStream) { s.Mute = mute }) })
	}
	_, err := pactl("set-sink-input-mute", itoa(index), boolArg(mute))
	return err
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
