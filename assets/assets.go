// Package assets embeds the icon set and desktop entries, so a single binary
// (e.g. installed with mise or go install) can show its icons and install
// itself into the user's session.
package assets

import "embed"

//go:embed icons/*.svg
var Icons embed.FS

//go:embed desktop/*.desktop
var Desktop embed.FS

const (
	AppID           = "pckz.AudioSwitcher"
	AppDesktopFile  = AppID + ".desktop"
	TrayDesktopFile = "pckz-audio-switcher-tray.desktop"
)
