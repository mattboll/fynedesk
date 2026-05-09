package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <wlr/types/wlr_scene.h>

static struct wlr_scene_tree *rs_scene_tree_create(struct wlr_scene_tree *parent) {
	return wlr_scene_tree_create(parent);
}
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
static void rs_scene_rect_set_size(struct wlr_scene_rect *rect, int w, int h) {
	wlr_scene_rect_set_size(rect, w, h);
}
static void rs_scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void rs_scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"
)

const regionBorderWidth = 2

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
	tree := C.rs_scene_tree_create(ovTree)
	C.rs_scene_node_set_position(&tree.node, C.int(minX), C.int(minY))
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

	log.Println("[SCREENSHOT] Region selection started — click and drag to select area, release to capture")
}

// updateRegionSelect repositions the scene rects as the cursor moves.
func (s *server) updateRegionSelect() {
	if !s.regionSelectActive || s.regionTree == nil {
		return
	}
	s.layoutRegionRects()
}

// layoutRegionRects positions the 4 dim rects and 4 border rects around the
// current selection rectangle. This is O(1) — just setting positions and sizes
// on 8 GPU scene nodes, with zero pixel manipulation.
//
// Layout (dim rects around selection hole):
//
//	+---------------------------+
//	|         top (0)           |
//	+------+----------+--------+
//	|left  | selection |  right |
//	| (2)  |  (hole)   |  (3)  |
//	+------+----------+--------+
//	|        bottom (1)         |
//	+---------------------------+
func (s *server) layoutRegionRects() {
	minX, minY, totalW, totalH := s.fullLayoutBounds()
	if totalW <= 0 || totalH <= 0 {
		return
	}

	// Before the first click, the "selection" is a degenerate point between
	// regionStartX/Y (cursor position at startRegionSelect time) and the
	// current cursor — which would render a stray border line at the cursor
	// as the user moves. Show only the full-screen dim until the anchor is
	// placed by handleRegionClick.
	if !s.regionAnchorSet {
		s.layoutRegionFullDim(totalW, totalH)
		return
	}

	// Convert to overlay-relative coordinates
	sx := int(s.regionStartX) - minX
	sy := int(s.regionStartY) - minY
	ex := int(s.cursor.X()) - minX
	ey := int(s.cursor.Y()) - minY

	// Normalize so x1<x2, y1<y2
	x1, x2 := sx, ex
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	y1, y2 := sy, ey
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	// Clamp
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > totalW {
		x2 = totalW
	}
	if y2 > totalH {
		y2 = totalH
	}

	selW := x2 - x1
	selH := y2 - y1

	// Top dim rect: full width, from top to selection top
	r := (*C.struct_wlr_scene_rect)(s.regionDimRects[0])
	C.rs_scene_rect_set_size(r, C.int(totalW), C.int(y1))
	C.rs_scene_node_set_position(&r.node, 0, 0)

	// Bottom dim rect: full width, from selection bottom to screen bottom
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[1])
	C.rs_scene_rect_set_size(r, C.int(totalW), C.int(totalH-y2))
	C.rs_scene_node_set_position(&r.node, 0, C.int(y2))

	// Left dim rect: from selection top to bottom, left edge to selection left
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[2])
	C.rs_scene_rect_set_size(r, C.int(x1), C.int(selH))
	C.rs_scene_node_set_position(&r.node, 0, C.int(y1))

	// Right dim rect: from selection top to bottom, selection right to right edge
	r = (*C.struct_wlr_scene_rect)(s.regionDimRects[3])
	C.rs_scene_rect_set_size(r, C.int(totalW-x2), C.int(selH))
	C.rs_scene_node_set_position(&r.node, C.int(x2), C.int(y1))

	// Border rects (2px wide/tall lines around selection)
	bw := regionBorderWidth

	// Top border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[0])
	C.rs_scene_rect_set_size(r, C.int(selW+2*bw), C.int(bw))
	C.rs_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y1-bw))

	// Bottom border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[1])
	C.rs_scene_rect_set_size(r, C.int(selW+2*bw), C.int(bw))
	C.rs_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y2))

	// Left border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[2])
	C.rs_scene_rect_set_size(r, C.int(bw), C.int(selH))
	C.rs_scene_node_set_position(&r.node, C.int(x1-bw), C.int(y1))

	// Right border
	r = (*C.struct_wlr_scene_rect)(s.regionBorderRects[3])
	C.rs_scene_rect_set_size(r, C.int(bw), C.int(selH))
	C.rs_scene_node_set_position(&r.node, C.int(x2), C.int(y1))
}

