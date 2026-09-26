package compositor

const (
	defaultInnerGap = 6 // Default pixel gap between adjacent snapped windows
	defaultOuterGap = 0 // Default pixel gap from screen edges (0 = flush like GNOME)
)

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// applyEdgeMagnet adjusts window position to snap to screen edges and other windows
func (s *server) applyEdgeMagnet(newX, newY float64) (float64, float64) {
	const magnetDist = 12.0 // Pixel threshold for magnetic snap

	// Get dragged window dimensions
	var winW, winH int
	var decorated bool
	if s.grabXdg != nil {
		geo := s.grabXdg.xdgToplevel.Base().Geometry()
		winW, winH = geo.Dx(), geo.Dy()
		decorated = s.grabXdg.decorated
	} else if s.grabXway != nil {
		winW = s.grabXway.surface.Width()
		winH = s.grabXway.surface.Height()
		decorated = s.grabXway.decorated
	} else {
		return newX, newY
	}

	// Window visual edges (v.x/v.y = geometry origin for XDG, surface origin for XWayland)
	wLeft := newX
	wRight := newX + float64(winW)
	wTop := newY
	wBottom := newY + float64(winH)
	if decorated {
		wTop = newY - float64(titlebarHeight)
	}

	// Collect target edges to snap against
	var hEdges []float64 // Horizontal edges (for snapping left/right)
	var vEdges []float64 // Vertical edges (for snapping top/bottom)

	// Screen edges (content bounds: primary reserves bar/widget space)
	outGeo := s.getActiveOutputGeo()
	cx, cy, cw, ch := s.contentBounds(outGeo)
	screenLeft := float64(cx)
	screenRight := float64(cx + cw)
	screenTop := float64(cy)
	screenBottom := float64(cy + ch)

	hEdges = append(hEdges, screenLeft, screenRight)
	vEdges = append(vEdges, screenTop, screenBottom)

	// Other visible windows on same desktop
	for _, v := range s.xdgViews {
		if v == s.grabXdg || !v.mapped || !v.onDesk(s.currentDesk) {
			continue
		}
		geo := v.xdgToplevel.Base().Geometry()
		vw, vh := float64(geo.Dx()), float64(geo.Dy())
		vt := v.y
		if v.decorated {
			vt = v.y - float64(titlebarHeight)
		}
		hEdges = append(hEdges, v.x, v.x+vw)
		vEdges = append(vEdges, vt, v.y+vh)
	}
	for _, v := range s.xwayViews {
		if v == s.grabXway || !v.mapped || v.isPanel || v.isOverlay || !v.onDesk(s.currentDesk) {
			continue
		}
		vw, vh := float64(v.surface.Width()), float64(v.surface.Height())
		vt := v.y
		if v.decorated {
			vt -= float64(titlebarHeight)
		}
		hEdges = append(hEdges, v.x, v.x+vw)
		vEdges = append(vEdges, vt, v.y+vh)
	}

	// Snap horizontal (window content left/right to any hEdge)
	bestDx := magnetDist + 1.0
	for _, edge := range hEdges {
		if d := abs(wLeft - edge); d < bestDx {
			bestDx = d
			newX = edge
		}
		if d := abs(wRight - edge); d < bestDx {
			bestDx = d
			newX = edge - float64(winW)
		}
	}

	// Snap vertical (window top/bottom to any vEdge)
	bestDy := magnetDist + 1.0
	for _, edge := range vEdges {
		if d := abs(wTop - edge); d < bestDy {
			bestDy = d
			if decorated {
				newY = edge + float64(titlebarHeight)
			} else {
				newY = edge
			}
		}
		if d := abs(wBottom - edge); d < bestDy {
			bestDy = d
			newY = edge - float64(winH)
		}
	}

	return newX, newY
}

