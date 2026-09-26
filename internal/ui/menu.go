package ui

import (
	"image/color"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/FyshOS/appie"

	_ "github.com/fyne-io/image/xpm" // load in unix image format

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
)

func (w *widgetPanel) appendAppCategories(acc *widget.Accordion, dismiss func()) {
	accList := acc.Items
	cats := w.desk.IconProvider().CategorizedApps()
	var catNames []string
	hasOther := false
	for cat := range cats {
		if cat == "Other" {
			hasOther = true
			continue
		}
		catNames = append(catNames, cat)
	}
	sort.Strings(catNames)
	if hasOther {
		catNames = append(catNames, "Other")
	}

	for _, cat := range catNames {
		list := cats[cat]
		sort.Slice(list, func(i, j int) bool {
			return strings.ToLower(list[i].Name()) < strings.ToLower(list[j].Name())
		})
		var items []fyne.CanvasObject
		for _, app := range list {
			if app.Hidden() {
				continue
			}
			btn := w.newAppButton(app, dismiss)
			items = append(items, btn)
			defer w.loadIcon(app, btn)
		}

		title := cat
		if cat == "Other" {
			title = locale.T("menu.other")
		}
		accList = append(accList, widget.NewAccordionItem(title,
			container.NewVBox(items...)))
	}

	fyne.Do(func() {
		acc.Items = accList
		acc.Refresh()
	})
}

func (w *widgetPanel) askLogout() {
	// A Wayland session shows the desktop as a panel below application
	// windows, so dialogs need windows of their own there.
	if wlipc.IsWaylandSession() {
		w.askPowerWindow()
		return
	}
	w.askLogoutOverlay()
}

func (w *widgetPanel) askLogoutOverlay() {
	var combined fyne.CanvasObject
	dismiss := func() {
		w.desk.HideOverlay(combined)
	}

	logout := widget.NewButtonWithIcon(locale.T("menu.logout"), theme.LogoutIcon(), func() {
		dismiss()
		afterDismiss(func() { w.desk.WindowManager().Close() })
	})
	logout.Importance = widget.DangerImportance
	cancel := widget.NewButton(locale.T("menu.cancel"), func() {
		dismiss()
	})

	header := widget.NewRichTextFromMarkdown("### " + locale.T("menu.logout"))
	header.Truncation = fyne.TextTruncateEllipsis
	bottomPad := canvas.NewRectangle(color.Transparent)
	bottomPad.SetMinSize(fyne.NewSquareSize(10))
	inner := container.NewBorder(
		header,
		container.NewVBox(
			container.NewHBox(layout.NewSpacer(),
				container.NewGridWithColumns(2, cancel, logout),
				layout.NewSpacer()), bottomPad,
		),
		nil, nil,
		widget.NewLabel(locale.T("menu.logoutConfirm")),
	)

	r, g, b, _ := theme.Color(theme.ColorNameOverlayBackground).RGBA()
	bgCol := &color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 230}

	bg := canvas.NewRectangle(bgCol)
	bg.Shadow = wmtheme.WindowShadow(true)
	icon := canvas.NewImageFromResource(theme.LogoutIcon())
	iconBox := container.NewWithoutLayout(icon)
	icon.Resize(fyne.NewSize(92, 92))
	icon.Move(fyne.NewPos(280-92-theme.Padding(), theme.Padding()))
	logoutContent := container.NewStack(
		iconBox, bg,
		container.NewPadded(inner),
	)

	primary := w.desk.Screens().Primary()
	scale := primary.CanvasScale()
	pW := float32(primary.Width) / scale
	pH := float32(primary.Height) / scale
	size := fyne.NewSize(280, 150)
	pos := fyne.NewPos((pW-size.Width)/2, (pH-size.Height)/2)
	combined = w.desk.(*desktop).ShowOverlayWithBackdrop(logoutContent, size, size, pos, fyne.Position{})
}

