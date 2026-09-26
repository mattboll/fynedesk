package compositor

import (
	"log"
	"math"
	"strings"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/wlipc"
)

func (s *server) positionNewXwayWindow(v *xwayView) {
	winWidth := v.surface.Width()
	winHeight := v.surface.Height()

	// Small windows: honour an X11-requested position only when the client
	// actually set one. Fyne splash windows (panel overlays like the calendar
	// popup) arrive with v.x = v.y = 0 between map and OnSetTitle's overlay
	// reclassification — short-circuiting there pins them to layout (0,0),
	// which on a multi-monitor setup lands on whichever output starts at the
	// origin (often the secondary). Let them fall through to content-area
	// placement; positionOverlay will move them to the IPC-requested spot
	// once the title arrives.
	// (The view starts at a default position, so the client's own is read
	// from the X11 window.)
	isPopup := winWidth < 400 || winHeight < 300
	if reqX, reqY := v.surface.X(), v.surface.Y(); isPopup && (reqX != 0 || reqY != 0) {
		v.x, v.y = float64(reqX), float64(reqY)
		v.surface.Configure(int16(v.x), int16(v.y), uint16(winWidth), uint16(winHeight))
		return
	}

	// Place window on the output under the cursor
	v.x, v.y, winWidth, winHeight = s.cascadePlace(winWidth, winHeight, v.decorated)

	// Configure the XWayland surface position and restack to ensure input works
	log.Printf("[POSITION] XWayland positioned: class=%q at=(%.0f,%.0f) size=%dx%d\n",
		getXwaylandSurfaceClass(v.surface), v.x, v.y, winWidth, winHeight)
	v.surface.Configure(int16(v.x), int16(v.y), uint16(winWidth), uint16(winHeight))
	restackXwaylandSurfaceAbove(v.surface)
}

// cascadePlace returns where a new w×h window goes on the output under the
// cursor, a little below and right of the last one placed there, and the
// size it should take to fit.
func (s *server) cascadePlace(w, h int, decorated bool) (float64, float64, int, int) {
	cx, cy, cw, ch := s.contentBounds(s.getActiveOutputGeo())
	contentX, contentY := cx+20, cy+20
	contentWidth, contentHeight := cw-40, ch-40
	// Room for the titlebar, so that it does not go off-screen.
	if decorated {
		contentY += titlebarHeight
		contentHeight -= titlebarHeight
	}

	outName := ""
	if out := s.getActiveOutput(); out != nil {
		outName = out.output.Name()
	}
	if s.cascadeOffsets == nil {
		s.cascadeOffsets = map[string]int{}
	}
	offset := s.cascadeOffsets[outName] * cascadeStep
	// Back to the corner before windows go too far right or down.
	if offset > contentWidth/3 || offset > contentHeight/3 {
		offset = 0
	}
	s.cascadeOffsets[outName] = (offset/cascadeStep + 1) % maxCascade

	w, h = min(w, contentWidth), min(h, contentHeight)
	x, y := contentX+offset, contentY+offset
	if w > 0 && h > 0 {
		if x+w > contentX+contentWidth {
			x = contentX
		}
		if y+h > contentY+contentHeight {
			y = contentY
		}
	}
	return float64(x), float64(y), w, h
}

// closeXwayWindow closes an XWayland window
func (s *server) closeXwayWindow(v *xwayView) {
	s.ensureThumbXway(v) // capture thumbnail for close animation
	v.surface.Close()
}

func (s *server) closeOverlay() {
	if s.overlayXway != nil {
		s.overlayXway.surface.Close() // Send WM_DELETE_WINDOW so panel closes its window
		s.overlayXway.mapped = false
		setViewSceneEnabled(s.overlayXway.sceneTree, false)
	}
	s.overlayXway = nil
	s.overlayW = 0
	s.overlayH = 0
}

