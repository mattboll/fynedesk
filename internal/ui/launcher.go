package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmTheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

// readCompositorState reads the current compositor state from the config file.
func readCompositorState() *CompositorState {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(configDir, "fynedesk", "compositor-state.json"))
	if err != nil {
		return nil
	}
	var state CompositorState
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	return &state
}

// positionLauncherAtCursor positions the launcher overlay centered on the output
// that contains the cursor at (cx, cy) in compositor layout-space pixels.
func positionLauncherAtCursor(title string, cx, cy float32, size fyne.Size) {
	// Find the output that contains the cursor
	screens := fynedesk.Instance().Screens()
	if screens == nil {
		return
	}
	primary := screens.Primary()
	if primary == nil {
		return
	}

	// Default to primary screen dimensions (in Fyne units)
	scale := primary.CanvasScale()
	screenW := float32(primary.Width) / scale
	screenH := float32(primary.Height) / scale
	offsetX := float32(primary.X) / scale
	offsetY := float32(primary.Y) / scale

	// Try to find the cursor's output from compositor state.
	// Compositor state uses logical (layout-space) pixels, same as cursor coords.
	state := readCompositorState()
	if state != nil {
		for _, out := range state.Outputs {
			ox, oy := float32(out.X), float32(out.Y)
			ow := float32(out.Width)
			oh := float32(out.Height)
			if cx >= ox && cx < ox+ow && cy >= oy && cy < oy+oh {
				screenW = ow
				screenH = oh
				offsetX = ox
				offsetY = oy
				break
			}
		}
	}

	// Center the launcher on that output (absolute layout coords)
	finalX := offsetX + (screenW-size.Width)/2
	finalY := offsetY + (screenH-size.Height)/2
	wlipc.RequestOverlayPositionAbsolute(title, finalX, finalY, size.Width, size.Height)
}

// positionLauncherOnPrimary centers the launcher on the primary screen
// using absolute layout coordinates.
func positionLauncherOnPrimary(title string, size fyne.Size) {
	screens := fynedesk.Instance().Screens()
	if screens == nil {
		return
	}
	primary := screens.Primary()
	if primary == nil {
		return
	}
	scale := primary.CanvasScale()
	screenW := float32(primary.Width) / scale
	screenH := float32(primary.Height) / scale
	offX := float32(primary.X) / scale
	offY := float32(primary.Y) / scale
	finalX := offX + (screenW-size.Width)/2
	finalY := offY + (screenH-size.Height)/2
	wlipc.RequestOverlayPositionAbsolute(title, finalX, finalY, size.Width, size.Height)
}

var appExec *picker

type appEntry struct {
	widget.Entry

	pick *picker
}

func (e *appEntry) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyEscape:
		e.pick.close()
	case fyne.KeyReturn:
		e.pick.pickSelected()
	case fyne.KeyUp:
		e.pick.setActiveIndex(e.pick.activeIndex - 1)
	case fyne.KeyDown:
		e.pick.setActiveIndex(e.pick.activeIndex + 1)
	default:
		e.Entry.TypedKey(ev)
	}
}

type picker struct {
	win      fyne.Window
	desk     fynedesk.Desktop
	callback func(data appie.AppData, actionID int)
	showMods bool

	entry            *appEntry
	appList          *fyne.Container
	appScroll        *container.Scroll
	activeIndex      int
	debounceTimer    *time.Timer
	debounceDuration time.Duration
}

func (l *picker) close() {
	l.win.Close()
}

func (l *picker) pickSelected() {
	if len(l.appList.Objects) == 0 {
		return
	}

	btn, ok := l.appList.Objects[l.activeIndex].(*widget.Button)
	if !ok {
		return
	}
	btn.OnTapped()
}

func (l *picker) setActiveIndex(index int) {
	if index < 0 || index >= len(l.appList.Objects) {
		return
	}

	oldActive, ok := l.appList.Objects[l.activeIndex].(*widget.Button)
	if !ok {
		return
	}
	oldActive.Importance = widget.MediumImportance
	oldActive.Refresh()
	active, ok := l.appList.Objects[index].(*widget.Button)
	if !ok {
		return
	}
	active.Importance = widget.HighImportance
	active.Refresh()

	l.activeIndex = index
	l.appScroll.Offset = fyne.NewPos(0,
		active.Position().Y+active.Size().Height/2-l.appScroll.Size().Height/2)
	l.appScroll.Refresh()
}

func (l *picker) updateAppListMatching(input string) {
	l.activeIndex = 0
	l.appScroll.ScrollToTop()
	l.appList.Objects = l.appButtonListMatching(input)
	l.appList.Refresh()
}

func (l *picker) appButtonListMatching(input string) []fyne.CanvasObject {
	var appList []fyne.CanvasObject
	var iconList = []appie.AppData{}

	dataRange := l.desk.IconProvider().FindAppsMatching(input)
	for _, data := range dataRange {
		if data == nil || data.Hidden() {
			continue
		}
		appData := data // capture for goroutine below
		app := widget.NewButtonWithIcon(appData.Name(), wmTheme.BrokenImageIcon, func() {
			l.callback(appData, -1)
			l.win.Close()
		})
		app.Alignment = widget.ButtonAlignLeading

		appList = append(appList, app)
		iconList = append(iconList, data)

		for id, action := range appData.Actions() {
			actionID := id // capture for goroutine below
			app := widget.NewButtonWithIcon(data.Name()+" : "+action.Name(), wmTheme.BrokenImageIcon, func() {
				l.callback(appData, actionID)
				l.win.Close()
			})
			app.Alignment = widget.ButtonAlignLeading

			appList = append(appList, app)
			iconList = append(iconList, data)
		}
	}
	go l.loadIcons(iconList, appList)

	appList = append(appList, l.loadSuggestionsMatching(input)...)
	if len(appList) > 0 {
		appList[0].(*widget.Button).Importance = widget.HighImportance
	}

	if len(appList) == 0 {
		noResults := widget.NewLabel(locale.T("launcher.noApps"))
		noResults.Alignment = fyne.TextAlignCenter
		appList = append(appList, noResults)
	}

	return appList
}

