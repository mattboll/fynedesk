package compositor

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fyshos.com/tyde/wlipc"
)

// useOwnSocketWhenNested gives a nested compositor a socket of its own, for
// itself and the programs it starts (the panel, tyde_wmctl), when the
// default one is served by the session it runs in.
func (s *server) useOwnSocketWhenNested() {
	if !s.nestedMode || os.Getenv(wlipc.SocketEnv) != "" || !wlipc.SocketServed() {
		return
	}
	path := filepath.Join(filepath.Dir(wlipc.SocketPath()), fmt.Sprintf("tyde-compositor-%d.sock", os.Getpid()))
	os.Setenv(wlipc.SocketEnv, path)
	log.Printf("[IPC] Nested: own socket %s\n", path)
}

// startSocketIPC starts the UNIX socket IPC server alongside the existing
// file-based IPC. Socket clients get real-time events via subscription.
func (s *server) startSocketIPC() {
	srv, err := wlipc.NewIPCServer(s.handleSocketRequest)
	if err != nil {
		log.Printf("[IPC] Socket server failed to start: %v (file-based IPC still active)\n", err)
		return
	}
	s.ipcServer = srv
	wlipc.SetDefaultServer(srv) // enables broadcasts from wlipc.Notify* functions
}

// socketHandler answers one socket request.
type socketHandler func(s *server, msg *wlipc.Message) (json.RawMessage, error)

// socketHandlers maps each socket request name to its handler.
var socketHandlers = map[string]socketHandler{
	wlipc.ReqListWindows:        (*server).socketListWindows,
	wlipc.ReqGetDesktop:         (*server).socketGetDesktop,
	wlipc.ReqWindowAction:       (*server).socketWindowAction,
	wlipc.ReqDesktopSwitch:      (*server).socketDesktopSwitch,
	wlipc.ReqSettingsChanged:    (*server).socketSettingsChanged,
	wlipc.ReqKeyboardLayout:     (*server).socketKeyboardLayout,
	wlipc.ReqEmojiPaste:         (*server).socketEmojiPaste,
	wlipc.ReqClipboardPaste:     (*server).socketClipboardPaste,
	wlipc.ReqClipboardClear:     (*server).socketClipboardClear,
	wlipc.ReqCompositorAction:   (*server).socketCompositorAction,
	wlipc.ReqWindowPreview:      (*server).socketWindowPreview,
	wlipc.ReqOverlay:            (*server).socketOverlay,
	wlipc.ReqLock:               (*server).socketLock,
	wlipc.ReqLogout:             (*server).socketLogout,
	wlipc.ReqRestart:            (*server).socketRestart,
	wlipc.ReqShutdown:           (*server).socketShutdown,
	wlipc.ReqHibernate:          (*server).socketHibernate,
	wlipc.ReqSuspend:            (*server).socketSuspend,
	wlipc.ReqLayoutRequest:      (*server).socketLayoutRequest,
	wlipc.ReqRaiseByTitle:       (*server).socketRaiseByTitle,
	wlipc.ReqRaiseByClass:       (*server).socketRaiseByClass,
	wlipc.ReqNotificationAction: (*server).socketNotificationAction,
	wlipc.ReqWindowAttention:    (*server).socketWindowAttention,
	wlipc.ReqDockIcons:          (*server).socketDockIcons,
}

// qaSocketHandlers drive the compositor like a user would (clicks,
// swipes) or dump its scene: for the tests only, as any local program could
// otherwise click into any window. TYDE_QA=1 turns them on.
var qaSocketHandlers = map[string]socketHandler{
	"dump-scene":      (*server).socketDumpScene,
	"simulate-click":  (*server).socketSimulateClick,
	"simulate-move":   (*server).socketSimulateMove,
	"simulate-swipe":  (*server).socketSimulateSwipe,
	"simulate-button": (*server).socketSimulateButton,
	"simulate-action": (*server).socketSimulateAction,
}

// qaEnabled reports whether the test requests are accepted.
var qaEnabled = os.Getenv("TYDE_QA") == "1"

// socketHandlerFor returns the handler of a request, if it is accepted.
func socketHandlerFor(name string) (socketHandler, bool) {
	if h, ok := socketHandlers[name]; ok {
		return h, true
	}
	if qaEnabled {
		h, ok := qaSocketHandlers[name]
		return h, ok
	}
	return nil, false
}

