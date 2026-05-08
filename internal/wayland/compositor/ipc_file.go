package compositor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"fyshos.com/fynedesk/wlipc"
	"github.com/fsnotify/fsnotify"
)

// removeIPC removes an IPC file, logging unexpected errors (ignoring ENOENT).
func removeIPC(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove IPC file %s: %v\n", path, err)
	}
}

// ipcFilePaths bundles the paths the file-IPC watcher cares about so the
// processing function can receive them without a long parameter list.
type ipcFilePaths struct {
	configDir            string
	modeRequest          string
	scaleRequest         string
	desktopRequest       string
	windowAction         string
	logoutRequest        string
	restartRequest       string
	settingsChanged      string
	layoutRequest        string
	lockRequest          string
	kbLayoutRequest      string
	raiseByTitle         string
	raiseByClass         string
	vrrRequest           string
}

func (s *server) buildIPCFilePaths() ipcFilePaths {
	dir := s.getConfigDir()
	return ipcFilePaths{
		configDir:       dir,
		modeRequest:     filepath.Join(dir, "mode-request.json"),
		scaleRequest:    filepath.Join(dir, "scale-request.json"),
		desktopRequest:  filepath.Join(dir, "desktop-request.json"),
		windowAction:    filepath.Join(dir, "window-action-request.json"),
		logoutRequest:   filepath.Join(dir, "logout-request.json"),
		restartRequest:  filepath.Join(dir, "restart-request.json"),
		settingsChanged: filepath.Join(dir, "settings-changed.json"),
		layoutRequest:   filepath.Join(dir, "layout-request.json"),
		lockRequest:     filepath.Join(dir, "lock-request.json"),
		kbLayoutRequest: filepath.Join(dir, "keyboard-layout-request.json"),
		raiseByTitle:    filepath.Join(dir, "raise-by-title.json"),
		raiseByClass:    filepath.Join(dir, "raise-by-class.json"),
		vrrRequest:      filepath.Join(dir, "vrr-request.json"),
	}
}

func (s *server) watchModeRequests() {
	paths := s.buildIPCFilePaths()

	// Clean up any old request files from a previous session.
	// Errors are intentionally ignored: these files usually don't exist on a fresh start.
	for _, p := range []string{
		paths.modeRequest, paths.scaleRequest, paths.desktopRequest, paths.windowAction,
		paths.logoutRequest, paths.restartRequest, paths.settingsChanged, paths.layoutRequest,
		paths.lockRequest, paths.kbLayoutRequest, paths.raiseByTitle, paths.raiseByClass,
		paths.vrrRequest,
	} {
		os.Remove(p)
	}

	// Write initial desktop state
	s.writeDesktopState()

	// Per-file consecutive parse-failure tracker. Local to this goroutine —
	// all access goes through the same loop so no locking needed.
	const maxParseRetries = 5
	parseFailures := make(map[string]int)
	logAndRemoveIfStuck := func(path string, err error) {
		parseFailures[path]++
		if parseFailures[path] >= maxParseRetries {
			log.Printf("[IPC] giving up on %s after %d parse failures: %v",
				filepath.Base(path), parseFailures[path], err)
			removeIPC(path)
			delete(parseFailures, path)
		}
	}
	clearFailures := func(path string) { delete(parseFailures, path) }

	// Try fsnotify on the config dir. If it fails (e.g., permissions, ENOSPC
	// from inotify watch limit), fall back to polling — same 100ms cadence
	// as before so behavior is unchanged.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[IPC] fsnotify watcher creation failed (%v), falling back to polling", err)
		s.pollIPCFilesLoop(paths, parseFailures, logAndRemoveIfStuck, clearFailures)
		return
	}
	defer watcher.Close()
	if err := watcher.Add(paths.configDir); err != nil {
		log.Printf("[IPC] fsnotify watch on %s failed (%v), falling back to polling", paths.configDir, err)
		s.pollIPCFilesLoop(paths, parseFailures, logAndRemoveIfStuck, clearFailures)
		return
	}
	log.Printf("[IPC] watching %s for IPC requests via fsnotify", paths.configDir)

	// Initial sweep — picks up any files that arrived before the watch was
	// established (panel can race the compositor startup).
	if !s.processIPCFiles(paths, parseFailures, logAndRemoveIfStuck, clearFailures) {
		return
	}

	// Event-driven loop. Debounce: collapse bursts of CREATE+WRITE into a
	// single sweep within ~30 ms so we don't tear-read a half-written file.
	const debounceWindow = 30 * time.Millisecond
	const safetyPoll = 5 * time.Second // catches missed events (rare but possible)
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	debouncePending := false
	armDebounce := func() {
		if !debouncePending {
			debouncePending = true
			debounce.Reset(debounceWindow)
		}
	}

	safety := time.NewTicker(safetyPoll)
	defer safety.Stop()

	for {
		if s.shuttingDown.Load() {
			return
		}
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			// We only care about new content — Create/Write/Rename target.
			// Filter to files we actually handle so background noise (other
			// config writes by the panel) doesn't trigger unnecessary sweeps.
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			if !s.isIPCRequestFile(ev.Name, paths) {
				continue
			}
			armDebounce()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[IPC] fsnotify error: %v", err)
		case <-debounce.C:
			debouncePending = false
			if !s.processIPCFiles(paths, parseFailures, logAndRemoveIfStuck, clearFailures) {
				return
			}
		case <-safety.C:
			// Backstop: even if events were dropped or arrived before our
			// watch attached, this guarantees IPC requests are eventually
			// observed.
			if !s.processIPCFiles(paths, parseFailures, logAndRemoveIfStuck, clearFailures) {
				return
			}
		}
	}
}

