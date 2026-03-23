// Package wayland provides a Wayland compositor desktop implementation for FyneDesk.
package wayland

import (
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/FyshOS/appie"
	"github.com/FyshOS/saver"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/wayland/wm"
	wmutil "fyshos.com/fynedesk/wm"
)

// Desktop implements fynedesk.Desktop for Wayland compositors.
type Desktop struct {
	wmutil.ShortcutHandler
	mu sync.RWMutex

	app      fyne.App
	wm       *wm.WaylandWM
	icons    appie.Provider
	settings fynedesk.DeskSettings
	screens  fynedesk.ScreenList
	recent   []appie.AppData

	moduleCache []fynedesk.Module
	desk        int

	// Callbacks to the compositor
	runFunc      func()
	showMenuFunc func(*fyne.Menu, fyne.Position)
	rootWindow   fyne.Window
}

// NewDesktop creates a new Wayland desktop.
func NewDesktop(wmgr *wm.WaylandWM, icons appie.Provider, settings fynedesk.DeskSettings) *Desktop {
	d := &Desktop{
		app:      app.New(),
		wm:       wmgr,
		icons:    icons,
		settings: settings,
		screens:  newScreenList(),
	}

	fynedesk.SetInstance(d)
	go d.screens.(*screenList).watchCompositorState()
	return d
}

// SetCallbacks sets the compositor callbacks.
func (d *Desktop) SetCallbacks(run func(), showMenu func(*fyne.Menu, fyne.Position), root fyne.Window) {
	d.runFunc = run
	d.showMenuFunc = showMenu
	d.rootWindow = root
}

// Run starts the desktop environment.
func (d *Desktop) Run() {
	if d.runFunc != nil {
		d.runFunc()
	}
}

// RunApp launches an application.
func (d *Desktop) RunApp(data appie.AppData) error {
	vars := d.scaleVars(d.Screens().Active().CanvasScale())
	err := data.Run(vars)
	if err == nil {
		d.addRecent(data)
	}
	return err
}

// RunAppAction runs a specific action of an application.
func (d *Desktop) RunAppAction(data appie.AppData, id int) error {
	if data.Actions() == nil || len(data.Actions())-1 < id {
		return nil
	}
	vars := d.scaleVars(d.Screens().Active().CanvasScale())
	err := data.Actions()[id].Run(vars)
	if err == nil {
		d.addRecent(data)
	}
	return err
}

func (d *Desktop) addRecent(data appie.AppData) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.recent = append([]appie.AppData{data}, d.recent...)
	// Remove duplicates
	for i := 1; i < len(d.recent); i++ {
		if d.recent[i] == data {
			d.recent = append(d.recent[:i], d.recent[i+1:]...)
			break
		}
	}
	// Limit to 5
	if len(d.recent) > 5 {
		d.recent = d.recent[:5]
	}
}

// RecentApps returns recently launched applications.
func (d *Desktop) RecentApps() []appie.AppData {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.recent
}

// Settings returns the desktop settings.
func (d *Desktop) Settings() fynedesk.DeskSettings {
	return d.settings
}

// ContentBoundsPixels returns the usable content area for a screen.
func (d *Desktop) ContentBoundsPixels(screen *fynedesk.Screen) (x, y, w, h uint32) {
	screenW := uint32(screen.Width)
	screenH := uint32(screen.Height)

	// Reserve space for bar and widget panel
	barWidth := uint32(64) // Narrow bar width
	widgetWidth := uint32(196)
	if d.Settings().NarrowWidgetPanel() {
		widgetWidth = 36
	}

	return barWidth, 0, screenW - barWidth - widgetWidth, screenH
}

// RootSizePixels returns the total root window size.
// Handles negative screen positions (e.g., "above" layout with negative Y).
func (d *Desktop) RootSizePixels() (w, h uint32) {
	screens := d.Screens().Screens()
	if len(screens) == 0 {
		return 0, 0
	}
	minX, minY := screens[0].X, screens[0].Y
	maxX, maxY := screens[0].X+screens[0].Width, screens[0].Y+screens[0].Height
	for _, screen := range screens[1:] {
		if screen.X < minX {
			minX = screen.X
		}
		if screen.Y < minY {
			minY = screen.Y
		}
		right := screen.X + screen.Width
		bottom := screen.Y + screen.Height
		if right > maxX {
			maxX = right
		}
		if bottom > maxY {
			maxY = bottom
		}
	}
	return uint32(maxX - minX), uint32(maxY - minY)
}

