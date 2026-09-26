package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The veil lets the clicks through: they reach the dimmed windows.
static bool focus_dim_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
    return false;
}

// focus_dim_create makes the veil: one black pixel, stretched.
static struct wlr_scene_buffer *focus_dim_create(struct wlr_scene_tree *parent) {
    struct pixel_buffer *px = pixel_buffer_create(1, 1);
    if (!px) {
        return NULL;
    }
    const unsigned char black[4] = {0, 0, 0, 255};
    pixel_buffer_update(px, (void *)black, 1, 1);
    struct wlr_scene_buffer *veil = wlr_scene_buffer_create(parent, &px->base);
    wlr_buffer_drop(&px->base); // the scene buffer holds it now
    if (veil) {
        veil->point_accepts_input = focus_dim_no_input;
        wlr_scene_node_set_enabled(&veil->node, false);
    }
    return veil;
}

// focus_dim_place puts the veil over the whole layout, just below the
// window left in the light (or disables it).
static void focus_dim_place(struct wlr_scene_buffer *veil, struct wlr_scene_tree *below,
        int x, int y, int w, int h, float alpha) {
    if (!below || alpha <= 0 || w <= 0 || h <= 0) {
        wlr_scene_node_set_enabled(&veil->node, false);
        return;
    }
    if (veil->node.parent != below->node.parent) {
        wlr_scene_node_reparent(&veil->node, below->node.parent);
    }
    wlr_scene_node_place_below(&veil->node, &below->node);
    wlr_scene_node_set_position(&veil->node, x, y);
    wlr_scene_buffer_set_dest_size(veil, w, h);
    wlr_scene_buffer_set_opacity(veil, alpha);
    wlr_scene_node_set_enabled(&veil->node, true);
}
*/
import "C"

import (
	"log"
	"math"
	"unsafe"
)

// Focus mode: a veil dims everything but the focused window, which it
// follows. Toggled by the focus_mode action (Super+F).
const (
	focusDimAlpha = 0.6
	focusDimStep  = 0.08 // per frame, fading in and out
)

// toggleFocusMode turns the veil on or off.
func (s *server) toggleFocusMode() {
	s.focusModeActive = !s.focusModeActive
	log.Printf("[FOCUS] Focus mode %v", s.focusModeActive)
	for _, o := range s.outputs {
		scheduleOutputFrame(o.output)
	}
}

// litTree returns the tree of the window to keep in the light: the root of
// the focused one, so that its dialogs, above it, stay lit too.
func (s *server) litTree() unsafe.Pointer {
	if v := s.activeXdg; v != nil && v.mapped {
		for v.parent != nil {
			v = v.parent
		}
		return v.sceneTree
	}
	if v := s.activeXway; v != nil && v.mapped && !v.isPanel && !v.isOverlay {
		for v.parent != nil {
			v = v.parent
		}
		return v.sceneTree
	}
	return nil
}

// tickFocusDim keeps the veil below the focused window and fades it; it
// reports whether it is fading.
func (s *server) tickFocusDim() bool {
	target := float32(0)
	if s.focusModeActive {
		target = focusDimAlpha
	}
	if s.focusDim == nil {
		if target == 0 {
			return false
		}
		s.focusDim = unsafe.Pointer(C.focus_dim_create((*C.struct_wlr_scene_tree)(s.windowsTree)))
		if s.focusDim == nil {
			return false
		}
	}

	switch {
	case s.reduceMotion:
		s.focusDimAlpha = target
	case s.focusDimAlpha < target:
		s.focusDimAlpha = float32(math.Min(float64(s.focusDimAlpha+focusDimStep), float64(target)))
	case s.focusDimAlpha > target:
		s.focusDimAlpha = float32(math.Max(float64(s.focusDimAlpha-focusDimStep), float64(target)))
	}

	lx, ly, lw, lh := s.fullLayoutBounds()
	C.focus_dim_place((*C.struct_wlr_scene_buffer)(s.focusDim), (*C.struct_wlr_scene_tree)(s.litTree()),
		C.int(lx), C.int(ly), C.int(lw), C.int(lh), C.float(s.focusDimAlpha))
	return s.focusDimAlpha != target
}