// isIPCRequestFile reports whether path matches one of the IPC request files
// we handle, so we can ignore unrelated writes in the same dir.
func (s *server) isIPCRequestFile(path string, paths ipcFilePaths) bool {
	switch path {
	case paths.modeRequest, paths.scaleRequest, paths.desktopRequest,
		paths.windowAction, paths.logoutRequest, paths.restartRequest,
		paths.settingsChanged, paths.layoutRequest, paths.lockRequest,
		paths.kbLayoutRequest, paths.raiseByTitle, paths.raiseByClass,
		paths.vrrRequest:
		return true
	}
	return false
}

// pollIPCFilesLoop is the fallback for environments without inotify support.
// Same 100 ms cadence as the original implementation.
func (s *server) pollIPCFilesLoop(paths ipcFilePaths, parseFailures map[string]int,
	logAndRemoveIfStuck func(string, error), clearFailures func(string)) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if s.shuttingDown.Load() {
			return
		}
		if !s.processIPCFiles(paths, parseFailures, logAndRemoveIfStuck, clearFailures) {
			return
		}
	}
}

// processIPCFiles is the body of the original polling loop, refactored so it
// can be driven by either fsnotify events or a fallback ticker.
// Returns false if the caller should stop (shutdown / restart / logout
// terminated the event loop).
func (s *server) processIPCFiles(paths ipcFilePaths, parseFailures map[string]int,
	logAndRemoveIfStuck func(string, error), clearFailures func(string)) bool {
	locked := s.locked.Load()

	// Check for mode change request
	if data, err := os.ReadFile(paths.modeRequest); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.modeRequest)
				clearFailures(paths.modeRequest)
			} else {
				var req ModeRequest
				if perr := json.Unmarshal(data, &req); perr == nil {
					removeIPC(paths.modeRequest)
					clearFailures(paths.modeRequest)
					r := req
					s.mainThreadActions <- func() { s.setResolution(r) }
					s.triggerWakeup()
				} else {
					logAndRemoveIfStuck(paths.modeRequest, perr)
				}
			}
		}
	}

	// Check for scale change request
	if data, err := os.ReadFile(paths.scaleRequest); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.scaleRequest)
				clearFailures(paths.scaleRequest)
			} else {
				var req ScaleRequest
				if perr := json.Unmarshal(data, &req); perr == nil {
					removeIPC(paths.scaleRequest)
					clearFailures(paths.scaleRequest)
					r := req
					s.mainThreadActions <- func() { s.setOutputScale(r) }
					s.triggerWakeup()
				} else {
					logAndRemoveIfStuck(paths.scaleRequest, perr)
				}
			}
		}
	}

	// Check for desktop change request
	if data, err := os.ReadFile(paths.desktopRequest); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.desktopRequest)
				clearFailures(paths.desktopRequest)
			} else {
				var req DesktopRequest
				if perr := json.Unmarshal(data, &req); perr == nil {
					removeIPC(paths.desktopRequest)
					clearFailures(paths.desktopRequest)
					desk := req.Desktop
					s.mainThreadActions <- func() { s.switchDesk(desk) }
					s.triggerWakeup()
				} else {
					logAndRemoveIfStuck(paths.desktopRequest, perr)
				}
			}
		}
	}

	// Check for logout request — blocked while locked
	if _, err := os.Stat(paths.logoutRequest); err == nil {
		if locked {
			removeIPC(paths.logoutRequest)
			log.Println("[IPC] logout request rejected: screen is locked")
		} else {
			removeIPC(paths.logoutRequest)
			s.shuttingDown.Store(true)
			s.saveSessionState()
			s.display.Terminate()
			return false
		}
	}

	// Check for restart request — blocked while locked
	if _, err := os.Stat(paths.restartRequest); err == nil {
		if locked {
			removeIPC(paths.restartRequest)
			log.Println("[IPC] restart request rejected: screen is locked")
		} else {
			removeIPC(paths.restartRequest)
			log.Println("Restart requested via IPC, terminating event loop")
			s.wantRestart.Store(true)
			s.shuttingDown.Store(true)
			s.saveSessionState()
			s.display.Terminate()
			return false
		}
	}

	// Check for settings change notification (allowed while locked — needed for lock client config)
	if data, err := os.ReadFile(paths.settingsChanged); err == nil {
		removeIPC(paths.settingsChanged)
		var msg wlipc.SettingsChanged
		if jsonErr := json.Unmarshal(data, &msg); jsonErr == nil && msg.Prefs != nil {
			prefs := msg.Prefs
			s.mainThreadActions <- func() { s.reloadSettingsFrom(prefs) }
		} else {
			s.mainThreadActions <- func() { s.reloadSettings() }
		}
		s.triggerWakeup()
	}

	// Check for layout change request — blocked while locked
	if data, err := os.ReadFile(paths.layoutRequest); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.layoutRequest)
			} else {
				var req LayoutRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(paths.layoutRequest)
					log.Printf("[IPC] layout-request: output=%q pos=%q ref=%q primary=%v\n",
						req.OutputName, req.Position, req.RelativeTo, req.Primary)
					r := req
					s.mainThreadActions <- func() { s.setOutputLayout(r) }
					s.triggerWakeup()
				} else {
					log.Printf("[IPC] layout-request: unmarshal error: %v\n", err)
				}
			}
		}
	}

	// Check for lock request from panel (always allowed)
	if _, err := os.Stat(paths.lockRequest); err == nil {
		removeIPC(paths.lockRequest)
		go s.lockScreen()
	}

	// Check for keyboard layout switch request (allowed while locked — needed for lock screen input)
	if data, err := os.ReadFile(paths.kbLayoutRequest); err == nil {
		if len(data) > 0 {
			var req wlipc.KeyboardLayoutRequest
			if err := json.Unmarshal(data, &req); err == nil {
				removeIPC(paths.kbLayoutRequest)
				idx := req.Index
				s.mainThreadActions <- func() {
					if idx >= 0 && idx < len(s.keyboardLayouts) {
						s.activeLayoutIndex = idx
						s.applyKeyboardLayout()
					}
				}
				s.triggerWakeup()
			}
		}
	}

	// Check for window action request — blocked while locked
	if data, err := os.ReadFile(paths.windowAction); err == nil {
		if len(data) == 0 {
			// Partial write, defer to next sweep
			return true
		}
		if locked {
			removeIPC(paths.windowAction)
			clearFailures(paths.windowAction)
		} else {
			var req wlipc.WindowActionRequest
			if perr := json.Unmarshal(data, &req); perr == nil {
				removeIPC(paths.windowAction)
				clearFailures(paths.windowAction)
				r := req
				s.mainThreadActions <- func() { s.handleWindowAction(r) }
				s.triggerWakeup()
			} else {
				logAndRemoveIfStuck(paths.windowAction, perr)
			}
		}
	}

	// Check for raise-by-title request — blocked while locked
	if data, err := os.ReadFile(paths.raiseByTitle); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.raiseByTitle)
			} else {
				var req wlipc.RaiseByTitleRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(paths.raiseByTitle)
					title := req.Title
					s.mainThreadActions <- func() { s.raiseByTitle(title) }
					s.triggerWakeup()
				}
			}
		}
	}

	// Check for raise-by-class request — blocked while locked
	if data, err := os.ReadFile(paths.raiseByClass); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.raiseByClass)
			} else {
				var req wlipc.RaiseByClassRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(paths.raiseByClass)
					class := req.Class
					s.mainThreadActions <- func() { s.raiseByClass(class) }
					s.triggerWakeup()
				}
			}
		}
	}

	// Check for VRR (adaptive sync) toggle request — blocked while locked
	if data, err := os.ReadFile(paths.vrrRequest); err == nil {
		if len(data) > 0 {
			if locked {
				removeIPC(paths.vrrRequest)
			} else {
				var req VRRRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(paths.vrrRequest)
					r := req
					s.mainThreadActions <- func() { s.setOutputVRR(r) }
					s.triggerWakeup()
				}
			}
		}
	}

	return true
}

