package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"sync"
	"sync/atomic"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/notify"
	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
	"github.com/FyshOS/saver"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type embededWM struct {
	mu              sync.RWMutex
	windows         []fynedesk.Window
	listeners       []fynedesk.StackListener
	root            fyne.Window
	ipcMode         atomic.Bool
	overlay         fyne.Window   // currently open overlay menu
	onOverlayClosed func()        // extra cleanup when overlay is closed
	screenW         int           // pixel screen width (from compositor)
	screenH         int           // pixel screen height (from compositor)
	fileIPCDone     chan struct{} // closed to stop file-based IPC watchers
}

func (e *embededWM) AddWindow(win fynedesk.Window) {
	e.windows = append(e.windows, win)
}

func (e *embededWM) RaiseToTop(win fynedesk.Window) {
	win.RaiseToTop()
}

func (e *embededWM) RemoveWindow(win fynedesk.Window) {
	for i, w := range e.windows {
		if w != win {
			continue
		}

		e.windows = append(e.windows[:i], e.windows[i+1:]...)
		return
	}
}

func (e *embededWM) Run() {
}

func (e *embededWM) ShowOverlay(w fyne.Window, s fyne.Size, p fyne.Position) {
	// Close any existing overlay first (toggle behavior)
	if e.overlay != nil {
		e.overlay.Close()
		e.overlay = nil
	}
	w.Resize(s)
	if e.ipcMode.Load() {
		// Callers pass positions relative to the primary screen's top-left.
		// RequestOverlayPosition adds the primary screen offset automatically.
		screen := fynedesk.Instance().Screens().Primary()
		scale := screen.CanvasScale()
		px, py := p.X, p.Y

		// Clamp so overlay fits on primary screen
		screenW := float32(screen.Width) / scale
		screenH := float32(screen.Height) / scale
		if px+s.Width > screenW {
			px = screenW - s.Width
		}
		if py+s.Height > screenH {
			py = screenH - s.Height
		}
		if px < 0 {
			px = 0
		}
		if py < 0 {
			py = 0
		}
		w.SetTitle("FyneDesk Menu")
		wlipc.RequestOverlayPosition("FyneDesk Menu", px, py, s.Width, s.Height)
	}
	// Clear e.overlay when window is closed (by button callback or compositor dismiss)
	// to avoid double-close on next ShowOverlay call.
	// Also invoke caller's onClosed if one was set before ShowOverlay.
	e.onOverlayClosed = nil
	w.SetOnClosed(func() {
		if e.overlay == w {
			e.overlay = nil
		}
		if e.onOverlayClosed != nil {
			e.onOverlayClosed()
			e.onOverlayClosed = nil
		}
	})
	e.overlay = w
	w.Show()
}

func (e *embededWM) ShowMenuOverlay(*fyne.Menu, fyne.Size, fyne.Position) {
	// no-op, handled by desktop in embed mode
}

func (e *embededWM) ShowModal(w fyne.Window, s fyne.Size) {
	w.Resize(s)
	w.CenterOnScreen()
	w.Show()
}

func (e *embededWM) TopWindow() fynedesk.Window {
	if len(e.windows) == 0 {
		return nil
	}

	return e.windows[len(e.windows)-1]
}

func (e *embededWM) Windows() []fynedesk.Window {
	return e.windows
}

func (e *embededWM) AddStackListener(l fynedesk.StackListener) {
	e.mu.Lock()
	e.listeners = append(e.listeners, l)
	e.mu.Unlock()
}

func (e *embededWM) Blank() {
	// no-op, we don't control screen brightness
}

func (e *embededWM) Capture() image.Image {
	return nil // would mean accessing the underling OS screen functions...
}

func (e *embededWM) Close() {
	if e.ipcMode.Load() {
		wlipc.RequestLogout()
	}

	windows := fyne.CurrentApp().Driver().AllWindows()
	if len(windows) > 0 {
		windows[0].Close() // ensure our root is asked to close as well
	}
}

