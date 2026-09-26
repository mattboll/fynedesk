// Package ui implements the Tyde desktop user interface components including the panel bar, launcher, settings dialogs, and background rendering.
package ui

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"sync"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/notify"
	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wm"
)

const (
	// RootWindowName is the base string that all root windows will have in their title and is used to identify root windows.
	RootWindowName = "Tyde Desktop"
	// SkipTaskbarHint should be added to the title of normal windows that should be skipped like the X11 SkipTaskbar hint.
	SkipTaskbarHint = "Tyde:skip"
	// NoFocusHint prevents the compositor from stealing keyboard focus when the window maps.
	NoFocusHint = "Tyde:nofocus"
)

// screenWindow holds the Fyne window and per-screen widgets for a single monitor.
type screenWindow struct {
	screen            *tyde.Screen
	win               fyne.Window
	compositor        *CompositorWidget
	compositorOverlay *CompositorWidget
	bg                *background
	overlay           *fyne.Container

	// deskShaderBG is a black rectangle drawn directly behind deskShader so the
	// transparent area around the cube reads as empty rather than letting the
	// live desktop show through. Shown and hidden together with deskShader.
	deskShaderBG *canvas.Rectangle
	// deskShader is a full-window overlay that plays the 3D cube transition
	// between virtual desktops. It is hidden except while a switch is animating.
	deskShader *canvas.Shader
	// deskSnapshots caches the last seen frame of each virtual desktop, keyed by
	// desktop id, so a switch can show the target desktop on the rolling face.
	// Only genuine captures are stored here, never derived images, so an entry
	// always faithfully represents that desktop.
	deskSnapshots map[int]image.Image

	// overviewShader is a full-window overlay that plays the "reveal all" desktop
	// overview zoom. Hidden except while the overview is opening, shown or closing.
	overviewShader *canvas.Shader
}

// ScreenCompositors groups the compositor widgets for a single screen,
// passed to the platform compositor so it can route windows per-monitor.
type ScreenCompositors struct {
	Screen  *tyde.Screen
	Normal  *CompositorWidget
	Overlay *CompositorWidget
}

// CompositorScreensChanged is registered by the running compositor so the
// desktop can hand it an updated per-screen widget list when screens are
// added, removed or resized at runtime. It is called on the Fyne main
// goroutine and is nil when no compositor is running.
var CompositorScreensChanged func([]ScreenCompositors)

// CompositorWindowSnapshot is registered by the running compositor so the
// desktop can build the cube's rolling face for a desktop it has never captured
// live. It renders that desktop's windows — the one offsetY pixels from the
// current viewport, matching SetDesktop's slide — into a transparent RGBA image
// for the given screen. It is called on the Fyne main goroutine and is nil when
// no compositor is running.
var CompositorWindowSnapshot func(screen *tyde.Screen, offsetY int) image.Image

type desktop struct {
	// What the last settings change built the modules, wallpaper and dock
	// from (see fireSettingsChangeListener).
	lastModulesKey, lastBackgroundKey, lastBarKey string
	wm.ShortcutHandler
	app      fyne.App
	wm       tyde.WindowManager
	icons    appie.Provider
	recent   []appie.AppData
	screens  tyde.ScreenList
	settings tyde.DeskSettings

	run         func()
	showMenu    func(*fyne.Menu, fyne.Position)
	moduleCache []tyde.Module

	bar             *bar
	widgets         *widgetPanel
	mouse           fyne.CanvasObject
	overlayLayer    *overlayLayer   // above-windows layer for OverlayAreaModule widgets (primary screen)
	accessoryLayer  *fyne.Container // embedded-mode home for WindowAccessoryModule items (no compositor to host them)
	screenWindows   []*screenWindow
	primaryWin      *screenWindow
	running         bool // true once the run loop has started and windows can be shown directly
	desk            int
	deskAnim        *fyne.Animation
	deskAnimTargets map[tyde.Window]fyne.Position // where the in-flight animation is heading
	deskCubeAnim    *fyne.Animation               // drives the 3D cube transition overlay
	overview        *deskOverview                 // the "reveal all" overview, when on screen
	compositorDone  chan struct{}

	// overlayShapes maps each shown overlay to the screen-pixel rectangle it occupies,
	// so frame input shapes can be made transparent only under the overlay content.
	overlayShapes map[fyne.CanvasObject]image.Rectangle
	// canvasOverlay records whether the root window canvas currently has a Fyne
	// overlay (a dialog or pop-up) shown on it - see canvasOverlaysChanged.
	canvasOverlay bool
	// root is the input-aware wrapper handed out by Root().
	root *rootWindow

	// welcomeDone guards the first-run welcome splash so it is only ever triggered
	// once per session, from the first primary-window layout with a real size.
	welcomeDone bool

	// activityLayer, in embedded mode only, watches for mouse movement to defer the
	// screen saver.
	activityLayer fyne.CanvasObject

	// settingsListenersOnce guards addSettingsChangeListener — Fyne provides
	// no RemoveListener, so duplicate adds would leak listener entries that
	// fire on every settings change forever.
	settingsListenersOnce sync.Once
}

