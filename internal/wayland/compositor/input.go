package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <wayland-server-core.h>
#include <wlr/types/wlr_cursor.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_data_device.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/xwayland.h>

static void cursor_warp_closest(struct wlr_cursor *cursor, double x, double y) {
    wlr_cursor_warp_closest(cursor, NULL, x, y);
}

// Hit-test the scene graph
static int scene_view_at(struct wlr_scene *scene, double lx, double ly,
        struct wlr_surface **surface_out, double *sx, double *sy, void **data_out) {
    struct wlr_scene_node *node = wlr_scene_node_at(&scene->tree.node, lx, ly, sx, sy);
    if (node == NULL) {
        *surface_out = NULL;
        *data_out = NULL;
        return 0;
    }
    struct wlr_scene_tree *tree = node->parent;
    while (tree != NULL && tree->node.data == NULL) {
        tree = tree->node.parent;
    }
    *data_out = tree ? tree->node.data : NULL;
    if (node->type != WLR_SCENE_NODE_BUFFER) {
        *surface_out = NULL;
        return 0;
    }
    struct wlr_scene_buffer *scene_buffer = wlr_scene_buffer_from_node(node);
    struct wlr_scene_surface *scene_surface = wlr_scene_surface_try_from_buffer(scene_buffer);
    if (!scene_surface) {
        *surface_out = NULL;
        return 0;
    }
    *surface_out = scene_surface->surface;
    return 1;
}

static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
    wlr_scene_node_set_position(node, x, y);
}
static void scene_node_place_above(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_above(node, sibling);
}
static void scene_node_place_below(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_below(node, sibling);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}

// Debug: walk scene tree and list node types (lightweight — no GPU buffer access)
static void debug_scene_tree_buffers(struct wlr_scene_tree *tree, const char *label) {
    int child_count = 0;
    struct wlr_scene_node *node;
    wl_list_for_each(node, &tree->children, link) {
        if (node->type == WLR_SCENE_NODE_BUFFER) {
            struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(node);
            int n = 0;
            pixman_region32_rectangles(&sb->opaque_region, &n);
            struct wlr_scene_surface *ss = wlr_scene_surface_try_from_buffer(sb);
            if (ss) {
                printf("[SCENE-NODE] %s child[%d] SURFACE pos=(%d,%d) en=%d w=%d h=%d opaque=%d\n",
                    label, child_count, node->x, node->y, node->enabled,
                    ss->surface->current.width, ss->surface->current.height, n);
            } else {
                int bw = sb->buffer ? sb->buffer->width : 0;
                int bh = sb->buffer ? sb->buffer->height : 0;
                printf("[SCENE-NODE] %s child[%d] BUFFER pos=(%d,%d) en=%d size=%dx%d opaque=%d\n",
                    label, child_count, node->x, node->y, node->enabled, bw, bh, n);
            }
        } else if (node->type == WLR_SCENE_NODE_RECT) {
            struct wlr_scene_rect *sr = wlr_scene_rect_from_node(node);
            printf("[SCENE-NODE] %s child[%d] RECT pos=(%d,%d) en=%d %dx%d a=%.2f\n",
                label, child_count, node->x, node->y, node->enabled,
                sr->width, sr->height, sr->color[3]);
        } else if (node->type == WLR_SCENE_NODE_TREE) {
            char sublabel[128];
            snprintf(sublabel, sizeof(sublabel), "%s/t%d", label, child_count);
            debug_scene_tree_buffers(wlr_scene_tree_from_node(node), sublabel);
        }
        child_count++;
    }
    if (child_count == 0) {
        printf("[SCENE-NODE] %s: empty\n", label);
    }
    fflush(stdout);
}

// Get view data pointers from windowsTree children in z-order (topmost first).
// Only returns enabled nodes with non-NULL data.
static int get_views_z_order(struct wlr_scene_tree *parent, void **data_out, int max) {
    int count = 0;
    struct wlr_scene_node *node;
    wl_list_for_each_reverse(node, &parent->children, link) {
        if (count >= max) break;
        if (node->data != NULL && node->enabled) {
            data_out[count++] = node->data;
        }
    }
    return count;
}

