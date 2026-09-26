package compositor

/*
#include <wlr/types/wlr_scene.h>

// Only the window's own surfaces (and, below, its title bar): the shadow and
// the halo drawn with it keep their own opacity.
static void _opacity_iter(struct wlr_scene_buffer *buffer, int sx, int sy, void *data) {
	if (wlr_scene_surface_try_from_buffer(buffer)) {
		wlr_scene_buffer_set_opacity(buffer, *(float *)data);
	}
}

static void scene_tree_set_opacity(struct wlr_scene_tree *tree, float opacity) {
	wlr_scene_node_for_each_buffer(&tree->node, _opacity_iter, &opacity);
}

static void scene_buffer_set_opacity_c(struct wlr_scene_buffer *buffer, float opacity) {
	if (buffer) {
		wlr_scene_buffer_set_opacity(buffer, opacity);
	}
}

static void scene_rect_set_color_op(struct wlr_scene_rect *rect, const float color[4]) {
	wlr_scene_rect_set_color(rect, color);
}
*/
import "C"
import "unsafe"

// clampOpacity keeps an opacity in [0.1, 1].
func clampOpacity(opacity float32) float32 {
	return min(max(opacity, 0.1), 1)
}

// setViewOpacity sets the opacity of the surfaces of a view's tree and of
// its title bar buffers.
func setViewOpacity(tree unsafe.Pointer, opacity float32, decoBuffers ...unsafe.Pointer) {
	if tree != nil {
		C.scene_tree_set_opacity((*C.struct_wlr_scene_tree)(tree), C.float(opacity))
	}
	for _, b := range decoBuffers {
		C.scene_buffer_set_opacity_c((*C.struct_wlr_scene_buffer)(b), C.float(opacity))
	}
}

// setXdgViewOpacity sets the opacity of an XDG view and its decorations.
func (s *server) setXdgViewOpacity(v *xdgView, opacity float32) {
	v.opacity = clampOpacity(opacity)
	setViewOpacity(v.sceneTree, v.opacity, v.decoTitlebar, v.decoCornerBL, v.decoCornerBR, v.decoIconBuf)
	if v.decorated {
		s.updateBorderOpacity(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.opacity, s.activeXdg == v)
	}
}

// setXwayViewOpacity sets the opacity of an XWayland view and its decorations.
func (s *server) setXwayViewOpacity(v *xwayView, opacity float32) {
	v.opacity = clampOpacity(opacity)
	setViewOpacity(v.sceneTree, v.opacity, v.decoTitlebar, v.decoCornerBL, v.decoCornerBR, v.decoIconBuf)
	if v.decorated {
		s.updateBorderOpacity(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.opacity, s.activeXway == v)
	}
}

// reapplyViewOpacity sets the opacity of the translucent windows again
// before a frame: the scene resets a surface's opacity on each of its
// commits (to the client's alpha-modifier, 1 by default).
func (s *server) reapplyViewOpacity() {
	for _, v := range s.xdgViews {
		if v.mapped && v.opacity > 0 && v.opacity < 1 {
			setViewOpacity(v.sceneTree, v.opacity)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && v.opacity > 0 && v.opacity < 1 {
			setViewOpacity(v.sceneTree, v.opacity)
		}
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
