package compositor

import "deedles.dev/wlr"

// beginGrabMove starts an interactive move operation for a window.
// If the window is maximized or fullscreen, the grab enters a "resistance"
// phase: the window stays in its filled state until the cursor moves past
// restoreDragThreshold, then restores under the cursor. This mirrors the
// drag-to-restore behaviour of macOS / GNOME.
func (s *server) beginGrabMove(xdgV *xdgView, xwayV *xwayView) {
	if s.isDragActive() {
		return // Don't start compositor grab during client DnD
	}
	s.grab = grabMove
	s.grabXdg = xdgV
	s.grabXway = xwayV
	s.grabX = s.cursor.X()
	s.grabY = s.cursor.Y()
	s.grabRestorePending = false
	if xdgV != nil {
		s.grabViewX = xdgV.x
		s.grabViewY = xdgV.y
		s.grabRestorePending = xdgV.fullscreen || xdgV.maximized
	} else if xwayV != nil {
		s.grabViewX = xwayV.x
		s.grabViewY = xwayV.y
		s.grabRestorePending = xwayV.fullscreen || xwayV.maximized
	}
	s.cursor.SetXCursor(s.cursorMgr, "grabbing")
}

// restoreForDrag exits the grabbed view's maximized/fullscreen state and
// repositions it under the cursor so the drag continues with the titlebar
// near the pointer. Grab anchors are reset so subsequent motion is computed
// from the restored geometry.
func (s *server) restoreForDrag() {
	var savedW int
	var titlebarOffset float64

	if s.grabXdg != nil {
		v := s.grabXdg
		if v.fullscreen {
			s.fullscreenXdgWindow(v, false)
		}
		if v.maximized {
			s.maximizeXdgWindow(v) // toggle: maximized -> restored
			v.anim.active = false  // cancel restore animation; we'll position under cursor
		}
		savedW = v.savedWidth
		if v.decorated {
			titlebarOffset = float64(titlebarHeight) / 2
		} else {
			titlebarOffset = float64(csdTitlebarHeight) / 2
		}
	} else if s.grabXway != nil {
		v := s.grabXway
		if v.fullscreen {
			s.fullscreenXwayWindow(v, false)
		}
		if v.maximized {
			s.maximizeXwayWindow(v) // toggle: maximized -> restored
			v.anim.active = false
		}
		savedW = v.savedWidth
		if v.decorated {
			titlebarOffset = float64(titlebarHeight) / 2
		} else {
			titlebarOffset = float64(csdTitlebarHeight) / 2
		}
	} else {
		return
	}

	if savedW <= 0 {
		savedW = 400 // Fallback when window opened in a filled state and never had a normal size
	}

	newX := s.cursor.X() - float64(savedW)/2
	newY := s.cursor.Y() - titlebarOffset

	if s.grabXdg != nil {
		s.grabXdg.x = newX
		s.grabXdg.y = newY
		setXdgScenePos(s.grabXdg)
	} else if s.grabXway != nil {
		s.grabXway.x = newX
		s.grabXway.y = newY
		s.grabXway.surface.Configure(int16(newX), int16(newY),
			uint16(s.grabXway.surface.Width()), uint16(s.grabXway.surface.Height()))
		setXwayScenePos(s.grabXway)
	}

	s.grabX = s.cursor.X()
	s.grabY = s.cursor.Y()
	s.grabViewX = newX
	s.grabViewY = newY
}

// endGrab ends any interactive grab operation
func (s *server) endGrab() {
	// Apply edge snap if we were moving and cursor is near screen edge
	if s.grab == grabMove && s.snapZone != snapNone {
		s.applySnap()
	}

	// Notify XDG client that resize is finished
	if s.grab == grabResize && s.grabXdg != nil {
		s.grabXdg.xdgToplevel.SetResizing(false)
	}

	s.grab = grabNone
	s.grabXdg = nil
	s.grabXway = nil
	s.grabRestorePending = false
	s.snapZone = snapNone
	s.cursor.SetXCursor(s.cursorMgr, "default")

	s.writeWindowsState()
}

// beginGrabResize starts an interactive resize operation for a window
func (s *server) beginGrabResize(xdgV *xdgView, xwayV *xwayView, edges wlr.Edges) {
	if s.isDragActive() {
		return // Don't start compositor grab during client DnD
	}
	s.grab = grabResize
	s.grabXdg = xdgV
	s.grabXway = xwayV
	s.grabEdges = edges
	s.grabX = s.cursor.X()
	s.grabY = s.cursor.Y()

	if xdgV != nil {
		xdgV.xdgToplevel.SetResizing(true)
		s.grabViewX = xdgV.x
		s.grabViewY = xdgV.y
		// Use geometry dimensions (content area without CSD shadows)
		// since SetSize operates on geometry, not surface size
		geo := xdgV.xdgToplevel.Base().GetGeometry()
		s.grabWidth = geo.Dx()
		s.grabHeight = geo.Dy()
	} else if xwayV != nil {
		s.grabViewX = xwayV.x
		s.grabViewY = xwayV.y
		s.grabWidth = xwayV.surface.Width()
		s.grabHeight = xwayV.surface.Height()
	}

	// Set appropriate resize cursor
	s.cursor.SetXCursor(s.cursorMgr, s.resizeCursorName(edges))
}

// resizeCursorName returns the cursor name for resize edges
func (s *server) resizeCursorName(edges wlr.Edges) string {
	switch edges {
	case wlr.EdgeTop:
		return "top_side"
	case wlr.EdgeBottom:
		return "bottom_side"
	case wlr.EdgeLeft:
		return "left_side"
	case wlr.EdgeRight:
		return "right_side"
	case wlr.EdgeTop | wlr.EdgeLeft:
		return "top_left_corner"
	case wlr.EdgeTop | wlr.EdgeRight:
		return "top_right_corner"
	case wlr.EdgeBottom | wlr.EdgeLeft:
		return "bottom_left_corner"
	case wlr.EdgeBottom | wlr.EdgeRight:
		return "bottom_right_corner"
	default:
		return "grabbing"
	}
}