func (l *picker) loadIcons(dataRange []appie.AppData, appList []fyne.CanvasObject) {
	iconTheme := l.desk.Settings().IconTheme()

	for i, data := range dataRange {
		app := appList[i].(*widget.Button)
		icon := data.Icon(iconTheme, 32)
		if icon != nil {
			fyne.Do(func() {
				app.SetIcon(icon)
			})
		}
	}
}

func (l *picker) loadSuggestionsMatching(input string) []fyne.CanvasObject {
	var suggestList []fyne.CanvasObject

	for _, m := range l.desk.Modules() {
		suggest, ok := m.(fynedesk.LaunchSuggestionModule)
		if !ok {
			continue
		}

		for _, item := range suggest.LaunchSuggestions(input) {
			launchData := item // capture for goroutine below
			button := widget.NewButtonWithIcon(item.Title(), item.Icon(), func() {
				l.win.Close()
				launchData.Launch()
			})

			suggestList = append(suggestList, button)
		}
	}

	return suggestList
}

func (l *picker) Show() {
	l.win.Show()
}

func newAppPicker(title string, callback func(appie.AppData, int)) *picker {
	var win fyne.Window
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		win = d.CreateSplashWindow()
		win.SetPadded(true)
		win.SetTitle(title)
	} else {
		win = fyne.CurrentApp().NewWindow(title)
	}

	win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			win.Close()
			return
		}
	})

	appList := container.NewVBox()
	appScroller := container.NewScroll(appList)
	l := &picker{win: win, desk: fynedesk.Instance(), appList: appList, appScroll: appScroller, callback: callback,
		debounceDuration: 150 * time.Millisecond}

	entry := &appEntry{pick: l}
	entry.ExtendBaseWidget(entry)
	entry.SetPlaceHolder(locale.T("launcher.app"))
	entry.OnChanged = func(input string) {
		if l.debounceTimer != nil {
			l.debounceTimer.Stop()
		}
		if input == "" {
			appList.Objects = nil
			appList.Refresh()
			return
		}
		if l.debounceDuration <= 0 {
			l.updateAppListMatching(input)
			return
		}
		l.debounceTimer = time.AfterFunc(l.debounceDuration, func() {
			fyne.Do(func() {
				l.updateAppListMatching(input)
			})
		})
	}
	l.entry = entry

	clearBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		entry.SetText("")
		appList.Objects = nil
		appList.Refresh()
	})
	searchRow := container.NewBorder(nil, nil, nil, clearBtn, entry)

	cancel := widget.NewButtonWithIcon(locale.T("launcher.cancel"), theme.CancelIcon(), func() {
		win.Close()
	})

	fyne.Do(func() {
		ideal := fyne.NewSize(300,
			cancel.MinSize().Height*4+theme.Padding()*6+entry.MinSize().Height)
		win.SetContent(container.NewBorder(searchRow, cancel, nil, nil, appScroller))
		win.Resize(ideal)

		// Position on the correct output using cursor position from compositor.
		// Do this BEFORE CenterOnScreen to avoid a visible jump.
		cx, cy := launcherCursorX, launcherCursorY
		launcherCursorX, launcherCursorY = 0, 0 // reset for next use
		if cx > 0 || cy > 0 {
			positionLauncherAtCursor(win.Title(), cx, cy, ideal)
		} else {
			positionLauncherOnPrimary(win.Title(), ideal)
		}

		// CenterOnScreen as Fyne-level fallback (non-Wayland or if IPC fails)
		win.CenterOnScreen()
		win.Canvas().Focus(entry)

		go ensureFocused(win, entry)
	})
	return l
}

// ShowAppLauncherAt opens the launcher, positioning it on the screen that
// contains the given cursor coordinates (layout-space pixels). If cx and cy
// are both 0 it falls back to CenterOnScreen (primary display).
func ShowAppLauncherAt(cx, cy float32) {
	launcherCursorX = cx
	launcherCursorY = cy
	ShowAppLauncher()
}

var launcherCursorX, launcherCursorY float32

// ShowAppLauncher opens a new application launcher, closing an old one if it existed.
func ShowAppLauncher() {
	if appExec != nil {
		prev := appExec
		appExec = nil // clear immediately to avoid stale reference
		prev.close()
		return
	}

	appExec = newAppPicker("Application Launcher "+SkipTaskbarHint, func(app appie.AppData, actionID int) {
		var err error
		if actionID == -1 {
			err = fynedesk.Instance().RunApp(app)
		} else {
			err = fynedesk.Instance().RunAppAction(app, actionID)
		}
		if err != nil {
			fyne.LogError("Failed to start app", err)
			wm.SendNotification(wm.NewNotification("Launch failed", "Could not start "+app.Name()))
			return
		}
	})
	appExec.showMods = true
	appExec.win.SetOnClosed(func() {
		appExec = nil
	})
	appExec.Show()
}
