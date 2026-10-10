package ui

import (
	"github.com/godbus/dbus/v5"
)

// notify shows a desktop notification. It is used for errors from tray
// actions, when the window (and its toasts) is not on screen.
func notify(summary, body string) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()
	conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"Audio Switcher", uint32(0), AppIcon, summary, body,
		[]string{}, map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant(AppID)}, int32(-1))
}