func (w *widgetPanel) showAccountMenu(from fyne.CanvasObject) {
	if wlipc.IsWaylandSession() {
		w.showAccountMenuWindow(from)
		return
	}
	w.showAccountMenuOverlay()
}

func (w *widgetPanel) showAccountMenuOverlay() {
	var combined fyne.CanvasObject
	dismiss := func() {
		w.desk.HideOverlay(combined)
	}

	items1 := []fyne.CanvasObject{
		&widget.Button{Icon: theme.LogoutIcon(), Importance: widget.DangerImportance, OnTapped: func() {
			dismiss()
			w.askLogout()
		}},
	}
	items1 = append(items1, &widget.Button{Icon: wmtheme.LockIcon, Importance: widget.LowImportance, OnTapped: func() {
		dismiss()
		w.desk.TriggerScreenSaver(false)
	}})
	if os.Getenv("FYNE_DESK_RUNNER") != "" {
		items1 = append(items1, &widget.Button{Icon: theme.ViewRefreshIcon(), Importance: widget.LowImportance, OnTapped: func() {
			os.Exit(5)
		}})
	}

	items2 := []fyne.CanvasObject{
		&widget.Button{Icon: theme.QuestionIcon(), Importance: widget.LowImportance, OnTapped: func() {
			dismiss()
			w.showAbout()
		}},
		&widget.Button{Icon: theme.SettingsIcon(), Importance: widget.LowImportance, OnTapped: func() {
			dismiss()
			w.showSettings("")
		}},
	}
	items := container.NewBorder(nil, nil, container.NewHBox(items1...), container.NewHBox(items2...),
		&widget.Button{Icon: theme.SearchIcon(), Text: locale.T("menu.search"), Importance: widget.LowImportance, OnTapped: func() {
			dismiss()
			ShowAppLauncher()
		}})

	var recent []fyne.CanvasObject
	for _, app := range w.desk.RecentApps() {
		btn := w.newAppButton(app, dismiss)
		recent = append(recent, btn)
		btn.Icon = app.Icon(w.desk.Settings().IconTheme(), int(64*w.desk.Screens().Primary().CanvasScale()))
	}

	acc := widget.NewAccordion(widget.NewAccordionItem(locale.T("menu.recent"),
		container.NewVBox(recent...)))
	acc.MultiOpen = true
	acc.Open(0)
	go w.appendAppCategories(acc, dismiss)

	r, g, b, _ := theme.Color(theme.ColorNameOverlayBackground).RGBA()
	bgCol := &color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 230}
	bg := canvas.NewRectangle(bgCol)
	bg.Shadow = wmtheme.WindowShadow(true)

	inner := container.NewBorder(items, nil, nil, nil, container.NewScroll(acc))
	menuContent := container.NewStack(bg, container.NewPadded(inner))

	// Anchor to the window canvas, bottom right.
	winSize := w.desk.(*desktop).primaryWin.win.Canvas().Size()
	menuSize := fyne.NewSize(300, 360)
	pos := fyne.NewPos(winSize.Width-menuSize.Width, winSize.Height-menuSize.Height)
	combined = w.desk.(*desktop).ShowOverlayWithBackdrop(menuContent, menuSize, menuSize, pos, fyne.Position{})
}

// afterDismiss runs action on the Fyne thread once the dialog had a moment
// to go away (it used to sleep on the Fyne thread, which froze the panel).
func afterDismiss(action func()) {
	time.AfterFunc(time.Second/10, func() { fyne.Do(action) })
}

