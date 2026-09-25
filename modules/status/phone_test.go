package status

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"

	"fyshos.com/tyde/internal/phone"
)

// fakePhoneADB stands for adb: a phone shows up in the devices once paired.
func fakePhoneADB(t *testing.T) string {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
devices) echo "List of devices attached"
  [ -f ` + dir + `/paired ] && echo "192.168.1.20:41235     device product:shiba model:Pixel_8 device:shiba transport_id:3" ;;
pair) touch ` + dir + `/paired; echo "Successfully paired to $2 [guid=adb-PIXEL-1]" ;;
connect) echo "connected to $2" ;;
esac
`
	path := filepath.Join(dir, "adb")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakePhoneNet is the network with a phone that scanned the code.
type fakePhoneNet struct{}

func (fakePhoneNet) Browse(ctx context.Context, service string, found func(phone.Service)) error {
	select {
	case <-time.After(10 * time.Millisecond):
	case <-ctx.Done():
		return nil
	}
	switch service {
	case phone.PairingService:
		found(phone.Service{Instance: "tyde-test", Addr: "192.168.1.20:37000"})
	case phone.ConnectService:
		found(phone.Service{Instance: "adb-PIXEL-1", Addr: "192.168.1.20:41235"})
	}
	<-ctx.Done()
	return nil
}

func TestPhoneModuleConnectsAScannedPhone(t *testing.T) {
	test.NewTempApp(t)
	newPairing = func() phone.Pairing { return phone.Pairing{Name: "tyde-test", Code: "code"} }
	defer func() { newPairing = phone.NewPairing }()
	noKDE(t)

	known := phone.LoadKnown(filepath.Join(t.TempDir(), "phones.json"))
	p := &phoneModule{adb: phone.ADB{Path: fakePhoneADB(t)}, browser: fakePhoneNet{}, known: known}
	if p.StatusAreaWidget() == nil {
		t.Fatal("no widget with adb there")
	}

	box := container.NewVBox()
	p.startPairing(box, widget.NewButton("", nil))
	assert.Eventually(t, func() bool { return len(known.GUIDs()) == 1 }, 5*time.Second, 20*time.Millisecond,
		"the phone is not remembered")
	assert.Equal(t, []string{"adb-PIXEL-1"}, known.GUIDs())

	p.Destroy()
	p.running.Wait()
	assert.Equal(t, "Pixel 8", p.label.Text)
	if devices, _ := p.phones(); assert.Len(t, devices, 1) {
		assert.True(t, devices[0].Wireless())
	}
}

func TestPhoneModuleNeedsAdbOrKDEConnect(t *testing.T) {
	test.NewTempApp(t)
	t.Setenv("PATH", t.TempDir())
	p := newPhone().(*phoneModule)
	assert.Nil(t, p.StatusAreaWidget(), "a widget without adb nor KDE Connect")
}

// noKDE keeps the test away from the KDE Connect of the machine.
func noKDE(t *testing.T) {
	connectKDE = func() kdeLink { return nil }
	t.Cleanup(func() { connectKDE = defaultConnectKDE })
}

var defaultConnectKDE = connectKDE

// fakeKDE is KDE Connect with a paired phone and one asking to pair.
type fakeKDE struct {
	mu       sync.Mutex
	accepted []string
}

func (f *fakeKDE) Devices(context.Context) ([]phone.KDEDevice, error) {
	return []phone.KDEDevice{
		{ID: "pixel", Name: "Pixel 8", Reachable: true, Paired: true, Battery: 78},
		{ID: "tablet", Name: "Tab S9", Reachable: true, PairRequested: true, Battery: -1},
	}, nil
}

func (f *fakeKDE) AcceptPairing(_ context.Context, id string) error {
	f.mu.Lock()
	f.accepted = append(f.accepted, id)
	f.mu.Unlock()
	return nil
}
func (*fakeKDE) RequestPairing(context.Context, string) error       { return nil }
func (*fakeKDE) CancelPairing(context.Context, string) error        { return nil }
func (*fakeKDE) Ring(context.Context, string) error                 { return nil }
func (*fakeKDE) ShareFiles(context.Context, string, []string) error { return nil }
func (*fakeKDE) MountFiles(context.Context, string) (string, error) { return "", nil }
func (*fakeKDE) Watch(ctx context.Context, _ func()) error          { <-ctx.Done(); return nil }

func TestPhoneModuleShowsKDEConnect(t *testing.T) {
	test.NewTempApp(t)
	t.Setenv("PATH", t.TempDir()) // no adb
	kde := &fakeKDE{}
	p := &phoneModule{kde: kde, known: phone.LoadKnown(filepath.Join(t.TempDir(), "phones.json"))}
	if p.StatusAreaWidget() == nil {
		t.Fatal("no widget with KDE Connect there")
	}
	assert.Eventually(t, func() bool {
		_, linked := p.phones()
		return len(linked) == 2
	}, 5*time.Second, 20*time.Millisecond)

	// The tablet asks to pair: it can be accepted from Tyde.
	_, linked := p.phones()
	row := p.linkedRow(linked[1]).(*fyne.Container)
	buttons := row.Objects[1].(*fyne.Container) // the border's right side
	test.Tap(buttons.Objects[0].(*widget.Button))

	p.Destroy()
	p.running.Wait()
	assert.Equal(t, "Pixel 8 · 78 %", p.label.Text)
	kde.mu.Lock()
	defer kde.mu.Unlock()
	assert.Equal(t, []string{"tablet"}, kde.accepted)
}

func TestPanelText(t *testing.T) {
	usb := []phone.Device{{Serial: "0A15", Model: "Pixel 7"}}
	away := []phone.KDEDevice{{Name: "S23", Paired: true, Battery: 50}}
	here := []phone.KDEDevice{{Name: "S23", Paired: true, Reachable: true, Battery: -1}}
	assert.Equal(t, "Pixel 7", panelText(usb, away), "a phone out of reach is not shown")
	assert.Equal(t, "S23", panelText(usb, here), "KDE Connect first, battery unknown")
}
