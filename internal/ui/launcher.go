package ui

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/modules/ai"
	wmTheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

const (
	launcherWidth      = 350
	launcherMaxResults = 5
)

var appExec *picker

// readCompositorState reads the current compositor state from the config file.
func readCompositorState() *CompositorState {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(configDir, "tyde", "compositor-state.json"))
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
	screens := tyde.Instance().Screens()
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
			if out.MirrorOf != "" {
				continue // shows another output, not part of the desktop
			}
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
	screens := tyde.Instance().Screens()
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

// runAsync runs f on a new goroutine in production. Tests override it to run inline.
var runAsync = func(f func()) { go f() }

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
	win      fyne.Window // separate launcher window, used in a Wayland session
	title    string
	desk     tyde.Desktop
	callback func(data appie.AppData, actionID int)
	showMods bool

	entry       *appEntry
	appList     *fyne.Container
	appScroll   *container.Scroll
	activeIndex int

	debounceTimer    *time.Timer
	debounceDuration time.Duration

	bg       *canvas.Rectangle
	fullSize fyne.Size

	overlay  fyne.CanvasObject
	onClosed func()
}

func (l *picker) close() {
	if l.win != nil {
		l.win.Close() // runs onClosed
		return
	}
	if l.overlay != nil {
		l.desk.HideOverlay(l.overlay)
	}
	if l.onClosed != nil {
		l.onClosed()
	}
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
	l.updateBgSize()
}

func (l *picker) updateBgSize() {
	if l.bg == nil {
		return
	}

	pad := l.entry.Theme().Size(theme.SizeNamePadding)
	entryHeight := l.entry.MinSize().Height
	h := entryHeight + pad*2

	if len(l.appList.Objects) > 0 {
		btnHeight := l.appList.Objects[0].MinSize().Height
		count := len(l.appList.Objects)
		if count > launcherMaxResults {
			count = launcherMaxResults
		}
		h += float32(count) * (btnHeight + pad)
	}
	if h > l.fullSize.Height {
		h = l.fullSize.Height
	}

	l.bg.Resize(fyne.NewSize(l.fullSize.Width, h))
	l.bg.Refresh()
}

func (l *picker) appButtonListMatching(input string) []fyne.CanvasObject {
	var appList []fyne.CanvasObject
	iconList := []appie.AppData{}

	dataRange := l.desk.IconProvider().FindAppsMatching(input)
	for _, data := range dataRange {
		if data == nil || data.Hidden() {
			continue
		}
		appData := data // capture for goroutine below
		app := widget.NewButtonWithIcon(appData.Name(), wmTheme.BrokenImageIcon, func() {
			l.callback(appData, -1)
			l.close()
		})
		app.Alignment = widget.ButtonAlignLeading

		appList = append(appList, app)
		iconList = append(iconList, data)

		for id, action := range appData.Actions() {
			actionID := id // capture for goroutine below
			app := widget.NewButtonWithIcon(data.Name()+" : "+action.Name(), wmTheme.BrokenImageIcon, func() {
				l.callback(appData, actionID)
				l.close()
			})
			app.Alignment = widget.ButtonAlignLeading

			appList = append(appList, app)
			iconList = append(iconList, data)
		}
	}
	runAsync(func() { l.loadIcons(iconList, appList) })

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
	var suggestList, searchList, aiList []fyne.CanvasObject

	for _, m := range l.desk.Modules() {
		suggest, ok := m.(tyde.LaunchSuggestionModule)
		if !ok {
			continue
		}

		name := m.Metadata().Name
		for _, item := range suggest.LaunchSuggestions(input) {
			launchData := item // capture for goroutine below
			button := widget.NewButtonWithIcon(item.Title(), item.Icon(), func() {
				l.close()
				launchData.Launch()
			})

			switch {
			case name == ai.ModuleName:
				aiList = append(aiList, button) // AI assistant sits after web search
			case strings.Contains(strings.ToLower(name), "search"):
				searchList = append(searchList, button)
			default:
				suggestList = append(suggestList, button)
			}
		}
	}

	if len(suggestList) == 0 && len(searchList) == 0 && len(aiList) == 0 {
		return nil
	}
	return append(append(suggestList, searchList...), aiList...)
}

