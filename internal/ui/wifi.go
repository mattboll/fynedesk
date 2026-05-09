package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
)

var wifiPicker *wifiPickerWindow

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

	p := &wifiPickerWindow{
		win:    win,
		status: widget.NewLabel(""),
	}
	wifiPicker = p

	win.SetOnClosed(func() {
		wifiPicker = nil
	})

	// Scan networks
	go rescanWifi()
	networks, _ := scanWifiNetworks()
	p.networks = networks

	// Header
	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		p.setStatus("Scanning...")
		go func() {
			rescanWifi()
			p.refreshNetworks()
			fyne.Do(func() { p.status.SetText("") })
		}()
	})
	refreshBtn.Importance = widget.LowImportance

	title := widget.NewLabel("Wi-Fi Networks")
	title.TextStyle = fyne.TextStyle{Bold: true}
	header := container.NewBorder(nil, nil, nil, refreshBtn, title)

	// Content
	p.netList = container.NewVBox()
	p.buildNetworkRows()

	var content fyne.CanvasObject
	if !isWifiEnabled() {
		content = p.buildDisabledView()
	} else if len(p.networks) == 0 {
		content = p.buildEmptyView()
	} else {
		content = container.NewScroll(p.netList)
	}

	p.body = container.NewStack(content)
	root := container.NewBorder(header, p.status, nil, nil, p.body)

	pickerSize := fyne.NewSize(300, 400)

	fyne.Do(func() {
		win.SetContent(root)

		screen := fynedesk.Instance().Screens().Primary()
		centerX := float32(screen.Width)/2 - pickerSize.Width/2
		centerY := float32(screen.Height)/2 - pickerSize.Height/2
		pos := fyne.NewPos(centerX, centerY)
		wm := fynedesk.Instance().WindowManager()
		wm.ShowOverlay(win, pickerSize, pos)
		if ewm, ok := wm.(*embededWM); ok {
			ewm.onOverlayClosed = func() {
				wifiPicker = nil
			}
		}
	})
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
			p.setStatus("Disconnecting...")
			go p.doDisconnect()
		} else if !n.IsSecured() {
			p.setStatus(fmt.Sprintf("Connecting to %s...", n.SSID))
			go p.doConnect(n.SSID, "", n.Security)
		} else if hasSavedWifiProfile(n.SSID) {
			// Reuse the saved password — only fall back to the password
			// prompt if activation fails (e.g. the saved key is now wrong).
			p.setStatus(fmt.Sprintf("Connecting to %s...", n.SSID))
			go func() {
				if err := connectSavedWifi(n.SSID); err != nil {
					fyne.Do(func() { p.showPasswordForm(n.SSID, n.Security) })
					return
				}
				p.setStatus("Connected!")
				p.refreshNetworks()
			}()
		} else {
			p.showPasswordForm(n.SSID, n.Security)
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

func (p *wifiPickerWindow) refreshNetworks() {
	networks, _ := scanWifiNetworks()
	fyne.Do(func() {
		p.networks = networks
		if len(networks) == 0 {
			p.body.Objects = []fyne.CanvasObject{p.buildEmptyView()}
		} else {
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
	p.setStatus("Connected!")
	p.refreshNetworks()
}

func (p *wifiPickerWindow) doDisconnect() {
	err := disconnectWifi()
	if err != nil {
		p.setStatus(err.Error())
		return
	}
	p.setStatus("Disconnected.")
	p.refreshNetworks()
}

func (p *wifiPickerWindow) showPasswordForm(ssid, security string) {
	passEntry := widget.NewPasswordEntry()
	passEntry.SetPlaceHolder("Password")

	errLabel := widget.NewLabel("")
	errLabel.Wrapping = fyne.TextWrapWord

	var connectBtn *widget.Button
	connectBtn = widget.NewButtonWithIcon("Connect", theme.ConfirmIcon(), func() {
		pw := passEntry.Text
		if pw == "" {
			errLabel.SetText("Password required.")
			return
		}
		connectBtn.Disable()
		errLabel.SetText("Connecting...")
		go func() {
			err := connectWifi(ssid, pw, security)
			fyne.Do(func() {
				if err != nil {
					errLabel.SetText(err.Error())
					connectBtn.Enable()
					return
				}
				p.setStatus("Connected!")
				p.showListView()
				p.refreshNetworks()
			})
		}()
	})

	cancelBtn := widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), func() {
		p.showListView()
	})

	label := widget.NewLabel("Connect to " + ssid)
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
	msg := widget.NewLabel("WiFi is disabled.")
	msg.Alignment = fyne.TextAlignCenter
	enableBtn := widget.NewButton("Enable WiFi", func() {
		toggleWifi(true)
		p.setStatus("Enabling...")
		go func() {
			rescanWifi()
			p.refreshNetworks()
			p.setStatus("")
		}()
	})
	return container.NewVBox(layout.NewSpacer(), msg, container.NewCenter(enableBtn), layout.NewSpacer())
}

func (p *wifiPickerWindow) buildEmptyView() fyne.CanvasObject {
	empty := widget.NewLabel("No networks found.")
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
