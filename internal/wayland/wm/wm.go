// Package wm provides Wayland window manager implementation with IPC-based window state tracking.
package wm

import (
	"image"
	"sync"

	"github.com/FyshOS/saver"

	"fyne.io/fyne/v2"

	"fyshos.com/fynedesk"
)

// WaylandWM implements fynedesk.WindowManager for Wayland compositors.
type WaylandWM struct {
	mu sync.RWMutex

	windows   []*Window
	listeners []fynedesk.StackListener

	// Callbacks to the compositor
	showOverlayFunc func(fyne.Window, fyne.Size, fyne.Position)
	showModalFunc   func(fyne.Window, fyne.Size)
	captureFunc     func() image.Image
	runFunc         func()
	closeFunc       func()
}

// NewWaylandWM creates a new Wayland window manager.
func NewWaylandWM() *WaylandWM {
	return &WaylandWM{
		windows:   make([]*Window, 0),
		listeners: make([]fynedesk.StackListener, 0),
	}
}

// SetCallbacks sets the compositor callbacks.
func (wm *WaylandWM) SetCallbacks(
	showOverlay func(fyne.Window, fyne.Size, fyne.Position),
	showModal func(fyne.Window, fyne.Size),
	capture func() image.Image,
	run func(),
	close func(),
) {
	wm.showOverlayFunc = showOverlay
	wm.showModalFunc = showModal
	wm.captureFunc = capture
	wm.runFunc = run
	wm.closeFunc = close
}

// Stack interface implementation

// AddWindow adds a new window to the stack.
func (wm *WaylandWM) AddWindow(w fynedesk.Window) {
	wm.mu.Lock()
	win, ok := w.(*Window)
	if !ok {
		wm.mu.Unlock()
		return
	}
	wm.windows = append(wm.windows, win)
	wm.mu.Unlock()

	wm.notifyWindowAdded(w)
}

// RaiseToTop moves a window to the top of the stack.
func (wm *WaylandWM) RaiseToTop(w fynedesk.Window) {
	wm.mu.Lock()
	for i, win := range wm.windows {
		if win == w {
			// Remove from current position
			wm.windows = append(wm.windows[:i], wm.windows[i+1:]...)
			// Add to front (top)
			wm.windows = append([]*Window{win}, wm.windows...)
			break
		}
	}
	wm.mu.Unlock()

	wm.notifyOrderChanged()
}

// RemoveWindow removes a window from the stack.
func (wm *WaylandWM) RemoveWindow(w fynedesk.Window) {
	wm.mu.Lock()
	for i, win := range wm.windows {
		if win == w {
			wm.windows = append(wm.windows[:i], wm.windows[i+1:]...)
			break
		}
	}
	wm.mu.Unlock()

	wm.notifyWindowRemoved(w)
}

// TopWindow returns the currently top window.
func (wm *WaylandWM) TopWindow() fynedesk.Window {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	if len(wm.windows) == 0 {
		return nil
	}
	return wm.windows[0]
}

// Windows returns all managed windows.
func (wm *WaylandWM) Windows() []fynedesk.Window {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	result := make([]fynedesk.Window, len(wm.windows))
	for i, w := range wm.windows {
		result[i] = w
	}
	return result
}

// AddStackListener adds a listener for stack events.
func (wm *WaylandWM) AddStackListener(l fynedesk.StackListener) {
	wm.mu.Lock()
	wm.listeners = append(wm.listeners, l)
	wm.mu.Unlock()
}

// WindowManager interface implementation

// Blank blanks the screen (screensaver).
func (wm *WaylandWM) Blank() {
	// TODO: implement screen blanking
}

// Capture captures the entire desktop.
func (wm *WaylandWM) Capture() image.Image {
	if wm.captureFunc != nil {
		return wm.captureFunc()
	}
	return image.NewRGBA(image.Rect(0, 0, 1920, 1080))
}

// Close shuts down the window manager.
func (wm *WaylandWM) Close() {
	if wm.closeFunc != nil {
		wm.closeFunc()
	}
}

// Run starts the window manager event loop.
func (wm *WaylandWM) Run() {
	if wm.runFunc != nil {
		wm.runFunc()
	}
}

// ShowOverlay shows an overlay window.
func (wm *WaylandWM) ShowOverlay(win fyne.Window, size fyne.Size, pos fyne.Position) {
	if wm.showOverlayFunc != nil {
		wm.showOverlayFunc(win, size, pos)
	}
}

// ShowMenuOverlay shows a menu overlay.
func (wm *WaylandWM) ShowMenuOverlay(menu *fyne.Menu, size fyne.Size, pos fyne.Position) {
	// TODO: implement menu overlay
}

// ShowModal shows a modal window.
func (wm *WaylandWM) ShowModal(win fyne.Window, size fyne.Size) {
	if wm.showModalFunc != nil {
		wm.showModalFunc(win, size)
	}
}

// ShowScreensaver shows the screensaver.
func (wm *WaylandWM) ShowScreensaver(s *saver.ScreenSaver) {
	// TODO: implement screensaver
}

// FocusWindow focuses a specific window.
func (wm *WaylandWM) FocusWindow(w *Window) {
	wm.mu.Lock()
	for _, win := range wm.windows {
		win.SetFocused(win == w)
	}
	wm.mu.Unlock()

	wm.RaiseToTop(w)
}

// Helper methods for the compositor

// CreateWindow creates and registers a new window.
func (wm *WaylandWM) CreateWindow(title string) *Window {
	w := NewWindow(wm, title)
	wm.AddWindow(w)
	return w
}

// Notification helpers

func (wm *WaylandWM) notifyWindowAdded(w fynedesk.Window) {
	wm.mu.RLock()
	listeners := make([]fynedesk.StackListener, len(wm.listeners))
	copy(listeners, wm.listeners)
	wm.mu.RUnlock()

	for _, l := range listeners {
		l.WindowAdded(w)
	}
}

func (wm *WaylandWM) notifyWindowRemoved(w fynedesk.Window) {
	wm.mu.RLock()
	listeners := make([]fynedesk.StackListener, len(wm.listeners))
	copy(listeners, wm.listeners)
	wm.mu.RUnlock()

	for _, l := range listeners {
		l.WindowRemoved(w)
	}
}

func (wm *WaylandWM) notifyOrderChanged() {
	wm.mu.RLock()
	listeners := make([]fynedesk.StackListener, len(wm.listeners))
	copy(listeners, wm.listeners)
	wm.mu.RUnlock()

	for _, l := range listeners {
		l.WindowOrderChanged()
	}
}

func (wm *WaylandWM) notifyStateChanged(w fynedesk.Window) {
	wm.mu.RLock()
	listeners := make([]fynedesk.StackListener, len(wm.listeners))
	copy(listeners, wm.listeners)
	wm.mu.RUnlock()

	for _, l := range listeners {
		l.WindowStateChanged(w)
	}
}