// handleWindowAction processes a window action request from the panel
func (s *server) handleWindowAction(req wlipc.WindowActionRequest) {
	// Find the window by ID
	xdgV, xwayV := s.findWindowByID(req.WindowID)
	if xdgV == nil && xwayV == nil {
		return
	}

	switch req.Action {
	case "focus":
		if xdgV != nil {
			if xdgV.minimized {
				s.restoreXdgWindow(xdgV)
			}
			s.focusXdgView(xdgV)
		} else if xwayV != nil {
			if xwayV.minimized {
				s.restoreXwayWindow(xwayV)
			}
			restackXwaylandSurfaceAbove(xwayV.surface)
			s.focusXwayView(xwayV)
		}
		s.writeWindowsState()
	case "close":
		if xdgV != nil {
			s.closeXdgWindow(xdgV)
		} else if xwayV != nil {
			s.closeXwayWindow(xwayV)
		}
	case "iconify":
		if xdgV != nil {
			s.minimizeXdgWindow(xdgV)
			s.focusTopmostOnDesk(s.currentDesk)
		} else if xwayV != nil {
			s.minimizeXwayWindow(xwayV)
			s.focusTopmostOnDesk(s.currentDesk)
		}
		s.writeWindowsState()
	case "uniconify":
		if xdgV != nil {
			s.restoreXdgWindow(xdgV)
		} else if xwayV != nil {
			s.restoreXwayWindow(xwayV)
		}
		s.writeWindowsState()
	case "maximize":
		if xdgV != nil {
			if !xdgV.maximized {
				s.maximizeXdgWindow(xdgV)
			}
		} else if xwayV != nil {
			if !xwayV.maximized {
				s.maximizeXwayWindow(xwayV)
			}
		}
		s.writeWindowsState()
	case "unmaximize":
		if xdgV != nil {
			if xdgV.maximized {
				s.maximizeXdgWindow(xdgV)
			}
		} else if xwayV != nil {
			if xwayV.maximized {
				s.maximizeXwayWindow(xwayV)
			}
		}
		s.writeWindowsState()
	case "fullscreen":
		if xdgV != nil {
			s.fullscreenXdgWindow(xdgV, true)
		} else if xwayV != nil {
			s.fullscreenXwayWindow(xwayV, true)
		}
		s.writeWindowsState()
	case "unfullscreen":
		if xdgV != nil {
			s.fullscreenXdgWindow(xdgV, false)
		} else if xwayV != nil {
			s.fullscreenXwayWindow(xwayV, false)
		}
		s.writeWindowsState()
	case "raise":
		// Restore the window if it was minimized (e.g. by show desktop)
		if xdgV != nil {
			if xdgV.minimized {
				s.restoreXdgWindow(xdgV)
			}
			s.focusXdgView(xdgV)
		} else if xwayV != nil {
			if xwayV.minimized {
				s.restoreXwayWindow(xwayV)
			}
			restackXwaylandSurfaceAbove(xwayV.surface)
			s.focusXwayView(xwayV)
		}
		s.writeWindowsState()
	}
}

