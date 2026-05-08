package compositor

import (
	"encoding/json"
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