func newAppPicker(callback func(appie.AppData, int)) *picker {
	appList := container.NewVBox()
	appScroller := container.NewScroll(appList)
	l := &picker{desk: tyde.Instance(), appList: appList, appScroll: appScroller, callback: callback}

	entry := &appEntry{pick: l}
	entry.ExtendBaseWidget(entry)
	entry.SetPlaceHolder(locale.T("launcher.app"))
	entry.OnChanged = func(input string) {
		if l.debounceTimer != nil {
			l.debounceTimer.Stop()
		}
		if input == "" {
			appList.Objects = nil
			l.appList.Refresh()
			l.updateBgSize()
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

	return l
}

func (l *picker) show() {
	// In a Wayland session the desktop is a panel below application windows,
	// so the launcher is a window of its own that the compositor places on top.
	if wlipc.IsWaylandSession() {
		l.showWindow()
		return
	}

	r, g, b, _ := theme.Color(theme.ColorNameOverlayBackground).RGBA()
	bgCol := &color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 230}
	l.bg = canvas.NewRectangle(bgCol)
	th := l.entry.Theme()
	pad := theme.Padding()
	l.bg.CornerRadius = th.Size(theme.SizeNameInputRadius) + pad
	l.bg.Shadow = wmTheme.WindowShadow(true)

	inner := container.NewBorder(l.entry, nil, nil, nil, l.appScroll)
	content := container.NewStack(container.NewWithoutLayout(l.bg), container.NewPadded(inner))

	d := l.desk.(*desktop)
	primary := d.Screens().Primary()
	scale := primary.CanvasScale()
	midX := float32(primary.Width/2) / scale
	midY := float32(primary.Height/2) / scale

	entryHeight := l.entry.MinSize().Height
	l.fullSize = fyne.NewSize(launcherWidth,
		entryHeight*float32(launcherMaxResults+1)+pad*float32(launcherMaxResults+4)-3)
	pos := fyne.NewPos(midX-l.fullSize.Width/2, midY-l.fullSize.Height/2)

	// Start bg at entry-only height
	l.bg.Resize(fyne.NewSize(l.fullSize.Width, entryHeight+pad*2))

	l.overlay = d.showOverlayWithBackdrop(content, l.fullSize, l.fullSize, pos, l.entry, fyne.Position{})
}

// showWindow shows the picker in a window of its own.
func (l *picker) showWindow() {
	title := l.title
	if title == "" {
		title = locale.T("launcher.app")
	}
	title += " " + SkipTaskbarHint

	var win fyne.Window
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		win = d.CreateSplashWindow()
		win.SetPadded(true)
		win.SetTitle(title)
	} else {
		win = fyne.CurrentApp().NewWindow(title)
	}
	l.win = win
	win.SetOnClosed(func() {
		if l.onClosed != nil {
			l.onClosed()
		}
	})
	win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			win.Close()
		}
	})

	clearBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		l.entry.SetText("")
		l.appList.Objects = nil
		l.appList.Refresh()
	})
	searchRow := container.NewBorder(nil, nil, nil, clearBtn, l.entry)

	cancel := widget.NewButtonWithIcon(locale.T("launcher.cancel"), theme.CancelIcon(), func() {
		win.Close()
	})

	ideal := fyne.NewSize(300,
		cancel.MinSize().Height*4+theme.Padding()*6+l.entry.MinSize().Height)
	win.SetContent(container.NewBorder(searchRow, cancel, nil, nil, l.appScroll))
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

	// CenterOnScreen as Fyne-level fallback if IPC fails
	win.CenterOnScreen()
	win.Show()
	win.Canvas().Focus(l.entry)

	go ensureFocused(win, l.entry)
}

// ShowAppLauncherAt opens the launcher, positioning it on the screen that
// contains the given cursor coordinates (layout-space pixels). If cx and cy
// are both 0 it falls back to the primary display.
func ShowAppLauncherAt(cx, cy float32) {
	launcherCursorX = cx
	launcherCursorY = cy
	ShowAppLauncher()
}

var launcherCursorX, launcherCursorY float32

// ShowAppLauncher opens a new application launcher, closing an old one if it existed.
func ShowAppLauncher() {
	if appExec != nil {
		appExec.close()
		return
	}

	appExec = newAppPicker(func(app appie.AppData, actionID int) {
		var err error
		if actionID == -1 {
			err = tyde.Instance().RunApp(app)
		} else {
			err = tyde.Instance().RunAppAction(app, actionID)
		}
		if err != nil {
			fyne.LogError("Failed to start app", err)
			wm.SendNotification(wm.NewNotification(locale.T("launcher.failed"),
				locale.T("launcher.startFailed")+" "+app.Name()))
			return
		}
	})
	appExec.title = "Application Launcher"
	appExec.debounceDuration = 150 * time.Millisecond
	appExec.showMods = true
	appExec.onClosed = func() {
		appExec = nil
	}
	appExec.show()
}
