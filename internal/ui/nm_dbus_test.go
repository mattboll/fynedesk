package ui

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// fakeNM is NetworkManager for a test: the connection comes up when the
// password is right.
type fakeNM struct {
	conn     *dbus.Conn
	password string

	mu       sync.Mutex
	settings map[string]map[string]dbus.Variant
	deleted  bool
}

func (f *fakeNM) GetDeviceByIpIface(iface string) (dbus.ObjectPath, *dbus.Error) {
	return dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/" + iface), nil
}

func (f *fakeNM) AddAndActivateConnection(settings map[string]map[string]dbus.Variant, dev, ap dbus.ObjectPath) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	f.settings = settings
	f.mu.Unlock()
	profile := dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/1")
	active := dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")
	_ = f.conn.Export(&fakeProfile{f}, profile, nmBus+".Settings.Connection")
	props, _ := prop.Export(f.conn, active, prop.Map{
		nmBus + ".Connection.Active": {"State": {Value: uint32(1)}},
	})
	psk, _ := settings["802-11-wireless-security"]["psk"].Value().(string)
	go func() {
		time.Sleep(100 * time.Millisecond)
		state := uint32(nmDeactivated)
		if psk == f.password {
			state = nmActivated
		}
		props.SetMust(nmBus+".Connection.Active", "State", state)
	}()
	return profile, active, nil
}

type fakeProfile struct{ nm *fakeNM }

func (p *fakeProfile) Delete() *dbus.Error {
	p.nm.mu.Lock()
	p.nm.deleted = true
	p.nm.mu.Unlock()
	return nil
}

func fakeNetworkManager(t *testing.T, password string) *fakeNM {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "bus.conf")
	if err := os.WriteFile(config, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig><type>session</type><listen>unix:dir=`+dir+`</listen>
<policy context="default"><allow send_destination="*" eavesdrop="true"/><allow eavesdrop="true"/><allow own="*"/></policy>
</busconfig>`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(daemon, "--config-file="+config, "--nofork", "--print-address=1")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *dbus.Conn {
		conn, err := dbus.Connect(strings.TrimSpace(addr))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}

	f := &fakeNM{conn: connect(), password: password}
	if err := f.conn.Export(f, nmPath, nmBus); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.RequestName(nmBus, 0); err != nil {
		t.Fatal(err)
	}
	client := connect()
	saved := nmSystemBus
	nmSystemBus = func() (*dbus.Conn, error) { return client, nil }
	t.Cleanup(func() { nmSystemBus = saved })
	return f
}

func TestAddWifiProfile(t *testing.T) {
	f := fakeNetworkManager(t, "right horse")
	if err := addWifiProfile("wlan0", "Home:5G", "sae", "right horse"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if ssid, _ := f.settings["802-11-wireless"]["ssid"].Value().([]byte); string(ssid) != "Home:5G" {
		t.Errorf("ssid %q", ssid)
	}
	if km, _ := f.settings["802-11-wireless-security"]["key-mgmt"].Value().(string); km != "sae" {
		t.Errorf("key-mgmt %q", km)
	}
	if f.deleted {
		t.Error("the profile of a working connection is deleted")
	}
}

func TestAddWifiProfileWrongPassword(t *testing.T) {
	f := fakeNetworkManager(t, "right horse")
	if err := addWifiProfile("wlan0", "Home", "wpa-psk", "wrong"); err == nil {
		t.Fatal("a wrong password connects")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("the profile of a failed connection is kept")
	}
}