func (s *server) requestLauncher() {
	configDir := s.getConfigDir()
	os.MkdirAll(configDir, 0700)

	req := wlipc.LauncherRequest{
		Timestamp: time.Now().UnixMilli(),
		CursorX:   float32(s.cursor.X()),
		CursorY:   float32(s.cursor.Y()),
	}
	data, _ := json.Marshal(req)
	writeAtomic(filepath.Join(configDir, "launcher-request.json"), data)

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventLauncherRequest, req)
	}

	// Focus the panel so it can receive keyboard input for the launcher.
	s.focusPanelKeyboard()
}

// requestEmojiPicker sends an emoji picker request to the panel via IPC and focuses it
func (s *server) requestEmojiPicker() {
	// Save the currently active window so we can refocus it after the picker closes
	s.preOverlayXdg = s.activeXdg
	s.preOverlayXway = s.activeXway
	if s.preOverlayXdg != nil {
		log.Printf("[emoji] saving pre-overlay: XDG %s\n", s.preOverlayXdg.id)
	} else if s.preOverlayXway != nil {
		log.Printf("[emoji] saving pre-overlay: XWay %s\n", s.preOverlayXway.id)
	} else {
		log.Printf("[emoji] no active window to save as pre-overlay\n")
	}

	configDir := s.getConfigDir()
	os.MkdirAll(configDir, 0700)

	emojiReq := struct {
		X         float64 `json:"x"`
		Y         float64 `json:"y"`
		Timestamp int64   `json:"timestamp"`
	}{
		X:         s.cursor.X(),
		Y:         s.cursor.Y(),
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(emojiReq)
	ipcPath := filepath.Join(configDir, "emoji-picker-request.json")
	if err := atomicWriteFile(ipcPath, data); err != nil {
		log.Printf("Error writing emoji picker IPC: %v\n", err)
	}

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventEmojiPicker, emojiReq)
	}

	// Focus the panel so it can process the request
	s.focusPanelKeyboard()
}