var visible bool

func (e *embededWM) ShowScreensaver(s *saver.ScreenSaver) {
	if visible {
		return
	}

	visible = true
	over := container.NewStack(canvas.NewRectangle(color.Black))

	s.OnUnlocked = func() {
		visible = false
		e.root.Canvas().Overlays().Remove(over)
	}

	over.Add(s.MakeUI(e.root))
	over.Resize(e.root.Canvas().Size())
	e.root.Canvas().Overlays().Add(over)
}

func (e *embededWM) setWindow(win fyne.Window) fyne.CanvasObject {
	e.root = win

	// Start IPC watcher for Wayland mode
	e.startIPCWatcher()

	return newSaverMonitor(fynedesk.Instance().DelayScreenSaver)
}

type saverMonitor struct {
	widget.BaseWidget

	cb func()
}

func newSaverMonitor(cb func()) fyne.CanvasObject {
	s := &saverMonitor{cb: cb}
	s.ExtendBaseWidget(s)
	return s
}

func (s *saverMonitor) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// startIPCWatcher starts watching for window changes from compositor via IPC.
// It tries the UNIX socket first for instant push events. If the socket is
// unavailable it falls back to the legacy file-based polling.
func (e *embededWM) startIPCWatcher() {
	if !wlipc.IsWaylandSession() {
		return
	}
	e.ipcMode.Store(true)

	// Try socket connection
	client, err := wlipc.Connect()
	if err == nil {
		wlipc.SetDefaultClient(client) // upgrades all Request* functions too
		if err := client.Subscribe(
			wlipc.EventWindowsState,
			wlipc.EventDesktopState,
			wlipc.EventContextMenu,
			wlipc.EventLauncherRequest,
			wlipc.EventEmojiPicker,
			wlipc.EventClipboardShow,
			wlipc.EventClipboardHist,
			wlipc.EventScreenshot,
			wlipc.EventNotification,
			wlipc.EventCommandPalette,
			wlipc.EventSidebar,
			wlipc.EventOverview,
			wlipc.EventPanelHotspot,
		); err == nil {
			log.Println("[panel] Connected to compositor via socket IPC")
			e.setupSocketEventHandlers(client)
			return
		}
		// Subscribe failed — close and fall back
		client.Close()
		wlipc.SetDefaultClient(nil)
	}

	// Fallback: file-based polling
	log.Println("[panel] Socket unavailable, using file-based IPC polling")
	e.startFileIPCWatcher()
}