// askPowerWindow asks to log out or change the power state, in a window of its own.
func (w *widgetPanel) askPowerWindow() {
	win := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()

	closeAndDo := func(action func()) {
		win.Close()
		afterDismiss(action)
	}

	logout := widget.NewButtonWithIcon(locale.T("menu.logout"), theme.LogoutIcon(), func() {
		closeAndDo(func() { w.desk.WindowManager().Close() })
	})
	logout.Importance = widget.DangerImportance

	shutdown := widget.NewButtonWithIcon(locale.T("menu.powerOff"), wmtheme.PowerIcon, func() {
		closeAndDo(func() { wlipc.RequestShutdown() })
	})
	shutdown.Importance = widget.DangerImportance

	restart := widget.NewButtonWithIcon(locale.T("menu.restart"), theme.ViewRefreshIcon(), func() {
		closeAndDo(func() {
			if wlipc.IsWaylandSession() {
				go func() { // it waits for the reboot to be scheduled
					if err := exec.Command("systemctl", "reboot").Run(); err != nil {
						fyne.LogError("systemctl reboot failed", err)
					}
				}()
			} else {
				os.Exit(5)
			}
		})
	})

	hibernate := widget.NewButtonWithIcon(locale.T("menu.hibernate"), theme.DownloadIcon(), func() {
		closeAndDo(func() { wlipc.RequestHibernate() })
	})

	suspend := widget.NewButtonWithIcon(locale.T("menu.suspend"), theme.MediaPauseIcon(), func() {
		closeAndDo(func() { wlipc.RequestSuspend() })
	})

	cancel := widget.NewButton(locale.T("menu.cancel"), func() {
		win.Close()
	})

	header := widget.NewRichTextFromMarkdown("### " + locale.T("menu.logout"))
	header.Truncation = fyne.TextTruncateEllipsis

	buttons := container.NewGridWithColumns(3,
		logout, restart, shutdown,
		suspend, hibernate, cancel,
	)

	content := container.NewBorder(header, nil, nil, nil, buttons)

	r, g, b, _ := theme.Color(theme.ColorNameBackground).RGBA()
	bgCol := &color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 240}
	bg := canvas.NewRectangle(bgCol)

	icon := canvas.NewImageFromResource(wmtheme.PowerIcon)
	iconBox := container.NewWithoutLayout(icon)
	icon.Resize(fyne.NewSize(72, 72))
	icon.Move(fyne.NewPos(420-72-theme.Padding(), theme.Padding()))
	win.SetContent(container.NewStack(
		iconBox, bg,
		container.NewPadded(content)))

	const modalW, modalH = 420, 200
	if wlipc.IsWaylandSession() {
		screen := w.desk.Screens().Primary()
		scale := screen.CanvasScale()
		screenW := float32(screen.Width) / scale
		screenH := float32(screen.Height) / scale
		pos := fyne.NewPos((screenW-modalW)/2, (screenH-modalH)/2)
		w.desk.WindowManager().ShowOverlay(win, fyne.NewSize(modalW, modalH), pos)
		return
	}

	win.Resize(fyne.NewSize(modalW, modalH))
	win.CenterOnScreen()
	win.Show()
}

// accountMenuReopenDelay is how long after the account menu closed a click on
// the account button is taken as the one that closed it: the compositor
// closes the menu on a click outside it, then delivers that click to the panel.
const accountMenuReopenDelay = 400 * time.Millisecond

