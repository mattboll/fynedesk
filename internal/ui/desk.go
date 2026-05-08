// Package ui implements the FyneDesk desktop user interface components including the panel bar, launcher, settings dialogs, and background rendering.
package ui

import (
	"math"
	"os/exec"
	"strconv"
	"sync"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/notify"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wm"
)

const (
	// RootWindowName is the base string that all root windows will have in their title and is used to identify root windows.
	RootWindowName = "Fyne Desktop"
	// SkipTaskbarHint should be added to the title of normal windows that should be skipped like the X11 SkipTaskbar hint.
	SkipTaskbarHint = "FyneDesk:skip"
	// NoFocusHint prevents the compositor from stealing keyboard focus when the window maps.
	NoFocusHint = "FyneDesk:nofocus"
)

type desktop struct {
	wm.ShortcutHandler
	app      fyne.App
	wm       fynedesk.WindowManager
	icons    appie.Provider
	recent   []appie.AppData
	screens  fynedesk.ScreenList
	settings fynedesk.DeskSettings

	run         func()
	showMenu    func(*fyne.Menu, fyne.Position)
	moduleCache []fynedesk.Module

	bar        *bar
	widgets    *widgetPanel
	mouse      fyne.CanvasObject
	root       fyne.Window
	desk       int
	background *background // stored for direct settings updates

	// settingsListenersOnce guards addSettingsChangeListener — Fyne provides
	// no RemoveListener, so duplicate adds would leak listener entries that
	// fire on every settings change forever.
	settingsListenersOnce sync.Once
}

// setScreenAreaVisible shows or hides screen area modules (desktop icons).
// Called when the panel is raised/lowered via hotspot to prevent desktop
// icons from appearing above windows.
func (l *desktop) setScreenAreaVisible(visible bool) {
	if l.background != nil {
		l.background.setScreenAreaVisible(visible)
	}
}

func (l *desktop) Desktop() int {
	return l.desk
}

func (l *desktop) SetDesktop(id int) {
	diff := id - l.desk
	l.desk = id

	_, height := l.RootSizePixels()
	offPix := float32(diff * -int(height))
	wins := l.wm.Windows()

	starts := make([]fyne.Position, len(wins))
	deltas := make([]fyne.Delta, len(wins))
	for i, win := range wins {
		starts[i] = win.Position()

		display := l.Screens().ScreenForWindow(win)
		off := offPix / display.Scale
		deltas[i] = fyne.NewDelta(0, off)
	}

	if l.Settings().ReduceMotion() {
		for i, item := range l.wm.Windows() {
			if item.Pinned() {
				continue
			}
			item.Move(fyne.NewPos(starts[i].X+deltas[i].DX, starts[i].Y+deltas[i].DY))
		}
	} else {
		fyne.NewAnimation(canvas.DurationStandard, func(f float32) {
			for i, item := range l.wm.Windows() {
				if item.Pinned() {
					continue
				}
				newX := starts[i].X + deltas[i].DX*f
				newY := starts[i].Y + deltas[i].DY*f
				item.Move(fyne.NewPos(newX, newY))
			}
		}).Start()
	}

	for _, m := range l.Modules() {
		if desk, ok := m.(notify.DesktopNotify); ok {
			desk.DesktopChangeNotify(id)
		}
	}
}

func (l *desktop) ShowSettings() {
	l.widgets.showSettings()
}

func (l *desktop) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	bg := objects[0].(*background)
	bg.Resize(size)

	pos := l.Settings().BarPosition()
	switch pos {
	case "left":
		l.bar.Resize(fyne.NewSize(wmtheme.NarrowBarWidth, size.Height))
		l.bar.Move(fyne.NewPos(0, 0))
	default: // "bottom" or unset
		// Use the zoom-scaled height so Fyne doesn't clip zoomed icons that
		// extend above the base bar area. Icons are bottom-aligned within
		// this container; the zoom effect grows upward into the extra space.
		barHeight := float32(l.Settings().LauncherIconSize())*float32(l.Settings().LauncherZoomScale()) + 2
		l.bar.Resize(fyne.NewSize(size.Width, barHeight+1))
		barY := size.Height - barHeight
		l.bar.Move(fyne.NewPos(0, barY))
	}
	l.bar.Refresh()

	widgetsWidth := l.widgets.MinSize().Width
	l.widgets.Resize(fyne.NewSize(widgetsWidth, size.Height))
	l.widgets.Move(fyne.NewPos(size.Width-widgetsWidth, 0))
	l.widgets.Refresh()

}