// handleSocketRequest dispatches socket requests to the appropriate handler.
// Requests are queued to the main thread via mainThreadActions.
func (s *server) handleSocketRequest(msg *wlipc.Message) (json.RawMessage, error) {
	// Security: while locked, only allow safe read-only or lock-related requests.
	if s.locked.Load() {
		switch msg.Name {
		case wlipc.ReqListWindows, wlipc.ReqGetDesktop, wlipc.ReqLock,
			wlipc.ReqSettingsChanged, wlipc.ReqKeyboardLayout:
			// Allowed while locked
		default:
			return nil, fmt.Errorf("rejected while locked: %s", msg.Name)
		}
	}

	if handler, ok := socketHandlerFor(msg.Name); ok {
		return handler(s, msg)
	}
	return nil, fmt.Errorf("unknown request: %s", msg.Name)
}

// socketListWindows returns the current window state.
func (s *server) socketListWindows(msg *wlipc.Message) (json.RawMessage, error) {
	// Synchronous: build and return current window state.
	// Runs on the main thread to avoid racing with view list mutations.
	state, err := runOnMainThread(s, func() wlipc.WindowsState {
		return s.buildWindowsState()
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(state)
}

// socketGetDesktop returns the current desktop, their count and names.
func (s *server) socketGetDesktop(msg *wlipc.Message) (json.RawMessage, error) {
	// Read currentDesk/numDesks/desktopNames on the main thread to
	// avoid racing with switchDesk and desktop-name updates.
	state, err := runOnMainThread(s, func() DesktopState {
		names := append([]string(nil), s.desktopNames...)
		return DesktopState{Current: s.currentDesk, NumDesks: s.numDesks, Names: names}
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(state)
}

// socketWindowAction queues a window action from the panel.
func (s *server) socketWindowAction(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.WindowActionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid window action: %w", err)
	}
	r := req
	if err := s.enqueueAction(func() { s.handleWindowAction(r) }); err != nil {
		return nil, err
	}
	return nil, nil
}

// socketDesktopSwitch queues a switch to another desktop.
func (s *server) socketDesktopSwitch(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.DesktopSwitchRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid desktop switch: %w", err)
	}
	desk := req.Desktop
	if err := s.enqueueAction(func() { s.switchDesk(desk) }); err != nil {
		return nil, err
	}
	return nil, nil
}

// socketSettingsChanged queues a reload of the settings.
func (s *server) socketSettingsChanged(msg *wlipc.Message) (json.RawMessage, error) {
	return nil, s.enqueueAction(func() { s.reloadSettings() })
}

// socketKeyboardLayout queues a switch to another keyboard layout.
func (s *server) socketKeyboardLayout(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.KeyboardLayoutRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid keyboard layout: %w", err)
	}
	idx := req.Index
	return nil, s.enqueueAction(func() {
		if idx >= 0 && idx < len(s.keyboardLayouts) {
			s.activeLayoutIndex = idx
			s.applyKeyboardLayout()
		}
	})
}

// socketEmojiPaste puts the picked emoji in the clipboard.
func (s *server) socketEmojiPaste(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.EmojiPasteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid emoji paste: %w", err)
	}
	emoji := req.Emoji
	return nil, s.enqueueAction(func() {
		s.setClipboard(emoji)
		log.Printf("[emoji] clipboard set via socket to %q\n", emoji)
	})
}

// socketClipboardPaste puts the given text in the clipboard.
func (s *server) socketClipboardPaste(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.ClipboardPasteRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid clipboard paste: %w", err)
	}
	text := req.Text
	return nil, s.enqueueAction(func() {
		s.setClipboard(text)
	})
}

// socketClipboardClear clears the clipboard history.
func (s *server) socketClipboardClear(msg *wlipc.Message) (json.RawMessage, error) {
	return nil, s.enqueueAction(func() {
		s.clearClipboardHistory()
	})
}

// socketCompositorAction queues a compositor action, such as a keybinding one.
func (s *server) socketCompositorAction(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.CompositorActionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid compositor action: %w", err)
	}
	action := req.Action
	// For switcher actions via IPC: auto-confirm since there's no modifier
	// key to release. Open → cycle → confirm in one shot.
	if action == wlipc.ActionSwitchAppNext || action == wlipc.ActionSwitchAppPrev {
		if err := s.enqueueAction(func() {
			if !s.switcherActive {
				s.openSwitcher()
				if action == wlipc.ActionSwitchAppPrev {
					s.switcherCyclePrev()
				}
				// openSwitcher already cycles to next
			} else {
				if action == wlipc.ActionSwitchAppNext {
					s.switcherCycleNext()
				} else {
					s.switcherCyclePrev()
				}
			}
			s.confirmSwitcher()
		}); err != nil {
			return nil, err
		}
	} else {
		if err := s.enqueueAction(func() { s.dispatchAction(action) }); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// socketWindowPreview returns a preview image of a window.
func (s *server) socketWindowPreview(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.WindowPreviewRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid window preview: %w", err)
	}
	// The window lists are read on the main thread; the picture is encoded
	// here (a thumbnail is replaced, never changed).
	res, err := runOnMainThread(s, func() windowThumb { return s.windowThumb(req.WindowID) })
	if err != nil {
		return nil, err
	}
	if !res.found {
		return nil, fmt.Errorf("no preview for window %s", req.WindowID)
	}
	if res.thumb == nil {
		log.Printf("[PREVIEW] request for %s: not captured yet (scheduled)", req.WindowID)
		return nil, fmt.Errorf("preview not yet captured for window %s (scheduled)", req.WindowID)
	}
	return encodePreview(res.thumb, req.WindowID, res.title)
}

