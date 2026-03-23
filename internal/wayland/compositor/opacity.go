package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <wlr/types/wlr_scene.h>

static void _opacity_iter(struct wlr_scene_buffer *buffer, int sx, int sy, void *data) {
	float *opacity = (float *)data;
	wlr_scene_buffer_set_opacity(buffer, *opacity);
}

static void scene_tree_set_opacity(struct wlr_scene_tree *tree, float opacity) {
	wlr_scene_node_for_each_buffer(&tree->node, _opacity_iter, &opacity);
}

static void scene_rect_set_color_op(struct wlr_scene_rect *rect, const float color[4]) {
	wlr_scene_rect_set_color(rect, color);
}
*/
import "C"
import "unsafe"

// setXdgViewOpacity sets the opacity of an XDG view and its decorations.
func (s *server) setXdgViewOpacity(v *xdgView, opacity float32) {
	if opacity < 0.1 {
		opacity = 0.1
	}
	if opacity > 1.0 {
		opacity = 1.0
	}
	v.opacity = opacity

	// Apply opacity to all scene buffers in the view's scene tree
	tree := (*C.struct_wlr_scene_tree)(v.sceneTree)
	if tree != nil {
		C.scene_tree_set_opacity(tree, C.float(opacity))
	}

	// Update border rect alpha (decorations)
	if v.decorated {
		s.updateBorderOpacity(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, opacity, s.activeXdg == v)
	}
}

// setXwayViewOpacity sets the opacity of an XWayland view and its decorations.
func (s *server) setXwayViewOpacity(v *xwayView, opacity float32) {
	if opacity < 0.1 {
		opacity = 0.1
	}
	if opacity > 1.0 {
		opacity = 1.0
	}
	v.opacity = opacity

	// Apply opacity to all scene buffers in the view's scene tree
	tree := (*C.struct_wlr_scene_tree)(v.sceneTree)
	if tree != nil {
		C.scene_tree_set_opacity(tree, C.float(opacity))
	}

	// Update border rect alpha (decorations)
	if v.decorated {
		s.updateBorderOpacity(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, opacity, s.activeXway == v)
	}
}

// updateBorderOpacity sets the alpha channel of border rects to match the view opacity.
func (s *server) updateBorderOpacity(borderT, borderB, borderL, borderR unsafe.Pointer, opacity float32, active bool) {
	bColor := borderColor
	if active {
		bColor = borderActiveColor
	}
	bc := colorToFloat4(bColor)
	bc[3] = C.float(opacity) // Override alpha with view opacity

	for _, p := range []unsafe.Pointer{borderT, borderB, borderL, borderR} {
		if p != nil {
			C.scene_rect_set_color_op((*C.struct_wlr_scene_rect)(p), &bc[0])
		}
	}
}
