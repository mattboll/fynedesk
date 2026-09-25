package status

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if devices := p.phones(); assert.Len(t, devices, 1) {
		assert.True(t, devices[0].Wireless())
	}
}

func TestPhoneModuleNeedsAdb(t *testing.T) {
	test.NewTempApp(t)
	t.Setenv("PATH", t.TempDir())
	p := newPhone().(*phoneModule)
	assert.Nil(t, p.StatusAreaWidget(), "a widget without adb")
}
