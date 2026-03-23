package ui

import (
	"image/color"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/FyshOS/appie"

	_ "github.com/fyne-io/image/xpm" // load in unix image format

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

func (w *widgetPanel) appendAppCategories(acc *widget.Accordion, win fyne.Window) {
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
		var items []fyne.CanvasObject
		for _, app := range list {
			if app.Hidden() {
				continue
			}
			btn := w.newAppButton(app, win)
			items = append(items, btn)
			defer w.loadIcon(app, btn)
		}
		accList = append(accList, widget.NewAccordionItem(cat,
			container.NewVBox(items...)))
	}

	fyne.Do(func() {
		acc.Items = accList
		acc.Refresh()
	})
}

func (w *widgetPanel) askLogout() {
	win := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()

	closeAndDo := func(action func()) {
		win.Close()
		time.Sleep(time.Second / 10)
		action()
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
				exec.Command("systemctl", "reboot").Run()
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

func (w *widgetPanel) showAccountMenu(_ fyne.CanvasObject) {
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
		}}}
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
			w.showSettings()
			w2.Close()
		}}}
	items := container.NewBorder(nil, nil, container.NewHBox(items1...), container.NewHBox(items2...),
		&widget.Button{Icon: theme.SearchIcon(), Text: locale.T("menu.search"), Importance: widget.LowImportance, OnTapped: func() {
			ShowAppLauncher()
			w2.Close()
		}})

	var recent []fyne.CanvasObject
	for _, app := range w.desk.RecentApps() {
		btn := w.newAppButton(app, w2)
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
	go w.appendAppCategories(acc, w2)

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
	w.desk.WindowManager().ShowOverlay(w2, fyne.NewSize(300, 360), pos)
}

func (w *widgetPanel) newAppButton(app appie.AppData, w2 fyne.Window) *widget.Button {
	b := widget.NewButtonWithIcon(app.Name(), wmtheme.BrokenImageIcon, func() {
		w2.Close()
		_ = w.desk.RunApp(app)
	})
	b.Alignment = widget.ButtonAlignLeading
	return b
}

func (w *widgetPanel) loadIcon(app appie.AppData, btn *widget.Button) {
	iconRes := app.Icon(w.desk.Settings().IconTheme(), int(64*w.desk.Screens().Primary().CanvasScale()))
	if iconRes == nil {
		return
	}

	fyne.Do(func() {
		btn.SetIcon(iconRes)
	})
}
