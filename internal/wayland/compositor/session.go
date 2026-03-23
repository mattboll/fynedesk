package compositor

import (
	"log"
	"os/exec"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// saveSessionState captures the current window state for restore on next login.
// Called during clean shutdown (quit/logout).
func (s *server) saveSessionState() {
	var windows []wlipc.SessionWindow

	for _, v := range s.xdgViews {
		if !v.mapped {
			continue
		}
		appID := getXdgToplevelAppID(v.xdgToplevel)
		if appID == "" {
			continue
		}
		var vw, vh int
		if v.maximized && v.savedWidth > 0 {
			// Use saved (pre-maximize) geometry
			vw, vh = v.savedWidth, v.savedHeight
		} else if v.configuredW > 0 {
			vw, vh = v.configuredW, v.configuredH
		} else {
			state := v.xdgToplevel.Base().Surface().Current()
			vw, vh = state.Width(), state.Height()
		}
		var x, y float64
		if v.maximized && (v.savedX != 0 || v.savedY != 0) {
			x, y = v.savedX, v.savedY
		} else {
			x, y = v.x, v.y
		}
		windows = append(windows, wlipc.SessionWindow{
			AppID:     appID,
			Title:     v.xdgToplevel.Title(),
			Desktop:   v.desk,
			X:         x,
			Y:         y,
			Width:     vw,
			Height:    vh,
			Maximized: v.maximized,
			Minimized: v.minimized,
			Pinned:    v.pinned,
			Floating:  v.floating,
			FocusSeq:  v.focusSeq,
		})
	}

	for _, v := range s.xwayViews {
		if !v.mapped || v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		class := getXwaylandSurfaceClass(v.surface)
		if class == "" {
			continue
		}
		var vw, vh int
		if v.maximized && v.savedWidth > 0 {
			vw, vh = v.savedWidth, v.savedHeight
		} else {
			vw, vh = v.surface.Width(), v.surface.Height()
		}
		var x, y float64
		if v.maximized && (v.savedX != 0 || v.savedY != 0) {
			x, y = v.savedX, v.savedY
		} else {
			x, y = v.x, v.y
		}
		windows = append(windows, wlipc.SessionWindow{
			AppID:     class,
			Title:     v.surface.Title(),
			Desktop:   v.desk,
			X:         x,
			Y:         y,
			Width:     vw,
			Height:    vh,
			Maximized: v.maximized,
			Minimized: v.minimized,
			Pinned:    v.pinned,
			Floating:  v.floating,
			FocusSeq:  v.focusSeq,
		})
	}

	if len(windows) == 0 {
		log.Println("[SESSION] No windows to save")
		wlipc.ClearSessionState()
		return
	}

	state := &wlipc.SessionState{
		Version:        1,
		Timestamp:      time.Now().UnixMilli(),
		CurrentDesktop: s.currentDesk,
		Windows:        windows,
	}

	if err := wlipc.WriteSessionState(state); err != nil {
		log.Printf("[SESSION] Failed to save: %v\n", err)
		return
	}
	log.Printf("[SESSION] Saved %d windows (desktop=%d)\n", len(windows), s.currentDesk)
}

// restoreSession launches apps from the saved session state.
// Called after the panel is started (with a delay for window mapping).
func (s *server) restoreSession() {
	state, err := wlipc.ReadSessionState()
	if err != nil {
		log.Printf("[SESSION] No session to restore: %v\n", err)
		return
	}

	// Reject stale sessions (older than 24h = probably not from last logout)
	age := time.Since(time.UnixMilli(state.Timestamp))
	if age > 24*time.Hour {
		log.Printf("[SESSION] Session too old (%v), skipping restore\n", age)
		wlipc.ClearSessionState()
		return
	}

	log.Printf("[SESSION] Restoring %d windows from session (age=%v)\n", len(state.Windows), age.Round(time.Second))

	// Restore current desktop first
	if state.CurrentDesktop >= 0 && state.CurrentDesktop < s.numDesks {
		s.switchDesk(state.CurrentDesktop)
	}

	// Store session state for matching newly-mapped windows
	s.sessionMu.Lock()
	s.pendingSession = state.Windows
	s.sessionMu.Unlock()

	// Launch each unique app_id
	launched := make(map[string]bool)
	for _, w := range state.Windows {
		if launched[w.AppID] {
			continue
		}
		launched[w.AppID] = true

		appID := w.AppID
		cmd := exec.Command(appID)
		if err := cmd.Start(); err != nil {
			log.Printf("[SESSION] Failed to launch %q: %v\n", appID, err)
			continue
		}
		log.Printf("[SESSION] Launched %q (pid=%d)\n", appID, cmd.Process.Pid)
	}

	// Clear pending session after timeout (windows that didn't map)
	go func() {
		time.Sleep(15 * time.Second)
		s.sessionMu.Lock()
		remaining := len(s.pendingSession)
		if remaining > 0 {
			s.pendingSession = nil
		}
		s.sessionMu.Unlock()
		if remaining > 0 {
			log.Printf("[SESSION] Timeout: %d windows not restored\n", remaining)
		}
	}()

	// Clear saved state so it's not restored again on crash-restart
	wlipc.ClearSessionState()
}

// applySessionWindowXdg applies saved session state to an XDG view before positioning.
func (s *server) applySessionWindowXdg(v *xdgView, sw *wlipc.SessionWindow) {
	log.Printf("[SESSION] Restoring XDG %q: desk=%d pos=(%.0f,%.0f) size=%dx%d max=%v\n",
		sw.AppID, sw.Desktop, sw.X, sw.Y, sw.Width, sw.Height, sw.Maximized)

	if sw.Desktop >= 0 && sw.Desktop < s.numDesks {
		v.desk = sw.Desktop
	}
	v.pinned = sw.Pinned
	v.floating = sw.Floating

	// Set position (will be used by positionNewXdgWindow as a hint)
	if sw.X != 0 || sw.Y != 0 {
		v.x, v.y = sw.X, sw.Y
	}

	// Set maximize state — actual maximize happens after positioning
	if sw.Maximized {
		v.savedX, v.savedY = sw.X, sw.Y
		v.savedWidth, v.savedHeight = sw.Width, sw.Height
	}
}

// applySessionWindowXway applies saved session state to an XWayland view before positioning.
func (s *server) applySessionWindowXway(v *xwayView, sw *wlipc.SessionWindow) {
	log.Printf("[SESSION] Restoring XWayland %q: desk=%d pos=(%.0f,%.0f) size=%dx%d max=%v\n",
		sw.AppID, sw.Desktop, sw.X, sw.Y, sw.Width, sw.Height, sw.Maximized)

	if sw.Desktop >= 0 && sw.Desktop < s.numDesks {
		v.desk = sw.Desktop
	}
	v.pinned = sw.Pinned
	v.floating = sw.Floating

	// Set position
	if sw.X != 0 || sw.Y != 0 {
		v.x, v.y = sw.X, sw.Y
	}

	// Set maximize state
	if sw.Maximized {
		v.savedX, v.savedY = sw.X, sw.Y
		v.savedWidth, v.savedHeight = sw.Width, sw.Height
	}
}

// matchSessionWindow finds and removes a pending session window matching the given app_id.
// Returns nil if no match found.
func (s *server) matchSessionWindow(appID string) *wlipc.SessionWindow {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()

	if len(s.pendingSession) == 0 {
		return nil
	}

	for i := range s.pendingSession {
		if s.pendingSession[i].AppID == appID {
			// Save before removing from pending list
			w := s.pendingSession[i]
			s.pendingSession = append(s.pendingSession[:i], s.pendingSession[i+1:]...)
			return &w
		}
	}
	return nil
}
