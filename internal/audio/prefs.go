package audio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Remembered card configurations: when the user picks a profile for a card
// (say SBC-XQ for a Bluetooth speaker, or 5.1 for an HDMI receiver), it is
// saved per card and restored whenever that card shows up again. Card names
// are stable across reconnects and reboots (bluez_card.<MAC>,
// alsa_card.<bus path>), so they make good keys.

type prefs struct {
	Profiles map[string]string `json:"profiles"` // card name → profile name
}

var (
	prefsMu     sync.Mutex
	prefsCache  *prefs
	prefsLoaded bool
)

func prefsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pckz-audio-switcher", "prefs.json")
}

func loadPrefs() *prefs {
	if !prefsLoaded {
		prefsLoaded = true
		prefsCache = &prefs{}
		if data, err := os.ReadFile(prefsPath()); err == nil {
			_ = json.Unmarshal(data, prefsCache)
		}
		if prefsCache.Profiles == nil {
			prefsCache.Profiles = map[string]string{}
		}
	}
	return prefsCache
}

// PreferredProfile returns the profile the user chose for a card, or "".
func PreferredProfile(card string) string {
	if Demo {
		return ""
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	return loadPrefs().Profiles[card]
}

// RememberProfile saves the user's profile choice for a card.
func RememberProfile(card, profile string) error {
	if Demo {
		return nil
	}
	prefsMu.Lock()
	defer prefsMu.Unlock()
	p := loadPrefs()
	if p.Profiles[card] == profile {
		return nil
	}
	p.Profiles[card] = profile
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := prefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ChooseProfile switches a card's profile because the user asked for it, and
// remembers the choice.
func ChooseProfile(card, profile string) error {
	if err := SetProfile(card, profile); err != nil {
		return err
	}
	return RememberProfile(card, profile)
}

// Card is the profile state of a card, used to restore remembered choices.
type Card struct {
	Name          string
	ActiveProfile string
	Available     map[string]bool
}

// RestoreTarget returns the remembered profile this card should switch to,
// or "" if there is none, it is already active, or it is unavailable.
func (c Card) RestoreTarget() string {
	want := PreferredProfile(c.Name)
	if want == "" || want == c.ActiveProfile || !c.Available[want] {
		return ""
	}
	return want
}
