package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/modules/status"
	wmtheme "fyshos.com/tyde/theme"
)

var wifiPicker *wifiPickerWindow

func init() {
	status.WifiPicker = ShowWifiPicker
}

type wifiPickerWindow struct {
	win      fyne.Window
	networks []WifiNetwork
	netList  *fyne.Container // VBox of network rows
	body     *fyne.Container // swapped between list view and password form
	status   *widget.Label
}

// ShowWifiPicker opens the WiFi network picker overlay.
// If already open, it toggles closed.
func ShowWifiPicker() {
	if wifiPicker != nil {
		wifiPicker.close()
		return
	}

	var win fyne.Window
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		win = d.CreateSplashWindow()
		win.SetPadded(true)
		win.SetTitle("WiFi " + SkipTaskbarHint)
	} else {
		win = fyne.CurrentApp().NewWindow("WiFi " + SkipTaskbarHint)
	}

	p, root := newWifiPanel(win)
	wifiPicker = p

	win.SetOnClosed(func() {
		wifiPicker = nil
	})

	pickerSize := fyne.NewSize(300, 400)

	fyne.Do(func() {
		win.SetContent(root)

		screen := tyde.Instance().Screens().Primary()
		centerX := float32(screen.Width)/2 - pickerSize.Width/2
		centerY := float32(screen.Height)/2 - pickerSize.Height/2
		pos := fyne.NewPos(centerX, centerY)
		wm := tyde.Instance().WindowManager()
		wm.ShowOverlay(win, pickerSize, pos)
		if ewm, ok := wm.(*embededWM); ok {
			ewm.onOverlayClosed = func() {
				wifiPicker = nil
			}
		}
	})
}

// newWifiPanel builds the NetworkManager Wi-Fi network list, for the picker
// overlay and the Network settings panel. win is the window it is shown in.
func newWifiPanel(win fyne.Window) (*wifiPickerWindow, fyne.CanvasObject) {
	p := &wifiPickerWindow{
		win:    win,
		status: widget.NewLabel(""),
	}
	// The networks are listed off the Fyne thread (nmcli takes its time):
	// the panel shows at once and fills in.
	go func() {
		rescanWifi()
		p.refreshNetworks()
	}()

	// Header
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		p.setStatus(locale.T("wifi.scanning"))
		go func() {
			rescanWifi()
			p.refreshNetworks()
			fyne.Do(func() { p.status.SetText("") })
		}()
	})
	refreshBtn.Importance = widget.LowImportance

	title := widget.NewLabel(locale.T("wifi.networks"))
	title.TextStyle = fyne.TextStyle{Bold: true}
	header := container.NewBorder(nil, nil, nil, refreshBtn, title)

	// Content
	p.netList = container.NewVBox()
	scanning := widget.NewLabel(locale.T("wifi.scanning"))
	scanning.Alignment = fyne.TextAlignCenter
	p.body = container.NewStack(container.NewCenter(scanning))
	root := container.NewBorder(header, p.status, nil, nil, p.body)

	return p, root
}

func (p *wifiPickerWindow) buildNetworkRows() {
	p.netList.Objects = nil
	for _, n := range p.networks {
		n := n // capture
		row := p.buildNetworkRow(n)
		p.netList.Objects = append(p.netList.Objects, row)
	}
}

func (p *wifiPickerWindow) buildNetworkRow(n WifiNetwork) fyne.CanvasObject {
	ssid := widget.NewLabel(n.SSID)
	ssid.Truncation = fyne.TextTruncateEllipsis
	if n.Active {
		ssid.TextStyle = fyne.TextStyle{Bold: true}
	}

	signal := widget.NewLabel(signalText(n.Signal))
	signal.Alignment = fyne.TextAlignTrailing

	var icons []fyne.CanvasObject
	if n.IsSecured() {
		icons = append(icons, widget.NewIcon(wmtheme.LockIcon))
	}
	icons = append(icons, signal)
	if n.Active {
		icons = append(icons, widget.NewIcon(wmtheme.WifiIcon))
	}
	right := container.NewHBox(icons...)

	row := container.NewBorder(nil, nil, nil, right, ssid)

	btn := widget.NewButton("", func() {
		if n.Active {
			p.setStatus(locale.T("wifi.disconnecting"))
			go p.doDisconnect()
		} else if !n.IsSecured() {
			p.setStatus(locale.Tf("wifi.connectingTo", n.SSID))
			go p.doConnect(n.SSID, "", n.Security)
		} else {
			// Reuse a saved password — only fall back to the password
			// prompt if there is none or activation fails (e.g. the saved
			// key is now wrong). nmcli is asked off the Fyne thread.
			p.setStatus(locale.Tf("wifi.connectingTo", n.SSID))
			go func() {
				if !hasSavedWifiProfile(n.SSID) || connectSavedWifi(n.SSID) != nil {
					p.setStatus("")
					fyne.Do(func() { p.showPasswordForm(n.SSID, n.Security) })
					return
				}
				p.setStatus(locale.T("wifi.connected"))
				p.refreshNetworks()
			}()
		}
	})
	btn.Importance = widget.LowImportance

	return container.NewStack(btn, row)
}