// setupSocketEventHandlers registers event handlers on the client and starts
// the read pump via ListenEvents. This ensures a single goroutine reads the
// socket, so Request() and event dispatch don't race on the scanner.
func (e *embededWM) setupSocketEventHandlers(client *wlipc.IPCClient) {
	done := make(chan struct{})

	client.OnEvent(wlipc.EventWindowsState, func(data json.RawMessage) {
		var state wlipc.WindowsState
		if json.Unmarshal(data, &state) == nil {
			fyne.Do(func() { e.updateFromIPC(&state) })
		}
	})
	client.OnEvent(wlipc.EventDesktopState, func(data json.RawMessage) {
		var state wlipc.DesktopState
		if json.Unmarshal(data, &state) == nil {
			fyne.Do(func() {
				if d, ok := fynedesk.Instance().(*desktop); ok {
					d.desk = state.Current
				}
				for _, m := range fynedesk.Instance().Modules() {
					if dn, ok := m.(notify.DesktopNotify); ok {
						dn.DesktopChangeNotify(state.Current)
					}
				}
			})
		}
	})
	client.OnEvent(wlipc.EventContextMenu, func(data json.RawMessage) {
		var req wlipc.ContextMenuRequest
		if json.Unmarshal(data, &req) == nil {
			fyne.Do(func() { e.showWindowContextMenu(&req) })
		}
	})
	client.OnEvent(wlipc.EventLauncherRequest, func(data json.RawMessage) {
		var req wlipc.LauncherRequest
		if json.Unmarshal(data, &req) == nil {
			fyne.Do(func() { ShowAppLauncherAt(req.CursorX, req.CursorY) })
		} else {
			fyne.Do(func() { ShowAppLauncher() })
		}
	})
	client.OnEvent(wlipc.EventEmojiPicker, func(data json.RawMessage) {
		var req wlipc.EmojiPickerRequest
		if json.Unmarshal(data, &req) == nil {
			fyne.Do(func() { ShowEmojiPicker(req.X, req.Y) })
		}
	})
	client.OnEvent(wlipc.EventClipboardShow, func(data json.RawMessage) {
		fyne.Do(func() { ShowClipboardManager() })
	})
	client.OnEvent(wlipc.EventClipboardHist, func(data json.RawMessage) {
		var hist wlipc.ClipboardHistory
		if json.Unmarshal(data, &hist) == nil {
			setClipboardHistory(hist.Entries)
		}
	})
	client.OnEvent(wlipc.EventScreenshot, func(data json.RawMessage) {
		var evt wlipc.ScreenshotEvent
		if json.Unmarshal(data, &evt) == nil {
			fyne.Do(func() {
				n := wm.NewNotification("Screenshot Saved", evt.FilePath)
				wm.SendNotification(n)
			})
		}
	})
	client.OnEvent(wlipc.EventCommandPalette, func(data json.RawMessage) {
		fyne.Do(func() { ShowCommandPalette() })
	})
	client.OnEvent(wlipc.EventSidebar, func(data json.RawMessage) {
		fyne.Do(func() { ToggleSidebar() })
	})
	client.OnEvent(wlipc.EventPanelHotspot, func(data json.RawMessage) {
		var evt wlipc.PanelHotspotEvent
		if json.Unmarshal(data, &evt) == nil {
			fyne.Do(func() {
				if d, ok := fynedesk.Instance().(*desktop); ok {
					d.setScreenAreaVisible(!evt.Raised)
				}
			})
		}
	})
	client.OnEvent(wlipc.EventNotification, func(data json.RawMessage) {
		var dn wlipc.DBusNotification
		if json.Unmarshal(data, &dn) == nil {
			fyne.Do(func() {
				n := wm.NewNotificationWithTimeout(dn.Title, dn.Body, dn.Timeout)
				wm.SendNotification(n)
			})
		}
	})

	// Start the read pump — single goroutine reads all messages and dispatches.
	client.ListenEvents(done)

	// Monitor for connection loss in a separate goroutine.
	go func() {
		// ListenEvents' internal goroutine will close pending channels when
		// the connection drops. We detect this by trying a no-op after the
		// scanner loop exits. Use a simple channel trick: wait for the done
		// channel to be written (which won't happen), or detect conn close.
		// Simpler: just poll until DefaultClient becomes nil or conn errors.
		<-done // will block forever unless explicitly closed
	}()
}