func (l *desktop) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(640, 480) // tiny - window manager will scale up to screen size
}

func (l *desktop) Root() fyne.Window {
	return l.root
}

func (l *desktop) ShowMenuAt(menu *fyne.Menu, pos fyne.Position) {
	l.showMenu(menu, pos)
}

func (l *desktop) updateBackgrounds(path, bgType string) {
	if l.background != nil {
		l.background.updateBackground(path, bgType)
	}
}

func (l *desktop) createPrimaryContent() fyne.CanvasObject {
	l.bar = newBar(l)
	l.widgets = newWidgetPanel(l)
	l.mouse = newMouse()
	l.mouse.Hide()
	l.background = newBackground()

	return container.New(l, l.background, l.bar, l.widgets, l.mouse)
}

func (l *desktop) createRoot(screens fynedesk.ScreenList) fyne.Window {
	win := l.newDesktopWindowFull()

	win.SetContent(l.createPrimaryContent())

	return win
}

func (l *desktop) setupRoot() {
	if l.root == nil {
		l.root = l.createRoot(l.screens)
	}

	scale := l.screens.Primary().CanvasScale()
	l.root.Resize(fyne.NewSize(float32(l.screens.Primary().Width)/scale, float32(l.screens.Primary().Height)/scale))
}

func (l *desktop) RecentApps() []appie.AppData {
	return l.recent
}

func (l *desktop) Run() {
	go l.wm.Run()
	go l.watchScreenActivity()
	l.run() // use the configured run method
}

func (l *desktop) RunApp(app appie.AppData) error {
	return l.runExec(app, app.Run)
}

func (l *desktop) RunAppAction(app appie.AppData, id int) error {
	if app.Actions() == nil || len(app.Actions())-1 < id {
		return nil
	}

	return l.runExec(app, app.Actions()[id].Run)
}

func (l *desktop) runExec(app appie.AppData, runner func(env []string) error) error {
	vars := l.scaleVars(l.Screens().Active().CanvasScale())
	err := runner(vars)

	if err == nil {
		l.recent = append([]appie.AppData{app}, l.recent...)
		// remove if it was already on the list
		for i := 1; i < len(l.recent); i++ {
			if l.recent[i] == app {
				if i == len(l.recent)-1 {
					l.recent = l.recent[:i]
				} else {
					l.recent = append(l.recent[:i], l.recent[i+1:]...)
				}
				break
			}
		}
		// limit to 5 items
		if len(l.recent) > 5 {
			l.recent = l.recent[:5]
		}
		l.settings.(*deskSettings).saveRecents()
	}
	return err
}

func (l *desktop) Settings() fynedesk.DeskSettings {
	return l.settings
}

func (l *desktop) ContentBoundsPixels(screen *fynedesk.Screen) (x, y, w, h uint32) {
	screenW := uint32(screen.Width)
	screenH := uint32(screen.Height)
	pad := wmtheme.WidgetPanelWidth
	if l.Settings().NarrowWidgetPanel() {
		pad = wmtheme.NarrowBarWidth
	}
	if l.screens.Primary() == screen {
		wid := uint32(pad * screen.CanvasScale())
		pos := l.Settings().BarPosition()
		switch pos {
		case "left":
			bar := uint32(wmtheme.NarrowBarWidth * screen.CanvasScale())
			return bar, 0, screenW - bar - wid, screenH
		default: // "bottom"
			barH := uint32(wmtheme.NarrowBarWidth * screen.CanvasScale()) // approximate bar height
			return 0, 0, screenW - wid, screenH - barH
		}
	}
	return 0, 0, screenW, screenH
}