// positionOverlay positions an overlay XWayland surface using the IPC request,
// clamping to screen bounds using the actual surface dimensions.
func (s *server) positionOverlay(v *xwayView, surface wlr.XwaylandSurface) {
	// Check socket-based pending overlay first, fall back to file-based
	pos := s.pendingOverlay
	if pos != nil {
		s.pendingOverlay = nil
	} else {
		pos = s.readOverlayRequest()
	}
	if pos == nil {
		log.Printf("[OVERLAY] positionOverlay: no IPC position for title=%q", surface.Title())
		return
	}
	// Use requested size for clamping — the actual XWayland surface may be
	// larger than requested (e.g. HiDPI or Fyne internal scaling) which would
	// push the overlay further left/up than intended.
	surfW := int(pos.Width)
	surfH := int(pos.Height)
	realW := surface.Width()
	realH := surface.Height()
	log.Printf("[OVERLAY] positionOverlay: title=%q req=(%v,%v) size=%vx%v surfaceSize=%dx%d",
		surface.Title(), pos.X, pos.Y, pos.Width, pos.Height, realW, realH)

	x, y, ok := s.overlayPosition(pos)
	if !ok {
		return
	}
	log.Printf("[OVERLAY] final pos=(%v,%v)", x, y)

	v.x = x
	v.y = y
	s.overlayW = float64(surfW)
	s.overlayH = float64(surfH)
	// Configure uses actual surface size (or requested if surface not yet sized)
	confW, confH := realW, realH
	if confW <= 0 || confH <= 0 {
		confW, confH = surfW, surfH
	}
	surface.Configure(int16(x), int16(y), uint16(confW), uint16(confH))

	// Slide-up entrance animation for small overlay popups (menus, calendar).
	// Skip for large overlays (sidebar, notifications) that handle their own
	// slide-in animation via IPC repositioning — the compositor animation would
	// override the IPC positions and leave the window stuck at the wrong offset.
	isLargeOverlay := surfH > 400
	if !s.reduceMotion && !isLargeOverlay {
		s.animateXwayPos(v, x, y+30, x, y)
	} else {
		setXwayScenePos(v)
	}
}

// overlayPosition returns where an overlay goes: the requested position,
// in layout coordinates (the panel adds the offsets of other outputs), kept
// on the output that holds it with the requested size.
func (s *server) overlayPosition(req *wlipc.OverlayRequest) (float64, float64, bool) {
	x, y := float64(req.X), float64(req.Y)
	out := s.getOutputForPosition(x, y)
	if out == nil {
		out = s.primaryOutput()
	}
	if out == nil {
		return 0, 0, false
	}
	g := s.getOutputGeometry(out)
	x = math.Max(math.Min(x, float64(g.x+g.width)-float64(req.Width)), float64(g.x))
	y = math.Max(math.Min(y, float64(g.y+g.height)-float64(req.Height)), float64(g.y))
	return x, y, true
}

// repositionMappedOverlay moves an already-mapped overlay window to a new position.
// This is called when an overlay position request arrives via IPC for a window that
// has already been mapped (e.g. animation frames for sidebar/notification slide-in).
func (s *server) repositionMappedOverlay(req *wlipc.OverlayRequest) {
	if req == nil {
		return
	}
	// Find the mapped overlay window matching the title
	for _, v := range s.xwayViews {
		if !v.isOverlay || !v.mapped {
			continue
		}
		title := v.surface.Title()
		if title != req.Title && !strings.Contains(title, req.Title) {
			continue
		}
		x, y, ok := s.overlayPosition(req)
		if !ok {
			return
		}
		surfW, surfH := int(req.Width), int(req.Height)

		v.x = x
		v.y = y
		realW := v.surface.Width()
		realH := v.surface.Height()
		if realW <= 0 || realH <= 0 {
			realW, realH = surfW, surfH
		}
		v.surface.Configure(int16(x), int16(y), uint16(realW), uint16(realH))
		setXwayScenePos(v)
		return
	}
}

func (s *server) maximizeXwayWindow(v *xwayView) {
	oldX, oldY := v.x, v.y

	if v.maximized {
		// Restore
		log.Printf("[MAXIMIZE] Restoring XWay: title=%q saved=(%v,%v %dx%d)",
			v.surface.Title(), v.savedX, v.savedY, v.savedWidth, v.savedHeight)
		restX, restY, restW, restH := s.restoreGeometry(v.x, v.y, v.savedX, v.savedY, v.savedWidth, v.savedHeight)
		v.surface.Configure(int16(restX), int16(restY), uint16(restW), uint16(restH))
		v.maximized = false
		v.snapped = snapNone
		s.animateXwayPos(v, oldX, oldY, restX, restY)
	} else {
		// Maximize to the output the window is on
		s.setXwayZone(v, s.getOutputGeoForView(v.x, v.y), snapTop)
	}
}

func (s *server) minimizeXwayWindow(v *xwayView) {
	v.surface.SetMinimized(true)
	v.minimized = true
	v.mapped = false
	setViewSceneEnabled(v.sceneTree, false)
}

// restoreXwayWindow shows a minimized window again, on the current desktop,
// unless its application hid it meanwhile (it shows again when it maps).
func (s *server) restoreXwayWindow(v *xwayView) {
	v.surface.SetMinimized(false)
	v.minimized = false
	v.mapped = v.surfaceMapped
	if !v.mapped {
		return
	}
	if !v.pinned {
		v.desk = s.currentDesk
	}
	setViewSceneEnabled(v.sceneTree, true)
	s.focusXwayView(v)
}