// startFileIPCWatcher starts legacy file-based IPC polling (100ms interval).
func (e *embededWM) startFileIPCWatcher() {
	// Close previous watchers if any
	if e.fileIPCDone != nil {
		close(e.fileIPCDone)
	}
	e.fileIPCDone = make(chan struct{})
	done := e.fileIPCDone

	wlipc.WatchWindowsState(func(state *wlipc.WindowsState) {
		fyne.Do(func() {
			e.updateFromIPC(state)
		})
	}, done)

	wlipc.WatchContextMenu(func(req *wlipc.ContextMenuRequest) {
		fyne.Do(func() {
			e.showWindowContextMenu(req)
		})
	}, done)

	wlipc.WatchLauncherRequest(func() {
		fyne.Do(func() {
			ShowAppLauncher()
		})
	}, done)

	wlipc.WatchEmojiPickerRequest(func(req *wlipc.EmojiPickerRequest) {
		fyne.Do(func() {
			ShowEmojiPicker(req.X, req.Y)
		})
	}, done)

	wlipc.WatchClipboardShowRequest(func() {
		fyne.Do(func() {
			ShowClipboardManager()
		})
	}, done)

	wlipc.WatchClipboardHistory(func(hist *wlipc.ClipboardHistory) {
		setClipboardHistory(hist.Entries)
	}, done)

	wlipc.WatchScreenshotEvent(func(evt *wlipc.ScreenshotEvent) {
		fyne.Do(func() {
			n := wm.NewNotification("Screenshot Saved", evt.FilePath)
			wm.SendNotification(n)
		})
	}, done)

	wlipc.WatchDBusNotification(func(dn *wlipc.DBusNotification) {
		fyne.Do(func() {
			n := wm.NewNotificationFull(dn.AppName, "", dn.Title, dn.Body, nil, dn.Timeout)
			wm.SendNotification(n)
		})
	}, done)

	wlipc.WatchDesktopState(func(state *wlipc.DesktopState) {
		fyne.Do(func() {
			if d, ok := fynedesk.Instance().(*desktop); ok {
				d.desk = state.Current
			}
			for _, m := range fynedesk.Instance().Modules() {
				if dn, ok := m.(notify.DesktopNotify); ok {
					dn.DesktopChangeNotify(state.Current)
				}
			}
		})
	}, done)
}

