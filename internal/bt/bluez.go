// Package bt talks to BlueZ over the system bus to list paired audio devices
// and connect them.
package bt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Device is a paired Bluetooth device that can carry audio.
type Device struct {
	Path      dbus.ObjectPath
	Address   string
	Name      string
	Icon      string // BlueZ icon hint, e.g. "audio-headset", "audio-card"
	Class     uint32
	Connected bool
	CanSink   bool // offers A2DP sink: it can play audio sent from this PC
}

var audioUUIDs = []string{
	"0000110b", // A2DP sink
	"0000110a", // A2DP source
	"0000110d", // A2DP
	"00001108", // HSP
	"0000111e", // HFP
	"0000184e", // LE Audio (ASCS)
}

var (
	connOnce sync.Once
	conn     *dbus.Conn
	connErr  error
)

func bus() (*dbus.Conn, error) {
	connOnce.Do(func() { conn, connErr = dbus.ConnectSystemBus() })
	return conn, connErr
}

func isAudio(uuids []string, icon string) bool {
	if strings.HasPrefix(icon, "audio-") {
		return true
	}
	for _, u := range uuids {
		for _, a := range audioUUIDs {
			if strings.HasPrefix(strings.ToLower(u), a) {
				return true
			}
		}
	}
	return false
}

// Devices returns paired audio devices. It returns nil if BlueZ is unavailable.
func Devices() []Device {
	c, err := bus()
	if err != nil {
		return nil
	}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Object("org.bluez", "/").CallWithContext(ctx,
		"org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return nil
	}
	var out []Device
	for path, ifaces := range objs {
		p, ok := ifaces["org.bluez.Device1"]
		if !ok {
			continue
		}
		var d Device
		var paired bool
		var uuids []string
		d.Path = path
		get(p, "Address", &d.Address)
		get(p, "Alias", &d.Name)
		if d.Name == "" {
			get(p, "Name", &d.Name)
		}
		get(p, "Icon", &d.Icon)
		get(p, "Class", &d.Class)
		get(p, "Connected", &d.Connected)
		get(p, "Paired", &paired)
		get(p, "UUIDs", &uuids)
		for _, u := range uuids {
			if strings.EqualFold(u, a2dpSink) {
				d.CanSink = true
			}
		}
		if paired && isAudio(uuids, d.Icon) {
			out = append(out, d)
		}
	}
	return out
}

func get[T any](props map[string]dbus.Variant, key string, dst *T) {
	if v, ok := props[key]; ok {
		if t, ok := v.Value().(T); ok {
			*dst = t
		}
	}
}

// ErrRefused means the device answered but rejected the connection.
var ErrRefused = errors.New("it refused the connection — it may be connected to another phone or computer, or it forgot this PC (remove and pair it again)")

const (
	a2dpSink   = "0000110b-0000-1000-8000-00805f9b34fb"
	a2dpSource = "0000110a-0000-1000-8000-00805f9b34fb"
)

// Connect connects the device as an audio output. It asks for the A2DP sink
// profile specifically: a plain Device1.Connect tries every profile, and
// devices that also offer A2DP *source* (Echo speakers, phones) may refuse
// that one and fail the whole call. Other devices fall back to Connect.
func Connect(path dbus.ObjectPath) error {
	c, err := bus()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	obj := c.Object("org.bluez", path)
	err = obj.CallWithContext(ctx, "org.bluez.Device1.ConnectProfile", 0, a2dpSink).Err
	if err != nil && strings.Contains(err.Error(), "busy") {
		// The A2DP link is taken the other way round: the device (an Echo,
		// a phone) is streaming *to* this PC. Drop that role and ask again.
		_ = obj.CallWithContext(ctx, "org.bluez.Device1.DisconnectProfile", 0, a2dpSource).Err
		time.Sleep(time.Second)
		err = obj.CallWithContext(ctx, "org.bluez.Device1.ConnectProfile", 0, a2dpSink).Err
	}
	if err != nil && !isRefused(err) && !strings.Contains(err.Error(), "busy") {
		err = obj.CallWithContext(ctx, "org.bluez.Device1.Connect", 0).Err
	}
	return friendly(err)
}

func isRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "refused")
}

// friendly turns BlueZ error codes into something a person can act on.
func friendly(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case isRefused(err):
		return ErrRefused
	case strings.Contains(msg, "page-timeout"), strings.Contains(msg, "Host is down"), strings.Contains(msg, "timed out"),
		errors.Is(err, context.DeadlineExceeded):
		return errors.New("the device didn't answer — is it on and in range?")
	case strings.Contains(msg, "In Progress"), strings.Contains(msg, "busy"):
		return errors.New("a connection is already in progress, try again in a moment")
	case strings.Contains(msg, "Not Ready"), strings.Contains(msg, "not powered"):
		return errors.New("Bluetooth is turned off")
	}
	return err
}

// Watch calls fn whenever a Bluetooth device connects, disconnects or is
// (un)paired. It runs until the process exits.
func Watch(fn func()) {
	c, err := bus()
	if err != nil {
		return
	}
	_ = c.AddMatchSignal(dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"))
	_ = c.AddMatchSignal(dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"))
	ch := make(chan *dbus.Signal, 16)
	c.Signal(ch)
	go func() {
		for s := range ch {
			if s.Name == "org.freedesktop.DBus.Properties.PropertiesChanged" {
				if len(s.Body) < 2 {
					continue
				}
				if iface, _ := s.Body[0].(string); iface != "org.bluez.Device1" {
					continue
				}
				changed, _ := s.Body[1].(map[string]dbus.Variant)
				_, conn := changed["Connected"]
				_, pair := changed["Paired"]
				if !conn && !pair {
					continue
				}
			}
			fn()
		}
	}()
}