// socketOverlay places the next or current panel overlay window.
func (s *server) socketOverlay(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.OverlayRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid overlay request: %w", err)
	}
	oReq := &req
	// Set pendingOverlay AND reposition any already-mapped overlay together
	// on the main thread — writing pendingOverlay from the IPC goroutine
	// races with map handlers that read it.
	if err := s.enqueueAction(func() {
		s.pendingOverlay = oReq
		s.repositionMappedOverlay(oReq)
	}); err != nil {
		return nil, err
	}
	return nil, nil
}

// socketLock locks the screen.
func (s *server) socketLock(msg *wlipc.Message) (json.RawMessage, error) {
	go s.lockScreen()
	return nil, nil
}

// socketLogout ends the session.
func (s *server) socketLogout(msg *wlipc.Message) (json.RawMessage, error) {
	s.requestEndSession(false)
	return nil, nil
}

// socketRestart ends the event loop so that the compositor restarts.
func (s *server) socketRestart(msg *wlipc.Message) (json.RawMessage, error) {
	log.Println("Restart requested via socket IPC, terminating event loop")
	s.requestEndSession(true)
	return nil, nil
}

// socketShutdown powers the machine off.
func (s *server) socketShutdown(msg *wlipc.Message) (json.RawMessage, error) {
	log.Println("Shutdown requested via socket IPC")
	_, _ = runOnMainThread(s, func() struct{} { s.saveSessionState(); return struct{}{} })
	go exec.Command(findBinary("systemctl"), "poweroff").Run()
	return nil, nil
}

// socketHibernate locks the screen then hibernates the machine.
func (s *server) socketHibernate(msg *wlipc.Message) (json.RawMessage, error) {
	log.Println("Hibernate requested via socket IPC")
	go func() {
		s.lockScreen()
		exec.Command(findBinary("systemctl"), "hibernate").Run()
	}()
	return nil, nil
}

// socketSuspend locks the screen then suspends the machine.
func (s *server) socketSuspend(msg *wlipc.Message) (json.RawMessage, error) {
	log.Println("Suspend requested via socket IPC")
	go func() {
		s.lockScreen()
		exec.Command(findBinary("systemctl"), "suspend").Run()
	}()
	return nil, nil
}

// socketLayoutRequest queues a change of the output layout.
func (s *server) socketLayoutRequest(msg *wlipc.Message) (json.RawMessage, error) {
	var req LayoutRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid layout request: %w", err)
	}
	log.Printf("[IPC] layout-request (socket): output=%q pos=%q ref=%q primary=%v\n",
		req.OutputName, req.Position, req.RelativeTo, req.Primary)
	r := req
	return nil, s.enqueueAction(func() { s.setOutputLayout(r) })
}

// socketRaiseByTitle queues raising the window with the given title.
func (s *server) socketRaiseByTitle(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.RaiseByTitleRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid raise-by-title: %w", err)
	}
	title := req.Title
	return nil, s.enqueueAction(func() { s.raiseByTitle(title) })
}

// socketRaiseByClass queues raising the window with the given class.
func (s *server) socketRaiseByClass(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.RaiseByClassRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid raise-by-class: %w", err)
	}
	class := req.Class
	return nil, s.enqueueAction(func() { s.raiseByClass(class) })
}

// socketNotificationAction emits the action chosen on a notification.
func (s *server) socketNotificationAction(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.NotificationActionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid notification action: %w", err)
	}
	// Emitting a D-Bus signal is thread-safe and independent of the render
	// loop, so do it directly rather than hopping to the main thread.
	s.notifDBus.emitAction(req.ID, req.ActionKey)
	return nil, nil
}

// socketWindowAttention queues setting or clearing the attention of a window.
func (s *server) socketWindowAttention(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.WindowAttentionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid window attention: %w", err)
	}
	_ = s.enqueueAction(func() { s.setWindowAttention(req.Title, req.On) })
	return nil, nil
}