// showAccountMenuWindow shows the account menu in a window of its own, or
// closes it if it is open: the account button toggles the menu.
func (w *widgetPanel) showAccountMenuWindow(_ fyne.CanvasObject) {
	if w.accountMenu != nil {
		w.accountMenu.Close()
		return
	}
	if time.Since(w.accountMenuClosedAt) < accountMenuReopenDelay {
		return
	}

	w2 := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	w2.SetPadded(true)
	w2.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			w2.Close()
		}
	})
	items1 := []fyne.CanvasObject{
		&widget.Button{Icon: theme.LogoutIcon(), Importance: widget.DangerImportance, OnTapped: func() {
			w2.Close()
			w.askLogout()
		}},
	}
	isEmbed := w.desk.(*desktop).root.Title() != RootWindowName
	items1 = append(items1, &widget.Button{Icon: wmtheme.LockIcon, Importance: widget.LowImportance, OnTapped: func() {
		w2.Close()
		if wlipc.IsWaylandSession() {
			wlipc.RequestLock()
		} else {
			w.desk.TriggerScreenSaver(false)
		}
	}})
	if !isEmbed {
		if os.Getenv("FYNE_DESK_RUNNER") != "" {
			items1 = append(items1, &widget.Button{Icon: theme.ViewRefreshIcon(), Importance: widget.LowImportance, OnTapped: func() {
				os.Exit(5)
			}})
		}
	} else if wlipc.IsWaylandSession() {
		items1 = append(items1, &widget.Button{Icon: theme.ViewRefreshIcon(), Importance: widget.LowImportance, OnTapped: func() {
			w2.Close()
			wlipc.RequestRestart()
		}})
	}

	if wlipc.IsWaylandSession() {
		items1 = append(items1, &widget.Button{Icon: theme.ComputerIcon(), Importance: widget.LowImportance, OnTapped: func() {
			w2.Close()
			if client := wlipc.DefaultClient(); client != nil {
				client.SendRequest(wlipc.ReqCompositorAction, struct {
					Action string `json:"action"`
				}{Action: wlipc.ActionShowDesktop})
			}
		}})
	}

	items2 := []fyne.CanvasObject{
		&widget.Button{Icon: theme.QuestionIcon(), Importance: widget.LowImportance, OnTapped: func() {
			w.showAbout()
			w2.Close()
		}},
		&widget.Button{Icon: theme.SettingsIcon(), Importance: widget.LowImportance, OnTapped: func() {
			w.showSettings("")
			w2.Close()
		}},
	}
	items := container.NewBorder(nil, nil, container.NewHBox(items1...), container.NewHBox(items2...),
		&widget.Button{Icon: theme.SearchIcon(), Text: locale.T("menu.search"), Importance: widget.LowImportance, OnTapped: func() {
			ShowAppLauncher()
			w2.Close()
		}})

	var recent []fyne.CanvasObject
	for _, app := range w.desk.RecentApps() {
		btn := w.newAppButton(app, w2.Close)
		recent = append(recent, btn)

		icon := app.Icon(w.desk.Settings().IconTheme(), int(64*w.desk.Screens().Primary().CanvasScale()))
		if icon != nil {
			btn.Icon = icon
		}
	}

	acc := widget.NewAccordion(widget.NewAccordionItem(locale.T("menu.recent"),
		container.NewVBox(recent...)))
	acc.MultiOpen = true
	acc.Open(0)
	go w.appendAppCategories(acc, w2.Close)

	w2.SetContent(container.NewBorder(
		items, nil, nil, nil,
		container.NewScroll(acc)))
	screen := w.desk.Screens().Primary()
	scale := screen.CanvasScale()
	screenW := float32(screen.Width) / scale
	screenH := float32(screen.Height) / scale
	panelW := wmtheme.WidgetPanelWidth
	if w.desk.Settings().NarrowWidgetPanel() {
		panelW = wmtheme.NarrowBarWidth
	}
	pos := fyne.NewPos(screenW-300-panelW, screenH-360)
	wm := w.desk.WindowManager()
	wm.ShowOverlay(w2, fyne.NewSize(300, 360), pos)
	w.accountMenu = w2
	onClosed := func() {
		if w.accountMenu == w2 {
			w.accountMenu = nil
		}
		w.accountMenuClosedAt = time.Now()
	}
	if ewm, ok := wm.(*embededWM); ok {
		ewm.onOverlayClosed = onClosed // ShowOverlay owns the window's OnClosed
	} else {
		w2.SetOnClosed(onClosed)
	}
}

func (w *widgetPanel) newAppButton(app appie.AppData, dismiss func()) *widget.Button {
	b := widget.NewButtonWithIcon(app.Name(), wmtheme.BrokenImageIcon, func() {
		dismiss()
		_ = w.desk.RunApp(app)
	})
	b.Alignment = widget.ButtonAlignLeading
	return b
}

func (w *widgetPanel) loadIcon(app appie.AppData, btn *widget.Button) {
	iconRes := app.Icon(w.desk.Settings().IconTheme(), int(64*w.desk.Screens().Primary().CanvasScale()))

	fyne.Do(func() {
		btn.SetIcon(iconRes)
	})
}