func (p *wifiPickerWindow) close() {
	wifiPicker = nil
	if p.win != nil {
		p.win.Close()
	}
}

func (p *wifiPickerWindow) setStatus(text string) {
	fyne.Do(func() { p.status.SetText(text) })
}

// refreshNetworks lists the networks again and shows them. Not on the Fyne
// thread: it runs nmcli.
func (p *wifiPickerWindow) refreshNetworks() {
	enabled := isWifiEnabled()
	networks, _ := scanWifiNetworks()
	fyne.Do(func() {
		p.networks = networks
		switch {
		case !enabled:
			p.body.Objects = []fyne.CanvasObject{p.buildDisabledView()}
		case len(networks) == 0:
			p.body.Objects = []fyne.CanvasObject{p.buildEmptyView()}
		default:
			p.buildNetworkRows()
			p.body.Objects = []fyne.CanvasObject{container.NewScroll(p.netList)}
		}
		p.body.Refresh()
	})
}

func (p *wifiPickerWindow) doConnect(ssid, password, security string) {
	err := connectWifi(ssid, password, security)
	if err != nil {
		p.setStatus(err.Error())
		return
	}
	p.setStatus(locale.T("wifi.connected"))
	p.refreshNetworks()
}

func (p *wifiPickerWindow) doDisconnect() {
	err := disconnectWifi()
	if err != nil {
		p.setStatus(err.Error())
		return
	}
	p.setStatus(locale.T("wifi.disconnected"))
	p.refreshNetworks()
}

func (p *wifiPickerWindow) showPasswordForm(ssid, security string) {
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder(locale.T("wifi.password"))

	errLabel := widget.NewLabel("")
	errLabel.Wrapping = fyne.TextWrapWord

	var connectBtn *widget.Button
	connectBtn = widget.NewButtonWithIcon(locale.T("wifi.connect"), theme.ConfirmIcon(), func() {
		pw := passEntry.Text
		if pw == "" {
			errLabel.SetText(locale.T("wifi.passwordRequired"))
			return
		}
		connectBtn.Disable()
		errLabel.SetText(locale.T("wifi.connecting"))
		go func() {
			err := connectWifi(ssid, pw, security)
			fyne.Do(func() {
				if err != nil {
					errLabel.SetText(err.Error())
					connectBtn.Enable()
					return
				}
				p.setStatus(locale.T("wifi.connected"))
				p.showListView()
				p.refreshNetworks()
			})
		}()
	})

	cancelBtn := widget.NewButtonWithIcon(locale.T("wifi.cancel"), theme.CancelIcon(), func() {
		p.showListView()
	})

	label := widget.NewLabel(locale.Tf("wifi.connectTo", ssid))
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Wrapping = fyne.TextWrapWord

	buttons := container.NewHBox(layout.NewSpacer(), cancelBtn, connectBtn)
	form := container.NewVBox(label, passEntry, errLabel, buttons)

	fyne.Do(func() {
		p.body.Objects = []fyne.CanvasObject{form}
		p.body.Refresh()
		p.win.Canvas().Focus(passEntry)
		go ensureFocused(p.win, passEntry)
	})
}

func (p *wifiPickerWindow) showListView() {
	fyne.Do(func() {
		p.buildNetworkRows()
		p.body.Objects = []fyne.CanvasObject{container.NewScroll(p.netList)}
		p.body.Refresh()
		p.status.SetText("")
	})
}

func (p *wifiPickerWindow) buildDisabledView() fyne.CanvasObject {
	msg := widget.NewLabel(locale.T("wifi.disabled"))
	msg.Alignment = fyne.TextAlignCenter
	enableBtn := widget.NewButton(locale.T("wifi.enable"), func() {
		p.setStatus(locale.T("wifi.enabling"))
		go func() {
			toggleWifi(true)
			rescanWifi()
			p.refreshNetworks()
			p.setStatus("")
		}()
	})
	return container.NewVBox(layout.NewSpacer(), msg, container.NewCenter(enableBtn), layout.NewSpacer())
}

func (p *wifiPickerWindow) buildEmptyView() fyne.CanvasObject {
	empty := widget.NewLabel(locale.T("wifi.noNetworks"))
	empty.Alignment = fyne.TextAlignCenter
	return container.NewCenter(empty)
}

func signalText(signal int) string {
	switch {
	case signal >= 75:
		return "▂▄▆█"
	case signal >= 50:
		return "▂▄▆"
	case signal >= 25:
		return "▂▄"
	default:
		return "▂"
	}
}