// socketDockIcons records where the dock shows each window.
func (s *server) socketDockIcons(msg *wlipc.Message) (json.RawMessage, error) {
	var icons map[string]wlipc.DockIcon
	if err := json.Unmarshal(msg.Data, &icons); err != nil {
		return nil, fmt.Errorf("invalid dock icons: %w", err)
	}
	_ = s.enqueueAction(func() { s.dockIcons = icons })
	return nil, nil
}

// socketDumpScene logs the scene graph for debugging.
func (s *server) socketDumpScene(msg *wlipc.Message) (json.RawMessage, error) {
	return nil, s.enqueueAction(func() {
		s.dumpSceneOrder("ipc-dump")
		s.dumpSceneLayers()
		s.debugViewAt(640, 360)
		s.debugViewAt(100, 100)
		s.debugViewAt(500, 300)
	})
}

// socketSimulateAction runs an action as its key would: unlike
// compositor-action, the window switcher it opens stays open.
func (s *server) socketSimulateAction(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.CompositorActionRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid simulate-action: %w", err)
	}
	return nil, s.enqueueAction(func() { s.dispatchAction(req.Action) })
}

// socketSimulateClick queues a simulated pointer click.
func (s *server) socketSimulateClick(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.PointerRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid simulate-click: %w", err)
	}
	return nil, s.enqueueAction(func() {
		s.simulateClick(req.X, req.Y)
	})
}

// socketSimulateMove queues a simulated pointer motion.
func (s *server) socketSimulateMove(msg *wlipc.Message) (json.RawMessage, error) {
	var req wlipc.PointerRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid simulate-move: %w", err)
	}
	return nil, s.enqueueAction(func() {
		s.simulateMove(req.X, req.Y)
	})
}

// socketSimulateSwipe runs a simulated touchpad swipe.
func (s *server) socketSimulateSwipe(msg *wlipc.Message) (json.RawMessage, error) {
	// A touchpad swipe for QA: begin (unless it continues one), move by
	// (dx, dy) in steps, then end unless hold is set.
	var req struct {
		Fingers  uint32  `json:"fingers"`
		DX       float64 `json:"dx"`
		DY       float64 `json:"dy"`
		Steps    int     `json:"steps"`
		Hold     bool    `json:"hold"`
		Continue bool    `json:"continue"`
	}
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid simulate-swipe: %w", err)
	}
	go s.simulateSwipe(req.Fingers, req.DX, req.DY, max(req.Steps, 1), req.Hold, req.Continue)
	return nil, nil
}

// socketSimulateButton queues a simulated pointer button press or release.
func (s *server) socketSimulateButton(msg *wlipc.Message) (json.RawMessage, error) {
	var req struct {
		Pressed bool `json:"pressed"`
	}
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return nil, fmt.Errorf("invalid simulate-button: %w", err)
	}
	return nil, s.enqueueAction(func() {
		s.simulateButton(req.Pressed)
	})
}

// raiseByTitle finds a window by title and raises it to the top.
func (s *server) raiseByTitle(title string) {
	for _, v := range s.xwayViews {
		if v.mapped && v.surface.Title() == title {
			restackXwaylandSurfaceAbove(v.surface)
			s.focusXwayView(v)
			s.writeWindowsState()
			return
		}
	}
	for _, v := range s.xdgViews {
		if v.mapped && v.xdgToplevel.Title() == title {
			s.focusXdgView(v)
			s.writeWindowsState()
			return
		}
	}
}