func (l *desktop) RootSizePixels() (w, h uint32) {
	for _, screen := range l.Screens().Screens() {
		right := uint32(screen.X + screen.Width)
		bottom := uint32(screen.Y + screen.Height)

		if right > w {
			w = right
		}
		if bottom > h {
			h = bottom
		}
	}

	return w, h
}

func (l *desktop) IconProvider() appie.Provider {
	return l.icons
}

func (l *desktop) WindowManager() fynedesk.WindowManager {
	return l.wm
}

func (l *desktop) clearModuleCache() {
	for _, mod := range l.moduleCache {
		mod.Destroy()
	}

	l.moduleCache = nil
}

func (l *desktop) Modules() []fynedesk.Module {
	if l.moduleCache != nil {
		return l.moduleCache
	}

	var mods []fynedesk.Module
	for _, meta := range fynedesk.AvailableModules() {
		if !isModuleEnabled(meta.Name, l.settings) {
			continue
		}

		instance := meta.NewInstance()
		mods = append(mods, instance)

		if bind, ok := instance.(fynedesk.KeyBindModule); ok {
			for sh, f := range bind.Shortcuts() {
				l.AddShortcut(sh, f)
			}
		}
	}

	l.moduleCache = mods
	return mods
}

func (l *desktop) qtScreenScales() string {
	screenScales := ""
	for i, screen := range l.Screens().Screens() {
		if i > 0 {
			screenScales += ";"
		}
		// Qt toolkit cannot handle scale < 1
		positiveScale := math.Max(1.0, float64(screen.CanvasScale()))
		screenScales += screen.Name + "=" + strconv.FormatFloat(positiveScale, 'f', 1, 32)
	}
	return screenScales
}

func (l *desktop) scaleVars(scale float32) []string {
	intScale := int(math.Round(float64(scale)))

	return []string{
		"QT_SCREEN_SCALE_FACTORS=" + l.qtScreenScales(),
		"GDK_SCALE=" + strconv.Itoa(intScale),
		"ELM_SCALE=" + strconv.FormatFloat(float64(scale), 'f', 1, 32),
	}
}

// MouseInNotify can be called by the window manager to alert the desktop that the cursor has entered the canvas
func (l *desktop) MouseInNotify(pos fyne.Position) {
	if l.bar == nil {
		return
	}

	fyne.Do(func() {
		mouseX, mouseY := pos.X, pos.Y
		barX, barY := l.bar.Position().X, l.bar.Position().Y
		barWidth, barHeight := l.bar.Size().Width, l.bar.Size().Height
		if mouseX >= barX && mouseX <= barX+barWidth {
			if mouseY >= barY && mouseY <= barY+barHeight {
				l.bar.MouseIn(&deskDriver.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: pos, Position: pos}})
			}
		}
	})
}

// MouseOutNotify can be called by the window manager to alert the desktop that the cursor has left the canvas
func (l *desktop) MouseOutNotify() {
	if l.bar == nil {
		return
	}
	fyne.Do(l.bar.MouseOut)
}

func (l *desktop) fireSettingsChangeListener(s fynedesk.DeskSettings) {
	locale.SetLanguage(s.Language())
	l.clearModuleCache()
	bgType := fyne.CurrentApp().Preferences().String("background_type")
	l.updateBackgrounds(s.Background(), bgType)
	l.widgets.reloadModules(l.Modules())

	// Update locale-dependent labels
	if np, ok := l.widgets.notifications.(*notificationPanel); ok {
		np.updateLocale()
	}

	l.bar.iconSize = l.Settings().LauncherIconSize()
	l.bar.iconScale = l.Settings().LauncherZoomScale()
	l.bar.disableZoom = l.Settings().LauncherDisableZoom()
	l.bar.updateIcons()
	l.bar.updateIconOrder()
	l.bar.updateTaskbar()
}

