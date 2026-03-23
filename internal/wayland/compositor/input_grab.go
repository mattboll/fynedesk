package compositor

import "deedles.dev/wlr"

// beginGrabMove starts an interactive move operation for a window
func (s *server) beginGrabMove(xdgV *xdgView, xwayV *xwayView) {
	if s.isDragActive() {
		return // Don't start compositor grab during client DnD
	}
	s.grab = grabMove
	s.grabXdg = xdgV
	s.grabXway = xwayV
	s.grabX = s.cursor.X()
	s.grabY = s.cursor.Y()
	if xdgV != nil {
		s.grabViewX = xdgV.x
		s.grabViewY = xdgV.y
	} else if xwayV != nil {
		s.grabViewX = xwayV.x
		s.grabViewY = xwayV.y
	}
	s.cursor.SetXCursor(s.cursorMgr, "grabbing")
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
