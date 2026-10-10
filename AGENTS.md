# AGENTS.md

Audio Switcher is a Linux tray app and small window for switching sound
devices. It is written in Go, with GTK 4 and libadwaita loaded at runtime (no
cgo).

## Commands

```sh
make build   # bin/audio-switcher
make test    # go test ./...
make lint    # gofmt check + go vet; CI runs this, so run it before committing
make demo    # the app on fake devices (AUDIO_SWITCHER_DEMO=1)
make icons   # regenerate assets/icons/*.svg and docs/icons.svg
make dist VERSION=x.y.z   # release tarballs + .debs (what CI publishes)
```

`./bin/audio-switcher --list` prints what the backend sees, which is the
fastest way to check device detection on a real machine.

## Layout

| Path | What |
|---|---|
| `cmd/audio-switcher` | CLI entry: window, `--tray`, `--list`, `--install` |
| `internal/audio` | Sound model. `pactl -f json` → `Snapshot` (devices per card *port*), profile choice, classification into `Kind`s, actions, event watching, remembered profiles, demo data |
| `internal/bt` | BlueZ over the system D-Bus: paired audio devices, connect |
| `internal/tray` | Hand-written StatusNotifierItem + `com.canonical.dbusmenu` on godbus |
| `internal/ui` | libadwaita window, tray glue, autostart toggle |
| `internal/install` | Per-user install from embedded assets (for mise and `go install`) |
| `assets` | Embedded icons and desktop entries |
| `packaging` | `.deb` scripts; `build-deb.sh` stages the package |
| `tools/genicons` | Source of truth for every icon (fill-only SVG paths) |

## Rules that aren't obvious from the code

- **GTK only on the main thread.** Background work (pactl, D-Bus, tray clicks)
  hands results over with `post(func())` in `internal/ui/mainthread.go`.
- **Never create a Go closure per widget for a signal.** puregotk allocates a
  C trampoline per distinct callback and never frees it (2000 max). Use the
  shared handlers (`onRow`, `onClick`, `onValue`) that dispatch by widget
  pointer, and call `forget` when removing widgets.
- **List devices per card port, not per sink.** One card exposes one sink at a
  time; listing ports is what makes every HDMI connector selectable. Keep
  `chooseProfile` behaving like GNOME Settings: keep the active profile if
  it includes the port, otherwise keep the other direction intact.
- **Icons are generated.** Edit `tools/genicons/main.go`, run `make icons`,
  and commit the SVGs. Symbolic icons must be fill-only (no strokes), and
  holes must be wound counter-clockwise.
- **On PipeWire, don't move streams on switch.** Moving pins them to a
  device, and they stop following the default.
- **Bluetooth is shared between sessions.** Only act on a device when this
  session's sound server has its card (see `disconnectedBT`).
- **No real device data.** Tests, demo data and screenshots use made-up names
  and `00:1B:66:00:00:0x` addresses.

## UI changes

Check visually with demo mode. GTK's Broadway backend renders into a
browser, which helps when there's no desktop to screenshot:

```sh
gtk4-broadwayd :5 &
GDK_BACKEND=broadway BROADWAY_DISPLAY=:5 ADW_DEBUG_COLOR_SCHEME=prefer-dark make demo
# open http://127.0.0.1:8085
```

Screenshots in `docs/screenshots` come from this setup.

## Commits and releases

Use [Conventional Commits](https://www.conventionalcommits.org). For now
every change ships as a patch release: write `fix:` (or `fix(scope):`)
commits. `.releaserc.json` also maps `feat` and breaking changes to patch.
Each push to `main` runs CI, then semantic-release picks the version, runs
`make dist` (with `REPO` taken from `GITHUB_REPOSITORY`, which turns on
in-app update checks), and publishes a GitHub release with tarballs, `.deb`s
and checksums. Don't edit versions by hand.

The update flow lives in `internal/update`: `Detect` decides between mise,
`.deb` and manual installs, and `Apply` upgrades the same way.