// Screens returns the screen provider.
func (d *Desktop) Screens() fynedesk.ScreenList {
	return d.screens
}

// IconProvider returns the application icon provider.
func (d *Desktop) IconProvider() appie.Provider {
	return d.icons
}

// WindowManager returns the window manager.
func (d *Desktop) WindowManager() fynedesk.WindowManager {
	return d.wm
}

// Modules returns enabled modules.
func (d *Desktop) Modules() []fynedesk.Module {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.moduleCache != nil {
		return d.moduleCache
	}

	var mods []fynedesk.Module
	for _, meta := range fynedesk.AvailableModules() {
		if !isModuleEnabled(meta.Name, d.settings) {
			continue
		}
		instance := meta.NewInstance()
		mods = append(mods, instance)

		if bind, ok := instance.(fynedesk.KeyBindModule); ok {
			for sh, f := range bind.Shortcuts() {
				d.AddShortcut(sh, f)
			}
		}
	}

	d.moduleCache = mods
	return mods
}

func isModuleEnabled(name string, settings fynedesk.DeskSettings) bool {
	for _, mod := range settings.ModuleNames() {
		if mod == name {
			return true
		}
	}
	return false
}

// ShowMenuAt shows a context menu at a position.
func (d *Desktop) ShowMenuAt(menu *fyne.Menu, pos fyne.Position) {
	if d.showMenuFunc != nil {
		d.showMenuFunc(menu, pos)
	}
}

// Root returns the root window (nil for Wayland compositor).
func (d *Desktop) Root() fyne.Window {
	return d.rootWindow
}

// Desktop returns the current virtual desktop index.
func (d *Desktop) Desktop() int {
	return d.desk
}

// SetDesktop switches to a virtual desktop.
func (d *Desktop) SetDesktop(id int) {
	d.desk = id
}

// ShowSettings opens the settings dialog.
func (d *Desktop) ShowSettings() {
	// TODO: implement settings dialog
}

// DelayScreenSaver delays the screensaver activation.
func (d *Desktop) DelayScreenSaver() {
	// TODO: implement screensaver delay
}

// TriggerScreenSaver triggers the screensaver.
func (d *Desktop) TriggerScreenSaver(lock bool) {
	// TODO: implement screensaver
}

func (d *Desktop) scaleVars(scale float32) []string {
	return []string{
		"QT_SCALE_FACTOR=1",
		"GDK_SCALE=1",
	}
}

// WaylandWM interface additions for compositor integration

// Blank blanks the screen.
func (d *Desktop) Blank() {
	d.wm.Blank()
}

// Capture captures the desktop.
func (d *Desktop) Capture() image.Image {
	return d.wm.Capture()
}

// ShowOverlay shows an overlay window.
func (d *Desktop) ShowOverlay(win fyne.Window, size fyne.Size, pos fyne.Position) {
	d.wm.ShowOverlay(win, size, pos)
}

// ShowModal shows a modal window.
func (d *Desktop) ShowModal(win fyne.Window, size fyne.Size) {
	d.wm.ShowModal(win, size)
}

// ShowScreensaver shows the screensaver.
func (d *Desktop) ShowScreensaver(s *saver.ScreenSaver) {
	d.wm.ShowScreensaver(s)
}

// compositorOutputInfo mirrors a single output in the compositor's multi-output JSON.
type compositorOutputInfo struct {
	OutputName string  `json:"output_name"`
	PhysWidth  int     `json:"phys_width"`
	PhysHeight int     `json:"phys_height"`
	Scale      float32 `json:"scale"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Primary    bool    `json:"primary"`
}

// compositorStateInfo mirrors the compositor's JSON output.
type compositorStateInfo struct {
	Outputs []compositorOutputInfo `json:"outputs"`

	// Legacy single-output fields for backward compat
	OutputName string  `json:"output_name"`
	PhysWidth  int     `json:"phys_width"`
	PhysHeight int     `json:"phys_height"`
	Scale      float32 `json:"scale"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
}

