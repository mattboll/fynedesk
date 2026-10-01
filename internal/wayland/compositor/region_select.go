package compositor

/*
#include <wlr/types/wlr_scene.h>

static const float rs_dim_color[4] = {0.0f, 0.0f, 0.0f, 0.5f};
static const float rs_border_color[4] = {1.0f, 1.0f, 1.0f, 0.9f};
static struct wlr_scene_rect *rs_scene_rect_create_dim(struct wlr_scene_tree *parent,
		int w, int h) {
	return wlr_scene_rect_create(parent, w, h, rs_dim_color);
}
static struct wlr_scene_rect *rs_scene_rect_create_border(struct wlr_scene_tree *parent,
		int w, int h) {
	return wlr_scene_rect_create(parent, w, h, rs_border_color);
}
*/
import "C"

import (
	"fmt"
	"log"
	"unsafe"
)

const regionBorderWidth = 2

// regionPurpose is what a zone is selected for.
type regionPurpose int

const (
	regionCapture regionPurpose = iota // a screenshot
	regionText                         // its text, for the clipboard
	regionRecord                       // a screen recording
)

// startRegionSelectFor lets the user select a zone for purpose.
func (s *server) startRegionSelectFor(purpose regionPurpose) {
	if s.regionSelectActive {
		return
	}
	s.startRegionSelect()
	s.regionPurpose = purpose
}

// startRegionSelect enters region selection mode using GPU-native scene rects.
// Four dim rects surround the selection area; four border rects frame it.
func (s *server) startRegionSelect() {
	if s.regionSelectActive {
		return
	}

	minX, minY, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return
	}

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	tree := C.wlr_scene_tree_create(ovTree)
	C.wlr_scene_node_set_position(&tree.node, C.int(minX), C.int(minY))
	s.regionTree = unsafe.Pointer(tree)

	// Create 4 dim rects (initially covering the full area as one big rect at index 0)
	for i := 0; i < 4; i++ {
		rect := C.rs_scene_rect_create_dim(tree, C.int(w), C.int(h))
		s.regionDimRects[i] = unsafe.Pointer(rect)
	}
	// Create 4 border rects (initially zero-sized)
	for i := 0; i < 4; i++ {
		rect := C.rs_scene_rect_create_border(tree, 0, 0)
		s.regionBorderRects[i] = unsafe.Pointer(rect)
	}

	s.regionSelectActive = true
	s.regionAnchorSet = false
	s.regionStartX = s.cursor.X()
	s.regionStartY = s.cursor.Y()
	s.regionEndX = s.regionStartX
	s.regionEndY = s.regionStartY

	// Initial layout: full dim overlay, no selection hole
	s.layoutRegionRects()

	log.Println("[SCREENSHOT] Region selection started — drag a zone, or click a window (the screen outside of any)")
}

// updateRegionSelect repositions the scene rects as the cursor moves.
func (s *server) updateRegionSelect() {
	if !s.regionSelectActive || s.regionTree == nil {
		return
	}
	s.layoutRegionRects()
}

// regionRect is a zone, in layout coordinates.
type regionRect struct{ x, y, w, h int }

// dragRect is the zone between two corners, in any order.
func dragRect(x1, y1, x2, y2 float64) regionRect {
	return regionRect{
		x: int(min(x1, x2)), y: int(min(y1, y2)),
		w: int(max(x1, x2) - min(x1, x2)), h: int(max(y1, y2) - min(y1, y2)),
	}
}

// regionTarget is the zone the selection stands on: the one dragged once
// the button is down, before that the window under the pointer, if any.
func (s *server) regionTarget() (regionRect, bool) {
	if s.regionAnchorSet {
		return dragRect(s.regionStartX, s.regionStartY, s.cursor.X(), s.cursor.Y()), true
	}
	return s.windowRectAt(s.cursor.X(), s.cursor.Y())
}

// layoutRegionRects dims everything but the zone the selection stands on,
// frames it and shows its size; with no zone, it dims the whole screen.
// Setting positions and sizes on scene nodes: no pixel is drawn but the
// size label.
//
// Layout (dim rects around the zone):
//
//	+---------------------------+
//	|         top (0)           |
//	+------+----------+--------+
//	|left  |   zone    |  right |
//	| (2)  |  (hole)   |  (3)  |
//	+------+----------+--------+
//	|        bottom (1)         |
//	+---------------------------+
func (s *server) layoutRegionRects() {
	minX, minY, totalW, totalH := s.fullLayoutBounds()
	if totalW <= 0 || totalH <= 0 {
		return
	}
	zone, ok := s.regionTarget()
	if !ok {
		s.layoutRegionFullDim(totalW, totalH)
		s.hideRegionSize()
		return
	}
	// Overlay-relative and clamped to the screens.
	x1, y1 := max(zone.x-minX, 0), max(zone.y-minY, 0)
	x2, y2 := min(zone.x-minX+zone.w, totalW), min(zone.y-minY+zone.h, totalH)
	if x2 <= x1 || y2 <= y1 {
		s.layoutRegionFullDim(totalW, totalH)
		s.hideRegionSize()
		return
	}
	s.layoutRegionHole(x1, y1, x2, y2, totalW, totalH)
	s.showRegionSize(x2-x1, y2-y1, x2, y2, totalW, totalH)
}