// --- Drag icon support (duplicated from main.go preamble for CGO static scope) ---
static struct wlr_scene_tree *drag_icon_scene_tree = NULL;
static struct wlr_scene_tree *drag_icon_tree_node = NULL;

static void update_drag_icon_position(int x, int y) {
    if (drag_icon_tree_node) {
        wlr_scene_node_set_position(&drag_icon_tree_node->node, x, y);
    }
}

// Check if a client drag is in progress (prevents compositor grabs during DnD)
static int is_drag_active(struct wlr_seat *seat) {
	return seat->drag != NULL ? 1 : 0;
}

// --- Compositor clipboard (wlr_data_source) ---
#include <string.h>
#include <unistd.h>

struct compositor_clipboard {
    struct wlr_data_source base;
    char *text;
};

static void clipboard_send(struct wlr_data_source *source, const char *mime_type, int32_t fd) {
    struct compositor_clipboard *cb = wl_container_of(source, cb, base);
    if (cb->text) {
        write(fd, cb->text, strlen(cb->text));
    }
    close(fd);
}

static void clipboard_destroy(struct wlr_data_source *source) {
    struct compositor_clipboard *cb = wl_container_of(source, cb, base);
    free(cb->text);
    free(cb);
}

static const struct wlr_data_source_impl compositor_clipboard_impl = {
    .send = clipboard_send,
    .destroy = clipboard_destroy,
};

static void set_clipboard_text(struct wlr_seat *seat, const char *text) {
    struct compositor_clipboard *cb = calloc(1, sizeof(*cb));
    if (!cb) return;
    wlr_data_source_init(&cb->base, &compositor_clipboard_impl);
    cb->text = strdup(text);

    // Add mime types
    const char *types[] = {"text/plain", "text/plain;charset=utf-8", "UTF8_STRING"};
    for (int i = 0; i < 3; i++) {
        char **dst = wl_array_add(&cb->base.mime_types, sizeof(char *));
        if (dst) *dst = strdup(types[i]);
    }

    wlr_seat_set_selection(seat, &cb->base, wl_display_next_serial(seat->display));
}

// --- Synthetic Ctrl+V for emoji paste ---
#include <linux/input-event-codes.h>
#include <xkbcommon/xkbcommon.h>

static void inject_ctrl_v(struct wlr_seat *seat, struct wlr_keyboard *keyboard) {
    if (!seat || !keyboard || !keyboard->xkb_state) {
        fprintf(stderr, "[inject_ctrl_v] null seat=%p keyboard=%p\n", seat, keyboard);
        return;
    }

    struct wlr_surface *focused = seat->keyboard_state.focused_surface;
    fprintf(stderr, "[inject_ctrl_v] focused_surface=%p\n", focused);
    if (!focused) {
        fprintf(stderr, "[inject_ctrl_v] NO focused surface — aborting\n");
        return;
    }

    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    uint32_t time_ms = ts.tv_sec * 1000 + ts.tv_nsec / 1000000;

    // Press Ctrl — update XKB state to get correct modifier mask
    xkb_state_update_key(keyboard->xkb_state, KEY_LEFTCTRL + 8, XKB_KEY_DOWN);
    struct wlr_keyboard_modifiers ctrl_mods = {
        .depressed = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_DEPRESSED),
        .latched = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_LATCHED),
        .locked = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_LOCKED),
        .group = xkb_state_serialize_layout(keyboard->xkb_state, XKB_STATE_LAYOUT_EFFECTIVE),
    };
    wlr_seat_keyboard_notify_key(seat, time_ms, KEY_LEFTCTRL, WL_KEYBOARD_KEY_STATE_PRESSED);
    wlr_seat_keyboard_notify_modifiers(seat, &ctrl_mods);

    // Press V, Release V
    wlr_seat_keyboard_notify_key(seat, time_ms + 1, KEY_V, WL_KEYBOARD_KEY_STATE_PRESSED);
    wlr_seat_keyboard_notify_key(seat, time_ms + 2, KEY_V, WL_KEYBOARD_KEY_STATE_RELEASED);

    // Release Ctrl — restore XKB state
    xkb_state_update_key(keyboard->xkb_state, KEY_LEFTCTRL + 8, XKB_KEY_UP);
    struct wlr_keyboard_modifiers no_mods = {
        .depressed = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_DEPRESSED),
        .latched = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_LATCHED),
        .locked = xkb_state_serialize_mods(keyboard->xkb_state, XKB_STATE_MODS_LOCKED),
        .group = xkb_state_serialize_layout(keyboard->xkb_state, XKB_STATE_LAYOUT_EFFECTIVE),
    };
    wlr_seat_keyboard_notify_key(seat, time_ms + 3, KEY_LEFTCTRL, WL_KEYBOARD_KEY_STATE_RELEASED);
    wlr_seat_keyboard_notify_modifiers(seat, &no_mods);
}
*/
import "C"