// screenList implements fynedesk.ScreenList for Wayland.
type screenList struct {
	mu        sync.RWMutex
	screens   []*fynedesk.Screen
	primary   *fynedesk.Screen
	active    *fynedesk.Screen
	listeners []func()
}

func newScreenList() *screenList {
	// Default screen until we get real output info
	defaultScreen := &fynedesk.Screen{
		Name:   "default",
		X:      0,
		Y:      0,
		Width:  1920,
		Height: 1080,
	}
	return &screenList{
		screens: []*fynedesk.Screen{defaultScreen},
		primary: defaultScreen,
		active:  defaultScreen,
	}
}

func (s *screenList) AddChangeListener(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, f)
}

func (s *screenList) Screens() []*fynedesk.Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.screens
}

func (s *screenList) Primary() *fynedesk.Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.primary
}

func (s *screenList) Active() *fynedesk.Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *screenList) SetActive(screen *fynedesk.Screen) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = screen
}

func (s *screenList) ScreenForWindow(win fynedesk.Window) *fynedesk.Screen {
	return s.Primary()
}

func (s *screenList) ScreenForGeometry(x, y, w, h int) *fynedesk.Screen {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.screens) <= 1 {
		return s.primary
	}
	// Center-point hit test (same logic as X11 implementation)
	cx := x + w/2
	cy := y + h/2
	for _, scr := range s.screens {
		if cx >= scr.X && cy >= scr.Y &&
			cx <= scr.X+scr.Width && cy <= scr.Y+scr.Height {
			return scr
		}
	}
	return s.active
}

func (s *screenList) RefreshScreens() {
	// Will be called by compositor when outputs change
}

// SetScreens updates the screen list from compositor (primary defaults to first screen).
func (s *screenList) SetScreens(screens []*fynedesk.Screen) {
	s.SetScreensWithPrimary(screens, nil)
}

// SetScreensWithPrimary updates the screen list with an explicit primary screen.
func (s *screenList) SetScreensWithPrimary(screens []*fynedesk.Screen, primary *fynedesk.Screen) {
	s.mu.Lock()
	s.screens = screens
	if primary != nil {
		s.primary = primary
		s.active = primary
	} else if len(screens) > 0 {
		s.primary = screens[0]
		s.active = screens[0]
	}
	listeners := make([]func(), len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()

	for _, l := range listeners {
		l()
	}
}

// watchCompositorState polls compositor-state.json and updates the screen list.
func (s *screenList) watchCompositorState() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	statePath := filepath.Join(configDir, "fynedesk", "compositor-state.json")

	var lastMod time.Time
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		info, err := os.Stat(statePath)
		if err != nil || !info.ModTime().After(lastMod) {
			continue
		}
		lastMod = info.ModTime()

		data, err := os.ReadFile(statePath)
		if err != nil || len(data) == 0 {
			continue
		}

		var state compositorStateInfo
		if json.Unmarshal(data, &state) != nil {
			continue
		}

		var screens []*fynedesk.Screen

		if len(state.Outputs) > 0 {
			// Multi-output format
			for _, out := range state.Outputs {
				scale := out.Scale
				if scale < 1.0 {
					scale = 1.0
				}
				screens = append(screens, &fynedesk.Screen{
					Name:   out.OutputName,
					X:      out.X,
					Y:      out.Y,
					Width:  out.Width,
					Height: out.Height,
					Scale:  scale,
				})
			}
		} else {
			// Legacy single-output format
			scale := state.Scale
			if scale < 1.0 {
				scale = 1.0
			}
			screens = append(screens, &fynedesk.Screen{
				Name:   state.OutputName,
				X:      0,
				Y:      0,
				Width:  state.Width,
				Height: state.Height,
				Scale:  scale,
			})
		}

		// Identify primary screen from compositor's Primary flag
		var primary *fynedesk.Screen
		if len(state.Outputs) > 0 {
			for i, out := range state.Outputs {
				if out.Primary {
					primary = screens[i]
					break
				}
			}
		}
		s.SetScreensWithPrimary(screens, primary)
	}
}

// Calculator launches calculator app.
func (d *Desktop) Calculator() {
	exec.Command("gnome-calculator").Start()
}
