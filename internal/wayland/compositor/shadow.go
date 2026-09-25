package compositor

import (
	"image"
	"image/color"
	"unsafe"
)

// Soft shadows under the windows Tyde decorates (the others draw their
// own): a light halo of black, a little lower than the window, fainter under
// the windows without the focus. Windows that fill a screen or a zone of it
// (maximized, fullscreen, snapped) have none: they touch the edges. A shadow
// never falls outside the area of the windows, on the dock or the widget
// panel, and it bends with the window that wobbles.
var shadowStyle = haloStyle{color: color.NRGBA{A: 0xFF}, radius: 16, strength: 0.3}

const (
	shadowOffsetY         = 4   // the light comes from above: less shadow at the top
	shadowInactiveOpacity = 0.4 // of the shadow of a window without the focus
)

// shadowGeometry is where a view's shadow was laid, to lay it again when
// the view moves.
type shadowGeometry struct {
	width, height int
	opacity       float32
	treeX, treeY  int  // where the view tree was
	cut           bool // part of the shadow fell outside the windows' area
}

// shadowWanted reports whether a view has a shadow.
func (s *server) shadowWanted(view any) bool {
	if !s.windowShadows {
		return false
	}
	switch v := view.(type) {
	case *xdgView:
		return v.decorated && !v.maximized && !v.fullscreen && v.snapped == snapNone
	case *xwayView:
		return v.decorated && !v.maximized && !v.fullscreen && v.snapped == snapNone
	}
	return false
}

// viewTreeCorner returns where a view's tree is in the layout, and its
// window's extent (with its titlebar).
func viewTreeCorner(view any) (x, y, w, h int) {
	switch v := view.(type) {
	case *xdgView:
		x, y = int(v.x), int(v.y)
		w, h = xdgDecoSize(v)
		if v.decorated && !v.fullscreen {
			y -= titlebarHeight
			h += titlebarHeight
		}
	case *xwayView:
		x, y = int(v.x), int(v.y)
		w, h = xwayDecoSize(v)
		if v.decorated && !v.fullscreen {
			y -= titlebarHeight
			h += titlebarHeight
		}
	}
	return x, y, w, h
}

// updateShadow puts or moves the shadow of a decorated view whose surface
// is width×height, in its tree; it takes it away if the view has none.
func (s *server) updateShadow(view any, tree unsafe.Pointer, width, height int, active bool) {
	if tree == nil || width <= 0 || height <= 0 {
		return
	}
	if !s.shadowWanted(view) {
		s.removeShadow(view)
		return
	}
	if s.shadowParts == nil {
		s.shadowParts = newGlowParts(shadowStyle, cornerRadius+borderWidth)
	}
	g := s.shadows[view]
	if g != nil && g.parent != tree {
		delete(s.shadows, view) // a new tree: the old shadow went with the old one
		g = nil
	}
	if g == nil {
		if g = newGlow(tree, s.shadowParts, false); g == nil {
			return
		}
		s.shadows[view] = g
	}
	opacity := float32(shadowInactiveOpacity)
	if active {
		opacity = 1
	}
	g.shadow = shadowGeometry{width: width, height: height, opacity: opacity}
	s.layShadow(view, g)
}

// layShadow lays a view's shadow, cut to the windows' area of its screen.
func (s *server) layShadow(view any, g *glow) {
	sh := &g.shadow
	tx, ty, w, h := viewTreeCorner(view)
	sh.treeX, sh.treeY = tx, ty
	var clip image.Rectangle
	if out := s.getOutputForPosition(float64(tx+w/2), float64(ty+h/2)); out != nil {
		cx, cy, cw, ch := s.contentBounds(s.getOutputGeometry(out))
		clip = image.Rect(cx-tx, cy-ty, cx+cw-tx, cy+ch-ty) // in the tree
	}
	// The top of the shadow starts lower, under the window; its bottom stays
	// at the window's so that no light shows between them.
	sh.cut = g.placeClipped(s.shadowParts, -borderWidth, shadowOffsetY,
		sh.width+2*borderWidth, titlebarHeight+sh.height+borderWidth-shadowOffsetY, sh.opacity, clip)
}

// tickShadows keeps the shadows of moving windows out of the dock and the
// widget panel, and takes away those of windows that fill their screen.
// Nothing moves on its own: it never asks for another frame.
func (s *server) tickShadows() bool {
	for view, g := range s.shadows {
		if !s.viewAlive(view, g.parent) {
			continue
		}
		if !s.shadowWanted(view) {
			s.removeShadow(view)
			continue
		}
		tx, ty, w, h := viewTreeCorner(view)
		if tx == g.shadow.treeX && ty == g.shadow.treeY {
			continue
		}
		if !g.shadow.cut && s.shadowInside(tx, ty, w, h) {
			g.shadow.treeX, g.shadow.treeY = tx, ty // still clear of the edges
			continue
		}
		s.layShadow(view, g)
	}
	return false
}

// shadowInside reports whether the shadow of a window whose tree is at (tx,
// ty), w×h, falls within the windows' area of its screen.
func (s *server) shadowInside(tx, ty, w, h int) bool {
	out := s.getOutputForPosition(float64(tx+w/2), float64(ty+h/2))
	if out == nil {
		return false
	}
	cx, cy, cw, ch := s.contentBounds(s.getOutputGeometry(out))
	r := shadowStyle.radius + borderWidth
	shadow := image.Rect(tx-r, ty-r, tx+w+r, ty+h+r)
	return shadow.In(image.Rect(cx, cy, cx+cw, cy+ch))
}

// removeShadow takes the shadow of a view away, when it loses its
// decorations or fills its screen.
func (s *server) removeShadow(view any) {
	g := s.shadows[view]
	if g == nil {
		return
	}
	if s.viewAlive(view, g.parent) {
		g.destroy()
	}
	delete(s.shadows, view)
}

// setWindowShadows turns the shadows on or off (Settings > Advanced).
func (s *server) setWindowShadows(on bool) {
	if on == s.windowShadows {
		return
	}
	s.windowShadows = on
	for _, v := range s.xdgViews {
		if v.mapped {
			s.updateXdgViewDecorations(v)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.isOverlay {
			s.updateXwayViewDecorations(v)
		}
	}
	s.scheduleAllOutputFrames()
}
