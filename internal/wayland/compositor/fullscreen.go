package compositor

import (
	"log"
)

// handleXwayRequestFullscreen follows an X11 client's _NET_WM_STATE
// fullscreen request.
func (s *server) handleXwayRequestFullscreen(v *xwayView) {
	wantsFS := v.surface.Fullscreen()
	log.Printf("[FULLSCREEN] XWayland request_fullscreen: class=%q fullscreen=%v\n",
		getXwaylandSurfaceClass(v.surface), wantsFS)
	s.fullscreenXwayWindow(v, wantsFS)
}

// handleXwaySetParent follows WM_TRANSIENT_FOR changes.
func (s *server) handleXwaySetParent(child *xwayView) {
	// Skip panel and override-redirect windows
	if child.isPanel || child.overrideRedirect {
		return
	}
	parent := child.surface.Parent()
	if !parent.Valid() {
		child.parent = nil
		return
	}
	for _, v := range s.xwayViews {
		if v.surface.Ptr() == parent.Ptr() {
			child.parent = v
			child.desk = v.desk
			log.Printf("[PARENT] XWayland set_parent: child=%s parent=%s\n", child.id, v.id)
			return
		}
	}
}

// handleXwaySurfaceResized reacts to a new committed size of an X11 window's
// wl_surface: decorations follow the window and legacy fullscreen games
// asking for more than the output get a mode/scale switch.
func (s *server) handleXwaySurfaceResized(v *xwayView, width, height int) {
	// Skip unmapped, panel, overlay, and override-redirect windows
	if !v.mapped || v.isPanel || v.isOverlay || v.overrideRedirect {
		return
	}

	// Update decorations to match new surface dimensions (fixes Steam resize)
	if v.decorated && !v.fullscreen {
		log.Printf("[RESIZE] XWay commit: title=%q wlr_surface=%dx%d xway_surface=%dx%d maximized=%v",
			v.surface.Title(), width, height, v.surface.Width(), v.surface.Height(), v.maximized)
		s.updateXwayViewDecorations(v)
	}

	// If a fullscreen surface commits at a HIGHER resolution than
	// the output, switch the output mode to match (legacy XWayland
	// game case). Smaller surfaces will be told via Configure to
	// grow to the output size, no mode switch needed.
	if v.fullscreen && width > 0 && height > 0 {
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		if out != nil && (width > out.width || height > out.height) {
			log.Printf("[FULLSCREEN] Surface commit resize: %dx%d (output=%dx%d), adjusting scale\n",
				width, height, out.width, out.height)
			newGeo := s.switchModeForFullscreen(out, width, height)
			v.x = float64(newGeo.x)
			v.y = float64(newGeo.y)
			v.surface.Configure(int16(v.x), int16(v.y), uint16(newGeo.width), uint16(newGeo.height))
			setXwayScenePos(v)
		}
	}
}