// layoutRegionHole dims everything around (x1, y1)-(x2, y2) and frames it.
func (s *server) layoutRegionHole(x1, y1, x2, y2, totalW, totalH int) {
	selW := x2 - x1
	selH := y2 - y1

	// Top dim rect: full width, from top to selection top
	r := (*C.struct_wlr_scene_rect)(s.regionDimRects[0])
	C.wlr_scene_rect_set_size(r, C.int(totalW), C.int(y1))
	C.wlr_scene_node_set_position(&r.node, 0, 0)

	// Bottom dim rect: full width, from selection bottom to screen bottom
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[1])
	C.wlr_scene_rect_set_size(r, C.int(totalW), C.int(totalH-y2))
	C.wlr_scene_node_set_position(&r.node, 0, C.int(y2))

	// Left dim rect: from selection top to bottom, left edge to selection left
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[2])
	C.wlr_scene_rect_set_size(r, C.int(x1), C.int(selH))
	C.wlr_scene_node_set_position(&r.node, 0, C.int(y1))

	// Right dim rect: from selection top to bottom, selection right to right edge
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[3])
	C.wlr_scene_rect_set_size(r, C.int(totalW-x2), C.int(selH))
	C.wlr_scene_node_set_position(&r.node, C.int(x2), C.int(y1))

	// Border rects (2px wide/tall lines around selection)
	bw := regionBorderWidth

	// Top border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[0])
	C.wlr_scene_rect_set_size(r, C.int(selW+2*bw), C.int(bw))
	C.wlr_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y1-bw))

	// Bottom border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[1])
	C.wlr_scene_rect_set_size(r, C.int(selW+2*bw), C.int(bw))
	C.wlr_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y2))

	// Left border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[2])
	C.wlr_scene_rect_set_size(r, C.int(bw), C.int(selH))
	C.wlr_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y1))

	// Right border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[3])
	C.wlr_scene_rect_set_size(r, C.int(bw), C.int(selH))
	C.wlr_scene_node_set_position(&r.node, C.int(x2), C.int(y1))
}

// layoutRegionFullDim covers the whole screen with the first dim rect and
// collapses the others (and all border rects) to zero size, so nothing is
// drawn at the cursor before the first click.
func (s *server) layoutRegionFullDim(totalW, totalH int) {
	r := (*C.struct_wlr_scene_rect)(s.regionDimRects[0])
	C.wlr_scene_rect_set_size(r, C.int(totalW), C.int(totalH))
	C.wlr_scene_node_set_position(&r.node, 0, 0)
	for i := 1; i < 4; i++ {
		r := (*C.struct_wlr_scene_rect)(s.regionDimRects[i])
		C.wlr_scene_rect_set_size(r, 0, 0)
	}
	for i := 0; i < 4; i++ {
		r := (*C.struct_wlr_scene_rect)(s.regionBorderRects[i])
		C.wlr_scene_rect_set_size(r, 0, 0)
	}
}

// finishRegionSelect takes the zone dragged.
func (s *server) finishRegionSelect() {
	if !s.regionSelectActive {
		return
	}
	s.finishRegion(dragRect(s.regionStartX, s.regionStartY, s.cursor.X(), s.cursor.Y()))
}

// finishRegionClick takes, on a click without drag, the window under the
// pointer, or else the screen.
func (s *server) finishRegionClick() {
	if !s.regionSelectActive {
		return
	}
	zone, ok := s.windowRectAt(s.cursor.X(), s.cursor.Y())
	if !ok {
		g := s.getActiveOutputGeo()
		zone = regionRect{g.x, g.y, g.width, g.height}
	}
	s.finishRegion(zone)
}

// finishRegion ends the selection and captures zone, clamped to the
// screens, for what it was selected for.
func (s *server) finishRegion(zone regionRect) {
	purpose := s.regionPurpose
	minX, minY, totalW, totalH := s.fullLayoutBounds()

	// Clean up overlay BEFORE capturing (so it's not in the screenshot)
	s.cancelRegionSelect()

	x1, y1 := max(zone.x, minX), max(zone.y, minY)
	x2, y2 := min(zone.x+zone.w, minX+totalW), min(zone.y+zone.h, minY+totalH)
	if x2-x1 < 5 || y2-y1 < 5 {
		log.Println("[SCREENSHOT] Region too small, cancelled")
		return
	}
	region := fmt.Sprintf("%d,%d %dx%d", x1, y1, x2-x1, y2-y1)
	switch purpose {
	case regionText:
		s.captureForText(region)
	case regionRecord:
		s.beginRecording(regionRect{x1, y1, x2 - x1, y2 - y1})
	default:
		s.captureScreen(region)
	}
}

// cancelRegionSelect cleans up the region selection overlay without capturing.
func (s *server) cancelRegionSelect() {
	s.regionSelectActive = false
	s.regionPurpose = regionCapture
	s.regionAnchorSet = false
	if s.regionTree != nil {
		tree := (*C.struct_wlr_scene_tree)(s.regionTree)
		C.wlr_scene_node_destroy(&tree.node) // destroys all children too
		s.forgetRegionSize()
		s.regionTree = nil
		for i := range s.regionDimRects {
			s.regionDimRects[i] = nil
		}
		for i := range s.regionBorderRects {
			s.regionBorderRects[i] = nil
		}
	}
}
