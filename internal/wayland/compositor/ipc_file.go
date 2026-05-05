package compositor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// removeIPC removes an IPC file, logging unexpected errors (ignoring ENOENT).
func removeIPC(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to remove IPC file %s: %v\n", path, err)
	}
}

func (s *server) watchModeRequests() {
	configDir := s.getConfigDir()
	modeRequestPath := filepath.Join(configDir, "mode-request.json")
	scaleRequestPath := filepath.Join(configDir, "scale-request.json")
	desktopRequestPath := filepath.Join(configDir, "desktop-request.json")
	windowActionPath := filepath.Join(configDir, "window-action-request.json")
	logoutRequestPath := filepath.Join(configDir, "logout-request.json")
	restartRequestPath := filepath.Join(configDir, "restart-request.json")
	settingsChangedPath := filepath.Join(configDir, "settings-changed.json")
	layoutRequestPath := filepath.Join(configDir, "layout-request.json")
	lockRequestPath := filepath.Join(configDir, "lock-request.json")
	kbLayoutRequestPath := filepath.Join(configDir, "keyboard-layout-request.json")
	raiseByTitlePath := filepath.Join(configDir, "raise-by-title.json")
	raiseByClassPath := filepath.Join(configDir, "raise-by-class.json")
	vrrRequestPath := filepath.Join(configDir, "vrr-request.json")

	// Clean up any old request files from a previous session.
	// Errors are intentionally ignored: these files usually don't exist on a fresh start.
	os.Remove(modeRequestPath)
	os.Remove(scaleRequestPath)
	os.Remove(desktopRequestPath)
	os.Remove(windowActionPath)
	os.Remove(logoutRequestPath)
	os.Remove(restartRequestPath)
	os.Remove(settingsChangedPath)
	os.Remove(layoutRequestPath)
	os.Remove(lockRequestPath)
	os.Remove(kbLayoutRequestPath)
	os.Remove(raiseByTitlePath)
	os.Remove(raiseByClassPath)
	os.Remove(vrrRequestPath)

	// Write initial desktop state
	s.writeDesktopState()

	ticker := time.NewTicker(100 * time.Millisecond) // Faster polling for desktop switching
	defer ticker.Stop()

	for range ticker.C {
		if s.shuttingDown.Load() {
			return
		}
		locked := s.locked

		// Check for mode change request
		if data, err := os.ReadFile(modeRequestPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(modeRequestPath)
				} else {
					var req ModeRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(modeRequestPath)
						r := req
						s.mainThreadActions <- func() { s.setResolution(r) }
						s.triggerWakeup()
					}
				}
			}
		}

		// Check for scale change request
		if data, err := os.ReadFile(scaleRequestPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(scaleRequestPath)
				} else {
					var req ScaleRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(scaleRequestPath)
						r := req
						s.mainThreadActions <- func() { s.setOutputScale(r) }
						s.triggerWakeup()
					}
				}
			}
		}

		// Check for desktop change request
		if data, err := os.ReadFile(desktopRequestPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(desktopRequestPath)
				} else {
					var req DesktopRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(desktopRequestPath)
						desk := req.Desktop
						s.mainThreadActions <- func() { s.switchDesk(desk) }
						s.triggerWakeup()
					}
				}
			}
		}

		// Check for logout request — blocked while locked
		if _, err := os.Stat(logoutRequestPath); err == nil {
			if locked {
				removeIPC(logoutRequestPath)
				log.Println("[IPC] logout request rejected: screen is locked")
			} else {
				removeIPC(logoutRequestPath)
				s.shuttingDown.Store(true)
				s.saveSessionState()
				s.display.Terminate()
				return
			}
		}

		// Check for restart request — blocked while locked
		if _, err := os.Stat(restartRequestPath); err == nil {
			if locked {
				removeIPC(restartRequestPath)
				log.Println("[IPC] restart request rejected: screen is locked")
			} else {
				removeIPC(restartRequestPath)
				log.Println("Restart requested via IPC, exiting with code 5")
				os.Exit(5)
			}
		}

		// Check for settings change notification (allowed while locked — needed for lock client config)
		if data, err := os.ReadFile(settingsChangedPath); err == nil {
			removeIPC(settingsChangedPath)
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
		if data, err := os.ReadFile(layoutRequestPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(layoutRequestPath)
				} else {
					var req LayoutRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(layoutRequestPath)
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
		if _, err := os.Stat(lockRequestPath); err == nil {
			removeIPC(lockRequestPath)
			go s.lockScreen()
		}

		// Check for keyboard layout switch request (allowed while locked — needed for lock screen input)
		if data, err := os.ReadFile(kbLayoutRequestPath); err == nil {
			if len(data) > 0 {
				var req wlipc.KeyboardLayoutRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(kbLayoutRequestPath)
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
		if data, err := os.ReadFile(windowActionPath); err == nil {
			if len(data) == 0 {
				continue // Partial write, wait for next poll
			}
			if locked {
				removeIPC(windowActionPath)
			} else {
				var req wlipc.WindowActionRequest
				if err := json.Unmarshal(data, &req); err == nil {
					removeIPC(windowActionPath)
					r := req
					s.mainThreadActions <- func() { s.handleWindowAction(r) }
					s.triggerWakeup()
				}
				// Don't remove on parse failure — may be a partial write
			}
		}

		// Check for raise-by-title request — blocked while locked
		if data, err := os.ReadFile(raiseByTitlePath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(raiseByTitlePath)
				} else {
					var req wlipc.RaiseByTitleRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(raiseByTitlePath)
						title := req.Title
						s.mainThreadActions <- func() { s.raiseByTitle(title) }
						s.triggerWakeup()
					}
				}
			}
		}

		// Check for raise-by-class request — blocked while locked
		if data, err := os.ReadFile(raiseByClassPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(raiseByClassPath)
				} else {
					var req wlipc.RaiseByClassRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(raiseByClassPath)
						class := req.Class
						s.mainThreadActions <- func() { s.raiseByClass(class) }
						s.triggerWakeup()
					}
				}
			}
		}

		// Check for VRR (adaptive sync) toggle request — blocked while locked
		if data, err := os.ReadFile(vrrRequestPath); err == nil {
			if len(data) > 0 {
				if locked {
					removeIPC(vrrRequestPath)
				} else {
					var req VRRRequest
					if err := json.Unmarshal(data, &req); err == nil {
						removeIPC(vrrRequestPath)
						r := req
						s.mainThreadActions <- func() { s.setOutputVRR(r) }
						s.triggerWakeup()
					}
				}
			}
		}
	}
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
		// Clear show-desktop state when user explicitly raises a window
		s.showDesktopActive = false
		// Flush immediately so the panel sees the updated state without
		// waiting for the next render frame (avoids stale "minimized" icon).
		s.flushWindowsState()
	case "set_desktop":
		if xdgV != nil {
			xdgV.desk = req.Desktop
		} else if xwayV != nil {
			xwayV.desk = req.Desktop
		}
		s.writeWindowsState()
	case "pin":
		if xdgV != nil {
			xdgV.pinned = true
		} else if xwayV != nil {
			xwayV.pinned = true
		}
		s.writeWindowsState()
	case "unpin":
		if xdgV != nil {
			xdgV.pinned = false
		} else if xwayV != nil {
			xwayV.pinned = false
		}
		s.writeWindowsState()
	}
}

// requestLauncher sends a launcher request to the panel via IPC and focuses it
func (s *server) requestLauncher() {
	configDir := s.getConfigDir()
	os.MkdirAll(configDir, 0700)

	req := wlipc.LauncherRequest{
		Timestamp: time.Now().UnixMilli(),
		CursorX:   float32(s.cursor.X()),
		CursorY:   float32(s.cursor.Y()),
	}
	data, _ := json.Marshal(req)
	atomicWriteFile(filepath.Join(configDir, "launcher-request.json"), data)

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
	atomicWriteFile(reqPath, data)

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
	atomicWriteFile(filepath.Join(configDir, "command-palette-request.json"), data)

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