import (
	"log"
	"time"
	"unsafe"

	"deedles.dev/wlr"
)

// viewZEntry represents a view found in the scene tree z-order traversal.
type viewZEntry struct {
	xdg  *xdgView
	xway *xwayView
}

// getViewsInZOrder returns views from windowsTree in topmost-first z-order.
// Only returns enabled nodes (visible on current desktop).
func (s *server) getViewsInZOrder() []viewZEntry {
	var dataArr [64]unsafe.Pointer
	count := int(C.get_views_z_order(
		(*C.struct_wlr_scene_tree)(s.windowsTree),
		&dataArr[0], 64))

	entries := make([]viewZEntry, 0, count)
	for i := 0; i < count; i++ {
		xdgV, xwayV := s.viewFromNodeData(dataArr[i])
		if xdgV != nil || xwayV != nil {
			entries = append(entries, viewZEntry{xdg: xdgV, xway: xwayV})
		}
	}
	return entries
}

func (s *server) getActiveOutputGeo() outputGeometry {
	if out := s.getActiveOutput(); out != nil {
		return s.getOutputGeometry(out)
	}
	if p := s.primaryOutput(); p != nil {
		return s.getOutputGeometry(p)
	}
	return outputGeometry{}
}

// getOutputGeoForView returns the geometry of the output containing the given view position
func (s *server) getOutputGeoForView(x, y float64) outputGeometry {
	if out := s.getOutputForPosition(x, y); out != nil {
		return s.getOutputGeometry(out)
	}
	if p := s.primaryOutput(); p != nil {
		return s.getOutputGeometry(p)
	}
	return outputGeometry{}
}

// isDragActive returns true if a client drag-and-drop is in progress.
// When active, the compositor must not start a window move/resize.
func (s *server) isDragActive() bool {
	seatPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.seat))
	return C.is_drag_active((*C.struct_wlr_seat)(seatPtr)) != 0
}

