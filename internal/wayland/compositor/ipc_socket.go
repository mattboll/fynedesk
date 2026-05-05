package compositor

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

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

// handleSocketRequest dispatches socket requests to the appropriate handler.
// Requests are queued to the main thread via mainThreadActions.
func (s *server) handleSocketRequest(msg *wlipc.Message) (json.RawMessage, error) {
	// Security: while locked, only allow safe read-only or lock-related requests.
	if s.locked {
		switch msg.Name {
		case wlipc.ReqListWindows, wlipc.ReqGetDesktop, wlipc.ReqLock,
			wlipc.ReqSettingsChanged, wlipc.ReqKeyboardLayout:
			// Allowed while locked
		default:
			return nil, fmt.Errorf("rejected while locked: %s", msg.Name)
		}
	}

	switch msg.Name {
	case wlipc.ReqListWindows:
		// Synchronous: build and return current window state
		state := s.buildWindowsState()
		return json.Marshal(state)

	case wlipc.ReqGetDesktop:
		state := DesktopState{Current: s.currentDesk, NumDesks: s.numDesks, Names: s.desktopNames}
		return json.Marshal(state)

	case wlipc.ReqWindowAction:
		var req wlipc.WindowActionRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid window action: %w", err)
		}
		r := req
		if err := s.enqueueAction(func() { s.handleWindowAction(r) }); err != nil {
			return nil, err
		}
		return nil, nil

	case wlipc.ReqDesktopSwitch:
		var req wlipc.DesktopSwitchRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid desktop switch: %w", err)
		}
		desk := req.Desktop
		if err := s.enqueueAction(func() { s.switchDesk(desk) }); err != nil {
			return nil, err
		}
		return nil, nil

	case wlipc.ReqSettingsChanged:
		var sc wlipc.SettingsChanged
		if err := json.Unmarshal(msg.Data, &sc); err != nil {
			return nil, fmt.Errorf("invalid settings: %w", err)
		}
		if sc.Prefs != nil {
			prefs := sc.Prefs
			s.mainThreadActions <- func() { s.reloadSettingsFrom(prefs) }
		} else {
			s.mainThreadActions <- func() { s.reloadSettings() }
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqKeyboardLayout:
		var req wlipc.KeyboardLayoutRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid keyboard layout: %w", err)
		}
		idx := req.Index
		s.mainThreadActions <- func() {
			if idx >= 0 && idx < len(s.keyboardLayouts) {
				s.activeLayoutIndex = idx
				s.applyKeyboardLayout()
			}
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqEmojiPaste:
		var req wlipc.EmojiPasteRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid emoji paste: %w", err)
		}
		emoji := req.Emoji
		s.mainThreadActions <- func() {
			s.setClipboard(emoji)
			log.Printf("[emoji] clipboard set via socket to %q\n", emoji)
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqClipboardPaste:
		var req wlipc.ClipboardPasteRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid clipboard paste: %w", err)
		}
		text := req.Text
		s.mainThreadActions <- func() {
			s.setClipboard(text)
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqClipboardClear:
		s.mainThreadActions <- func() {
			s.clearClipboardHistory()
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqCompositorAction:
		var req struct {
			Action string `json:"action"`
		}
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

	case wlipc.ReqWindowPreview:
		var req struct {
			WindowID string `json:"window_id"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid window preview: %w", err)
		}
		result, err := s.handleWindowPreview(req.WindowID)
		if err != nil {
			log.Printf("[PREVIEW] request for %s failed: %v", req.WindowID, err)
		} else {
			log.Printf("[PREVIEW] request for %s succeeded (%d bytes)", req.WindowID, len(result))
		}
		return result, err

	case wlipc.ReqOverlay:
		var req wlipc.OverlayRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid overlay request: %w", err)
		}
		oReq := &overlayRequest{
			Title:  req.Title,
			X:      req.X,
			Y:      req.Y,
			Width:  req.Width,
			Height: req.Height,
		}
		s.pendingOverlay = oReq
		// If an overlay with this title is already mapped, reposition it now.
		// This handles animation frames and position updates after initial map.
		s.mainThreadActions <- func() {
			s.repositionMappedOverlay(oReq)
		}
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqLock:
		go s.lockScreen()
		return nil, nil

	case wlipc.ReqLogout:
		s.shuttingDown.Store(true)
		s.display.Terminate()
		return nil, nil

	case wlipc.ReqRestart:
		log.Println("Restart requested via socket IPC, terminating event loop")
		s.wantRestart.Store(true)
		s.shuttingDown.Store(true)
		s.saveSessionState()
		s.display.Terminate()
		return nil, nil

	case wlipc.ReqShutdown:
		log.Println("Shutdown requested via socket IPC")
		s.saveSessionState()
		go exec.Command("systemctl", "poweroff").Run()
		return nil, nil

	case wlipc.ReqHibernate:
		log.Println("Hibernate requested via socket IPC")
		go func() {
			s.lockScreen()
			exec.Command("systemctl", "hibernate").Run()
		}()
		return nil, nil

	case wlipc.ReqSuspend:
		log.Println("Suspend requested via socket IPC")
		go func() {
			s.lockScreen()
			exec.Command("systemctl", "suspend").Run()
		}()
		return nil, nil

	case "run-action":
		var req struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid run-action: %w", err)
		}
		action := req.Action
		s.mainThreadActions <- func() { s.dispatchAction(action) }
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqLayoutRequest:
		var req LayoutRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid layout request: %w", err)
		}
		log.Printf("[IPC] layout-request (socket): output=%q pos=%q ref=%q primary=%v\n",
			req.OutputName, req.Position, req.RelativeTo, req.Primary)
		r := req
		s.mainThreadActions <- func() { s.setOutputLayout(r) }
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqRaiseByTitle:
		var req wlipc.RaiseByTitleRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid raise-by-title: %w", err)
		}
		title := req.Title
		s.mainThreadActions <- func() { s.raiseByTitle(title) }
		s.triggerWakeup()
		return nil, nil

	case wlipc.ReqRaiseByClass:
		var req wlipc.RaiseByClassRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid raise-by-class: %w", err)
		}
		class := req.Class
		s.mainThreadActions <- func() { s.raiseByClass(class) }
		s.triggerWakeup()
		return nil, nil

	case "dump-scene":
		s.mainThreadActions <- func() {
			s.dumpSceneOrder("ipc-dump")
			s.dumpSceneLayers()
			s.debugViewAt(640, 360)
			s.debugViewAt(100, 100)
			s.debugViewAt(500, 300)
		}
		s.triggerWakeup()
		return nil, nil

	case "simulate-click":
		var req struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			return nil, fmt.Errorf("invalid simulate-click: %w", err)
		}
		s.mainThreadActions <- func() {
			s.simulateClick(req.X, req.Y)
		}
		s.triggerWakeup()
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown request: %s", msg.Name)
	}
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

// buildWindowsState creates a wlipc.WindowsState snapshot for socket responses.
func (s *server) buildWindowsState() wlipc.WindowsState {
	var windows []wlipc.WindowInfo
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
		Windows:   windows,
		Timestamp: time.Now().UnixNano(),
	}
}

// handleWindowPreview returns a base64-encoded PNG thumbnail of the specified window.
func (s *server) handleWindowPreview(windowID string) (json.RawMessage, error) {
	// Search xdg views
	for _, v := range s.xdgViews {
		if v.id == windowID {
			if v.cachedThumb != nil {
				return encodePreview(v.cachedThumb, windowID, v.xdgToplevel.Title())
			}
			s.schedulePreviewCapture(windowID)
			return nil, fmt.Errorf("preview not yet captured for window %s (scheduled)", windowID)
		}
	}
	// Search xwayland views
	for _, v := range s.xwayViews {
		if v.id == windowID {
			if v.cachedThumb != nil {
				return encodePreview(v.cachedThumb, windowID, v.surface.Title())
			}
			s.schedulePreviewCapture(windowID)
			return nil, fmt.Errorf("preview not yet captured for window %s (scheduled)", windowID)
		}
	}
	return nil, fmt.Errorf("no preview for window %s", windowID)
}

// schedulePreviewCapture queues a window ID for thumbnail capture on the next
// render frame. Called from the IPC goroutine when a taskbar hover preview is
// requested but no cached thumbnail exists yet.
func (s *server) schedulePreviewCapture(windowID string) {
	s.mainThreadActions <- func() {
		// Avoid duplicates
		for _, id := range s.previewPendingIDs {
			if id == windowID {
				return
			}
		}
		s.previewPendingIDs = append(s.previewPendingIDs, windowID)
		s.lastThumbCapture = time.Time{} // reset throttle
	}
	s.triggerWakeup()
}
