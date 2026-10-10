// Package update finds out whether a newer release exists and upgrades the
// app the same way it was installed (mise or .deb).
//
// The repository to check is injected at build time (-X …update.Repo=owner/name,
// set by the release workflow from GITHUB_REPOSITORY), so source builds don't
// check anything and the code carries no account name.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var (
	Repo    string // "owner/name"; empty disables update checks
	Current string // running version, e.g. "1.0.1"
)

// Enabled reports whether this build knows where its releases live.
func Enabled() bool {
	return Repo != "" && os.Getenv("AUDIO_SWITCHER_NO_UPDATE_CHECK") == "" && parse(Current) != nil
}

type Release struct {
	Version string // without the leading "v"
	URL     string // release page
	assets  map[string]string
}

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context) (*Release, error) {
	if Repo == "" {
		return nil, errors.New("this build has no release repository")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "audio-switcher/"+Current)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag    string `json:"tag_name"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	r := &Release{Version: strings.TrimPrefix(body.Tag, "v"), URL: body.URL, assets: map[string]string{}}
	for _, a := range body.Assets {
		r.assets[a.Name] = a.URL
	}
	return r, nil
}

// Check returns the latest release if it is newer than the running version.
func Check(ctx context.Context) (*Release, error) {
	r, err := Latest(ctx)
	if err != nil {
		return nil, err
	}
	if !Newer(r.Version, Current) {
		return nil, nil
	}
	return r, nil
}

func parse(v string) []int {
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// Newer reports whether version a is newer than b.
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

// ── Install method ──────────────────────────────────────────────────────────

type Method int

const (
	Manual Method = iota
	Mise
	Deb
)

func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r
	}
	return exe
}

// MiseDataDir returns mise's data directory when exe was installed by mise:
// mise unpacks tools to <data>/installs/<tool>/<version>/ and leaves a
// .mise.backend.toml in <tool>. Checking for that file works with any
// MISE_DATA_DIR, not just the default ~/.local/share/mise.
func MiseDataDir(exe string) string {
	tool := filepath.Dir(filepath.Dir(exe))
	if _, err := os.Stat(filepath.Join(tool, ".mise.backend.toml")); err != nil {
		return ""
	}
	return filepath.Dir(filepath.Dir(tool))
}

// Detect tells how the running binary was installed.
func Detect() Method {
	exe := executable()
	switch {
	case MiseDataDir(exe) != "":
		return Mise
	case strings.HasPrefix(exe, "/usr/bin/"):
		if exec.Command("dpkg", "-S", exe).Run() == nil {
			return Deb
		}
	}
	return Manual
}

func (r *Release) miseSpec() string {
	return fmt.Sprintf("github:%s[matching=.tar.gz]@%s", Repo, r.Version)
}

func (r *Release) debName() string {
	return fmt.Sprintf("pckz-audio-switcher_%s_%s.deb", r.Version, runtime.GOARCH)
}

// Command is what the user would run by hand to upgrade.
func (r *Release) Command() string {
	switch Detect() {
	case Mise:
		return fmt.Sprintf("mise use -g '%s'", r.miseSpec())
	case Deb:
		return fmt.Sprintf("curl -fLo /tmp/%[1]s %[2]s && sudo apt install /tmp/%[1]s", r.debName(), r.assets[r.debName()])
	}
	return r.URL
}

// CanApply reports whether Apply can upgrade this installation by itself.
func CanApply() bool { return Detect() != Manual }

func findMise() string {
	if p, err := exec.LookPath("mise"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", "mise"),
		filepath.Join(home, ".local", "share", "mise", "bin", "mise"),
		"/home/linuxbrew/.linuxbrew/bin/mise",
		"/usr/local/bin/mise",
		"/usr/bin/mise",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Apply upgrades the installation and returns the new binary to relaunch
// ("" when the package manager restarts the app itself).
func (r *Release) Apply(ctx context.Context, out io.Writer) (string, error) {
	switch Detect() {
	case Mise:
		mise := findMise()
		if mise == "" {
			return "", errors.New("mise was not found on this system")
		}
		cmd := exec.CommandContext(ctx, mise, "use", "-g", r.miseSpec())
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("mise could not install %s (%w). If your mise config sets minimum_release_age, the release may be too new yet", r.Version, err)
		}
		where, err := exec.CommandContext(ctx, mise, "where", r.miseSpec()).Output()
		if err != nil {
			return "", err
		}
		bin := filepath.Join(strings.TrimSpace(string(where)), "audio-switcher")
		// Refresh launcher and autostart entries so they point at the new version.
		refresh := exec.CommandContext(ctx, bin, "--install", "--no-start")
		refresh.Stdout, refresh.Stderr = out, out
		_ = refresh.Run()
		return bin, nil

	case Deb:
		url := r.assets[r.debName()]
		if url == "" {
			return "", fmt.Errorf("release %s has no %s", r.Version, r.debName())
		}
		dir, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		file := filepath.Join(dir, "pckz-audio-switcher", r.debName())
		if err := download(ctx, url, file); err != nil {
			return "", err
		}
		// pkexec shows the desktop's password dialog. The package scripts stop
		// and restart the tray for every logged-in user.
		cmd := exec.CommandContext(ctx, "pkexec", "apt-get", "install", "-y", "--allow-downgrades", file)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("installing the update failed: %w", err)
		}
		return "", nil
	}
	return "", fmt.Errorf("download it from %s", r.URL)
}

func download(ctx context.Context, url, file string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ── Remembering what the user was told ──────────────────────────────────────

func statePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "pckz-audio-switcher", "update.json")
}

// Notified returns the last version the user got a notification for.
func Notified() string {
	var s struct{ Notified string }
	if data, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s.Notified
}

// SetNotified records that the user was told about version v.
func SetNotified(v string) {
	data, _ := json.Marshal(struct{ Notified string }{v})
	if os.MkdirAll(filepath.Dir(statePath()), 0o755) == nil {
		_ = os.WriteFile(statePath(), data, 0o644)
	}
}
