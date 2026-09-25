package compositor

import (
	"image/color"
	"unsafe"
)

// Soft shadows under the windows Tyde decorates (the others draw their
// own): a halo of black, a little lower than the window, darker under the
// window that has the focus.
var shadowStyle = haloStyle{color: color.NRGBA{A: 0xFF}, radius: 24, strength: 0.45}

const (
	shadowOffsetY         = 6    // the light comes from above: less shadow at the top
	shadowInactiveOpacity = 0.55 // of the shadow of a window without the focus
)

// updateShadow puts or moves the shadow of a decorated view whose surface
// is width×height, in its tree.
func (s *server) updateShadow(view any, tree unsafe.Pointer, width, height int, active bool) {
	if tree == nil || width <= 0 || height <= 0 {
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
		if g = newGlow(tree, s.shadowParts); g == nil {
			return
		}
		s.shadows[view] = g
	}
	opacity := float32(shadowInactiveOpacity)
	if active {
		opacity = 1
	}
	// The top of the shadow starts lower, under the window; its bottom stays
	// at the window's so that no light shows between them.
	g.place(s.shadowParts, -borderWidth, shadowOffsetY,
		width+2*borderWidth, titlebarHeight+height+borderWidth-shadowOffsetY, opacity)
}

// removeShadow takes the shadow of a view away, when it loses its
// decorations.
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

// setShadowVisible hides the shadow of a view, or shows it again.
func (s *server) setShadowVisible(view any, on bool) {
	if g := s.shadows[view]; g != nil && s.viewAlive(view, g.parent) {
		g.setVisible(on)
	}
}