// layoutRegionFullDim covers the whole screen with the first dim rect and
// collapses the others (and all border rects) to zero size, so nothing is
// drawn at the cursor before the first click.
func (s *server) layoutRegionFullDim(totalW, totalH int) {
	r := (*C.struct_wlr_scene_rect)(s.regionDimRects[0])
	C.rs_scene_rect_set_size(r, C.int(totalW), C.int(totalH))
	C.rs_scene_node_set_position(&r.node, 0, 0)
	for i := 1; i < 4; i++ {
		r := (*C.struct_wlr_scene_rect)(s.regionDimRects[i])
		C.rs_scene_rect_set_size(r, 0, 0)
	}
	for i := 0; i < 4; i++ {
		r := (*C.struct_wlr_scene_rect)(s.regionBorderRects[i])
		C.rs_scene_rect_set_size(r, 0, 0)
	}
}

// finishRegionSelect captures the selected region and cleans up the overlay.
func (s *server) finishRegionSelect() {
	if !s.regionSelectActive {
		return
	}

	// Calculate selection rectangle in layout coordinates
	x1, y1 := s.regionStartX, s.regionStartY
	x2, y2 := s.cursor.X(), s.cursor.Y()
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	selW := int(x2 - x1)
	selH := int(y2 - y1)

	// Clean up overlay BEFORE capturing (so it's not in the screenshot)
	s.cancelRegionSelect()

	if selW < 5 || selH < 5 {
		log.Println("[SCREENSHOT] Region too small, cancelled")
		return
	}

	// Capture the region with grim
	region := fmt.Sprintf("%d,%d %dx%d", int(x1), int(y1), selW, selH)
	go s.captureRegion(region)
}

// captureRegion runs grim with the specified geometry string.
func (s *server) captureRegion(region string) {
	homeDir, _ := os.UserHomeDir()
	picturesDir := filepath.Join(homeDir, "Pictures")
	os.MkdirAll(picturesDir, 0755)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(picturesDir, fmt.Sprintf("screenshot_%s.png", timestamp))

	grimCmd := exec.Command(findBinary("grim"), "-g", region, filename)
	grimCmd.Env = safeEnv()
	if err := grimCmd.Start(); err != nil {
		log.Printf("[SCREENSHOT] grim region capture failed to start: %v", err)
		return
	}
	go func() {
		if err := grimCmd.Wait(); err != nil {
			log.Printf("[SCREENSHOT] grim region capture failed: %v", err)
			return
		}
		log.Printf("[SCREENSHOT] Region saved to %s", filename)
		s.enqueueAction(func() { s.notifyScreenshot(filename) })
	}()
}

// cancelRegionSelect cleans up the region selection overlay without capturing.
func (s *server) cancelRegionSelect() {
	s.regionSelectActive = false
	s.regionAnchorSet = false
	if s.regionTree != nil {
		tree := (*C.struct_wlr_scene_tree)(s.regionTree)
		C.rs_scene_node_destroy(&tree.node) // destroys all children too
		s.regionTree = nil
		for i := range s.regionDimRects {
			s.regionDimRects[i] = nil
		}
		for i := range s.regionBorderRects {
			s.regionBorderRects[i] = nil
		}
	}
}