// cursorNearBorder checks if cursor is near a window border and returns which edges.
// Works for both SSD and CSD windows — provides compositor-side resize for all.
// Iterates views in actual scene tree z-order (topmost first) so that cross-type
// occlusion (XDG vs XWayland) is handled correctly.
func (s *server) cursorNearBorder(x, y float64) (wlr.Edges, *xdgView, *xwayView) {
	// Helper: check if cursor is in the resize border zone around a rectangle.
	// Returns matched edges, or EdgeNone. Sets occluded=true if cursor is inside
	// the window content (not on a border) — meaning views behind are hidden.
	// outerSize: how far outside the content area to detect edges.
	// innerSize: how far inside the content area to detect edges (0 for SSD
	// since borders are rendered outside; >0 for CSD where invisible borders
	// may be clipped at screen edges).
	var occluded bool
	checkBorder := func(left, top, right, bottom, outerSize, innerSize float64) wlr.Edges {
		if x < left-outerSize || x > right+outerSize || y < top-outerSize || y > bottom+outerSize {
			return wlr.EdgeNone // Outside border zone entirely
		}
		// Content interior — not on any border zone.
		if x >= left+innerSize && x <= right-innerSize && y >= top+innerSize && y <= bottom-innerSize {
			occluded = true
			return wlr.EdgeNone
		}
		var edges wlr.Edges
		if x < left+innerSize {
			edges |= wlr.EdgeLeft
		} else if x > right-innerSize {
			edges |= wlr.EdgeRight
		}
		if y < top+innerSize {
			edges |= wlr.EdgeTop
		} else if y > bottom-innerSize {
			edges |= wlr.EdgeBottom
		}
		return edges
	}

	// Get views in actual z-order from the scene tree (topmost first).
	var dataArr [64]unsafe.Pointer
	count := int(C.get_views_z_order(
		(*C.struct_wlr_scene_tree)(s.windowsTree),
		&dataArr[0], 64))

	for i := 0; i < count; i++ {
		xdgV, xwayV := s.viewFromNodeData(dataArr[i])

		if xdgV != nil {
			v := xdgV
			if !v.mapped || !v.onDesk(s.currentDesk) {
				continue
			}

			geo := v.xdgToplevel.Base().GetGeometry()
			w, h := float64(geo.Dx()), float64(geo.Dy())
			left := v.x
			right := left + w
			top := v.y
			bottom := top + h

			// Maximized/fullscreen windows have no resize handles but still occlude
			if v.fullscreen || v.maximized {
				if v.decorated {
					top -= float64(titlebarHeight)
				}
				if x >= left && x < right && y >= top && y < bottom {
					occluded = true
				}
			} else if v.decorated {
				top = v.y - float64(titlebarHeight)
				// SSD: borders are outside content, so inner hit zone = 0
				if edges := checkBorder(left, top, right, bottom, float64(edgeHitSize), 0); edges != wlr.EdgeNone {
					return edges, v, nil
				}
			} else {
				csdTitlebarSafe := top + 40.0
				csdCorner := float64(csdEdgeHitSize)
				if y >= top-csdCorner && y < csdTitlebarSafe && x >= left-csdCorner && x <= right+csdCorner {
					occluded = true
				} else {
					// CSD: resize zone is outside-only (innerSize=0) so clicks in
					// window content are never consumed by the resize grab.
					if edges := checkBorder(left, top, right, bottom, float64(csdEdgeHitSize), 0); edges != wlr.EdgeNone {
						edges &^= wlr.EdgeTop
						if edges != wlr.EdgeNone {
							return edges, v, nil
						}
					}
				}
			}
		} else if xwayV != nil {
			v := xwayV
			if !v.mapped || v.isPanel || v.isOverlay || !v.onDesk(s.currentDesk) {
				continue
			}

			w, h := float64(v.surface.Width()), float64(v.surface.Height())
			left := v.x
			right := v.x + w
			bottom := v.y + h
			top := v.y

			// Maximized/fullscreen windows have no resize handles but still occlude
			if v.fullscreen || v.maximized {
				if v.decorated {
					top -= float64(titlebarHeight)
				}
				if x >= left && x < right && y >= top && y < bottom {
					occluded = true
				}
			} else if v.decorated {
				top -= float64(titlebarHeight)
				// SSD: borders are outside content, so inner hit zone = 0
				if edges := checkBorder(left, top, right, bottom, float64(edgeHitSize), 0); edges != wlr.EdgeNone {
					return edges, nil, v
				}
			} else {
				csdTitlebarSafe := top + 40.0
				csdCorner := float64(csdEdgeHitSize)
				if y >= top-csdCorner && y < csdTitlebarSafe && x >= left-csdCorner && x <= right+csdCorner {
					occluded = true
				} else {
					// CSD: resize zone is outside-only (innerSize=0) so clicks in
					// window content are never consumed by the resize grab.
					if edges := checkBorder(left, top, right, bottom, float64(csdEdgeHitSize), 0); edges != wlr.EdgeNone {
						edges &^= wlr.EdgeTop
						if edges != wlr.EdgeNone {
							return edges, nil, v
						}
					}
				}
			}
		}
		if occluded {
			return wlr.EdgeNone, nil, nil
		}
	}

	return wlr.EdgeNone, nil, nil
}

