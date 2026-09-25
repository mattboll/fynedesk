package status

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/fyqr/pkg/qrgen"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/phone"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

// PhoneModule names the module that connects Android phones for debugging
// over Wi-Fi (adb), without Android Studio.
const PhoneModule = "Phone"

var phoneMeta = tyde.ModuleMetadata{
	Name:        PhoneModule,
	NewInstance: newPhone,
}

//go:embed phone.svg
var phoneSVG []byte

// newPairing makes the pairing each QR code shows (a variable for tests).
var newPairing = phone.NewPairing

var phoneIcon = theme.NewThemedResource(fyne.NewStaticResource("phone.svg", phoneSVG))

const (
	phoneRefresh     = 10 * time.Second // how often the phones adb knows are listed
	phonePairTimeout = 3 * time.Minute  // to scan the code and pair
)

// phoneModule shows the phones connected with adb and connects new ones.
type phoneModule struct {
	adb     phone.ADB
	browser phone.Browser // finds the phones on the network
	known   *phone.Known
	cancel  context.CancelFunc
	ctx     context.Context

	btn   *widget.Button
	label *widget.Label

	mu      sync.Mutex
	devices []phone.Device // usable ones
	running sync.WaitGroup // its goroutines

	win         fyne.Window
	list        *fyne.Container
	pairCancel  context.CancelFunc
	scrcpyFound bool
}

func newPhone() tyde.Module {
	return &phoneModule{browser: phone.MDNS{}}
}

func (p *phoneModule) Metadata() tyde.ModuleMetadata {
	return phoneMeta
}

