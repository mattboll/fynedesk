package compositor

import (
	"log"
	"strings"

	"deedles.dev/wlr"
)

func (s *server) positionNewXwayWindow(v *xwayView) {
	winWidth := v.surface.Width()
	winHeight := v.surface.Height()

	// Small windows (popups, menus, tooltips) keep their X11-requested position
	isPopup := winWidth < 400 || winHeight < 300
	if isPopup {
		v.surface.Configure(int16(v.x), int16(v.y), uint16(winWidth), uint16(winHeight))
		return
	}

	// Place window on the output under the cursor
	outGeo := s.getActiveOutputGeo()
	cx, cy, cw, ch := s.contentBounds(outGeo)

	contentX := cx + 20
	contentY := cy + 20
	contentWidth := cw - 40
	contentHeight := ch - 40

	// Reserve space for SSD titlebar so it doesn't go off-screen
	if v.decorated {
		contentY += titlebarHeight
		contentHeight -= titlebarHeight
	}

	// Calculate cascade position (per-output)
	outName := s.getActiveOutput().output.Name()
	if s.cascadeOffsets == nil {
		s.cascadeOffsets = map[string]int{}
	}
	offset := s.cascadeOffsets[outName] * cascadeStep

	// Reset cascade if it would put window too far right/down
	if offset > contentWidth/3 || offset > contentHeight/3 {
		s.cascadeOffsets[outName] = 0
		offset = 0
	}

	// Constrain oversized windows to fit within content area
	if winWidth > contentWidth {
		winWidth = contentWidth
	}
	if winHeight > contentHeight {
		winHeight = contentHeight
	}

	if winWidth > 0 && winHeight > 0 {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
		// Ensure window fits in content area
		if int(v.x)+winWidth > contentX+contentWidth {
			v.x = float64(contentX)
		}
		if int(v.y)+winHeight > contentY+contentHeight {
			v.y = float64(contentY)
		}
	} else {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
	}

	// Increment cascade for next window
	s.cascadeOffsets[outName] = (s.cascadeOffsets[outName] + 1) % maxCascade

	// Configure the XWayland surface position and restack to ensure input works
	log.Printf("[POSITION] XWayland positioned: class=%q at=(%.0f,%.0f) size=%dx%d content=(%d,%d %dx%d)\n",
		getXwaylandSurfaceClass(v.surface), v.x, v.y, winWidth, winHeight, contentX, contentY, contentWidth, contentHeight)
	v.surface.Configure(int16(v.x), int16(v.y), uint16(winWidth), uint16(winHeight))
	restackXwaylandSurfaceAbove(v.surface)
}

// closeXwayWindow closes an XWayland window
func (s *server) closeXwayWindow(v *xwayView) {
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

	// Panel sends positions in absolute layout coordinates (including output
	// offsets for secondary monitors). Find the output containing the target
	// point and clamp to its bounds.
	x := float64(pos.X)
	y := float64(pos.Y)

	targetOut := s.getOutputForPosition(x, y)
	if targetOut == nil {
		targetOut = s.primaryOutput()
	}
	if targetOut == nil {
		return
	}
	outGeo := s.getOutputGeometry(targetOut)
	outRight := float64(outGeo.x + outGeo.width)
	outBottom := float64(outGeo.y + outGeo.height)
	if x+float64(surfW) > outRight {
		x = outRight - float64(surfW)
	}
	if y+float64(surfH) > outBottom {
		y = outBottom - float64(surfH)
	}
	if x < float64(outGeo.x) {
		x = float64(outGeo.x)
	}
	if y < float64(outGeo.y) {
		y = float64(outGeo.y)
	}

	log.Printf("[OVERLAY] final pos=(%v,%v) output=(%d,%d %dx%d)",
		x, y, outGeo.x, outGeo.y, outGeo.width, outGeo.height)

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

	// Slide-up entrance animation for overlay menus
	if !s.reduceMotion {
		s.animateXwayPos(v, x, y+30, x, y)
	} else {
		setXwayScenePos(v)
	}

}

// repositionMappedOverlay moves an already-mapped overlay window to a new position.
// This is called when an overlay position request arrives via IPC for a window that
// has already been mapped (e.g. animation frames for sidebar/notification slide-in).
func (s *server) repositionMappedOverlay(req *overlayRequest) {
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
		// Found it — reposition directly
		x := float64(req.X)
		y := float64(req.Y)
		surfW := int(req.Width)
		surfH := int(req.Height)

		// Clamp to target output bounds
		targetOut := s.getOutputForPosition(x, y)
		if targetOut == nil {
			targetOut = s.primaryOutput()
		}
		if targetOut == nil {
			return
		}
		outGeo := s.getOutputGeometry(targetOut)
		outRight := float64(outGeo.x + outGeo.width)
		outBottom := float64(outGeo.y + outGeo.height)
		if x+float64(surfW) > outRight {
			x = outRight - float64(surfW)
		}
		if y+float64(surfH) > outBottom {
			y = outBottom - float64(surfH)
		}
		if x < float64(outGeo.x) {
			x = float64(outGeo.x)
		}
		if y < float64(outGeo.y) {
			y = float64(outGeo.y)
		}

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
		v.surface.Configure(int16(v.savedX), int16(v.savedY), uint16(v.savedWidth), uint16(v.savedHeight))
		v.maximized = false
		v.snapped = snapNone
		s.animateXwayPos(v, oldX, oldY, v.savedX, v.savedY)
	} else {
		// Save current geometry only from normal state (preserve across snap→maximize)
		if v.snapped == snapNone {
			v.savedX = v.x
			v.savedY = v.y
			v.savedWidth = v.surface.Width()
			v.savedHeight = v.surface.Height()
		}
		v.snapped = snapNone

		// Maximize to the output the window is on
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)
		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		targetX := float64(cx)
		targetY := float64(cy + topMargin)
		v.surface.Configure(int16(targetX), int16(targetY), uint16(cw), uint16(ch-topMargin))
		v.maximized = true
		s.animateXwayPos(v, oldX, oldY, targetX, targetY)
	}
}

func (s *server) minimizeXwayWindow(v *xwayView) {
	v.surface.SetMinimized(true)
	v.minimized = true
	v.mapped = false
	setViewSceneEnabled(v.sceneTree, false)
}

func (s *server) restoreXwayWindow(v *xwayView) {
	v.surface.SetMinimized(false)
	v.minimized = false
	v.mapped = true
	setViewSceneEnabled(v.sceneTree, true)
	s.focusXwayView(v)
}
