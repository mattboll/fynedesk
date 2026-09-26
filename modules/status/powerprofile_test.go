package status

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// fakeProfileDaemon runs power-profiles-daemon's interface on a bus of its
// own, and points the module at it.
func fakeProfileDaemon(t *testing.T, profile string) *prop.Properties {
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

	const name = "org.freedesktop.UPower.PowerProfiles"
	server := connect()
	props, err := prop.Export(server, "/org/freedesktop/UPower/PowerProfiles", prop.Map{
		name: {"ActiveProfile": {Value: profile, Writable: true, Emit: prop.EmitTrue}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.RequestName(name, 0); err != nil {
		t.Fatal(err)
	}
	client := connect()
	saved := profileBus
	profileBus = func() (*dbus.Conn, error) { return client, nil }
	t.Cleanup(func() { profileBus = saved })
	return props
}

func TestProfileDaemon(t *testing.T) {
	props := fakeProfileDaemon(t, "balanced")
	d, err := findProfileDaemon()
	if err != nil {
		t.Fatal(err)
	}
	if cur, err := d.get(); err != nil || cur != "balanced" {
		t.Fatalf("get: %q %v", cur, err)
	}

	changes := make(chan string, 4)
	stop := d.watch(func(profile string) { changes <- profile })
	defer stop()
	props.SetMust("org.freedesktop.UPower.PowerProfiles", "ActiveProfile", "power-saver")
	select {
	case got := <-changes:
		if got != "power-saver" {
			t.Errorf("change: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the change was not followed")
	}

	if err := d.set("performance"); err != nil {
		t.Fatal(err)
	}
	if v, _ := props.Get("org.freedesktop.UPower.PowerProfiles", "ActiveProfile"); v.Value() != "performance" {
		t.Errorf("daemon profile %v", v.Value())
	}
}

func TestPowerProfileWidget(t *testing.T) {
	test.NewTempApp(t)
	fakeProfileDaemon(t, "balanced")
	p := newPowerProfile().(*powerProfile)
	if p.StatusAreaWidget() == nil {
		t.Fatal("no widget with the daemon there")
	}
	p.Destroy() // no change comes in while the label is read
	if p.label.Text != "Balanced" {
		t.Fatalf("label %q", p.label.Text)
	}
	p.cycleProfile() // balanced -> power-saver
	if p.label.Text != "Power Saver" {
		t.Errorf("after a tap: %q", p.label.Text)
	}
}