// viewAtDecoration finds a view by checking decoration areas (titlebar, buttons).
// Iterates views in actual scene tree z-order (topmost first) so that a foreground
// window's content area occludes background window decorations.
func (s *server) viewAtDecoration(x, y float64) (*xdgView, *xwayView, decoZone) {
	// Get views in actual z-order from the scene tree (topmost first).
	var dataArr [64]unsafe.Pointer
	count := int(C.get_views_z_order(
		(*C.struct_wlr_scene_tree)(s.windowsTree),
		&dataArr[0], 64))

	for i := 0; i < count; i++ {
		xdgV, xwayV := s.viewFromNodeData(dataArr[i])

		if xdgV != nil {
			v := xdgV
			if !v.mapped || !v.onDesk(s.currentDesk) {
				continue
			}

			geo := v.xdgToplevel.Base().GetGeometry()
			w, h := geo.Dx(), geo.Dy()

			// Check decoration hit (SSD windows only)
			if v.decorated {
				zone := s.hitTestDecoration(x, y, v.x, v.y, w, h)
				if zone != decoNone {
					return v, nil, zone
				}
			}

			// Occlusion: if cursor is inside this view's visible area,
			// no lower-z decoration can be reached.
			top := v.y
			if v.decorated {
				top -= float64(titlebarHeight)
			}
			if x >= v.x && x < v.x+float64(w) && y >= top && y < v.y+float64(h) {
				return nil, nil, decoNone
			}
		} else if xwayV != nil {
			v := xwayV
			if !v.mapped || v.isPanel || !v.onDesk(s.currentDesk) {
				continue
			}

			w := float64(v.surface.Width())
			h := float64(v.surface.Height())

			// Check decoration hit (SSD windows only)
			if v.decorated {
				zone := s.hitTestDecoration(x, y, v.x, v.y, int(w), int(h))
				if zone != decoNone {
					return nil, v, zone
				}
			}

			// Occlusion: if cursor is inside this view's visible area,
			// no lower-z decoration can be reached.
			top := v.y
			if v.decorated {
				top -= float64(titlebarHeight)
			}
			if x >= v.x && x < v.x+w && y >= top && y < v.y+h {
				return nil, nil, decoNone
			}
		}
	}

	return nil, nil, decoNone
}

func (s *server) viewAt(x, y float64) (*xdgView, *xwayView, wlr.Surface, float64, float64) {
	scene := (*C.struct_wlr_scene)(s.scene)

	var cSurface *C.struct_wlr_surface
	var sx, sy C.double
	var data unsafe.Pointer

	C.scene_view_at(scene, C.double(x), C.double(y), &cSurface, &sx, &sy, &data)

	if cSurface == nil {
		// No surface hit — might be on a decoration (rect/buffer) or empty space.
		// Resolve the view from node data so decoration clicks still work.
		if data != nil {
			xdgV, xwayV := s.viewFromNodeData(data)
			return xdgV, xwayV, wlr.Surface{}, 0, 0
		}
		return nil, nil, wlr.Surface{}, 0, 0
	}

	surface := surfaceFromCPtr(cSurface)
	xdgV, xwayV := s.viewFromNodeData(data)
	return xdgV, xwayV, surface, float64(sx), float64(sy)
}

// simulateClick moves the cursor to (x,y) and sends a left button press+release.
// Used for testing panel UI interactions via IPC.
func (s *server) simulateClick(x, y float64) {
	type curPtr struct{ p *C.struct_wlr_cursor }
	cp := (*curPtr)(unsafe.Pointer(&s.cursor))
	C.cursor_warp_closest(cp.p, C.double(x), C.double(y))
	t := time.Now()
	s.processCursorMotion(t)
	// Press
	s.handleCursorButton(wlr.Pointer{}, t, 272, wlr.ButtonPressed)
	// Release
	s.handleCursorButton(wlr.Pointer{}, t, 272, wlr.ButtonReleased)
	log.Printf("[SIMULATE] Click at (%.0f, %.0f)", x, y)
}