// setScreenAreaVisible shows or hides screen area modules (desktop icons).
// Called when the panel is raised/lowered via hotspot to prevent desktop
// icons from appearing above windows.
func (l *desktop) setScreenAreaVisible(visible bool) {
	if l.primaryWin != nil && l.primaryWin.bg != nil {
		l.primaryWin.bg.setScreenAreaVisible(visible)
	}
}

func (l *desktop) Desktop() int {
	return l.desk
}

func (l *desktop) SetDesktop(id int) {
	l.setDesktop(id, true)
}

// setDesktop switches to desktop id, sliding the windows into place. When cube is
// true the 3D cube transition is rolled over the top to mask the slide; the desktop
// overview passes false because its own zoom-in already masks the slide.
func (l *desktop) setDesktop(id int, cube bool) {
	old := l.desk
	if id != old && cube && !l.Settings().ReduceMotion() {
		// Roll the 3D cube over the top while the windows below slide into place.
		l.startDeskCube(old, id)
	}

	diff := id - l.desk
	prevTargets := l.deskAnimTargets

	// Stop any in-flight animation; the new one takes over.
	if l.deskAnim != nil {
		l.deskAnim.Stop()
		l.deskAnim = nil
	}

	l.desk = id

	_, height := l.RootSizePixels()
	offPix := float32(diff * -int(height))
	wins := l.wm.Windows()

	starts := make([]fyne.Position, len(wins))
	targets := make(map[tyde.Window]fyne.Position, len(wins))
	for i, win := range wins {
		// If the previous animation was heading somewhere, start from
		// that target rather than the current (mid-flight) position.
		if prev, ok := prevTargets[win]; ok {
			starts[i] = prev
		} else {
			starts[i] = win.Position()
		}

		display := l.Screens().ScreenForWindow(win)
		off := offPix / display.CanvasScale()
		targets[win] = fyne.NewPos(starts[i].X, starts[i].Y+off)
	}

	type visualMover interface {
		MoveVisual(fyne.Position)
	}

	l.deskAnimTargets = targets
	var a *fyne.Animation
	a = fyne.NewAnimation(canvas.DurationStandard, func(f float32) {
		if l.deskAnim != a {
			return // superseded by a newer animation
		}
		for i, item := range wins {
			if item.Pinned() {
				continue
			}

			target := targets[item]
			newX := starts[i].X + (target.X-starts[i].X)*f
			newY := starts[i].Y + (target.Y-starts[i].Y)*f
			pos := fyne.NewPos(newX, newY)

			if f >= 1.0 {
				item.Move(pos)
			} else if vm, ok := item.(visualMover); ok {
				vm.MoveVisual(pos)
			} else {
				item.Move(pos)
			}
		}

		// Completion runs once the slide finishes, independent of the window loop:
		// it must fire even with no windows, or when the last window is pinned and
		// skipped above, so modules (e.g. the pager) always learn of the switch.
		if f >= 1.0 {
			l.deskAnim = nil
			l.deskAnimTargets = nil
			for _, m := range l.Modules() {
				if desk, ok := m.(notify.DesktopNotify); ok {
					desk.DesktopChangeNotify(id)
				}
			}
			l.raiseTopWindow(id)
		}
	})
	l.deskAnim = a
	if l.Settings().ReduceMotion() {
		a.Tick(1) // jump straight to the end of the slide
		return
	}
	a.Start()
}

