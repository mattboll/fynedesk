// Package wm provides a Wayland window manager implementation for FyneDesk.
package wm

import (
	"image"
	"sync"

	"fyne.io/fyne/v2"

	"fyshos.com/fynedesk"
)

// Window wraps a Wayland XDG toplevel surface as a fynedesk.Window.
type Window struct {
	mu sync.RWMutex

	wm    *WaylandWM
	title string
	class []string

	focused     bool
	fullscreen  bool
	iconic      bool
	maximized   bool
	pinned      bool
	desktop     int
	skipTaskbar bool
	decorated   bool

	x, y          float64
	width, height float64

	// Callback to close the actual Wayland surface
	closeFunc func()
}

// NewWindow creates a new Window wrapper.
func NewWindow(wm *WaylandWM, title string) *Window {
	return &Window{
		wm:        wm,
		title:     title,
		decorated: true,
		desktop:   0,
	}
}

// Focused returns whether this window has input focus.
func (w *Window) Focused() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.focused
}

// SetFocused updates the focused state.
func (w *Window) SetFocused(focused bool) {
	w.mu.Lock()
	w.focused = focused
	w.mu.Unlock()
}

// Fullscreened returns whether the window is fullscreen.
func (w *Window) Fullscreened() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.fullscreen
}

// Iconic returns whether the window is iconified/minimized.
func (w *Window) Iconic() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.iconic
}

// Maximized returns whether the window is maximized.
func (w *Window) Maximized() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.maximized
}

// TopWindow returns whether this is the top window.
func (w *Window) TopWindow() bool {
	if w.wm == nil {
		return false
	}
	return w.wm.TopWindow() == w
}

// Capture captures the window contents to an image.
func (w *Window) Capture() image.Image {
	// TODO: implement via wlroots screencopy
	return image.NewRGBA(image.Rect(0, 0, int(w.width), int(w.height)))
}

// Close requests the window to close.
func (w *Window) Close() {
	if w.closeFunc != nil {
		w.closeFunc()
	}
}

// Focus requests input focus for this window.
func (w *Window) Focus() {
	if w.wm != nil {
		w.wm.FocusWindow(w)
	}
}

// Fullscreen requests fullscreen mode.
func (w *Window) Fullscreen() {
	w.mu.Lock()
	w.fullscreen = true
	w.mu.Unlock()
	// TODO: send XDG toplevel fullscreen request
}

// Iconify minimizes the window.
func (w *Window) Iconify() {
	w.mu.Lock()
	w.iconic = true
	w.mu.Unlock()
	// TODO: implement
}

// Maximize maximizes the window.
func (w *Window) Maximize() {
	w.mu.Lock()
	w.maximized = true
	w.mu.Unlock()
	// TODO: send XDG toplevel maximize request
}

// RaiseAbove raises this window above another.
func (w *Window) RaiseAbove(other fynedesk.Window) {
	// TODO: implement stacking order
}

// RaiseToTop raises this window to the top of the stack.
func (w *Window) RaiseToTop() {
	if w.wm != nil {
		w.wm.RaiseToTop(w)
	}
}

// Unfullscreen exits fullscreen mode.
func (w *Window) Unfullscreen() {
	w.mu.Lock()
	w.fullscreen = false
	w.mu.Unlock()
}

// Uniconify restores the window from minimized state.
func (w *Window) Uniconify() {
	w.mu.Lock()
	w.iconic = false
	w.mu.Unlock()
}

// Unmaximize restores the window from maximized state.
func (w *Window) Unmaximize() {
	w.mu.Lock()
	w.maximized = false
	w.mu.Unlock()
}

// Parent returns the parent window (nil for top-level).
func (w *Window) Parent() fynedesk.Window {
	return nil
}

// Properties returns the window properties.
func (w *Window) Properties() fynedesk.WindowProperties {
	return w
}

// Position returns the window position.
func (w *Window) Position() fyne.Position {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return fyne.NewPos(float32(w.x), float32(w.y))
}

// Size returns the window size.
func (w *Window) Size() fyne.Size {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return fyne.NewSize(float32(w.width), float32(w.height))
}

// Move moves the window to a new position.
func (w *Window) Move(pos fyne.Position) {
	w.mu.Lock()
	w.x = float64(pos.X)
	w.y = float64(pos.Y)
	w.mu.Unlock()
}

// Resize resizes the window.
func (w *Window) Resize(size fyne.Size) {
	w.mu.Lock()
	w.width = float64(size.Width)
	w.height = float64(size.Height)
	w.mu.Unlock()
}

// Desktop returns the desktop number this window is on.
func (w *Window) Desktop() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.desktop
}

// SetDesktop sets the desktop number.
func (w *Window) SetDesktop(d int) {
	w.mu.Lock()
	w.desktop = d
	w.mu.Unlock()
}

// Pin pins the window to all desktops.
func (w *Window) Pin() {
	w.mu.Lock()
	w.pinned = true
	w.mu.Unlock()
}

// Pinned returns whether the window is pinned.
func (w *Window) Pinned() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.pinned
}

// Unpin unpins the window.
func (w *Window) Unpin() {
	w.mu.Lock()
	w.pinned = false
	w.mu.Unlock()
}

// Urgent returns whether this window is requesting attention.
func (w *Window) Urgent() bool {
	return false
}

// WindowProperties interface implementation

// Class returns the window class.
func (w *Window) Class() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.class
}

// Command returns the command that started this window.
func (w *Window) Command() string {
	return ""
}

// Decorated returns whether the window should have decorations.
func (w *Window) Decorated() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.decorated
}

// Icon returns the window icon.
func (w *Window) Icon() fyne.Resource {
	return nil
}

// IconName returns the icon name.
func (w *Window) IconName() string {
	return ""
}

// SkipTaskbar returns whether to skip the taskbar.
func (w *Window) SkipTaskbar() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.skipTaskbar
}

// Title returns the window title.
func (w *Window) Title() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.title
}

// SetTitle updates the window title.
func (w *Window) SetTitle(title string) {
	w.mu.Lock()
	w.title = title
	w.mu.Unlock()
}

// SetClass updates the window class.
func (w *Window) SetClass(class []string) {
	w.mu.Lock()
	w.class = class
	w.mu.Unlock()
}

// SetSize updates the window size internally.
func (w *Window) SetSize(width, height float64) {
	w.mu.Lock()
	w.width = width
	w.height = height
	w.mu.Unlock()
}

// SetPosition updates the window position internally.
func (w *Window) SetPosition(x, y float64) {
	w.mu.Lock()
	w.x = x
	w.y = y
	w.mu.Unlock()
}

// SetCloseFunc sets the callback to close the Wayland surface.
func (w *Window) SetCloseFunc(f func()) {
	w.closeFunc = f
}