func (l *desktop) addSettingsChangeListener() {
	l.settingsListenersOnce.Do(func() {
		l.Settings().AddChangeListener(l.fireSettingsChangeListener)

		l.app.Settings().AddListener(func(_ fyne.Settings) {
			bgType := fyne.CurrentApp().Preferences().String("background_type")
			l.updateBackgrounds(l.Settings().Background(), bgType)
		})
	})
}

func (l *desktop) registerShortcuts() {
	l.AddShortcut(fynedesk.NewShortcut("Show Launcher", fyne.KeySpace, fynedesk.UserModifier),
		ShowAppLauncher)
	l.AddShortcut(fynedesk.NewShortcut("Switch App Next", fyne.KeyTab, fynedesk.UserModifier),
		func() {
			// dummy - the wm handles app switcher
		})
	l.AddShortcut(fynedesk.NewShortcut("Switch App Previous", fyne.KeyTab, fynedesk.UserModifier|fyne.KeyModifierShift),
		func() {
			// dummy - the wm handles app switcher
		})
	l.AddShortcut(fynedesk.NewShortcut("Iconify Window", fyne.KeyF9, fynedesk.UserModifier),
		l.iconifyCurrentWindow)
	l.AddShortcut(fynedesk.NewShortcut("Maximize Window", fyne.KeyF10, fynedesk.UserModifier),
		l.maximizeCurrentWindow)
	l.AddShortcut(fynedesk.NewShortcut("FullScreen Window", fyne.KeyF11, fynedesk.UserModifier),
		l.fullscreenCurrentWindow)
	l.AddShortcut(fynedesk.NewShortcut("Print Window", deskDriver.KeyPrintScreen, fyne.KeyModifierShift),
		l.screenshotWindow)
	l.AddShortcut(fynedesk.NewShortcut("Print Screen", deskDriver.KeyPrintScreen, 0),
		l.screenshot)
	l.AddShortcut(fynedesk.NewShortcut("Calculator", fynedesk.KeyCalculator, 0),
		l.calculator)
	l.AddShortcut(fynedesk.NewShortcut("Lock screen", fyne.KeyL, fynedesk.UserModifier),
		func() {
			l.TriggerScreenSaver(false)
		})
}

// Screens returns the screens provider of the current desktop environment for access to screen functionality.
func (l *desktop) Screens() fynedesk.ScreenList {
	return l.screens
}

// NewDesktop creates a new desktop in fullscreen for main usage.
// The WindowManager passed in will be used to manage the screen it is loaded on.
// An ApplicationProvider is used to lookup application icons from the operating system.
func NewDesktop(app fyne.App, mgr fynedesk.WindowManager, icons appie.Provider, screenProvider fynedesk.ScreenList) fynedesk.Desktop {
	desk := newDesktop(app, mgr, icons)
	desk.run = desk.runFull
	screenProvider.AddChangeListener(desk.setupRoot)
	desk.screens = screenProvider

	desk.setupRoot()
	wm.StartAuthAgent()
	if desk.Settings().ScreenSaverType() == "XScreensaver" {
		go desk.startXscreensaver()
	}
	return desk
}

// NewEmbeddedDesktop creates a new windowed desktop for test purposes.
// An ApplicationProvider is used to lookup application icons from the operating system.
// If run during CI for testing it will return an in-memory window using the
// fyne/test package.
func NewEmbeddedDesktop(app fyne.App, icons appie.Provider) fynedesk.Desktop {
	return newEmbeddedDesktop(app, icons, "Embedded "+RootWindowName)
}

// NewPanelDesktop creates an embedded desktop for use as a Wayland panel.
// The window title is set to "FyneDesk:Panel" so the compositor can identify it.
func NewPanelDesktop(app fyne.App, icons appie.Provider) fynedesk.Desktop {
	return newEmbeddedDesktop(app, icons, "FyneDesk:Panel")
}

