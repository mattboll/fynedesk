package status

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/fyqr/pkg/qrgen"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/phone"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

// PhoneModule names the module of the phones: KDE Connect (battery,
// notifications, files, ringing) and debugging over Wi-Fi with adb, without
// Android Studio.
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
	phoneRefresh     = 10 * time.Second // how often the phones are listed
	phonePairTimeout = 3 * time.Minute  // to scan the code and pair
)

// kdeLink is what the module asks KDE Connect (*phone.KDEConnect).
type kdeLink interface {
	Devices(ctx context.Context) ([]phone.KDEDevice, error)
	RequestPairing(ctx context.Context, id string) error
	AcceptPairing(ctx context.Context, id string) error
	CancelPairing(ctx context.Context, id string) error
	Ring(ctx context.Context, id string) error
	ShareFiles(ctx context.Context, id string, paths []string) error
	MountFiles(ctx context.Context, id string) (string, error)
	Watch(ctx context.Context, changed func()) error
}

// phoneModule shows the phones, linked with KDE Connect or connected with
// adb, and connects new ones.
type phoneModule struct {
	adb     phone.ADB
	hasADB  bool
	browser phone.Browser // finds the phones on the network
	known   *phone.Known
	kde     kdeLink // nil without KDE Connect
	cancel  context.CancelFunc
	ctx     context.Context

	btn   *widget.Button
	label *widget.Label

	mu      sync.Mutex
	devices []phone.Device    // usable with adb
	linked  []phone.KDEDevice // known to KDE Connect
	running sync.WaitGroup    // its goroutines
	kick    chan struct{}     // asks for the phones to be listed again
	shown   sync.Mutex        // one update of the panel and window at a time

	win         fyne.Window
	list        *fyne.Container // phones connected with adb
	kdeList     *fyne.Container // phones of KDE Connect
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

// StatusAreaWidget shows the phone in the widget panel, if there is a way to
// link one: KDE Connect or adb.
func (p *phoneModule) StatusAreaWidget() fyne.CanvasObject {
	p.findTools()
	if !p.hasADB && p.kde == nil {
		return nil
	}
	if p.known == nil {
		p.known = phone.LoadKnown(filepath.Join(wlipc.ConfigDir(), "phones.json"))
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.kick = make(chan struct{}, 1)

	p.label = widget.NewLabel(locale.T("phone.none"))
	p.label.Truncation = fyne.TextTruncateEllipsis // a long name with its battery
	p.btn = widget.NewButtonWithIcon("", phoneIcon, p.showWindow)
	p.btn.Importance = widget.LowImportance

	p.running.Add(1)
	go p.watch()
	if p.kde != nil {
		p.running.Add(1)
		go func() {
			defer p.running.Done()
			_ = p.kde.Watch(p.ctx, p.relist)
		}()
	}
	if p.hasADB {
		p.running.Add(1)
		go func() {
			defer p.running.Done()
			_ = phone.Keep(p.ctx, p.adb, p.browser, p.known.GUIDs, func(string, string) { p.relist() })
		}()
	}
	return container.New(&handleNarrow{}, p.btn, p.label)
}

// findTools looks for adb, scrcpy and KDE Connect, unless tests set them.
func (p *phoneModule) findTools() {
	if p.adb.Path == "" {
		if path, err := exec.LookPath("adb"); err == nil {
			p.adb.Path = path
		}
	}
	p.hasADB = p.adb.Path != ""
	if p.kde == nil {
		p.kde = connectKDE()
	}
}

// connectKDE returns KDE Connect, or nil when it is not installed (a
// variable for tests).
var connectKDE = func() kdeLink {
	if _, err := exec.LookPath("kdeconnectd"); err != nil {
		return nil
	}
	k, err := phone.NewKDEConnect()
	if err != nil {
		return nil
	}
	return k
}

// relist asks for the phones to be listed again, soon.
func (p *phoneModule) relist() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// watch lists the phones now and then, and when asked to.
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
		case <-p.kick:
			time.Sleep(200 * time.Millisecond) // signals come in bursts
		}
	}
}