// showWindowContextMenu displays a context menu for a window from compositor
func (e *embededWM) showWindowContextMenu(req *wlipc.ContextMenuRequest) {
	// Find the ipcWindow by ID
	e.mu.RLock()
	var win fynedesk.Window
	for _, w := range e.windows {
		if iw, ok := w.(*ipcWindow); ok && iw.id == req.WindowID {
			win = w
			break
		}
	}
	e.mu.RUnlock()

	if win == nil {
		return
	}

	name := req.Title
	if len(name) > 25 {
		name = name[:25] + "..."
	}
	title := fyne.NewMenuItem(name, func() {})
	title.Disabled = true

	maxLabel := locale.T("menu.maximize")
	if win.Maximized() {
		maxLabel = locale.T("menu.restore")
	}

	iconifyLabel := locale.T("menu.minimize")
	if win.Iconic() {
		iconifyLabel = locale.T("menu.restore")
	}

	deskCount := fynedesk.Instance().Settings().DesktopCount()
	deskNames := fynedesk.Instance().Settings().DesktopNames()
	desks := make([]*fyne.MenuItem, deskCount)
	for i := range deskCount {
		deskID := i
		label := fmt.Sprintf("%s%d", locale.T("desktops.prefix"), i+1)
		if i < len(deskNames) && deskNames[i] != "" {
			label = deskNames[i]
		}
		desks[i] = fyne.NewMenuItem(label, func() {
			win.SetDesktop(deskID)
		})
		if win.Desktop() == deskID {
			desks[i].Checked = true
		}
	}
	deskMenu := fyne.NewMenuItem(locale.T("menu.moveToDesktop"), nil)
	deskMenu.ChildMenu = fyne.NewMenu("", desks...)

	pinLabel := locale.T("menu.pinAllDesktops")
	if win.Pinned() {
		pinLabel = locale.T("menu.unpinAllDesktops")
	}

	menu := fyne.NewMenu("",
		title,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(iconifyLabel, func() {
			if win.Iconic() {
				win.Uniconify()
			} else {
				win.Iconify()
			}
		}),
		fyne.NewMenuItem(maxLabel, func() {
			if win.Maximized() {
				win.Unmaximize()
			} else {
				win.Maximize()
			}
		}),
		fyne.NewMenuItemSeparator(),
		deskMenu,
		fyne.NewMenuItem(pinLabel, func() {
			if win.Pinned() {
				win.Unpin()
			} else {
				win.Pin()
			}
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(locale.T("menu.close"), func() {
			win.Close()
		}),
	)

	// Position: use cursor position from compositor, converting to panel canvas coordinates
	pos := fyne.NewPos(req.X, req.Y)
	if e.screenW > 0 {
		canvasSize := e.root.Canvas().Size()
		if canvasSize.Width > 0 && canvasSize.Height > 0 {
			pos.X = req.X / float32(e.screenW) * canvasSize.Width
			pos.Y = req.Y / float32(e.screenH) * canvasSize.Height
		}
	}

	fynedesk.Instance().ShowMenuAt(menu, pos)
}

func (e *embededWM) updateFromIPC(state *wlipc.WindowsState) {
	e.mu.Lock()

	// Build map of current windows by ID
	currentByID := make(map[string]fynedesk.Window)
	for _, w := range e.windows {
		if iw, ok := w.(*ipcWindow); ok {
			currentByID[iw.id] = w
		}
	}

	// Build new windows list from IPC state
	var newWindows []fynedesk.Window
	var stateChanged []fynedesk.Window
	var moved []fynedesk.Window
	newByID := make(map[string]bool)
	orderChanged := false

	for idx, info := range state.Windows {
		if info.IsPanel {
			continue
		}
		newByID[info.ID] = true
		if existing, ok := currentByID[info.ID]; ok {
			// Update existing window — track if state changed
			if iw, ok := existing.(*ipcWindow); ok {
				sc, mv := iw.update(info)
				if sc {
					stateChanged = append(stateChanged, existing)
				}
				if mv {
					moved = append(moved, existing)
				}
			}
			newWindows = append(newWindows, existing)
		} else {
			// New window
			newWindows = append(newWindows, newIPCWindow(info))
		}

		// Detect order changes by comparing index
		if !orderChanged && idx < len(e.windows) {
			if iw, ok := e.windows[idx].(*ipcWindow); ok {
				if iw.id != info.ID {
					orderChanged = true
				}
			}
		}
	}
	if len(newWindows) != len(e.windows) {
		orderChanged = true
	}

	// Find removed windows
	var removed []fynedesk.Window
	for id, w := range currentByID {
		if !newByID[id] {
			removed = append(removed, w)
		}
	}

	// Find added windows
	var added []fynedesk.Window
	for _, w := range newWindows {
		if iw, ok := w.(*ipcWindow); ok {
			if _, exists := currentByID[iw.id]; !exists {
				added = append(added, w)
			}
		}
	}

	e.windows = newWindows

	// Copy listeners to avoid holding lock during callbacks
	listeners := make([]fynedesk.StackListener, len(e.listeners))
	copy(listeners, e.listeners)
	e.mu.Unlock()

	// Notify listeners
	for _, l := range listeners {
		for _, w := range added {
			l.WindowAdded(w)
		}
		for _, w := range removed {
			l.WindowRemoved(w)
		}
		for _, w := range stateChanged {
			l.WindowStateChanged(w)
		}
		for _, w := range moved {
			l.WindowMoved(w)
		}
		if orderChanged {
			l.WindowOrderChanged()
		}
	}
}

// ipcWindow represents a window from the compositor via IPC
type ipcWindow struct {
	id            string
	title         string
	appID         string
	desktop       int
	focused       bool
	iconic        bool
	maximized     bool
	fullscreened  bool
	pinned        bool
	urgent        bool
	x, y          float32
	width, height float32
}

func newIPCWindow(info wlipc.WindowInfo) *ipcWindow {
	return &ipcWindow{
		id:           info.ID,
		title:        info.Title,
		appID:        info.AppID,
		desktop:      info.Desktop,
		focused:      info.Focused,
		iconic:       info.Iconic,
		maximized:    info.Maximized,
		fullscreened: info.Fullscreened,
		pinned:       info.Pinned,
		urgent:       info.Urgent,
		x:            info.X,
		y:            info.Y,
		width:        info.Width,
		height:       info.Height,
	}
}

// update updates the window state and returns (stateChanged, moved)
func (w *ipcWindow) update(info wlipc.WindowInfo) (bool, bool) {
	changed := w.title != info.Title ||
		w.focused != info.Focused ||
		w.iconic != info.Iconic ||
		w.maximized != info.Maximized ||
		w.fullscreened != info.Fullscreened ||
		w.pinned != info.Pinned ||
		w.urgent != info.Urgent ||
		w.desktop != info.Desktop

	moved := w.x != info.X || w.y != info.Y ||
		w.width != info.Width || w.height != info.Height

	w.title = info.Title
	w.appID = info.AppID
	w.desktop = info.Desktop
	w.focused = info.Focused
	w.iconic = info.Iconic
	w.maximized = info.Maximized
	w.fullscreened = info.Fullscreened
	w.pinned = info.Pinned
	w.urgent = info.Urgent
	w.x = info.X
	w.y = info.Y
	w.width = info.Width
	w.height = info.Height
	return changed, moved
}

// fynedesk.Window interface implementation
func (w *ipcWindow) Capture() image.Image    { return nil }
func (w *ipcWindow) Close()                  { wlipc.RequestWindowAction(w.id, "close") }
func (w *ipcWindow) Desktop() int            { return w.desktop }
func (w *ipcWindow) Focus()                  { wlipc.RequestWindowAction(w.id, "focus") }
func (w *ipcWindow) Focused() bool           { return w.focused }
func (w *ipcWindow) Fullscreen()             { wlipc.RequestWindowAction(w.id, "fullscreen") }
func (w *ipcWindow) Fullscreened() bool      { return w.fullscreened }
func (w *ipcWindow) Iconic() bool            { return w.iconic }
func (w *ipcWindow) Iconify()                { wlipc.RequestWindowAction(w.id, "iconify") }
func (w *ipcWindow) Maximize()               { wlipc.RequestWindowAction(w.id, "maximize") }
func (w *ipcWindow) Maximized() bool         { return w.maximized }
func (w *ipcWindow) Move(fyne.Position)      {}
func (w *ipcWindow) Parent() fynedesk.Window { return nil }
func (w *ipcWindow) Pin()                    { wlipc.RequestWindowAction(w.id, "pin") }
func (w *ipcWindow) Pinned() bool            { return w.pinned }
func (w *ipcWindow) Position() fyne.Position { return fyne.NewPos(w.x, w.y) }
func (w *ipcWindow) Properties() fynedesk.WindowProperties {
	return &ipcWindowProps{title: w.title, appID: w.appID}
}
func (w *ipcWindow) RaiseAbove(fynedesk.Window) {}
func (w *ipcWindow) RaiseToTop()                { wlipc.RequestWindowAction(w.id, "raise") }
func (w *ipcWindow) Resize(fyne.Size)           {}
func (w *ipcWindow) SetDesktop(d int)           { wlipc.RequestWindowActionWithDesktop(w.id, "set_desktop", d) }
func (w *ipcWindow) Size() fyne.Size            { return fyne.NewSize(w.width, w.height) }
func (w *ipcWindow) TopWindow() bool            { return w.focused }
func (w *ipcWindow) Unfullscreen()              { wlipc.RequestWindowAction(w.id, "unfullscreen") }
func (w *ipcWindow) Uniconify()                 { wlipc.RequestWindowAction(w.id, "uniconify") }
func (w *ipcWindow) Unmaximize()                { wlipc.RequestWindowAction(w.id, "unmaximize") }
func (w *ipcWindow) Unpin()                     { wlipc.RequestWindowAction(w.id, "unpin") }
func (w *ipcWindow) Urgent() bool               { return w.urgent }

type ipcWindowProps struct {
	title string
	appID string
}

func (p *ipcWindowProps) Class() []string {
	if p.appID != "" {
		return []string{p.appID}
	}
	return nil
}
func (p *ipcWindowProps) Command() string     { return "" }
func (p *ipcWindowProps) Decorated() bool     { return true }
func (p *ipcWindowProps) Icon() fyne.Resource { return nil }
func (p *ipcWindowProps) IconName() string    { return p.appID }
func (p *ipcWindowProps) SkipTaskbar() bool   { return false }
func (p *ipcWindowProps) Title() string       { return p.title }