// refocusPreOverlayWindow restores focus to the window that was active before the overlay
func (s *server) refocusPreOverlayWindow() {
	activeXwayID := ""
	if s.activeXway != nil {
		activeXwayID = s.activeXway.id
	}
	activeXdgID := ""
	if s.activeXdg != nil {
		activeXdgID = s.activeXdg.id
	}
	preXwayID := ""
	if s.preOverlayXway != nil {
		preXwayID = s.preOverlayXway.id
	}
	preXdgID := ""
	if s.preOverlayXdg != nil {
		preXdgID = s.preOverlayXdg.id
	}
	log.Printf("[REFOCUS] activeXway=%s activeXdg=%s preOverlayXway=%s preOverlayXdg=%s",
		activeXwayID, activeXdgID, preXwayID, preXdgID)

	// If a new non-panel, non-overlay window was focused while the overlay
	// was open (e.g. Settings opened from the sidebar), don't override it.
	if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay &&
		s.activeXway != s.preOverlayXway {
		log.Printf("[REFOCUS] skip: new xway window %s focused during overlay", s.activeXway.id)
		s.preOverlayXdg = nil
		s.preOverlayXway = nil
		return
	}
	if s.activeXdg != nil && s.activeXdg != s.preOverlayXdg {
		log.Printf("[REFOCUS] skip: new xdg window %s focused during overlay", s.activeXdg.id)
		s.preOverlayXdg = nil
		s.preOverlayXway = nil
		return
	}

	if s.preOverlayXdg != nil && s.preOverlayXdg.mapped {
		log.Printf("[REFOCUS] restoring pre-overlay XDG: %s", s.preOverlayXdg.id)
		s.focusXdgView(s.preOverlayXdg)
	} else if s.preOverlayXway != nil && s.preOverlayXway.mapped && !s.preOverlayXway.isPanel {
		log.Printf("[REFOCUS] restoring pre-overlay XWay: %s", s.preOverlayXway.id)
		restackXwaylandSurfaceAbove(s.preOverlayXway.surface)
		s.focusXwayView(s.preOverlayXway)
	} else {
		log.Printf("[REFOCUS] fallback: focusTopmostOnDesk")
		s.focusTopmostOnDesk(s.currentDesk)
	}
	s.preOverlayXdg = nil
	s.preOverlayXway = nil
}