// raiseTopWindow raises and focuses the topmost window that is visible on the
// given desktop. Pinned windows count as visible on every desktop.
func (l *desktop) raiseTopWindow(id int) {
	for _, win := range l.wm.Windows() {
		if win.Iconic() {
			continue
		}
		if win.Desktop() != id && !win.Pinned() {
			continue
		}

		win.RaiseToTop()
		win.Focus()
		return
	}
}

func (l *desktop) ShowSettings(panel string) {
	l.widgets.showSettings(panel)
}

func (l *desktop) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if esp, ok := l.screens.(*embeddedScreensProvider); ok {
		esp.UpdatePrimarySize(int(size.Width), int(size.Height))
	}

	// Each window covers exactly one screen, so origin is always 0,0.
	pW := size.Width
	pH := size.Height

	// objects order: background, [compositor], bar, widgets, [compositorOverlay], overlay, mouse
	// Size all full-window layers (everything except bar and widgets) to fill.
	for _, o := range objects {
		if o == l.bar || o == l.widgets || o == l.mouse {
			continue
		}
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}

	switch l.Settings().BarPosition() {
	case "left":
		l.bar.Resize(fyne.NewSize(wmtheme.NarrowBarWidth, pH))
		l.bar.Move(fyne.NewPos(0, 0))
	default: // "bottom"
		// Use the zoom-scaled height so Fyne doesn't clip zoomed icons that
		// extend above the base bar area. Icons are bottom-aligned within
		// this container; the zoom effect grows upward into the extra space.
		barHeight := float32(l.Settings().LauncherIconSize())*float32(l.Settings().LauncherZoomScale()) + 2
		l.bar.Resize(fyne.NewSize(pW, barHeight+1))
		l.bar.Move(fyne.NewPos(0, pH-barHeight))
	}
	l.bar.Refresh()

	widgetsWidth := l.widgets.MinSize().Width
	l.widgets.Resize(fyne.NewSize(widgetsWidth, pH))
	l.widgets.Move(fyne.NewPos(pW-widgetsWidth, 0))
	l.widgets.Refresh()

	// On the very first boot, once the primary window has a real (full-screen)
	// size, present the welcome splash.
	if !l.welcomeDone && shouldShowWelcome() && l.primaryWin != nil &&
		size.Width >= welcomeWidth && size.Height >= welcomeHeight {
		l.welcomeDone = true
		fyne.Do(l.ShowWelcome)
	}
}

func (l *desktop) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(640, 480) // tiny - window manager will scale up to screen size
}

// Root returns the primary desktop window, wrapped so that any Fyne overlay shown
// on it (dialogs and pop-ups) is registered with the desktop - see rootWindow.
func (l *desktop) Root() fyne.Window {
	if l.primaryWin == nil || l.primaryWin.win == nil {
		return nil
	}

	// The primary window changes when the screen layout does, so re-wrap if needed.
	if l.root == nil || l.root.Window != l.primaryWin.win {
		l.root = newRootWindow(l.primaryWin.win, l)
	}
	return l.root
}

// canvasOverlaysChanged reacts to a Fyne overlay being added to or removed from the
// root window's canvas.
func (l *desktop) canvasOverlaysChanged(added bool) {
	if l.primaryWin == nil || l.primaryWin.win == nil {
		return
	}

	c := l.primaryWin.win.Canvas()
	l.canvasOverlay = len(c.Overlays().List()) > 0
	l.applyOverlayShapes()

	if added && l.canvasOverlay && c.Focused() == nil {
		c.FocusNext()
	}
}

func (l *desktop) ShowMenuAt(menu *fyne.Menu, pos fyne.Position) {
	l.showMenu(menu, pos)
}

