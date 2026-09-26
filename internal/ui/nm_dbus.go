package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	nmBus  = "org.freedesktop.NetworkManager"
	nmPath = "/org/freedesktop/NetworkManager"

	// NMActiveConnectionState values.
	nmActivated   = 2
	nmDeactivated = 4
)

// nmSystemBus is the bus NetworkManager is on (replaced in tests).
var nmSystemBus = dbus.SystemBus

// addWifiProfile creates the NetworkManager profile of a secured network and
// activates it, over D-Bus: the password never shows in a command line (the
// arguments of nmcli are readable by every user in /proc). The profile is
// deleted again if it does not come up, as with a wrong password.
func addWifiProfile(dev, ssid, keyMgmt, password string) error {
	conn, err := nmSystemBus()
	if err != nil {
		return err
	}
	nm := conn.Object(nmBus, nmPath)

	var device dbus.ObjectPath
	if err := nm.Call(nmBus+".GetDeviceByIpIface", 0, dev).Store(&device); err != nil {
		return err
	}

	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":   dbus.MakeVariant(ssid),
			"type": dbus.MakeVariant("802-11-wireless"),
		},
		"802-11-wireless": {
			"ssid": dbus.MakeVariant([]byte(ssid)),
			"mode": dbus.MakeVariant("infrastructure"),
		},
		"802-11-wireless-security": {
			"key-mgmt": dbus.MakeVariant(keyMgmt),
			"psk":      dbus.MakeVariant(password),
		},
	}
	var profile, active dbus.ObjectPath
	if err := nm.Call(nmBus+".AddAndActivateConnection", 0, settings, device, dbus.ObjectPath("/")).
		Store(&profile, &active); err != nil {
		return err
	}

	if err := waitActivated(conn, active, nmcliConnectTimeout); err != nil {
		conn.Object(nmBus, profile).Call(nmBus+".Settings.Connection.Delete", 0)
		return err
	}
	return nil
}

// waitActivated waits for an active connection to come up.
func waitActivated(conn *dbus.Conn, active dbus.ObjectPath, timeout time.Duration) error {
	obj := conn.Object(nmBus, active)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v, err := obj.GetProperty(nmBus + ".Connection.Active.State")
		if err != nil {
			// The active connection is gone: it failed (a wrong password,
			// most often).
			return errors.New("could not connect: check the password")
		}
		state, _ := v.Value().(uint32)
		switch state {
		case nmActivated:
			return nil
		case nmDeactivated:
			return errors.New("could not connect: check the password")
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("could not connect within %s", timeout)
}