// raiseByClass finds a window by WM_CLASS and raises it to the top.
// For mapped windows: raise and focus.
// For WM-minimized windows: fully restore (SetMinimized + scene enable + focus).
// For client-unmapped windows (e.g. Slack hidden to tray): send SetMinimized(false)
// X11 hint to trigger the app to remap, but don't force mapped=true (no buffer).
func (s *server) raiseByClass(class string) {
	classLower := strings.ToLower(class)
	log.Printf("[IPC] raiseByClass: looking for class=%q among %d xway + %d xdg views", class, len(s.xwayViews), len(s.xdgViews))

	for _, v := range s.xwayViews {
		if v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		xwayClass := strings.ToLower(getXwaylandSurfaceClass(v.surface))
		if xwayClass == classLower || strings.Contains(xwayClass, classLower) {
			log.Printf("[IPC] raiseByClass: found xway class=%q mapped=%v minimized=%v desk=%d", xwayClass, v.mapped, v.minimized, v.desk)
			if !v.pinned && v.desk != s.currentDesk {
				v.desk = s.currentDesk
			}
			if v.mapped {
				// Window is visible — just raise and focus
				restackXwaylandSurfaceAbove(v.surface)
				s.focusXwayView(v)
				s.writeWindowsState()
				return
			}
			if v.minimized {
				// WM-minimized — full restore
				s.restoreXwayWindow(v)
				s.writeWindowsState()
				return
			}
			// Client-unmapped (e.g. hidden to tray): skip and keep looking.
			// There may be a newer mapped view with the same class.
			log.Printf("[IPC] raiseByClass: skipping client-unmapped view %s, looking for mapped one", v.id)
			continue
		}
	}
	for _, v := range s.xdgViews {
		appID := strings.ToLower(getXdgToplevelAppID(v.xdgToplevel))
		if appID == classLower || strings.Contains(appID, classLower) {
			log.Printf("[IPC] raiseByClass: found xdg appID=%q mapped=%v minimized=%v desk=%d", appID, v.mapped, v.minimized, v.desk)
			if !v.pinned && v.desk != s.currentDesk {
				v.desk = s.currentDesk
			}
			if v.mapped {
				s.focusXdgView(v)
				s.writeWindowsState()
				return
			}
			if v.minimized {
				s.restoreXdgWindow(v)
				s.writeWindowsState()
				return
			}
			log.Printf("[IPC] raiseByClass: skipping client-unmapped xdg view, looking for mapped one")
			continue
		}
	}
	log.Printf("[IPC] raiseByClass: no raisable window found for class=%q", class)
}

// buildWindowsState snapshots the window list, for windows-state.json and
// socket responses. Main thread: wlroots views are not thread-safe.
func (s *server) buildWindowsState() wlipc.WindowsState {
	var windows []wlipc.WindowInfo

	// The windows shown, in z-order (topmost first). The pager iterates in
	// reverse, so wins[0] (topmost) is drawn last (on top).
	zOrder := s.getViewsInZOrder()
	seen := make(map[string]bool, len(zOrder))

	for _, entry := range zOrder {
		if entry.xdg != nil {
			v := entry.xdg
			if !v.mapped && !v.minimized {
				continue
			}
			seen[v.id] = true
			windows = append(windows, s.xdgWindowInfo(v))
		} else if entry.xway != nil {
			v := entry.xway
			if (!v.mapped && !v.minimized) || v.isPanel || v.isOverlay || v.overrideRedirect {
				continue
			}
			seen[v.id] = true
			windows = append(windows, s.xwayWindowInfo(v))
		}
	}

	// Then the windows not in the z-order list: minimized ones and those
	// on other desktops (their scene nodes are disabled). Their order does
	// not matter to the pager, which draws them below the visible ones.
	for _, v := range s.xdgViews {
		if seen[v.id] || (!v.mapped && !v.minimized) {
			continue
		}
		windows = append(windows, s.xdgWindowInfo(v))
	}
	for _, v := range s.xwayViews {
		if seen[v.id] || (!v.mapped && !v.minimized) || v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		windows = append(windows, s.xwayWindowInfo(v))
	}

	return wlipc.WindowsState{
		Version:   wlipc.IPCStateVersion,
		Windows:   windows,
		Timestamp: time.Now().UnixNano(),
	}
}

// windowThumb is what windowThumb found of a window.
type windowThumb struct {
	thumb *image.NRGBA
	title string
	found bool
}

// windowThumb returns the cached thumbnail of a window, and has one captured
// on the next frame when there is none yet. Main thread.
func (s *server) windowThumb(windowID string) windowThumb {
	for _, v := range s.xdgViews {
		if v.id == windowID {
			if v.cachedThumb == nil {
				s.schedulePreviewCapture(windowID)
			}
			return windowThumb{v.cachedThumb, v.xdgToplevel.Title(), true}
		}
	}
	for _, v := range s.xwayViews {
		if v.id == windowID {
			if v.cachedThumb == nil {
				s.schedulePreviewCapture(windowID)
			}
			return windowThumb{v.cachedThumb, v.surface.Title(), true}
		}
	}
	return windowThumb{}
}

// schedulePreviewCapture queues a window ID for thumbnail capture on the next
// render frame. Main thread (it used to send itself an action from there,
// which blocked for good once the queue was full).
func (s *server) schedulePreviewCapture(windowID string) {
	if slices.Contains(s.previewPendingIDs, windowID) {
		return
	}
	s.previewPendingIDs = append(s.previewPendingIDs, windowID)
	s.lastThumbCapture = time.Time{} // reset throttle
	s.triggerWakeup()
}