func (b *backdrop) MouseMoved(*deskDriver.MouseEvent) {}
func (b *backdrop) MouseOut()                         {}

func (h *hoverCatch) MouseMoved(*deskDriver.MouseEvent) {}
func (h *hoverCatch) MouseOut()                         {}

func (l *desktop) updateBackgrounds(path, bgType string) {
	for _, sw := range l.screenWindows {
		if sw.bg != nil {
			sw.bg.updateBackground(path, bgType)
		}
	}
}

func (l *desktop) createPrimaryContent(sw *screenWindow) fyne.CanvasObject {
	l.bar = newBar(l)
	l.widgets = newWidgetPanel(l)
	l.mouse = newMouse()
	l.mouse.Hide()

	sw.bg = newBackground()

	// Order: background -> compositor -> overlay modules -> bar -> widgets -> compositor overlay -> UI overlay -> mouse
	objects := []fyne.CanvasObject{sw.bg}

	// Embedded mode's screen-saver activity monitor sits just above the background.
	if l.activityLayer != nil {
		objects = append(objects, l.activityLayer)
	}

	// Normal compositor for regular windows below desktop chrome
	if sw.compositor != nil {
		objects = append(objects, sw.compositor)
	} else if l.accessoryLayer != nil {
		// Embedded mode has no compositor to host window accessories, so add here.
		objects = append(objects, l.accessoryLayer)
	}

	// Overlay-area modules (e.g. desktop pets) draw above regular windows but
	// below the bar, widget panel, fullscreen windows, menus and the cursor.
	l.overlayLayer = newOverlayLayer(l)
	objects = append(objects, l.overlayLayer)

	objects = append(objects, l.bar, l.widgets)

	// Compositor overlay for fullscreen windows above desktop chrome
	if sw.compositorOverlay != nil {
		objects = append(objects, sw.compositorOverlay)
	}

	// Desktop overview zoom, below the UI overlay so the interactive selection
	// layer (added to sw.overlay) sits above it, but above the bar and widgets so
	// it hides the chrome while playing.
	sw.overviewShader = newOverviewShader()
	objects = append(objects, sw.overviewShader)

	// UI overlay for menus, dialogs, switcher, notifications
	sw.overlay = container.NewWithoutLayout()
	objects = append(objects, sw.overlay, l.mouse)

	// Desktop transition cube, topmost so it covers the whole screen while playing.
	sw.deskShaderBG = newDeskShaderBG()
	sw.deskShader = newDeskShader()
	objects = append(objects, sw.deskShaderBG, sw.deskShader)
	return container.New(l, objects...)
}

// secondaryLayout is a simple layout for non-primary screen windows (no bar/widgets).
type secondaryLayout struct{}