// injectCtrlV sends a synthetic Ctrl+V key sequence to the focused client.
// Uses the CGO helper which properly updates XKB modifier state.
func (s *server) injectCtrlV(kb wlr.Keyboard) {
	type seatPtr struct{ p *C.struct_wlr_seat }
	type kbPtr struct{ p *C.struct_wlr_keyboard }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	kbP := (*kbPtr)(unsafe.Pointer(&kb))
	s.seat.SetKeyboard(kb)
	C.inject_ctrl_v(seatP.p, kbP.p)
}

// setClipboard sets the Wayland clipboard selection to the given text.
// Uses a compositor-owned wlr_data_source so the content persists
// independently of any client window.
func (s *server) setClipboard(text string) {
	type seatPtr struct{ p *C.struct_wlr_seat }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	C.set_clipboard_text(seatP.p, cText)
}

// debugViewAt logs information about what view is under the given coordinates.
func (s *server) debugViewAt(x, y float64) {
	var surf *C.struct_wlr_surface
	var sx, sy C.double
	var data unsafe.Pointer
	C.scene_view_at((*C.struct_wlr_scene)(s.scene), C.double(x), C.double(y), &surf, &sx, &sy, &data)

	if data == nil {
		log.Printf("[VIEWAT] (%v,%v): data=nil surf=%v", x, y, surf != nil)
		return
	}

	// Check if it's a known view
	for _, v := range s.xdgViews {
		if unsafe.Pointer(v.sceneTree) == data || unsafe.Pointer(v.surfaceTree) == data {
			log.Printf("[VIEWAT] (%v,%v): XDG view %q at (%.0f,%.0f) size=%dx%d mapped=%v",
				x, y, v.id, v.x, v.y, v.configuredW, v.configuredH, v.mapped)
			return
		}
	}
	for _, v := range s.xwayViews {
		if unsafe.Pointer(v.sceneTree) == data {
			log.Printf("[VIEWAT] (%v,%v): XWay view %q isPanel=%v mapped=%v",
				x, y, v.id, v.isPanel, v.mapped)
			return
		}
	}
	log.Printf("[VIEWAT] (%v,%v): unknown data=%p surf=%v", x, y, data, surf != nil)
}

func (s *server) dumpSceneLayers() {
	type layerInfo struct {
		name string
		tree unsafe.Pointer
	}
	layers := []layerInfo{
		{"backgroundTree", s.backgroundTree},
		{"panelTree", s.panelTree},
		{"windowsTree", s.windowsTree},
		{"fullscreenTree", s.fullscreenTree},
		{"overlayTree", s.overlayTree},
	}
	for _, l := range layers {
		cLabel := C.CString(l.name)
		C.debug_scene_tree_buffers((*C.struct_wlr_scene_tree)(l.tree), cLabel)
		C.free(unsafe.Pointer(cLabel))
	}
}

// --- Go wrappers for C scene operations (used by other input_*.go files) ---

// setPanelTreeEnabled enables or disables the panel scene tree node.
func (s *server) setPanelTreeEnabled(enabled bool) {
	val := C.int(0)
	if enabled {
		val = 1
	}
	panelNode := &(*C.struct_wlr_scene_tree)(s.panelTree).node
	C.scene_node_set_enabled(panelNode, val)
}

// raisePanelAboveOverlay places the panel tree above the overlay tree in z-order.
func (s *server) raisePanelAboveOverlay() {
	panelNode := &(*C.struct_wlr_scene_tree)(s.panelTree).node
	overlayNode := &(*C.struct_wlr_scene_tree)(s.overlayTree).node
	C.scene_node_place_above(panelNode, overlayNode)
}

// lowerPanelBelowWindows places the panel tree below the windows tree in z-order.
func (s *server) lowerPanelBelowWindows() {
	panelNode := &(*C.struct_wlr_scene_tree)(s.panelTree).node
	windowsNode := &(*C.struct_wlr_scene_tree)(s.windowsTree).node
	C.scene_node_place_below(panelNode, windowsNode)
}

// updateDragIconPos updates the drag icon position in the scene tree.
func (s *server) updateDragIconPos(x, y int) {
	C.update_drag_icon_position(C.int(x), C.int(y))
}