func (p *phoneModule) Destroy() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *phoneModule) StatusAreaWidget() fyne.CanvasObject {
	if p.adb.Path == "" {
		path, err := exec.LookPath("adb")
		if err != nil {
			return nil // no adb: nothing to connect with
		}
		p.adb.Path = path
	}
	_, err := exec.LookPath("scrcpy")
	p.scrcpyFound = err == nil
	if p.known == nil {
		p.known = phone.LoadKnown(filepath.Join(wlipc.ConfigDir(), "phones.json"))
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	p.label = widget.NewLabel(locale.T("phone.none"))
	p.btn = widget.NewButtonWithIcon("", phoneIcon, p.showWindow)
	p.btn.Importance = widget.LowImportance

	p.running.Add(2)
	go p.watch()
	go func() {
		defer p.running.Done()
		_ = phone.Keep(p.ctx, p.adb, p.browser, p.known.GUIDs, func(string, string) { p.refresh() })
	}()
	return container.New(&handleNarrow{}, p.btn, p.label)
}

// watch lists the phones adb knows now and then: some are plugged in with
// a cable, or connected from a terminal.
func (p *phoneModule) watch() {
	defer p.running.Done()
	ticker := time.NewTicker(phoneRefresh)
	defer ticker.Stop()
	for {
		p.refresh()
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// refresh lists the usable phones and shows them.
func (p *phoneModule) refresh() {
	all, err := p.adb.Devices(p.ctx)
	if err != nil {
		return
	}
	var usable []phone.Device
	for _, d := range all {
		if d.State == "device" {
			usable = append(usable, d)
		}
	}
	p.mu.Lock()
	p.devices = usable
	p.mu.Unlock()
	fyne.Do(p.show)
}

func (p *phoneModule) phones() []phone.Device {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]phone.Device(nil), p.devices...)
}

// show updates the panel and the window.
func (p *phoneModule) show() {
	devices := p.phones()
	switch len(devices) {
	case 0:
		p.label.SetText(locale.T("phone.none"))
	case 1:
		p.label.SetText(phoneName(devices[0]))
	default:
		p.label.SetText(fmt.Sprintf(locale.T("phone.count"), len(devices)))
	}
	if p.list != nil {
		p.fillList()
	}
}

func phoneName(d phone.Device) string {
	if d.Model != "" {
		return d.Model
	}
	return d.Serial
}

// showWindow opens the phone window: the connected phones, and a way to
// connect a new one.
func (p *phoneModule) showWindow() {
	if p.win != nil {
		p.win.RequestFocus()
		return
	}
	w := fyne.CurrentApp().NewWindow(locale.T("phone.title"))
	p.win = w
	p.list = container.NewVBox()
	pairBox := container.NewVBox()
	var pairBtn *widget.Button
	pairBtn = widget.NewButtonWithIcon(locale.T("phone.pair"), theme.ContentAddIcon(), func() {
		pairBtn.Hide()
		p.startPairing(pairBox, pairBtn)
	})
	pairBtn.Importance = widget.HighImportance

	content := container.NewVBox(
		widget.NewLabelWithStyle(locale.T("phone.connected"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		p.list,
		widget.NewSeparator(),
		pairBtn,
		pairBox,
	)
	if !p.scrcpyFound {
		hint := widget.NewLabel(locale.T("phone.scrcpyHint"))
		hint.Wrapping = fyne.TextWrapWord
		hint.Importance = widget.LowImportance
		content.Add(hint)
	}
	w.SetContent(container.NewPadded(content))
	w.SetOnClosed(func() {
		if p.pairCancel != nil {
			p.pairCancel()
			p.pairCancel = nil
		}
		p.win = nil
		p.list = nil
	})
	p.fillList()
	w.Resize(fyne.NewSize(440, 200))
	w.CenterOnScreen()
	w.Show()
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		p.refresh()
	}()
}

// fillList shows the connected phones, with their screen and a way to
// disconnect them.
func (p *phoneModule) fillList() {
	p.list.Objects = nil
	devices := p.phones()
	if len(devices) == 0 {
		none := widget.NewLabel(locale.T("phone.noneConnected"))
		none.Importance = widget.LowImportance
		p.list.Add(none)
	}
	for _, d := range devices {
		p.list.Add(p.deviceRow(d))
	}
	p.list.Refresh()
}

func (p *phoneModule) deviceRow(d phone.Device) fyne.CanvasObject {
	link := locale.T("phone.usb")
	if d.Wireless() {
		link = locale.T("phone.wifi")
	}
	name := widget.NewLabel(phoneName(d) + " · " + link)
	buttons := container.NewHBox()
	if p.scrcpyFound {
		serial, title := d.Serial, phoneName(d)
		buttons.Add(widget.NewButtonWithIcon(locale.T("phone.screen"), theme.ComputerIcon(), func() {
			showPhoneScreen(serial, title)
		}))
	}
	if d.Wireless() {
		serial := d.Serial
		buttons.Add(widget.NewButtonWithIcon(locale.T("phone.disconnect"), theme.CancelIcon(), func() {
			p.running.Add(1)
			go func() {
				defer p.running.Done()
				_ = p.adb.Disconnect(p.ctx, serial)
				p.refresh()
			}()
		}))
	}
	return container.NewBorder(nil, nil, nil, buttons, name)
}

// showPhoneScreen shows the screen of a phone in a window, where it can be
// used with the mouse and the keyboard (scrcpy).
func showPhoneScreen(serial, title string) {
	cmd := exec.Command("scrcpy", "-s", serial, "--window-title", title)
	if err := cmd.Start(); err != nil {
		fyne.LogError("scrcpy", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// startPairing shows the QR code to scan and connects the phone that scans
// it.
func (p *phoneModule) startPairing(box *fyne.Container, pairBtn *widget.Button) {
	pairing := newPairing()
	img, err := qrgen.NewQR(pairing.QR()).Image(512)
	if err != nil {
		fyne.LogError("phone QR", err)
		pairBtn.Show()
		return
	}
	qr := canvas.NewImageFromImage(img)
	qr.FillMode = canvas.ImageFillContain
	qr.SetMinSize(fyne.NewSize(220, 220))
	help := widget.NewLabel(locale.T("phone.pairHelp"))
	help.Wrapping = fyne.TextWrapWord
	status := widget.NewLabel("")
	status.Alignment = fyne.TextAlignCenter
	box.Objects = []fyne.CanvasObject{help, container.NewCenter(qr), status}
	box.Refresh()
	if p.win != nil {
		p.win.Resize(fyne.NewSize(440, 560))
	}

	ctx, cancel := context.WithTimeout(p.ctx, phonePairTimeout)
	p.pairCancel = cancel
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		defer cancel()
		guid, addr, err := phone.Pair(ctx, p.adb, p.browser, pairing, func(s phone.Step) {
			fyne.Do(func() { status.SetText(stepText(s)) })
		})
		p.refresh()
		model := ""
		for _, d := range p.phones() {
			if d.Serial == addr {
				model = d.Model
			}
		}
		if err == nil {
			_ = p.known.Add(guid, model)
		}
		fyne.Do(func() {
			switch {
			case err == nil:
				status.SetText(fmt.Sprintf(locale.T("phone.done"), model))
				qr.Hide()
				help.Hide()
			case errors.Is(err, context.Canceled):
				return // the window was closed
			default:
				status.SetText(fmt.Sprintf(locale.T("phone.failed"), err))
				qr.Hide()
			}
			pairBtn.Show()
		})
	}()
}

func stepText(s phone.Step) string {
	switch s {
	case phone.StepPairing:
		return locale.T("phone.pairing")
	case phone.StepConnecting:
		return locale.T("phone.connecting")
	case phone.StepConnected:
		return ""
	}
	return locale.T("phone.waiting")
}
