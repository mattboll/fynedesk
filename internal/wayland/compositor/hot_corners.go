package compositor

import (
	"log"
	"time"
)

// Corner identifiers for hot corner configuration.
type cornerID int

const (
	cornerTopLeft cornerID = iota
	cornerTopRight
	cornerBottomLeft
	cornerBottomRight
	cornerCount // sentinel
)

// Hot corner actions (empty string = disabled).
const (
	hotCornerActionOverview    = "overview"
	hotCornerActionLauncher    = "launcher"
	hotCornerActionShowDesktop = "show_desktop"
)

const (
	hotCornerSize     = 3 // Pixel zone at each corner (tight to avoid conflict with close buttons)
	hotCornerCooldown = 500 * time.Millisecond // Minimum time between activations
)

// checkHotCorners detects if the cursor is in a hot corner zone and
// triggers the configured action instantly (with cooldown to prevent rapid re-fire).
func (s *server) checkHotCorners() {
	if s.switcherActive || s.overviewActive || s.locked || s.grab != grabNone {
		return
	}

	out := s.getActiveOutput()
	if out == nil {
		return
	}
	outGeo := s.getOutputGeometry(out)
	cx := s.cursor.X()
	cy := s.cursor.Y()

	ox := float64(outGeo.x)
	oy := float64(outGeo.y)
	ow := float64(outGeo.width)
	oh := float64(outGeo.height)
	sz := float64(hotCornerSize)

	corner := cornerID(-1)
	if cx <= ox+sz && cy <= oy+sz {
		corner = cornerTopLeft
	} else if cx >= ox+ow-sz && cy <= oy+sz {
		corner = cornerTopRight
	} else if cx <= ox+sz && cy >= oy+oh-sz {
		corner = cornerBottomLeft
	} else if cx >= ox+ow-sz && cy >= oy+oh-sz {
		corner = cornerBottomRight
	}

	if corner < 0 {
		s.activeHotCorner = -1
		return
	}

	action := s.hotCornerActions[corner]
	if action == "" {
		return
	}

	// Already fired for this corner visit
	if s.activeHotCorner == int(corner) {
		return
	}

	s.activeHotCorner = int(corner)

	if time.Since(s.lastHotCornerTime) < hotCornerCooldown {
		return
	}

	s.fireHotCorner(action)
}

// fireHotCorner executes the action for a hot corner.
// Leaves activeHotCorner set so the same visit cannot re-fire — the cursor
// must leave the corner zone (which resets activeHotCorner in checkHotCorners)
// before another fire is allowed.
func (s *server) fireHotCorner(action string) {
	s.lastHotCornerTime = time.Now()

	log.Printf("[HOTCORNER] Activated: %s\n", action)

	switch action {
	case hotCornerActionOverview:
		s.toggleOverview()
	case hotCornerActionLauncher:
		s.requestLauncher()
	case hotCornerActionShowDesktop:
		s.toggleShowDesktop()
	}
}


// initHotCorners sets up default hot corner actions.
func (s *server) initHotCorners() {
	s.hotCornerActions = [cornerCount]string{
		cornerTopLeft: hotCornerActionOverview, // Top-left → overview (GNOME-style)
		// Other corners disabled by default
	}
	s.activeHotCorner = -1
}

// toggleFocusMode minimizes all windows except the focused one, or restores them.
func (s *server) toggleFocusMode() {
	if s.focusModeActive {
		// Restore previously minimized windows
		for _, v := range s.xdgViews {
			if v.minimized && v.parent == nil && v.onDesk(s.currentDesk) {
				s.restoreXdgWindow(v)
			}
		}
		for _, v := range s.xwayViews {
			if v.minimized && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
				s.restoreXwayWindow(v)
			}
		}
		s.focusModeActive = false
		log.Println("[FOCUS] Focus mode deactivated")
		return
	}

	// Minimize all windows except the focused one
	for _, v := range s.xdgViews {
		if v.mapped && !v.minimized && v.parent == nil && v.onDesk(s.currentDesk) && v != s.activeXdg {
			s.minimizeXdgWindow(v)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.minimized && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) && v != s.activeXway {
			s.minimizeXwayWindow(v)
		}
	}
	s.focusModeActive = true
	log.Println("[FOCUS] Focus mode activated")
}

// toggleShowDesktop minimizes all windows or restores them.
func (s *server) toggleShowDesktop() {
	// Check if any window is visible (not minimized)
	anyVisible := false
	for _, v := range s.xdgViews {
		if v.mapped && !v.minimized && v.parent == nil && v.onDesk(s.currentDesk) {
			anyVisible = true
			break
		}
	}
	if !anyVisible {
		for _, v := range s.xwayViews {
			if v.mapped && !v.minimized && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
				anyVisible = true
				break
			}
		}
	}

	if anyVisible {
		// Minimize all
		for _, v := range s.xdgViews {
			if v.mapped && !v.minimized && v.parent == nil && v.onDesk(s.currentDesk) {
				s.minimizeXdgWindow(v)
			}
		}
		for _, v := range s.xwayViews {
			if v.mapped && !v.minimized && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
				s.minimizeXwayWindow(v)
			}
		}
		s.showDesktopActive = true
	} else if s.showDesktopActive {
		// Restore previously minimized windows
		for _, v := range s.xdgViews {
			if v.minimized && v.parent == nil && v.onDesk(s.currentDesk) {
				s.restoreXdgWindow(v)
			}
		}
		for _, v := range s.xwayViews {
			if v.minimized && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
				s.restoreXwayWindow(v)
			}
		}
		s.showDesktopActive = false
	}
}