// refresh lists the phones and shows them.
func (p *phoneModule) refresh() {
	var usable []phone.Device
	if p.hasADB {
		all, _ := p.adb.Devices(p.ctx)
		for _, d := range all {
			if d.State == "device" {
				usable = append(usable, d)
			}
		}
	}
	var linked []phone.KDEDevice
	if p.kde != nil {
		linked, _ = p.kde.Devices(p.ctx)
	}
	if p.ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	p.devices, p.linked = usable, linked
	p.mu.Unlock()
	fyne.Do(p.show)
}

func (p *phoneModule) phones() ([]phone.Device, []phone.KDEDevice) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]phone.Device(nil), p.devices...), append([]phone.KDEDevice(nil), p.linked...)
}

// panelText says which phone is there: the one of KDE Connect in reach, with
// its battery, else those connected with adb.
func panelText(devices []phone.Device, linked []phone.KDEDevice) string {
	for _, l := range linked {
		if l.Paired && l.Reachable {
			if l.Battery >= 0 {
				return fmt.Sprintf("%s · %d %%", l.Name, l.Battery)
			}
			return l.Name
		}
	}
	switch len(devices) {
	case 0:
		return locale.T("phone.none")
	case 1:
		return adbPhoneName(devices[0], linked)
	}
	return fmt.Sprintf(locale.T("phone.count"), len(devices))
}

// show updates the panel and the window.
func (p *phoneModule) show() {
	p.shown.Lock()
	defer p.shown.Unlock()
	devices, linked := p.phones()
	p.label.SetText(panelText(devices, linked))
	if p.win != nil {
		p.fillLists()
	}
}

func phoneName(d phone.Device) string {
	if d.Model != "" {
		return d.Model
	}
	return d.Serial
}

// adbPhoneName names a phone connected with adb: by the name KDE Connect
// knows it by when it is the same phone (at the same address), else by its
// model.
func adbPhoneName(d phone.Device, linked []phone.KDEDevice) string {
	if host, _, err := net.SplitHostPort(d.Serial); err == nil {
		for _, l := range linked {
			if slices.Contains(l.Addresses, host) {
				return l.Name
			}
		}
	}
	return phoneName(d)
}

func heading(text string) *widget.Label {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func hint(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	return l
}

// showWindow opens the phone window: the phones of KDE Connect, those
// connected for debugging, and a way to connect a new one.
func (p *phoneModule) showWindow() {
	p.shown.Lock()
	defer p.shown.Unlock()
	if p.win != nil {
		p.win.RequestFocus()
		return
	}
	// Looked for each time: it may have been installed since.
	_, err := exec.LookPath("scrcpy")
	p.scrcpyFound = err == nil
	w := fyne.CurrentApp().NewWindow(locale.T("phone.title"))
	p.win = w
	content := container.NewVBox()
	if p.kde != nil {
		p.kdeList = container.NewVBox()
		content.Add(heading(locale.T("phone.kdeconnect")))
		content.Add(p.kdeList)
	}
	if p.hasADB {
		if p.kde != nil {
			content.Add(widget.NewSeparator())
		}
		p.list = container.NewVBox()
		pairBox := container.NewVBox()
		var pairBtn *widget.Button
		pairBtn = widget.NewButtonWithIcon(locale.T("phone.pair"), theme.ContentAddIcon(), func() {
			pairBtn.Hide()
			p.startPairing(pairBox, pairBtn)
		})
		content.Add(heading(locale.T("phone.connected")))
		content.Add(p.list)
		content.Add(pairBtn)
		content.Add(pairBox)
		if !p.scrcpyFound {
			content.Add(hint(locale.T("phone.scrcpyHint")))
		}
	}
	w.SetContent(container.NewPadded(content))
	w.SetOnClosed(func() {
		p.shown.Lock()
		defer p.shown.Unlock()
		if p.pairCancel != nil {
			p.pairCancel()
			p.pairCancel = nil
		}
		p.win, p.list, p.kdeList = nil, nil, nil
	})
	p.fillLists()
	w.Resize(fyne.NewSize(460, 260))
	w.CenterOnScreen()
	w.Show()
	p.relist()
}

func (p *phoneModule) fillLists() {
	devices, linked := p.phones()
	if p.kdeList != nil {
		p.kdeList.Objects = nil
		if len(linked) == 0 {
			p.kdeList.Add(hint(locale.T("phone.kdeHint")))
		}
		for _, l := range linked {
			p.kdeList.Add(p.linkedRow(l))
		}
		p.kdeList.Refresh()
	}
	if p.list != nil {
		p.list.Objects = nil
		if len(devices) == 0 {
			p.list.Add(hint(locale.T("phone.noneConnected")))
		}
		for _, d := range devices {
			p.list.Add(p.deviceRow(d, adbPhoneName(d, linked)))
		}
		p.list.Refresh()
	}
}

// act runs an action of KDE Connect off the Fyne thread, then lists the
// phones again.
func (p *phoneModule) act(action func(context.Context) error) {
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		if err := action(p.ctx); err != nil {
			fyne.LogError("KDE Connect", err)
		}
		p.relist()
	}()
}

