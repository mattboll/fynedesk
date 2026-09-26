package phone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPairingQR(t *testing.T) {
	p := Pairing{Name: "tyde-abc", Code: "s3cret"}
	if got, want := p.QR(), "WIFI:T:ADB;S:tyde-abc;P:s3cret;;"; got != want {
		t.Errorf("QR = %q, want %q", got, want)
	}
	a, b := NewPairing(), NewPairing()
	if a.Name == b.Name || a.Code == b.Code {
		t.Error("two pairings share a name or a code")
	}
	if !strings.HasPrefix(a.Name, "tyde-") || len(a.Code) != 10 {
		t.Errorf("unexpected pairing %+v", a)
	}
}

func TestParseDevices(t *testing.T) {
	out := `* daemon not running; starting now at tcp:5037
* daemon started successfully
List of devices attached
192.168.1.20:41235     device product:husky model:Pixel_8_Pro device:husky transport_id:3
0A151FDD4000KN         unauthorized usb:1-2 transport_id:1

`
	got := parseDevices(out)
	if len(got) != 2 {
		t.Fatalf("got %d devices: %+v", len(got), got)
	}
	if got[0] != (Device{Serial: "192.168.1.20:41235", State: "device", Model: "Pixel 8 Pro"}) {
		t.Errorf("first device: %+v", got[0])
	}
	if !got[0].Wireless() || got[1].Wireless() {
		t.Error("wireless is told wrong")
	}
	if got[1].State != "unauthorized" {
		t.Errorf("second device: %+v", got[1])
	}
}

// fakeADB writes a script standing for adb: it answers pair and connect
// like adb does, and logs its arguments.
func fakeADB(t *testing.T) (ADB, func() []string) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$@" >> ` + log + `
case "$1" in
pair) if [ "$3" = "good-code" ]; then echo "Successfully paired to $2 [guid=adb-SERIAL-xyz]"; else echo "Failed: Wrong password or connection was dropped."; exit 1; fi ;;
connect) echo "connected to $2" ;;
esac
`
	path := filepath.Join(dir, "adb")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return ADB{Path: path}, func() []string {
		data, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

// fakeNet announces services after a moment, as a phone does once it scanned
// the code.
type fakeNet struct {
	services map[string][]Service
}

func (n fakeNet) Browse(ctx context.Context, service string, found func(Service)) error {
	select {
	case <-time.After(20 * time.Millisecond):
	case <-ctx.Done():
		return nil
	}
	for _, s := range n.services[service] {
		found(s)
	}
	<-ctx.Done()
	return nil
}

func TestPairConnectsThePhoneThatScanned(t *testing.T) {
	adb, calls := fakeADB(t)
	net := fakeNet{services: map[string][]Service{
		PairingService: {
			{Instance: "someone-else", Addr: "192.168.1.99:1111"},
			{Instance: "tyde-me", Addr: "192.168.1.20:37000"},
		},
		ConnectService: {{Instance: "adb-SERIAL-xyz", Addr: "192.168.1.20:41235"}},
	}}
	var steps []Step
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	guid, addr, err := Pair(ctx, adb, net, Pairing{Name: "tyde-me", Code: "good-code"}, func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if guid != "adb-SERIAL-xyz" || addr != "192.168.1.20:41235" {
		t.Errorf("guid %q addr %q", guid, addr)
	}
	want := []string{"pair 192.168.1.20:37000 good-code", "connect 192.168.1.20:41235"}
	if got := calls(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("adb calls %q, want %q", got, want)
	}
	if len(steps) != 4 || steps[3] != StepConnected {
		t.Errorf("steps %v", steps)
	}
}

func TestPairFailsOnWrongCode(t *testing.T) {
	adb, _ := fakeADB(t)
	net := fakeNet{services: map[string][]Service{PairingService: {{Instance: "tyde-me", Addr: "10.0.0.2:1"}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := Pair(ctx, adb, net, Pairing{Name: "tyde-me", Code: "bad"}, func(Step) {}); err == nil ||
		!strings.Contains(err.Error(), "Wrong password") {
		t.Errorf("err = %v", err)
	}
}

func TestPairGivesUpWhenCancelled(t *testing.T) {
	adb, calls := fakeADB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := Pair(ctx, adb, fakeNet{}, NewPairing(), func(Step) {}); err == nil {
		t.Error("no phone, and yet paired")
	}
	if got := calls(); len(got) != 1 || got[0] != "" {
		t.Errorf("adb was called: %q", got)
	}
}

func TestKeepConnectsKnownPhonesOnly(t *testing.T) {
	adb, calls := fakeADB(t)
	net := fakeNet{services: map[string][]Service{ConnectService: {
		{Instance: "adb-KNOWN-1", Addr: "192.168.1.20:41235"},
		{Instance: "adb-STRANGER-2", Addr: "192.168.1.30:41000"},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	var connected []string
	_ = Keep(ctx, adb, net, func() []string { return []string{"adb-KNOWN-1"} }, func(guid, addr string) {
		mu.Lock()
		connected = append(connected, guid+"@"+addr)
		mu.Unlock()
	})
	if got := calls(); len(got) != 1 || got[0] != "connect 192.168.1.20:41235" {
		t.Errorf("adb calls %q", got)
	}
	if len(connected) != 1 || connected[0] != "adb-KNOWN-1@192.168.1.20:41235" {
		t.Errorf("connected %q", connected)
	}
}

func TestKnownPhonesAreKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tyde", "phones.json")
	k := LoadKnown(path)
	if len(k.GUIDs()) != 0 {
		t.Fatal("phones known from nowhere")
	}
	if err := k.Add("adb-A-1", "Pixel 8"); err != nil {
		t.Fatal(err)
	}
	_ = k.Add("adb-A-1", "") // paired again: its model stays
	_ = k.Add("adb-B-2", "Galaxy S24")

	again := LoadKnown(path)
	if got := again.GUIDs(); strings.Join(got, ",") != "adb-A-1,adb-B-2" {
		t.Errorf("guids after reading back: %q", got)
	}
	if again.phones[0].Model != "Pixel 8" {
		t.Errorf("model lost: %+v", again.phones[0])
	}
}

// failingNet fails its first searches, like a network not up yet.
type failingNet struct {
	fakeNet
	fails *atomic.Int32
}

func (n failingNet) Browse(ctx context.Context, service string, found func(Service)) error {
	if n.fails.Add(-1) >= 0 {
		return errors.New("no network")
	}
	return n.fakeNet.Browse(ctx, service, found)
}

func TestKeepSearchesAgainAfterAFailure(t *testing.T) {
	saved := keepRetry
	keepRetry = 10 * time.Millisecond
	defer func() { keepRetry = saved }()
	adb, calls := fakeADB(t)
	fails := &atomic.Int32{}
	fails.Store(2)
	net := failingNet{fakeNet{services: map[string][]Service{ConnectService: {
		{Instance: "adb-KNOWN-1", Addr: "192.168.1.20:41235"},
	}}}, fails}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = Keep(ctx, adb, net, func() []string { return []string{"adb-KNOWN-1"} }, func(string, string) {})
	if got := calls(); len(got) != 1 {
		t.Errorf("the phone was not connected once the network came: %q", got)
	}
}
