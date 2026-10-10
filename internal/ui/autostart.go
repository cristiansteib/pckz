package ui

import (
	"os"

	"path/filepath"
	"pckz/assets"
	"strings"
)

// The package installs a system-wide autostart entry so every user gets the
// tray. A user can opt out with a same-named entry in ~/.config/autostart
// carrying Hidden=true — the standard XDG override, which GNOME's "Startup
// Applications" tools also use.

const autostartFile = assets.TrayDesktopFile

func systemAutostart() string {
	dirs := os.Getenv("XDG_CONFIG_DIRS")
	if dirs == "" {
		dirs = "/etc/xdg"
	}
	for _, d := range strings.Split(dirs, ":") {
		p := filepath.Join(d, "autostart", autostartFile)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func userAutostart() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", autostartFile)
}

func autostartEnabled() bool {
	if data, err := os.ReadFile(userAutostart()); err == nil {
		s := string(data)
		return !strings.Contains(s, "\nHidden=true") && !strings.Contains(s, "X-GNOME-Autostart-enabled=false")
	}
	return systemAutostart() != ""
}

func setAutostart(on bool) error {
	user := userAutostart()
	if on && systemAutostart() != "" {
		err := os.Remove(user) // fall back to the system entry
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		return err
	}
	exe := "audio-switcher"
	if p, err := os.Executable(); err == nil && !strings.HasPrefix(p, "/usr/") {
		exe = p
	}
	entry := "[Desktop Entry]\nType=Application\nName=Audio Switcher\nExec=" + exe + " --tray\nIcon=" + AppIcon + "\nNoDisplay=true\n"
	if !on {
		entry += "Hidden=true\n"
	}
	return os.WriteFile(user, []byte(entry), 0o644)
}