func newEmbeddedDesktop(app fyne.App, icons appie.Provider, title string) fynedesk.Desktop {
	wm := &embededWM{}
	desk := newDesktop(app, wm, icons)
	desk.run = desk.runEmbed
	desk.showMenu = desk.showMenuEmbed

	desk.root = desk.newDesktopWindowEmbedWithTitle(title)
	over := wm.setWindow(desk.root)
	desk.root.SetContent(container.NewStack(desk.createPrimaryContent(), over))
	return desk
}

// SetScreenSize sets the pixel screen dimensions for the Wayland panel.
// This updates the embedded WM and screen provider with the actual screen size,
// so overlay positions can be correctly mapped to pixel coordinates.
func SetScreenSize(desk fynedesk.Desktop, w, h int) {
	if d, ok := desk.(*desktop); ok {
		if wm, ok := d.wm.(*embededWM); ok {
			wm.screenW = w
			wm.screenH = h
		}
		// Also update the embedded screen provider
		if esp, ok := d.screens.(*embeddedScreensProvider); ok {
			esp.screens[0].Width = w
			esp.screens[0].Height = h
		}
	}
}

// SetScreenPosition sets the primary output's layout position for the Wayland panel.
// When the primary output is not at (0,0) (e.g. multi-monitor), overlay positions
// must be offset by these coordinates so they land on the correct output.
func SetScreenPosition(desk fynedesk.Desktop, x, y int) {
	if d, ok := desk.(*desktop); ok {
		if esp, ok := d.screens.(*embeddedScreensProvider); ok {
			esp.screens[0].X = x
			esp.screens[0].Y = y
		}
	}
}

func newDesktop(app fyne.App, wm fynedesk.WindowManager, icons appie.Provider) *desktop {
	desk := &desktop{app: app, wm: wm, icons: icons, screens: newEmbeddedScreensProvider()}
	desk.showMenu = desk.showMenuFull

	fynedesk.SetInstance(desk)
	desk.settings = newDeskSettings()
	locale.SetLanguage(desk.settings.Language())
	desk.addSettingsChangeListener()

	// Load theme.json so custom colors (primary, background, etc.) apply at startup
	reloadFyneTheme()

	// Sync Fyne primary color changes to theme.json so the JSON theme
	// reflects the user's "Main Color" selection from Fyne Settings.
	watchFynePrimaryColor(app)

	// Watch for accent color extracted from wallpaper by the compositor
	accentDone := make(chan struct{})
	_ = accentDone // lives for process lifetime
	watchAccentColor(accentDone)

	desk.registerShortcuts()
	return desk
}

func (l *desktop) calculator() {
	err := exec.Command("calculator").Start()
	if err != nil {
		fyne.LogError("Failed to open calculator", err)
	}
}

func (l *desktop) fullscreenCurrentWindow() {
	if len(l.WindowManager().Windows()) == 0 {
		return
	}

	w := l.WindowManager().Windows()[0]
	if w.Fullscreened() {
		w.Unfullscreen()
	} else {
		w.Fullscreen()
	}
}

func (l *desktop) iconifyCurrentWindow() {
	if len(l.WindowManager().Windows()) == 0 {
		return
	}

	w := l.WindowManager().Windows()[0]
	w.Iconify()
}

func (l *desktop) maximizeCurrentWindow() {
	if len(l.WindowManager().Windows()) == 0 {
		return
	}

	w := l.WindowManager().Windows()[0]
	if w.Maximized() {
		w.Unmaximize()
	} else {
		w.Maximize()
	}
}

//func (l *desktop) runCommand() {
//	w := l.app.NewWindow("Run Command")
//	input := widget.NewEntry()
//	// TODO add history etc...
//	run := widget.NewButton("Run", func() {
//
//	})
//	run.Importance = widget.HighImportance
//
//	w.SetContent(container.NewVBox(widget.NewLabel("Enter command to run:"),
//		container.NewBorder(nil, nil, nil, run, input)))
//	w.Resize(fyne.NewSize(250, 40))
//	w.Show()
//}