// updateSnapPreview detects which snap zone the cursor is in during a move
func (s *server) updateSnapPreview() {
	const edgeThreshold = 20 // Pixels from screen edge to trigger snap

	x, y := s.cursor.X(), s.cursor.Y()

	// Detect edges relative to the output the cursor is on
	outGeo := s.getActiveOutputGeo()
	ox, oy := float64(outGeo.x), float64(outGeo.y)
	ow, oh := float64(outGeo.width), float64(outGeo.height)

	atLeft := x < ox+edgeThreshold
	atRight := x > ox+ow-edgeThreshold
	atTop := y < oy+edgeThreshold
	atBottom := y > oy+oh-edgeThreshold

	if atTop && atLeft {
		s.snapZone = snapTopLeft
	} else if atTop && atRight {
		s.snapZone = snapTopRight
	} else if atBottom && atLeft {
		s.snapZone = snapBottomLeft
	} else if atBottom && atRight {
		s.snapZone = snapBottomRight
	} else if atLeft {
		s.snapZone = snapLeft
	} else if atRight {
		s.snapZone = snapRight
	} else if atTop {
		s.snapZone = snapTop
	} else {
		s.snapZone = snapNone
	}
}

// applySnap applies the current snap zone to the grabbed window
func (s *server) applySnap() {
	zone := s.snapZone
	if zone == snapNone {
		return
	}
	s.snapZone = snapNone

	// Snap to the output the cursor is on
	outGeo := s.getActiveOutputGeo()
	if s.grabXdg != nil {
		s.setXdgZone(s.grabXdg, outGeo, zone)
	} else if s.grabXway != nil {
		s.setXwayZone(s.grabXway, outGeo, zone)
	}
}

// zoneRect returns where a window goes in a zone of the content area (cx,
// cy, cw, ch): the position and size of its client area, below its titlebar
// (topMargin), with outer gaps along the edges and inner gaps between the
// zones. snapTop is the whole area, without gaps: the window is maximized.
func zoneRect(cx, cy, cw, ch int, zone snapZone, topMargin, outer, inner int) (x, y float64, w, h int) {
	if zone == snapTop {
		return float64(cx), float64(cy + topMargin), cw, ch - topMargin
	}
	o, i := outer, inner
	top := cy + topMargin
	contentH := ch - topMargin
	halfW, halfH := cw/2, contentH/2
	left, right := cx+o, cx+halfW+i/2
	leftW, rightW := halfW-o-i/2, cw-halfW-o-i/2
	upperH, lowerY, lowerH := halfH-o-i/2, top+halfH+i/2, contentH-halfH-o-i/2
	switch zone {
	case snapLeft:
		return float64(left), float64(top + o), leftW, contentH - o*2
	case snapRight:
		return float64(right), float64(top + o), rightW, contentH - o*2
	case snapTopLeft:
		return float64(left), float64(top + o), leftW, upperH
	case snapTopRight:
		return float64(right), float64(top + o), rightW, upperH
	case snapBottomLeft:
		return float64(left), float64(lowerY), leftW, lowerH
	case snapBottomRight:
		return float64(right), float64(lowerY), rightW, lowerH
	}
	return float64(cx), float64(top), cw, contentH
}

// zoneTarget returns where a window goes in a zone of an output.
func (s *server) zoneTarget(outGeo outputGeometry, zone snapZone, decorated bool) (float64, float64, int, int) {
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if decorated {
		topMargin = titlebarHeight
	}
	return zoneRect(cx, cy, cw, ch, zone, topMargin, s.outerGap, s.innerGap)
}

// setXdgZone puts a window in a zone of an output: maximized for snapTop,
// snapped otherwise. The geometry it had in its normal state is kept for
// when it is restored.
func (s *server) setXdgZone(v *xdgView, outGeo outputGeometry, zone snapZone) {
	oldX, oldY := v.x, v.y
	if v.snapped == snapNone && !v.maximized {
		geo := v.xdgToplevel.Base().Geometry()
		v.savedX, v.savedY = v.x, v.y
		v.savedWidth, v.savedHeight = geo.Dx(), geo.Dy()
	}
	x, y, w, h := s.zoneTarget(outGeo, zone, v.decorated)
	v.maximized = zone == snapTop
	v.snapped = snapNone
	if !v.maximized {
		v.snapped = zone
	}
	v.xdgToplevel.SetMaximized(v.maximized)
	v.configuredW, v.configuredH = w, h
	v.xdgToplevel.SetSize(int32(w), int32(h))
	s.animateXdgPos(v, oldX, oldY, x, y)
}

