// Command audio-switcher switches sound outputs and inputs from the tray or a
// small window, the same way GNOME Settings does.
//
//	audio-switcher              open the window (starting the tray if needed)
//	audio-switcher --tray       run in the tray only (used at login)
//	audio-switcher --list       print the devices and exit
//	audio-switcher --install    add the launcher, icons and tray-at-login for this user
//	audio-switcher --uninstall  remove them again
//	audio-switcher --check-update / --update  look for / install a newer release
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"pckz/internal/audio"
	"pckz/internal/install"
	"pckz/internal/ui"
	"pckz/internal/update"
)

// repo is "owner/name" of the GitHub repository releases come from, set at
// release build time; source builds leave it empty and skip update checks.
var repo = ""

// version is set at release build time; `go install` builds fall back to the
// module version.
var version = ""

func appVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func init() {
	// GTK must stay on the thread that initialised it.
	runtime.LockOSThread()
}

func main() {
	ui.Version = appVersion()
	update.Repo, update.Current = repo, appVersion()
	args := []string{os.Args[0]}
	background := false
	for _, a := range os.Args[1:] {
		switch a {
		case "--tray", "--background":
			background = true
		case "--list", "-l":
			os.Exit(list())
		case "--install":
			exitOn(install.Install(!has("--no-start")))
			return
		case "--no-start":
		case "--check-update":
			os.Exit(checkUpdate())
		case "--update":
			os.Exit(runUpdate())
		case "--uninstall":
			exitOn(install.Uninstall())
			return
		case "--version", "-v":
			fmt.Println("audio-switcher", appVersion())
			return
		case "--help", "-h":
			fmt.Println("usage: audio-switcher [--tray | --list | --install | --uninstall | --check-update | --update | --version]")
			return
		default:
			args = append(args, a)
		}
	}
	if background && ui.Running() {
		return // already running for this session
	}
	os.Exit(ui.Run(args, background))
}

func has(flag string) bool {
	for _, a := range os.Args[1:] {
		if a == flag {
			return true
		}
	}
	return false
}

func checkUpdate() int {
	if update.Repo == "" {
		fmt.Println("This build doesn't know where its releases are published (built from source).")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := update.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio-switcher:", err)
		return 1
	}
	if r == nil {
		fmt.Printf("audio-switcher %s is up to date.\n", update.Current)
		return 0
	}
	fmt.Printf("audio-switcher %s is available (you have %s).\nUpgrade with:\n  audio-switcher --update\nor:\n  %s\n",
		r.Version, update.Current, r.Command())
	return 0
}

func runUpdate() int {
	if update.Repo == "" {
		fmt.Println("This build doesn't know where its releases are published (built from source).")
		return 1
	}
	ctx := context.Background()
	r, err := update.Check(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio-switcher:", err)
		return 1
	}
	if r == nil {
		fmt.Printf("audio-switcher %s is up to date.\n", update.Current)
		return 0
	}
	fmt.Printf("Updating %s → %s…\n", update.Current, r.Version)
	bin, err := r.Apply(ctx, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio-switcher:", err)
		return 1
	}
	if bin != "" {
		// Restart a running tray on the new version.
		if out, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", "audio-switcher --tray").Output(); len(out) > 0 {
			_ = exec.Command("pkill", "-u", strconv.Itoa(os.Getuid()), "-f", "audio-switcher --tray").Run()
			time.Sleep(500 * time.Millisecond)
			if c := exec.Command(bin, "--tray"); c.Start() == nil {
				_ = c.Process.Release()
			}
		}
	}
	fmt.Printf("Updated to %s.\n", r.Version)
	return 0
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "audio-switcher:", err)
		os.Exit(1)
	}
}

func list() int {
	s, err := audio.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, sec := range []struct {
		name string
		devs []audio.Device
		ep   *audio.Endpoint
	}{{"Output", s.Outputs, s.Sink}, {"Input", s.Inputs, s.Source}} {
		vol := ""
		if sec.ep != nil {
			vol = fmt.Sprintf(" — %d%%", sec.ep.Volume)
			if sec.ep.Muted {
				vol = " — muted"
			}
		}
		fmt.Printf("%s%s\n", sec.name, vol)
		for _, d := range sec.devs {
			mark := "  "
			if d.Active {
				mark = "● "
			}
			fmt.Printf("  %s%-28s %-36s [%s]\n", mark, d.Title, d.Subtitle, d.Kind)
		}
	}
	return 0
}
