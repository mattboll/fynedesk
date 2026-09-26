package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"
*/
import "C"

// contentBounds returns the usable content area for an output.
// On the primary output, it reserves space for the bar and widget panel.
// On secondary outputs with a bar, it reserves space for the bar only.
func (s *server) contentBounds(outGeo outputGeometry) (x, y, w, h int) {
	if s.isPrimaryOutput(outGeo) {
		leftWidth := 0
		if s.narrowLeftLauncher {
			leftWidth = barWidth
		}
		rightWidth := widgetWidth
		if s.narrowWidgetPanel {
			rightWidth = barWidth
		}
		return outGeo.x + leftWidth, outGeo.y, outGeo.width - leftWidth - rightWidth, outGeo.height
	}
	// Check if this secondary output has a bar
	if s.hasSecondaryBar(outGeo) {
		if s.narrowLeftLauncher {
			return outGeo.x + barWidth, outGeo.y, outGeo.width - barWidth, outGeo.height
		}
	}
	return outGeo.x, outGeo.y, outGeo.width, outGeo.height
}

// clampToContent fits a window of size (w,h) at (x,y) into the usable content
// area of whichever live output currently contains its origin (falling back to
// the primary). Top-level windows larger than the usable area are shrunk;
// transient/dialog children keep their size and only have their origin nudged.
// For decorated windows, y is the geometry origin (the titlebar sits above it),
// so the minimum y reserves titlebarHeight. Returns the adjusted geometry.
func (s *server) clampToContent(x, y float64, w, h int, decorated, isChild bool) (float64, float64, int, int) {
	outGeo := s.getOutputGeoForView(x, y)
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if decorated {
		topMargin = titlebarHeight
	}
	usableH := ch - topMargin
	if !isChild {
		if w > cw {
			w = cw
		}
		if h > usableH {
			h = usableH
		}
	}
	minX, maxX := float64(cx), float64(cx+cw-w)
	if maxX < minX {
		maxX = minX
	}
	minY, maxY := float64(cy+topMargin), float64(cy+ch-h)
	if maxY < minY {
		maxY = minY
	}
	if x < minX {
		x = minX
	} else if x > maxX {
		x = maxX
	}
	if y < minY {
		y = minY
	} else if y > maxY {
		y = maxY
	}
	return x, y, w, h
}

// refitWindowsToOutputs re-fits every window to the current set of outputs after
// a hotplug or layout change: fullscreen windows orphaned by a removed output
// move to the primary, maximized and snapped windows re-fit to per-output
// content bounds, and free-floating windows are clamped so their full extent
// stays clear of the reserved bar/widget-panel space. A final decoration
// reconcile pass repairs any window left with a stale decorated flag.
// Idempotent. MUST run on the main thread.
func (s *server) refitWindowsToOutputs() {
	if len(s.outputs) == 0 {
		return
	}

	// Bounding boxes of all live outputs (layout coordinates).
	type rect struct{ x, y, w, h int }
	rects := make([]rect, 0, len(s.outputs))
	for _, o := range s.outputs {
		rects = append(rects, rect{o.layoutX, o.layoutY, o.width, o.height})
	}
	pointInAnyOutput := func(px, py float64) bool {
		for _, r := range rects {
			if px >= float64(r.x) && px < float64(r.x+r.w) &&
				py >= float64(r.y) && py < float64(r.y+r.h) {
				return true
			}
		}
		return false
	}

	// 1. Fullscreen windows whose output went away: re-fit onto the primary.
	s.refitOrphanedFullscreen(pointInAnyOutput)

	// 2 & 3. Re-fit maximized and snapped windows to per-output content bounds.
	s.refreshMaximizedWindows()
	s.reSnapAllWindows()

	// 4. Clamp free-floating windows so their full extent stays inside the
	//    usable content area (clear of the bar and widget panel).
	s.clampFloatingWindows()

	// 5. Reconcile decorations for every mapped window (idempotent; repairs a
	//    window left fullscreen-undecorated on a removed output, etc.).
	s.reconcileAllDecorations()
}

// refitOrphanedFullscreen moves fullscreen windows that are on no output
// onto the primary output.
func (s *server) refitOrphanedFullscreen(pointInAnyOutput func(px, py float64) bool) {
	if p := s.primaryOutput(); p != nil {
		pGeo := s.getOutputGeometry(p)
		for _, v := range s.xdgViews {
			if v.fullscreen && v.mapped && !pointInAnyOutput(v.x, v.y) {
				v.x, v.y = float64(pGeo.x), float64(pGeo.y)
				v.xdgToplevel.SetSize(int32(pGeo.width), int32(pGeo.height))
				if v.sceneTree != nil {
					C.wlr_scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, C.int(pGeo.x), C.int(pGeo.y))
				}
			}
		}
		for _, v := range s.xwayViews {
			if v.fullscreen && v.mapped && !pointInAnyOutput(v.x, v.y) {
				v.x, v.y = float64(pGeo.x), float64(pGeo.y)
				v.surface.Configure(int16(pGeo.x), int16(pGeo.y), uint16(pGeo.width), uint16(pGeo.height))
				if v.sceneTree != nil {
					C.wlr_scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, C.int(pGeo.x), C.int(pGeo.y))
				}
			}
		}
	}
}

// clampFloatingWindows clamps free-floating windows to the content area.
func (s *server) clampFloatingWindows() {
	for _, v := range s.xdgViews {
		if !v.mapped || v.minimized || v.maximized || v.fullscreen || v.snapped != snapNone {
			continue
		}
		w, h := xdgDecoSize(v)
		nx, ny, nw, nh := s.clampToContent(v.x, v.y, w, h, v.decorated, v.parent != nil)
		if nw != w || nh != h {
			v.configuredW, v.configuredH = nw, nh
			v.xdgToplevel.SetSize(int32(nw), int32(nh))
		}
		v.x, v.y = nx, ny
		setXdgScenePos(v)
	}
	for _, v := range s.xwayViews {
		if !v.mapped || v.minimized || v.maximized || v.fullscreen || v.snapped != snapNone ||
			v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		w, h := xwayDecoSize(v)
		nx, ny, nw, nh := s.clampToContent(v.x, v.y, w, h, v.decorated, v.parent != nil)
		v.x, v.y = nx, ny
		v.surface.Configure(int16(nx), int16(ny), uint16(nw), uint16(nh))
		setXwayScenePos(v)
	}
}

// reconcileAllDecorations reconciles the decorations of every mapped window.
func (s *server) reconcileAllDecorations() {
	for _, v := range s.xdgViews {
		if v.mapped {
			s.reconcileXdgDecorations(v)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped {
			s.reconcileXwayDecorations(v)
		}
	}
}

// hasSecondaryBar checks if a secondary output has a bar window.
func (s *server) hasSecondaryBar(outGeo outputGeometry) bool {
	for name := range s.secondaryPanels {
		for _, out := range s.outputs {
			if out.output.Name() == name {
				geo := s.getOutputGeometry(out)
				if geo.x == outGeo.x && geo.y == outGeo.y {
					return true
				}
			}
		}
	}
	return false
}