func (s *secondaryLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func (s *secondaryLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(640, 480)
}

func (l *desktop) createSecondaryContent(sw *screenWindow) fyne.CanvasObject {
	sw.bg = newBackground()

	objects := []fyne.CanvasObject{sw.bg}

	// Normal compositor for regular windows
	if sw.compositor != nil {
		objects = append(objects, sw.compositor)
	}

	// Compositor overlay for fullscreen windows
	if sw.compositorOverlay != nil {
		objects = append(objects, sw.compositorOverlay)
	}

	sw.overviewShader = newOverviewShader()
	objects = append(objects, sw.overviewShader)

	sw.overlay = container.NewWithoutLayout()
	objects = append(objects, sw.overlay)

	sw.deskShaderBG = newDeskShaderBG()
	sw.deskShader = newDeskShader()
	objects = append(objects, sw.deskShaderBG, sw.deskShader)
	return container.New(&secondaryLayout{}, objects...)
}

// hasScreen reports whether a screen of that name is connected.
func hasScreen(screens []*tyde.Screen, name string) bool {
	for _, sc := range screens {
		if sc.Name == name {
			return true
		}
	}
	return false
}

func (l *desktop) setupRoot() {
	primary := l.screens.Primary()

	// Build or update screenWindow for each screen
	existingByName := make(map[string]*screenWindow, len(l.screenWindows))
	for _, sw := range l.screenWindows {
		existingByName[sw.screen.Name] = sw
	}

	// The primary's window is Fyne's master window, whose closing ends the
	// session: when its screen goes, it moves to the new primary screen, and
	// the window that screen had goes instead.
	if old := l.primaryWin; old != nil && primary != nil && old.screen.Name != primary.Name && !hasScreen(l.screens.Screens(), old.screen.Name) {
		if other := existingByName[primary.Name]; other != nil {
			other.win.Close()
		}
		delete(existingByName, old.screen.Name)
		existingByName[primary.Name] = old
	}

	var newWindows, createdWindows []*screenWindow
	for _, screen := range l.screens.Screens() {
		sw := existingByName[screen.Name]
		if sw != nil {
			// Update screen pointer (geometry may have changed)
			sw.screen = screen
			if sw.compositor != nil {
				sw.compositor.Screen = screen
			}
			if sw.compositorOverlay != nil {
				sw.compositorOverlay.Screen = screen
			}
			delete(existingByName, screen.Name)
		} else {
			// Create new screenWindow
			sw = &screenWindow{screen: screen}
			if l.compositorDone != nil {
				sw.compositor = NewCompositorWidget(screen)
				sw.compositorOverlay = NewCompositorWidget(screen)
			}
			win := l.app.NewWindow(RootWindowName + screen.Name)
			win.SetPadded(false)
			sw.win = win

			if screen == primary {
				win.SetMaster()
				win.SetOnClosed(func() {
					if l.compositorDone != nil {
						close(l.compositorDone)
					}
					l.wm.Close()
				})
				win.SetContent(l.createPrimaryContent(sw))
			} else {
				win.SetContent(l.createSecondaryContent(sw))
			}
			createdWindows = append(createdWindows, sw)
		}

		newWindows = append(newWindows, sw)
		if screen == primary {
			l.primaryWin = sw
		}
	}

	// Close windows for disconnected screens
	for _, sw := range existingByName {
		sw.win.Close()
	}

	l.screenWindows = newWindows

	// Resize each window to cover its screen
	for _, sw := range l.screenWindows {
		scale := sw.screen.CanvasScale()
		sw.win.Resize(fyne.NewSize(float32(sw.screen.Width)/scale, float32(sw.screen.Height)/scale))
	}

	// At startup runFull/runEmbed shows the windows once the run loop starts.
	// For screens hot-plugged at runtime the loop is already running, so the
	// newly created windows must be shown here or they never get mapped.
	if l.running {
		for _, sw := range createdWindows {
			if sw != l.primaryWin {
				sw.win.Show()
			}
		}
	}

	// Hand the running compositor the current per-screen widgets so windows
	// drawn on a newly connected screen get composited (and geometry stays in
	// sync). The initial list is passed to the compositor at startup instead.
	if CompositorScreensChanged != nil {
		CompositorScreensChanged(l.screenCompositors())
	}
}

func (l *desktop) RecentApps() []appie.AppData {
	return l.recent
}

func (l *desktop) Run() {
	go l.wm.Run()
	go l.watchScreenActivity()
	go l.watchSleep()
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

func (l *desktop) Settings() tyde.DeskSettings {
	return l.settings
}

func (l *desktop) ContentBoundsPixels(screen *tyde.Screen) (x, y, w, h uint32) {
	screenW := uint32(screen.Width)
	screenH := uint32(screen.Height)
	pad := wmtheme.WidgetPanelWidth
	if l.Settings().NarrowWidgetPanel() {
		pad = wmtheme.NarrowBarWidth
	}
	if l.screens.Primary() == screen {
		wid := uint32(pad * screen.CanvasScale())
		switch l.Settings().BarPosition() {
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

func (l *desktop) WindowManager() tyde.WindowManager {
	return l.wm
}

func (l *desktop) clearModuleCache() {
	for _, mod := range l.moduleCache {
		// Its shortcuts go with it: a disabled module's keys stayed bound
		// (Modules registers those of the enabled ones again).
		if bind, ok := mod.(tyde.KeyBindModule); ok {
			for sh := range bind.Shortcuts() {
				l.RemoveShortcut(sh)
			}
		}
		mod.Destroy()
	}

	l.moduleCache = nil
}

func (l *desktop) Modules() []tyde.Module {
	if l.moduleCache != nil {
		return l.moduleCache
	}

	var mods []tyde.Module
	for _, meta := range tyde.AvailableModules() {
		if !isModuleEnabled(meta.Name, l.settings) {
			continue
		}

		instance := meta.NewInstance()
		mods = append(mods, instance)

		if bind, ok := instance.(tyde.KeyBindModule); ok {
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

// AccessoryRefresher is installed by the platform compositor so that
// RefreshWindowAccessories can ask it to re-assemble WindowAccessoryModule
// items. It is nil before the compositor starts (and in embedded mode).
var AccessoryRefresher func()

// RefreshWindowAccessories asks the compositor to re-pull and re-stack the
// WindowAccessoryModule items (see modules implementing that interface).
func (l *desktop) RefreshWindowAccessories() {
	if AccessoryRefresher != nil {
		AccessoryRefresher()
	}
}

func (l *desktop) fireSettingsChangeListener(s tyde.DeskSettings) {
	locale.SetLanguage(s.Language())

	// Each part is rebuilt only when what it depends on changed: any setting
	// used to reload the modules, the wallpaper and the dock icons.
	modulesKey := fmt.Sprint(s.ModuleNames(), s.Language(), s.NarrowWidgetPanel(),
		s.ReduceMotion(), s.IconTheme(), s.DesktopNames(), s.DesktopCount(), s.BarPosition())
	bgType := fyne.CurrentApp().Preferences().String("background_type")
	bgKey := s.Background() + "\x00" + bgType
	barKey := fmt.Sprint(s.LauncherIconSize(), s.LauncherZoomScale(), s.LauncherDisableZoom(),
		s.LauncherIcons(), s.LauncherDisableTaskbar(), s.IconTheme(), s.BarPosition())
	if bgKey != l.lastBackgroundKey {
		l.lastBackgroundKey = bgKey
		l.updateBackgrounds(s.Background(), bgType)
	}
	if modulesKey == l.lastModulesKey && barKey == l.lastBarKey {
		return
	}
	modulesChanged := modulesKey != l.lastModulesKey
	l.lastModulesKey, l.lastBarKey = modulesKey, barKey
	if !modulesChanged {
		l.updateBar()
		return
	}
	l.clearModuleCache()
	l.widgets.reloadModules(l.Modules())

	// Update locale-dependent labels
	if np, ok := l.widgets.notifications.(*notificationPanel); ok {
		np.updateLocale()
	}
	if l.overlayLayer != nil {
		l.overlayLayer.rebuild()
	}
	l.RefreshWindowAccessories() // pick up enabling/disabling of accessory modules
	l.updateBar()
}

// updateBar applies the dock settings.
func (l *desktop) updateBar() {
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
	l.AddShortcut(tyde.NewShortcut("Show Launcher", fyne.KeySpace, tyde.UserModifier),
		ShowAppLauncher)
	l.AddShortcut(tyde.NewShortcut("Switch App Next", fyne.KeyTab, tyde.UserModifier),
		func() {
			// dummy - the wm handles app switcher
		})
	l.AddShortcut(tyde.NewShortcut("Switch App Previous", fyne.KeyTab, tyde.UserModifier|fyne.KeyModifierShift),
		func() {
			// dummy - the wm handles app switcher
		})
	l.AddShortcut(tyde.NewShortcut("Iconify Window", fyne.KeyF9, tyde.UserModifier),
		l.iconifyCurrentWindow)
	l.AddShortcut(tyde.NewShortcut("Maximize Window", fyne.KeyF10, tyde.UserModifier),
		l.maximizeCurrentWindow)
	l.AddShortcut(tyde.NewShortcut("FullScreen Window", fyne.KeyF11, tyde.UserModifier),
		l.fullscreenCurrentWindow)
	l.AddShortcut(tyde.NewShortcut("Print Window", deskDriver.KeyPrintScreen, fyne.KeyModifierShift),
		l.screenshotWindow)
	l.AddShortcut(tyde.NewShortcut("Print Screen", deskDriver.KeyPrintScreen, 0),
		l.screenshot)
	l.AddShortcut(tyde.NewShortcut("Calculator", tyde.KeyCalculator, 0),
		l.calculator)
	l.AddShortcut(tyde.NewShortcut("Lock screen", fyne.KeyL, tyde.UserModifier),
		func() {
			l.TriggerScreenSaver(false)
		})
}

// Screens returns the screens provider of the current desktop environment for access to screen functionality.
func (l *desktop) Screens() tyde.ScreenList {
	return l.screens
}

// CompositorRunFunc is a function that runs a platform compositor using the
// provided per-screen widgets. It blocks until done is closed.
type CompositorRunFunc func(done chan struct{}, screens []ScreenCompositors) error

// NewDesktop creates the full desktop environment with window management.
// If compositorRun is non-nil, the compositor is started in a background goroutine.
func NewDesktop(app fyne.App, mgr tyde.WindowManager, icons appie.Provider, screenProvider tyde.ScreenList, compositorRun CompositorRunFunc) tyde.Desktop {
	desk := newDesktop(app, mgr, icons)
	desk.run = desk.runFull
	if compositorRun != nil {
		desk.compositorDone = make(chan struct{})
	}
	// Screen changes arrive on the X11 event goroutine. Once the run loop is
	// up, the window operations in setupRoot must happen on the Fyne main
	// goroutine, so marshal them with fyne.Do. Before the loop starts (the
	// initial call below) we run it directly, as fyne.Do requires a running loop.
	screenProvider.AddChangeListener(func() {
		if desk.running {
			fyne.Do(desk.setupRoot)
		} else {
			desk.setupRoot()
		}
	})
	desk.screens = screenProvider

	desk.setupRoot()

	if compositorRun != nil {
		go func() {
			screens := desk.screenCompositors()
			if err := compositorRun(desk.compositorDone, screens); err != nil {
				fyne.LogError("Compositor failed", err)
			}
		}()
	}

	wm.StartAuthAgent()
	if desk.Settings().ScreenSaverType() == "XScreensaver" {
		go desk.startXscreensaver()
	}
	return desk
}

// screenCompositors returns the per-screen compositor widget pairs.
func (l *desktop) screenCompositors() []ScreenCompositors {
	var out []ScreenCompositors
	for _, sw := range l.screenWindows {
		if sw.compositor != nil {
			out = append(out, ScreenCompositors{
				Screen:  sw.screen,
				Normal:  sw.compositor,
				Overlay: sw.compositorOverlay,
			})
		}
	}
	return out
}

// NewEmbeddedDesktop creates a new windowed desktop for test purposes.
// An ApplicationProvider is used to lookup application icons from the operating system.
// If run during CI for testing it will return an in-memory window using the
// fyne/test package.
func NewEmbeddedDesktop(app fyne.App, icons appie.Provider) tyde.Desktop {
	return newEmbeddedDesktop(app, icons, "Embedded "+RootWindowName)
}

// NewPanelDesktop creates an embedded desktop for use as a Wayland panel.
// The window title is set to "Tyde:Panel" so the compositor can identify it.
func NewPanelDesktop(app fyne.App, icons appie.Provider) tyde.Desktop {
	return newEmbeddedDesktop(app, icons, "Tyde:Panel")
}

func newEmbeddedDesktop(app fyne.App, icons appie.Provider, title string) tyde.Desktop {
	wm := &embededWM{}
	desk := newDesktop(app, wm, icons)
	desk.run = desk.runEmbed
	desk.showMenu = desk.showMenuEmbed

	win := desk.newDesktopWindowEmbedWithTitle(title)
	sw := &screenWindow{
		screen: desk.screens.Primary(),
		win:    win,
	}
	desk.screenWindows = []*screenWindow{sw}
	desk.primaryWin = sw

	// Embedded mode runs without the platform compositor that normally hosts
	// window accessories, so install a refresher that renders them above desktop.
	desk.accessoryLayer = container.NewWithoutLayout()
	AccessoryRefresher = func() { rebuildEmbeddedAccessories(desk.accessoryLayer) }

	// The saver monitor watches mouse movement to defer the screen saver.
	desk.activityLayer = wm.setWindow(win)
	win.SetContent(desk.createPrimaryContent(sw))
	return desk
}

// SetScreenSize sets the pixel screen dimensions for the Wayland panel.
// This updates the embedded WM and screen provider with the actual screen size,
// so overlay positions can be correctly mapped to pixel coordinates.
func SetScreenSize(desk tyde.Desktop, w, h int) {
	if d, ok := desk.(*desktop); ok {
		if wm, ok := d.wm.(*embededWM); ok {
			wm.screenW = w
			wm.screenH = h
		}
		// Also update the embedded screen provider
		if esp, ok := d.screens.(*embeddedScreensProvider); ok {
			esp.UpdatePrimarySize(w, h)
		}
	}
}

// SetScreenPosition sets the primary output's layout position for the Wayland panel.
// When the primary output is not at (0,0) (e.g. multi-monitor), overlay positions
// must be offset by these coordinates so they land on the correct output.
func SetScreenPosition(desk tyde.Desktop, x, y int) {
	if d, ok := desk.(*desktop); ok {
		if esp, ok := d.screens.(*embeddedScreensProvider); ok {
			esp.screens[0].X = x
			esp.screens[0].Y = y
		}
	}
}

// rebuildEmbeddedAccessories collects the WindowAccessory items from the enabled
// modules and renders them flat into layer. Embedded mode has no compositor to
// interleave them with windows at the right z-levels, so they all draw together
// in this single layer; each window's decorations go in a container held over
// that window. Runs on the main goroutine (via RefreshWindowAccessories).
func rebuildEmbeddedAccessories(layer *fyne.Container) {
	inst := tyde.Instance()
	if inst == nil || layer == nil {
		return
	}

	byWindow := map[tyde.Window]*fyne.Container{}
	var objs []fyne.CanvasObject
	for _, m := range inst.Modules() {
		am, ok := m.(tyde.WindowAccessoryModule)
		if !ok {
			continue
		}
		for _, acc := range am.WindowAccessories() {
			if acc.Object == nil {
				continue
			}
			if acc.Window == nil {
				objs = append(objs, acc.Object) // positioned on the screen
				continue
			}
			cont, ok := byWindow[acc.Window]
			if !ok {
				cont = container.NewWithoutLayout()
				cont.Move(acc.Window.Position())
				cont.Resize(acc.Window.Size())
				byWindow[acc.Window] = cont
				objs = append(objs, cont)
			}
			cont.Objects = append(cont.Objects, acc.Object)
		}
	}

	layer.Objects = objs
	layer.Refresh()
}

func newDesktop(app fyne.App, wm tyde.WindowManager, icons appie.Provider) *desktop {
	desk := &desktop{app: app, wm: wm, icons: icons, screens: newEmbeddedScreensProvider()}
	desk.showMenu = desk.showMenuFull

	tyde.SetInstance(desk)
	desk.settings = newDeskSettings()
	locale.SetLanguage(desk.settings.Language())
	desk.addSettingsChangeListener()

	// Load theme.json so custom colors (primary, background, etc.) apply at startup
	reloadFyneTheme()

	// Sync Fyne primary color changes to theme.json so the JSON theme
	// reflects the user's "Main Color" selection from Fyne Settings.
	if ds, ok := desk.settings.(*deskSettings); ok {
		watchFynePrimaryColor(app, ds)
	}

	// Watch for accent color extracted from wallpaper by the compositor,
	// for the life of the panel.
	watchAccentColor(nil)

	desk.registerShortcuts()
	startCalendarService()
	startAppWatcher(desk)
	return desk
}

func (l *desktop) calculator() {
	err := wm.StartDetached("calculator")
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