// setXwayZone puts an XWayland window in a zone of an output (see setXdgZone).
func (s *server) setXwayZone(v *xwayView, outGeo outputGeometry, zone snapZone) {
	oldX, oldY := v.x, v.y
	if v.snapped == snapNone && !v.maximized {
		v.savedX, v.savedY = v.x, v.y
		v.savedWidth, v.savedHeight = v.surface.Width(), v.surface.Height()
	}
	x, y, w, h := s.zoneTarget(outGeo, zone, v.decorated)
	v.maximized = zone == snapTop
	v.snapped = snapNone
	if !v.maximized {
		v.snapped = zone
	}
	v.surface.Configure(int16(x), int16(y), uint16(w), uint16(h))
	s.animateXwayPos(v, oldX, oldY, x, y)
}

// snapActiveWindow snaps the active window to the given zone (left/right).
// Behaviour follows GNOME conventions:
//   - Normal → snap: save geometry, snap to half
//   - Same zone again → restore to saved geometry
//   - Other zone → re-snap (keep saved geometry)
//   - Maximized → snap: unmaximize, snap (keep saved geometry)
func (s *server) snapActiveWindow(zone snapZone) {
	if s.activeXdg != nil {
		s.snapXdgWindow(s.activeXdg, zone)
	} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
		s.snapXwayWindow(s.activeXway, zone)
	}
}

func (s *server) snapXdgWindow(v *xdgView, zone snapZone) {
	// Toggle: same snap zone → restore
	if v.snapped == zone {
		oldX, oldY := v.x, v.y
		s.unsnapXdg(v)
		s.animateXdgPos(v, oldX, oldY, v.savedX, v.savedY)
		return
	}
	s.setXdgZone(v, s.getOutputGeoForView(v.x, v.y), zone)
}

func (s *server) snapXwayWindow(v *xwayView, zone snapZone) {
	// Toggle: same snap zone → restore
	if v.snapped == zone {
		oldX, oldY := v.x, v.y
		s.unsnapXway(v, v.savedX, v.savedY)
		s.animateXwayPos(v, oldX, oldY, v.savedX, v.savedY)
		return
	}
	s.setXwayZone(v, s.getOutputGeoForView(v.x, v.y), zone)
}

// unsnapXdg gives a snapped window its size of before back; its position is
// left to the caller.
func (s *server) unsnapXdg(v *xdgView) {
	v.xdgToplevel.SetSize(int32(v.savedWidth), int32(v.savedHeight))
	v.configuredW = v.savedWidth
	v.configuredH = v.savedHeight
	v.snapped = snapNone
	v.maximized = false
}

// unsnapXway gives a snapped XWayland window its size of before back, at
// (x, y).
func (s *server) unsnapXway(v *xwayView, x, y float64) {
	v.surface.Configure(int16(x), int16(y), uint16(v.savedWidth), uint16(v.savedHeight))
	v.snapped = snapNone
	v.maximized = false
}

// reSnapAllWindows re-applies snap geometry for all snapped windows, when
// the gaps or the outputs change. The zone is set again as it is: the
// geometry kept for the restore stays the one of before the snap.
func (s *server) reSnapAllWindows() {
	for _, v := range s.xdgViews {
		if v.snapped != snapNone && v.mapped {
			s.setXdgZone(v, s.getOutputGeoForView(v.x, v.y), v.snapped)
		}
	}
	for _, v := range s.xwayViews {
		if v.snapped != snapNone && v.mapped {
			s.setXwayZone(v, s.getOutputGeoForView(v.x, v.y), v.snapped)
		}
	}
}