// linkedRow shows a phone of KDE Connect and what can be done with it.
func (p *phoneModule) linkedRow(l phone.KDEDevice) fyne.CanvasObject {
	id := l.ID
	text := l.Name
	buttons := container.NewHBox()
	button := func(label string, icon fyne.Resource, action func(context.Context) error) {
		buttons.Add(widget.NewButtonWithIcon(label, icon, func() { p.act(action) }))
	}
	switch {
	case l.PairRequested:
		text += " · " + locale.T("phone.wantsToPair")
		button(locale.T("phone.accept"), theme.ConfirmIcon(), func(ctx context.Context) error { return p.kde.AcceptPairing(ctx, id) })
		button(locale.T("phone.refuse"), theme.CancelIcon(), func(ctx context.Context) error { return p.kde.CancelPairing(ctx, id) })
	case l.PairPending:
		text += " · " + locale.T("phone.acceptOnPhone")
	case !l.Paired:
		button(locale.T("phone.pairKDE"), theme.ContentAddIcon(), func(ctx context.Context) error { return p.kde.RequestPairing(ctx, id) })
	case !l.Reachable:
		text += " · " + locale.T("phone.away")
	default:
		if l.Battery >= 0 {
			text += fmt.Sprintf(" · %d %%", l.Battery)
		}
		button("", theme.VolumeUpIcon(), func(ctx context.Context) error { return p.kde.Ring(ctx, id) })
		buttons.Add(widget.NewButtonWithIcon("", theme.UploadIcon(), func() { p.sendFiles(id) }))
		buttons.Add(widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() { p.browseFiles(id) }))
	}
	return container.NewBorder(nil, nil, nil, buttons, widget.NewLabel(text))
}

// sendFiles picks a file and sends it to the phone.
func (p *phoneModule) sendFiles(id string) {
	if p.win == nil {
		return
	}
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := r.URI().Path()
		_ = r.Close()
		p.act(func(ctx context.Context) error { return p.kde.ShareFiles(ctx, id, []string{path}) })
	}, p.win)
	d.Resize(fyne.NewSize(700, 500))
	d.Show()
}

// browseFiles opens the files of the phone in the file manager.
func (p *phoneModule) browseFiles(id string) {
	p.act(func(ctx context.Context) error {
		point, err := p.kde.MountFiles(ctx, id)
		if err != nil {
			return err
		}
		return wm.StartDetached("xdg-open", point)
	})
}

// deviceRow shows a phone connected with adb, with its screen and a way to
// disconnect it.
func (p *phoneModule) deviceRow(d phone.Device, title string) fyne.CanvasObject {
	link := locale.T("phone.usb")
	if d.Wireless() {
		link = locale.T("phone.wifi")
	}
	name := widget.NewLabel(title + " · " + link)
	buttons := container.NewHBox()
	if p.scrcpyFound {
		serial := d.Serial
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
				p.relist()
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
// it for debugging.
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
		p.win.Resize(fyne.NewSize(460, 620))
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
		devices, _ := p.phones()
		for _, d := range devices {
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
