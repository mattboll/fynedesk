package phone

import (
	"bufio"
	"context"
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

// privateBus starts a bus of its own for the test.
func privateBus(t *testing.T) string {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("no dbus-daemon")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "bus.conf")
	err = os.WriteFile(config, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig><type>session</type><listen>unix:dir=`+dir+`</listen>
<policy context="default"><allow send_destination="*" eavesdrop="true"/><allow eavesdrop="true"/><allow own="*"/></policy>
</busconfig>`), 0o600)
	if err != nil {
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
	return strings.TrimSpace(addr)
}

func connectTo(t *testing.T, addr string) *dbus.Conn {
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// fakeKDE is kdeconnectd with one paired phone in reach and one unpaired.
type fakeKDE struct {
	mu     sync.Mutex
	calls  []string
	shared []string
}

func (f *fakeKDE) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

type fakeDaemon struct{}

func (fakeDaemon) Devices(onlyReachable, onlyPaired bool) ([]string, *dbus.Error) {
	return []string{"pixel", "stranger", "gone"}, nil
}

type fakeDevice struct {
	f  *fakeKDE
	id string
}

func (d fakeDevice) RequestPairing() *dbus.Error { d.f.record("requestPairing " + d.id); return nil }
func (d fakeDevice) AcceptPairing() *dbus.Error  { d.f.record("acceptPairing " + d.id); return nil }

type fakeRing struct{ f *fakeKDE }

func (r fakeRing) Ring() *dbus.Error { r.f.record("ring"); return nil }

type fakeShare struct{ f *fakeKDE }

func (s fakeShare) ShareUrls(urls []string) *dbus.Error {
	s.f.mu.Lock()
	s.f.shared = urls
	s.f.mu.Unlock()
	return nil
}

func deviceProps(name string, reachable, paired bool) map[string]*prop.Prop {
	p := func(v any) *prop.Prop { return &prop.Prop{Value: v} }
	return map[string]*prop.Prop{
		"name": p(name), "type": p("phone"), "isReachable": p(reachable), "isPaired": p(paired),
		"isPairRequestedByPeer": p(false), "isPairRequested": p(false),
	}
}

func startFakeKDE(t *testing.T, conn *dbus.Conn) *fakeKDE {
	f := &fakeKDE{}
	export := func(v any, path dbus.ObjectPath, iface string) {
		if err := conn.ExportWithMap(v, map[string]string{
			"Devices": "devices", "RequestPairing": "requestPairing", "AcceptPairing": "acceptPairing",
			"Ring": "ring", "ShareUrls": "shareUrls",
		}, path, iface); err != nil {
			t.Fatal(err)
		}
	}
	export(fakeDaemon{}, kdeDaemon, kdeDaemonIf)
	for id, props := range map[string]map[string]*prop.Prop{
		"pixel":    deviceProps("Pixel 8", true, true),
		"stranger": deviceProps("Someone's phone", true, false),
		"gone":     deviceProps("Old phone", false, false),
	} {
		path := devicePath(id)
		export(fakeDevice{f, id}, path, kdeDeviceIf)
		if _, err := prop.Export(conn, path, map[string]map[string]*prop.Prop{kdeDeviceIf: props}); err != nil {
			t.Fatal(err)
		}
	}
	battery := map[string]*prop.Prop{"charge": {Value: int32(78)}, "isCharging": {Value: true}}
	if _, err := prop.Export(conn, devicePath("pixel")+"/battery", map[string]map[string]*prop.Prop{kdeBatteryIf: battery}); err != nil {
		t.Fatal(err)
	}
	export(fakeRing{f}, devicePath("pixel")+"/findmyphone", "org.kde.kdeconnect.device.findmyphone")
	export(fakeShare{f}, devicePath("pixel")+"/share", "org.kde.kdeconnect.device.share")
	if reply, err := conn.RequestName(kdeService, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("name: %v %v", reply, err)
	}
	return f
}

func TestKDEConnectDevices(t *testing.T) {
	addr := privateBus(t)
	startFakeKDE(t, connectTo(t, addr))
	k := newKDEConnectOn(connectTo(t, addr))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	devices, err := k.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices %+v, want the paired one then the one in reach", devices)
	}
	want := KDEDevice{ID: "pixel", Name: "Pixel 8", Type: "phone", Reachable: true, Paired: true, Battery: 78, Charging: true}
	if devices[0] != want {
		t.Errorf("paired phone %+v, want %+v", devices[0], want)
	}
	if devices[1].ID != "stranger" || devices[1].Paired || devices[1].Battery != -1 {
		t.Errorf("phone in reach %+v", devices[1])
	}
}

func TestKDEConnectActions(t *testing.T) {
	addr := privateBus(t)
	f := startFakeKDE(t, connectTo(t, addr))
	k := newKDEConnectOn(connectTo(t, addr))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, err := range []error{
		k.RequestPairing(ctx, "stranger"),
		k.Ring(ctx, "pixel"),
		k.ShareFiles(ctx, "pixel", []string{"/home/me/My photo.jpg"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.calls, ",") != "requestPairing stranger,ring" {
		t.Errorf("calls %q", f.calls)
	}
	if len(f.shared) != 1 || f.shared[0] != "file:///home/me/My%20photo.jpg" {
		t.Errorf("shared %q", f.shared)
	}
}

func TestKDEConnectWatch(t *testing.T) {
	addr := privateBus(t)
	server := connectTo(t, addr)
	startFakeKDE(t, server)
	k := newKDEConnectOn(connectTo(t, addr))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	changed := make(chan struct{}, 4)
	go func() { _ = k.Watch(ctx, func() { changed <- struct{}{} }) }()
	time.Sleep(200 * time.Millisecond) // the match is in place
	if err := server.Emit(devicePath("pixel")+"/battery", kdeBatteryIf+".refreshed", false, int32(77)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-ctx.Done():
		t.Fatal("the battery changed, and nothing was told")
	}
}
