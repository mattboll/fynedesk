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
		geo := s.grabXdg.xdgToplevel.Base().GetGeometry()
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
		geo := v.xdgToplevel.Base().GetGeometry()
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
	if s.snapZone == snapNone {
		return
	}

	// Snap to the output the cursor is on
	outGeo := s.getActiveOutputGeo()

	// Content area (primary output reserves bar/widget space, secondary uses full area)
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if (s.grabXdg != nil && s.grabXdg.decorated) || (s.grabXway != nil && s.grabXway.decorated) {
		topMargin = titlebarHeight
	}
	contentX := cx
	contentY := cy + topMargin
	contentW := cw
	contentH := ch - topMargin

	var x, y float64
	var w, h int

	o := s.outerGap // gap from screen edges
	i := s.innerGap // gap between adjacent windows
	halfW := contentW / 2
	halfH := contentH / 2

	switch s.snapZone {
	case snapLeft:
		x, y = float64(contentX+o), float64(contentY+o)
		w, h = halfW-o-i/2, contentH-o*2
	case snapRight:
		x, y = float64(contentX+halfW+i/2), float64(contentY+o)
		w, h = contentW-halfW-o-i/2, contentH-o*2
	case snapTop:
		// Full maximize
		x, y = float64(contentX+o), float64(contentY+o)
		w, h = contentW-o*2, contentH-o*2
	case snapTopLeft:
		x, y = float64(contentX+o), float64(contentY+o)
		w, h = halfW-o-i/2, halfH-o-i/2
	case snapTopRight:
		x, y = float64(contentX+halfW+i/2), float64(contentY+o)
		w, h = contentW-halfW-o-i/2, halfH-o-i/2
	case snapBottomLeft:
		x, y = float64(contentX+o), float64(contentY+halfH+i/2)
		w, h = halfW-o-i/2, contentH-halfH-o-i/2
	case snapBottomRight:
		x, y = float64(contentX+halfW+i/2), float64(contentY+halfH+i/2)
		w, h = contentW-halfW-o-i/2, contentH-halfH-o-i/2
	}

	if s.grabXdg != nil {
		oldX, oldY := s.grabXdg.x, s.grabXdg.y
		s.grabXdg.configuredW = w
		s.grabXdg.configuredH = h
		s.grabXdg.xdgToplevel.SetSize(int32(w), int32(h))
		s.animateXdgPos(s.grabXdg, oldX, oldY, x, y)
	} else if s.grabXway != nil {
		oldX, oldY := s.grabXway.x, s.grabXway.y
		s.grabXway.surface.Configure(int16(x), int16(y), uint16(w), uint16(h))
		s.animateXwayPos(s.grabXway, oldX, oldY, x, y)
	}

	s.snapZone = snapNone
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
	oldX, oldY := v.x, v.y

	// Toggle: same snap zone → restore
	if v.snapped == zone {
		v.xdgToplevel.SetSize(int32(v.savedWidth), int32(v.savedHeight))
		v.configuredW = v.savedWidth
		v.configuredH = v.savedHeight
		v.snapped = snapNone
		v.maximized = false
		s.animateXdgPos(v, oldX, oldY, v.savedX, v.savedY)
		return
	}

	// Save geometry only from normal state
	if v.snapped == snapNone && !v.maximized {
		geo := v.xdgToplevel.Base().GetGeometry()
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = geo.Dx()
		v.savedHeight = geo.Dy()
	}

	v.maximized = false
	v.snapped = zone

	outGeo := s.getOutputGeoForView(v.x, v.y)
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if v.decorated {
		topMargin = titlebarHeight
	}

	o := s.outerGap
	i := s.innerGap
	halfW := cw / 2
	contentH := ch - topMargin

	var x, y float64
	var w, h int
	switch zone {
	case snapLeft:
		x = float64(cx + o)
		y = float64(cy + topMargin + o)
		w, h = halfW-o-i/2, contentH-o*2
	case snapRight:
		x = float64(cx + halfW + i/2)
		y = float64(cy + topMargin + o)
		w, h = cw-halfW-o-i/2, contentH-o*2
	}

	v.configuredW = w
	v.configuredH = h
	v.xdgToplevel.SetSize(int32(w), int32(h))
	s.animateXdgPos(v, oldX, oldY, x, y)
}

func (s *server) snapXwayWindow(v *xwayView, zone snapZone) {
	oldX, oldY := v.x, v.y

	// Toggle: same snap zone → restore
	if v.snapped == zone {
		v.surface.Configure(int16(v.savedX), int16(v.savedY), uint16(v.savedWidth), uint16(v.savedHeight))
		v.snapped = snapNone
		v.maximized = false
		s.animateXwayPos(v, oldX, oldY, v.savedX, v.savedY)
		return
	}

	// Save geometry only from normal state
	if v.snapped == snapNone && !v.maximized {
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = v.surface.Width()
		v.savedHeight = v.surface.Height()
	}

	v.maximized = false
	v.snapped = zone

	outGeo := s.getOutputGeoForView(v.x, v.y)
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if v.decorated {
		topMargin = titlebarHeight
	}

	o := s.outerGap
	i := s.innerGap
	halfW := cw / 2
	contentH := ch - topMargin

	var x, y float64
	var w, h int
	switch zone {
	case snapLeft:
		x = float64(cx + o)
		y = float64(cy + topMargin + o)
		w, h = halfW-o-i/2, contentH-o*2
	case snapRight:
		x = float64(cx + halfW + i/2)
		y = float64(cy + topMargin + o)
		w, h = cw-halfW-o-i/2, contentH-o*2
	}

	v.surface.Configure(int16(x), int16(y), uint16(w), uint16(h))
	s.animateXwayPos(v, oldX, oldY, x, y)
}

// reSnapAllWindows re-applies snap geometry for all snapped windows,
// used when gap settings change at runtime.
func (s *server) reSnapAllWindows() {
	for _, v := range s.xdgViews {
		if v.snapped != snapNone && v.mapped {
			zone := v.snapped
			v.snapped = snapNone // prevent toggle-restore in snapXdgWindow
			s.snapXdgWindow(v, zone)
		}
	}
	for _, v := range s.xwayViews {
		if v.snapped != snapNone && v.mapped {
			zone := v.snapped
			v.snapped = snapNone // prevent toggle-restore in snapXwayWindow
			s.snapXwayWindow(v, zone)
		}
	}
}
