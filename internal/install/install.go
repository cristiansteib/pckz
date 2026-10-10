// Package install sets the app up for the current user from the embedded
// assets: icons, the launcher entry, and the tray at login. It is what makes
// a bare binary (mise, go install) behave like the .deb.
package install

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"

	"pckz/assets"
)

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

func iconDir() string { return filepath.Join(dataHome(), "icons", "hicolor", "scalable", "apps") }

func appEntry() string { return filepath.Join(dataHome(), "applications", assets.AppDesktopFile) }

func trayEntry() string { return filepath.Join(configHome(), "autostart", assets.TrayDesktopFile) }

// command returns how desktop entries should start the app, preferring a
// path that survives upgrades:
//   - mise installs: the mise shim (installs live in per-version directories)
//   - the .deb: /usr/bin/audio-switcher
//   - otherwise: whatever is on PATH, or this executable
func command() string {
	exe, err := os.Executable()
	if err != nil {
		return "audio-switcher"
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if i := strings.Index(exe, "/installs/"); i > 0 && strings.Contains(exe, "/mise/") {
		shim := filepath.Join(exe[:i], "shims", "audio-switcher")
		if _, err := os.Stat(shim); err == nil {
			return shim
		}
		return exe
	}
	if strings.HasPrefix(exe, "/usr/") {
		return exe
	}
	if p, err := exec.LookPath("audio-switcher"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
	}
	return exe
}

// Icons writes the icon set into the user's icon theme.
func Icons() error {
	dir := iconDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(assets.Icons, "icons", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := assets.Icons.ReadFile(path)
		if err != nil {
			return err
		}
		return writeIfChanged(filepath.Join(dir, d.Name()), data)
	})
}

func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

func entry(name, exec string) ([]byte, error) {
	data, err := assets.Desktop.ReadFile("desktop/" + name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "Exec=audio-switcher"); ok {
			line = "Exec=" + exec + rest
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n")), nil
}

// Install sets the app up for the current user and reports what it did.
// With start, the tray is launched right away instead of at next login.
func Install(start bool) error {
	cmd := command()
	if err := Icons(); err != nil {
		return err
	}
	for _, f := range []struct{ name, path string }{
		{assets.AppDesktopFile, appEntry()},
		{assets.TrayDesktopFile, trayEntry()},
	} {
		data, err := entry(f.name, cmd)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, data, 0o644); err != nil {
			return err
		}
	}
	_ = exec.Command("gtk-update-icon-cache", "-q", "-t", "-f", filepath.Join(dataHome(), "icons", "hicolor")).Run()
	_ = exec.Command("update-desktop-database", "-q", filepath.Dir(appEntry())).Run()

	fmt.Printf("Installed for %s:\n  launcher   %s\n  autostart  %s\n  icons      %s\n",
		os.Getenv("USER"), appEntry(), trayEntry(), iconDir())
	fmt.Printf("Starts with: %s\n", cmd)

	if !start {
		return nil
	}
	if running() {
		fmt.Println("The tray is already running; it uses the new entries from the next login.")
		return nil
	}
	tray := exec.Command(cmd, "--tray")
	if err := tray.Start(); err == nil {
		_ = tray.Process.Release()
		fmt.Println("The tray icon is running.")
	}
	return nil
}

// Uninstall removes what Install created (remembered settings are kept).
func Uninstall() error {
	paths := []string{appEntry(), trayEntry()}
	entries, _ := fs.ReadDir(assets.Icons, "icons")
	for _, e := range entries {
		paths = append(paths, filepath.Join(iconDir(), e.Name()))
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	fmt.Println("Removed the launcher, autostart entry and icons.")
	stopOthers()
	return nil
}

// running reports whether this user's session already has an instance.
func running() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	_ = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, assets.AppID).Store(&has)
	return has
}

// stopOthers ends this user's running instances (not this process).
func stopOthers() {
	out, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-x", "audio-switcher").Output()
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil && pid != os.Getpid() {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
}

// CachedIcons returns a directory holding the icon set, for running without
// installing anything (GTK and the panel get it as an extra search path).
func CachedIcons() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(base, "pckz-audio-switcher", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	entries, _ := fs.ReadDir(assets.Icons, "icons")
	for _, e := range entries {
		data, _ := assets.Icons.ReadFile("icons/" + e.Name())
		if writeIfChanged(filepath.Join(dir, e.Name()), data) != nil {
			return ""
		}
	}
	return dir
}