// handleEmojiPaste reads the emoji from the IPC file and sets the compositor-owned
// clipboard so it's available to all Wayland and XWayland clients.
func (s *server) handleEmojiPaste() {
	configDir := s.getConfigDir()
	pastePath := filepath.Join(configDir, "emoji-paste.json")

	data, err := os.ReadFile(pastePath)
	if err != nil {
		return
	}
	removeIPC(pastePath)

	var req wlipc.EmojiPasteRequest
	if err := json.Unmarshal(data, &req); err != nil || req.Emoji == "" {
		return
	}

	s.setClipboard(req.Emoji)
	log.Printf("[emoji] clipboard set to %q\n", req.Emoji)
}

// showWindowContextMenu writes a context menu request for the panel to display
func (s *server) showWindowContextMenu(xdgV *xdgView, xwayV *xwayView) {
	var windowID, title string
	if xdgV != nil {
		for i, v := range s.xdgViews {
			if v == xdgV {
				windowID = fmt.Sprintf("xdg-%d", i)
				break
			}
		}
		title = xdgV.xdgToplevel.Title()
	} else if xwayV != nil {
		for i, v := range s.xwayViews {
			if v == xwayV {
				windowID = fmt.Sprintf("xway-%d", i)
				break
			}
		}
		title = xwayV.surface.Title()
	}
	if windowID == "" {
		return
	}

	configDir := s.getConfigDir()
	reqPath := filepath.Join(configDir, "context-menu-request.json")
	ctxReq := struct {
		WindowID string  `json:"window_id"`
		Title    string  `json:"title"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
	}{
		WindowID: windowID,
		Title:    title,
		X:        s.cursor.X(),
		Y:        s.cursor.Y(),
	}
	data, _ := json.Marshal(ctxReq)
	writeAtomic(reqPath, data)

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventContextMenu, ctxReq)
	}
}

// requestCommandPalette sends a command palette request to the panel via IPC
func (s *server) requestCommandPalette() {
	ts := map[string]int64{"timestamp": time.Now().UnixMilli()}

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventCommandPalette, ts)
	}

	// File-based fallback
	configDir := s.getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, _ := json.Marshal(ts)
	writeAtomic(filepath.Join(configDir, "command-palette-request.json"), data)

	// Focus the panel so it can receive keyboard input
	s.focusPanelKeyboard()
}

// requestSidebar sends a sidebar toggle event to the panel via IPC.
func (s *server) requestSidebar() {
	ts := map[string]int64{"timestamp": time.Now().UnixMilli()}

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventSidebar, ts)
	}

	// Focus the panel so it can interact with the sidebar
	s.focusPanelKeyboard()
}
