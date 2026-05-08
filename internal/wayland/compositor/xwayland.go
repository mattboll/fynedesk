package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <stdio.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_compositor.h>
#include <wlr/xwayland.h>

static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
    return wlr_scene_tree_create(parent);
}
static struct wlr_scene_tree *scene_subsurface_tree_create(struct wlr_scene_tree *parent, struct wlr_surface *surface) {
    return wlr_scene_subsurface_tree_create(parent, surface);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
    wlr_scene_node_set_position(node, x, y);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
    wlr_scene_node_destroy(node);
}
static void scene_node_reparent(struct wlr_scene_node *node, struct wlr_scene_tree *new_parent) {
    wlr_scene_node_reparent(node, new_parent);
}
static void scene_node_place_above(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_above(node, sibling);
}
static void scene_node_place_below(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_below(node, sibling);
}

// Debug: check surface opaque region and format
static void debug_surface_info(struct wlr_surface *surface, const char *label) {
    if (!surface) {
        printf("[SURFACE-DEBUG] %s: surface=NULL\n", label);
        fflush(stdout);
        return;
    }
    pixman_region32_t *opaque = &surface->opaque_region;
    int n = 0;
    pixman_box32_t *boxes = pixman_region32_rectangles(opaque, &n);
    printf("[SURFACE-DEBUG] %s: opaque_rects=%d", label, n);
    for (int i = 0; i < n && i < 3; i++) {
        printf(" rect[%d]=(%d,%d,%d,%d)", i, boxes[i].x1, boxes[i].y1, boxes[i].x2, boxes[i].y2);
    }
    printf(" current_w=%d current_h=%d\n", surface->current.width, surface->current.height);
    fflush(stdout);
}

// Debug: dump scene tree child order
static void dump_scene_children(struct wlr_scene_tree *root, const char *label) {
    printf("[SCENE-DEBUG] %s children order (bottom→top):\n", label);
    struct wlr_scene_node *child;
    int idx = 0;
    wl_list_for_each(child, &root->children, link) {
        printf("  [%d] node=%p type=%d enabled=%d\n", idx, child, child->type, child->enabled);
        idx++;
    }
    printf("  total=%d\n", idx);
    fflush(stdout);
}

// --- XWayland fullscreen request listener (heap-allocated, self-cleaning) ---

struct xway_fullscreen_listener {
    struct wl_listener fullscreen_listener;
    struct wl_listener destroy_listener;
    struct wlr_xwayland_surface *surface;
};

extern void goXwayRequestFullscreen(void *surface);

static void handle_xway_fullscreen_destroy(struct wl_listener *listener, void *data) {
    struct xway_fullscreen_listener *xfl = wl_container_of(listener, xfl, destroy_listener);
    wl_list_remove(&xfl->fullscreen_listener.link);
    wl_list_remove(&xfl->destroy_listener.link);
    free(xfl);
}

static void handle_xway_request_fullscreen(struct wl_listener *listener, void *data) {
    struct xway_fullscreen_listener *xfl = wl_container_of(listener, xfl, fullscreen_listener);
    goXwayRequestFullscreen(xfl->surface);
}

static void listen_xway_request_fullscreen(struct wlr_xwayland_surface *surface) {
    struct xway_fullscreen_listener *xfl = calloc(1, sizeof(*xfl));
    xfl->surface = surface;
    xfl->fullscreen_listener.notify = handle_xway_request_fullscreen;
    wl_signal_add(&surface->events.request_fullscreen, &xfl->fullscreen_listener);
    xfl->destroy_listener.notify = handle_xway_fullscreen_destroy;
    wl_signal_add(&surface->events.destroy, &xfl->destroy_listener);
}

static int xway_surface_wants_fullscreen(struct wlr_xwayland_surface *surface) {
    return surface->fullscreen;
}

static void xway_surface_set_fullscreen(struct wlr_xwayland_surface *surface, int fullscreen) {
    wlr_xwayland_surface_set_fullscreen(surface, fullscreen);
}

// --- XWayland set_parent listener (heap-allocated, self-cleaning) ---

struct xway_set_parent_listener {
    struct wl_listener set_parent_listener;
    struct wl_listener destroy_listener;
    struct wlr_xwayland_surface *surface;
};

extern void goXwaySetParent(void *surface);

static void handle_xway_set_parent_destroy(struct wl_listener *listener, void *data) {
    struct xway_set_parent_listener *xpl = wl_container_of(listener, xpl, destroy_listener);
    wl_list_remove(&xpl->set_parent_listener.link);
    wl_list_remove(&xpl->destroy_listener.link);
    free(xpl);
}

static void handle_xway_set_parent(struct wl_listener *listener, void *data) {
    struct xway_set_parent_listener *xpl = wl_container_of(listener, xpl, set_parent_listener);
    goXwaySetParent(xpl->surface);
}

static void listen_xway_set_parent(struct wlr_xwayland_surface *surface) {
    struct xway_set_parent_listener *xpl = calloc(1, sizeof(*xpl));
    xpl->surface = surface;
    xpl->set_parent_listener.notify = handle_xway_set_parent;
    wl_signal_add(&surface->events.set_parent, &xpl->set_parent_listener);
    xpl->destroy_listener.notify = handle_xway_set_parent_destroy;
    wl_signal_add(&surface->events.destroy, &xpl->destroy_listener);
}

static struct wlr_xwayland_surface *get_xway_surface_parent(struct wlr_xwayland_surface *surface) {
    return surface->parent;
}

// Try to map an XWayland surface's wlr_surface if it already has a buffer.
// In wlroots 0.17, the surface_commit listener (which triggers wlr_surface_map)
// is registered during xwayland_surface_associate. If the first buffer was
// committed before association, the map event never fires for static windows.
static void try_map_xway_surface(struct wlr_surface *surface) {
    if (!surface) return;
    if (wlr_surface_has_buffer(surface) && !surface->mapped) {
        wlr_surface_map(surface);
    }
}

// --- XWayland surface commit listener (resize detection) ---
// Tracks last committed dimensions; only calls Go when they change.
// This avoids calling into Go on every single frame commit.

struct xway_commit_listener {
    struct wl_listener commit_listener;
    struct wl_listener destroy_listener;
    struct wlr_xwayland_surface *xway_surface;
    int last_w, last_h;
};

extern void goXwaySurfaceResized(void *xway_surface, int w, int h);

static void handle_xway_commit_destroy(struct wl_listener *listener, void *data) {
    struct xway_commit_listener *xcl = wl_container_of(listener, xcl, destroy_listener);
    wl_list_remove(&xcl->commit_listener.link);
    wl_list_remove(&xcl->destroy_listener.link);
    free(xcl);
}

static void handle_xway_surface_commit(struct wl_listener *listener, void *data) {
    struct xway_commit_listener *xcl = wl_container_of(listener, xcl, commit_listener);
    struct wlr_surface *surface = xcl->xway_surface->surface;
    if (!surface) return;
    int w = surface->current.width;
    int h = surface->current.height;
    if (w != xcl->last_w || h != xcl->last_h) {
        xcl->last_w = w;
        xcl->last_h = h;
        goXwaySurfaceResized(xcl->xway_surface, w, h);
    }
}

static void listen_xway_surface_commit(struct wlr_xwayland_surface *xway_surface, struct wlr_surface *wlr_surface) {
    struct xway_commit_listener *xcl = calloc(1, sizeof(*xcl));
    xcl->xway_surface = xway_surface;
    xcl->last_w = wlr_surface->current.width;
    xcl->last_h = wlr_surface->current.height;
    xcl->commit_listener.notify = handle_xway_surface_commit;
    wl_signal_add(&wlr_surface->events.commit, &xcl->commit_listener);
    xcl->destroy_listener.notify = handle_xway_commit_destroy;
    wl_signal_add(&wlr_surface->events.destroy, &xcl->destroy_listener);
}

*/
import "C"

import (
	"fmt"
	"log"
	"strings"
	"unsafe"

	"deedles.dev/wlr"
)

func (s *server) handleNewXwaylandSurface(surface wlr.XwaylandSurface) {
	// Skip if surface is not valid
	if !surface.Valid() {
		log.Println("Warning: Invalid XWayland surface, skipping")
		return
	}

	// For XWayland windows, check MOTIF hints to decide on server-side decorations.
	// Apps with CSD (Chrome, GTK, Electron) set MOTIF hints to no-border + no-title,
	// meaning they draw their own decorations and don't want SSD from the compositor.

	isOR := isXwaylandOverrideRedirect(surface)

	s.nextViewID++
	xwInitGeo := s.getActiveOutputGeo()
	xwCx, xwCy, _, _ := s.contentBounds(xwInitGeo)
	v := &xwayView{
		id:               fmt.Sprintf("xway-%d", s.nextViewID),
		surface:          surface,
		x:                float64(xwCx) + 20,
		y:                float64(xwCy) + 20,
		overrideRedirect: isOR,
		decorated:        !isOR, // No SSD for override-redirect (popups, menus)
		opacity:          1.0,
	}
	s.xwayViews = append(s.xwayViews, v)

	// Choose parent scene tree based on window type
	// We place in windowsTree initially; will reparent to panelTree/overrideTree/overlayTree on map
	parentTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	if isOR {
		parentTree = (*C.struct_wlr_scene_tree)(s.overrideTree)
	}

	viewTree := C.scene_tree_create(parentTree)
	v.sceneTree = unsafe.Pointer(viewTree)
	viewTree.node.data = unsafe.Pointer(v)

	// Non-OR windows start hidden until OnMap fires.
	// OR windows (menus, popups) must stay enabled so wlr_scene sends
	// frame_done events to XWayland, allowing buffer commits and surface mapping.
	if !isOR {
		C.scene_node_set_enabled(&viewTree.node, 0)
	}

	// Helper to setup map/unmap listeners
	setupMapListeners := func() {
		if v.mapListenerSetup {
			return
		}
		wlrSurface := surface.Surface()
		if !wlrSurface.Valid() {
			return
		}
		v.mapListenerSetup = true

		// Create the surface node (subsurface tree for XWayland)
		surfTree := C.scene_subsurface_tree_create(viewTree, surfacePtr(wlrSurface))
		v.surfaceTree = unsafe.Pointer(surfTree)

		// Listen for surface commits to detect resize (for SSD decoration updates).
		// Skip override-redirect windows (popups/menus don't have decorations).
		if !isOR {
			C.listen_xway_surface_commit(xwaySurfacePtr(surface), surfacePtr(wlrSurface))
		}

		// Map handler — extracted so it can be called from OnMap callback
		// AND directly when the surface is already mapped at setup time.
		handleMap := func() {
			v.mapped = true
			title := surface.Title()
			w, h := surface.Width(), surface.Height()
			// Assign to current desktop by default
			v.desk = s.currentDesk

			// Override-redirect windows (popups, menus, tooltips):
			// Use X11 position from the surface (set by the client at CreateWindow time)
			if v.overrideRedirect {
				// Read actual X11 position (not content bounds default)
				x11x, x11y := getXwaylandSurfacePos(v.surface)
				v.x = float64(x11x)
				v.y = float64(x11y)
				// Inherit desktop from active window so popup is visible
				if s.activeXway != nil {
					v.desk = s.activeXway.desk
				} else if s.activeXdg != nil {
					v.desk = s.activeXdg.desk
				}
				restackXwaylandSurfaceAbove(v.surface)
				// If there's a pending overlay position request, this is a panel
				// overlay (tooltip, menu) that will be properly positioned when
				// its title is set. Keep it hidden to avoid a flash at (0,0).
				if s.pendingOverlay == nil {
					C.scene_node_set_enabled(&viewTree.node, 1)
				}
				setXwayScenePos(v)
				return
			}

			// Read protocol-level parent (set_parent signal may have fired before map)
			cParent := C.get_xway_surface_parent(xwaySurfacePtr(v.surface))
			if cParent != nil && v.parent == nil {
				for _, pv := range s.xwayViews {
					if xwaySurfacePtr(pv.surface) == cParent {
						v.parent = pv
						v.desk = pv.desk
						break
					}
				}
			}
			// Inherit desktop from parent
			if v.parent != nil {
				v.desk = v.parent.desk
			}

			// Check if this is the panel
			if strings.Contains(title, "FyneDesk:Panel") {
				v.isPanel = true
				v.decorated = false
				s.panelXway = v
				log.Printf("[PANEL] Panel detected on map: title=%q mapped=%v sceneTree=%v\n",
					title, v.mapped, v.sceneTree != nil)
				// Remove any leftover decorations (shadows, titlebar, borders)
				// created before the window was identified as the panel.
				s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
				v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
				v.decoTitlebar, v.decoTitlePix = nil, nil
				s.removeShadowsXway(v)
				if v.surfaceTree != nil {
					C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
				}
				// Reparent to panelTree
				C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
				// Panel uses ARGB8888 DMA-BUF with empty opaque_region,
				// so alpha blending works natively — no commit listener needed.
				// Position panel on primary output (both XWayland configure + scene node)
				s.repositionPanel()
			} else if strings.HasPrefix(title, "FyneDesk:Bar:") {
				// Secondary bar window for a non-primary output
				outputName := strings.TrimPrefix(title, "FyneDesk:Bar:")
				v.isPanel = true
				v.decorated = false
				s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
				v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
				v.decoTitlebar, v.decoTitlePix = nil, nil
				s.removeShadowsXway(v)
				if v.surfaceTree != nil {
					C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
				}
				C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
				if s.secondaryPanels == nil {
					s.secondaryPanels = make(map[string]*xwayView)
				}
				s.secondaryPanels[outputName] = v
				log.Printf("[PANEL] Secondary bar detected on map: output=%q title=%q", outputName, title)
				s.repositionSecondaryPanel(outputName, v)
			} else if strings.Contains(title, "FyneDesk:skip") {
				// Panel utility window (app launcher, etc.) — no decorations
				v.decorated = false
				v.isOverlay = true
				log.Printf("[OVERLAY] map: title=%q surfW=%d surfH=%d", title, surface.Width(), surface.Height())
				// Remove any decorations/shadows that may have been created before identification
				s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
				v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
				v.decoTitlebar, v.decoTitlePix = nil, nil
				s.removeShadowsXway(v)
				// Reparent to overlayTree so it appears above normal windows
				C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
				restackXwaylandSurfaceAbove(v.surface)
				// Enable scene node (may have been hidden by override-redirect handler
				// when pendingOverlay was set, to avoid flash at wrong position)
				C.scene_node_set_enabled(&viewTree.node, 1)
				s.positionOverlay(v, surface)
				if !strings.Contains(title, "FyneDesk:nofocus") {
					s.focusOverlayKeyboard(v)
				}
			} else if title == "FyneDesk Menu" || strings.Contains(title, "FyneDesk:EmojiPicker") {
				// Overlay window (context menu or emoji picker) — position from IPC
				// Save pre-overlay focus so refocusPreOverlayWindow can restore it
				s.preOverlayXdg = s.activeXdg
				s.preOverlayXway = s.activeXway
				// Close any existing overlay first
				s.closeOverlay()
				v.decorated = false
				v.isOverlay = true
				s.overlayXway = v
				// Remove any decorations/shadows that may have been created before identification
				s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
				v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
				v.decoTitlebar, v.decoTitlePix = nil, nil
				s.removeShadowsXway(v)
				// Reparent to overlayTree
				C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
				s.positionOverlay(v, surface)
				restackXwaylandSurfaceAbove(v.surface)
				s.focusOverlayKeyboard(v)
				// Set pointer focus so the first click works without mouse movement
				s.sendPointerEnterIfOver(v.x, v.y, float64(w), float64(h), v.surface.Surface())
			} else if !v.isPanel {
				// Check MOTIF hints BEFORE positioning so decorated flag is correct
				// (affects titlebar offset in positioning)
				decoHints := surface.Decorations()
				hasCSD := decoHints&wlr.XwaylandSurfaceDecorationsNoBorder != 0 &&
					decoHints&wlr.XwaylandSurfaceDecorationsNoTitle != 0
				if hasCSD {
					v.decorated = false
				}
				xwayClass := getXwaylandSurfaceClass(surface)
				log.Printf("[DECO] XWayland map: class=%q title=%q size=%dx%d hints=0x%x hasCSD=%v → decorated=%v\n",
					xwayClass, title, w, h, decoHints, hasCSD, v.decorated)

				// Apply per-app window rules before positioning
				if rule := s.matchWindowRule(xwayClass); rule != nil {
					s.applyWindowRuleXway(v, rule)
				}

				// Restore session window state (position, desktop, maximize)
				if sw := s.matchSessionWindow(xwayClass); sw != nil {
					s.applySessionWindowXway(v, sw)
				}

				// Regular window - position in content area
				s.positionNewXwayWindow(v)

				// Apply maximize geometry if set by window rule (rule only sets flag, not geometry)
				if v.maximized {
					outGeo := s.getActiveOutputGeo()
					cx, cy, cw, ch := s.contentBounds(outGeo)
					topMargin := 0
					if v.decorated {
						topMargin = titlebarHeight
					}
					targetX := float64(cx)
					targetY := float64(cy + topMargin)
					v.x, v.y = targetX, targetY
					v.surface.Configure(int16(targetX), int16(targetY), uint16(cw), uint16(ch-topMargin))
				}

				onCurrentDesk := v.pinned || v.desk == s.currentDesk
				// Create modal scrim behind dialog windows (parent != nil)
				if v.parent != nil && onCurrentDesk {
					s.createModalScrimXway(v)
				}
				s.writeWindowsState()
				s.retile()
			}

			// Determine if open animation will run (only on first map, not remaps).
			// Remaps happen when apps hide to tray then reappear (e.g. Slack).
			isNormalWindow := !v.isPanel && !v.isOverlay && !v.overrideRedirect
			onCurrentDesk := v.pinned || v.desk == s.currentDesk
			isFirstMap := !v.everMapped
			willAnimate := isNormalWindow && onCurrentDesk && !s.reduceMotion && v.parent == nil && !v.fullscreen && isFirstMap
			v.everMapped = true

			// Enable scene node only if on current desktop AND no animation
			onDesk := v.isPanel || v.isOverlay || v.overrideRedirect || v.pinned || v.desk == s.currentDesk
			if onDesk && !willAnimate {
				C.scene_node_set_enabled(&viewTree.node, 1)
			}

			// If decorated, offset the surface down by titlebarHeight
			if v.decorated && !v.isPanel && isFirstMap {
				surfT := (*C.struct_wlr_scene_tree)(v.surfaceTree)
				C.scene_node_set_position(&surfT.node, 0, C.int(titlebarHeight))
				_, _, v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = s.createDecoNodes(viewTree, w, h, false)
				s.updateXwayViewDecorations(v)
			}
			setXwayScenePos(v)

			// Focus AFTER decorations are set up
			if isNormalWindow && onCurrentDesk {
				s.focusXwayView(v)
				if willAnimate {
					s.startOpenAnimXway(v)
				}
			}
		}

		v.listeners = append(v.listeners, wlrSurface.OnMap(func(Surface wlr.Surface) {
			handleMap()
		}))

		v.listeners = append(v.listeners, wlrSurface.OnUnmap(func(Surface wlr.Surface) {
			s.ensureThumbXway(v) // capture thumbnail before unmap for close animation
			v.mapped = false
			destroyModalScrim(&v.scrimRect)
			C.scene_node_set_enabled(&viewTree.node, 0)
			if v.isOverlay && s.overlayXway == v {
				s.overlayXway = nil
				s.overlayW = 0
				s.overlayH = 0
				// Refocus the window that was active before the overlay
				s.refocusPreOverlayWindow()
				// Check if there's a pending paste request (emoji or clipboard)
				s.handleEmojiPaste()
				s.handleClipboardPaste()
			}
			if !v.isPanel && !v.isOverlay {
				s.writeWindowsState()
				s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
				s.retile()
			}
		}))

		// If the surface already has a buffer but isn't mapped yet, trigger the map.
		// In wlroots 0.17, the surface_commit listener (which calls wlr_surface_map)
		// is registered during xwayland_surface_associate. For fast override-redirect
		// windows (menus, popups), the first buffer may have been committed BEFORE
		// association, so the commit listener never fires. Calling try_map here
		// will emit events._map, which triggers our OnMap handler above.
		C.try_map_xway_surface(surfacePtr(wlrSurface))
	}

	// Handle title changes to detect panel - also try to setup map listeners
	v.listeners = append(v.listeners, surface.OnSetTitle(func(surf wlr.XwaylandSurface, title string) {
		// Try to setup map listeners if not done yet
		setupMapListeners()

		if strings.Contains(title, "FyneDesk:Panel") {
			v.isPanel = true
			v.decorated = false
			s.panelXway = v
			log.Printf("[PANEL] Panel detected on title: title=%q mapped=%v\n", title, v.mapped)
			// Remove any leftover decorations
			s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
			v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
			v.decoTitlebar, v.decoTitlePix = nil, nil
			s.removeShadowsXway(v)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
			// Reparent to panelTree if not already
			C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
			// Position panel on primary output (both XWayland configure + scene node)
			s.repositionPanel()
		} else if strings.HasPrefix(title, "FyneDesk:Bar:") {
			outputName := strings.TrimPrefix(title, "FyneDesk:Bar:")
			v.isPanel = true
			v.decorated = false
			s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
			v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
			v.decoTitlebar, v.decoTitlePix = nil, nil
			s.removeShadowsXway(v)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
			C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
			if s.secondaryPanels == nil {
				s.secondaryPanels = make(map[string]*xwayView)
			}
			s.secondaryPanels[outputName] = v
			log.Printf("[PANEL] Secondary bar detected on title: output=%q title=%q", outputName, title)
			s.repositionSecondaryPanel(outputName, v)
		} else if strings.Contains(title, "FyneDesk:skip") && v.mapped {
			// Panel utility window title set after map
			v.decorated = false
			v.isOverlay = true
			// Remove any leftover decorations
			s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
			v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
			v.decoTitlebar, v.decoTitlePix = nil, nil
			s.removeShadowsXway(v)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
			C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
			// Enable now — the node may have been kept hidden at map time
			// (pending overlay position). positionOverlay will set the correct pos.
			C.scene_node_set_enabled(&viewTree.node, 1)
			restackXwaylandSurfaceAbove(v.surface)
			s.positionOverlay(v, surface)
			if strings.Contains(title, "FyneDesk:nofocus") {
				// No focus — e.g. toast notifications
			} else {
				s.focusOverlayKeyboard(v)
			}
		} else if (title == "FyneDesk Menu" || strings.Contains(title, "FyneDesk:EmojiPicker")) && v.mapped {
			// Overlay window title set after map — reposition from IPC
			// Save pre-overlay focus (use prevReal since this window already stole focus on map)
			if s.preOverlayXdg == nil && s.preOverlayXway == nil {
				s.preOverlayXdg = s.prevRealXdg
				s.preOverlayXway = s.prevRealXway
			}
			s.closeOverlay()
			v.decorated = false
			// Remove any leftover decorations
			s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
			v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
			v.decoTitlebar, v.decoTitlePix = nil, nil
			s.removeShadowsXway(v)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
			v.isOverlay = true
			s.overlayXway = v
			C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
			s.positionOverlay(v, surface)
			restackXwaylandSurfaceAbove(v.surface)
			s.focusOverlayKeyboard(v)
			// Set pointer focus so the first click works without mouse movement
			w, h := surface.Width(), surface.Height()
			s.sendPointerEnterIfOver(v.x, v.y, float64(w), float64(h), v.surface.Surface())
		}
	}))

	// Handle runtime MOTIF hint changes (some apps set hints after map)
	v.listeners = append(v.listeners, surface.OnSetDecorations(func(surf wlr.XwaylandSurface) {
		if !v.mapped || v.isPanel || v.isOverlay {
			return
		}
		decoHints := surf.Decorations()
		hasCSD := decoHints&wlr.XwaylandSurfaceDecorationsNoBorder != 0 &&
			decoHints&wlr.XwaylandSurfaceDecorationsNoTitle != 0
		log.Printf("[DECO] XWayland OnSetDecorations: class=%q hints=0x%x hasCSD=%v was_decorated=%v\n",
			getXwaylandSurfaceClass(surf), decoHints, hasCSD, v.decorated)
		if hasCSD && v.decorated {
			v.decorated = false
			// Remove existing decorations
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
			s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
			v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
			v.decoTitlebar, v.decoTitlePix = nil, nil
			setXwayScenePos(v)
		}
	}))

	// Try to setup map listeners immediately
	setupMapListeners()
	// If surface wasn't associated yet (common for override-redirect menus/popups),
	// defer setup to the associate event when wlr_surface becomes valid.
	if !v.mapListenerSetup {
		v.pendingSetup = setupMapListeners
		alreadyAssociated := setupXwayAssociateListener(surface)
		if alreadyAssociated {
			// C surface->surface is set but Go bindings returned invalid —
			// retry setup now (the associate event already fired).
			log.Printf("[XWAY-SETUP] surface already associated at C level, retrying id=%s\n", v.id)
			setupMapListeners()
		}
	}

	xwayDestroyIdx := len(v.listeners) // index where OnDestroy will be stored
	v.listeners = append(v.listeners, surface.OnDestroy(func(surf wlr.XwaylandSurface) {
		// Destroy all listeners EXCEPT this OnDestroy listener (currently executing).
		// Failing to do this causes use-after-free: when the wlr_surface is freed,
		// dangling wl_listener nodes corrupt the signal list, leading to SIGSEGV.
		for i, lis := range v.listeners {
			if i == xwayDestroyIdx {
				continue
			}
			lis.Destroy()
		}
		v.listeners = nil

		// Destroy modal scrim if any
		destroyModalScrim(&v.scrimRect)
		// Start close glitch animation before destroying the scene node
		s.startCloseAnimXway(v)

		// Restore output mode if window was fullscreen when destroyed
		if v.fullscreen {
			out := s.getOutputForPosition(v.x, v.y)
			if out == nil {
				out = s.primaryOutput()
			}
			if out != nil {
				s.restoreModeAfterFullscreen(out)
			}
		}

		// Handle overlay destruction (process killed without unmap)
		if v.isOverlay && s.overlayXway == v {
			s.overlayXway = nil
			s.overlayW = 0
			s.overlayH = 0
			s.refocusPreOverlayWindow()
			s.handleEmojiPaste()
			s.handleClipboardPaste()
		}
		// Clear parent reference on all children before removing
		for _, cv := range s.xwayViews {
			if cv.parent == v {
				cv.parent = nil
			}
		}
		// Scene node cleanup
		if v.sceneTree != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(v.sceneTree).node)
			v.sceneTree = nil
		}
		wasActive := s.activeXway == v && !v.isPanel && !v.overrideRedirect && !v.isOverlay
		wasPanel := v.isPanel
		for i, view := range s.xwayViews {
			if view == v {
				s.xwayViews = append(s.xwayViews[:i], s.xwayViews[i+1:]...)
				if s.activeXway == v {
					s.activeXway = nil
				}
				if s.panelXway == v {
					s.panelXway = nil
				}
				// Remove from secondary panels if applicable
				for name, sv := range s.secondaryPanels {
					if sv == v {
						delete(s.secondaryPanels, name)
						log.Printf("[PANEL] Secondary bar removed: output=%q", name)
						break
					}
				}
				break
			}
		}
		// Focus next window if this was active (not for override-redirect popups or overlays)
		if wasActive {
			s.focusTopmostOnDesk(s.currentDesk)
		}
		if !wasPanel && !v.overrideRedirect {
			s.writeWindowsState()
			s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
			s.retile()
		}
	}))

	// Handle configure requests
	v.listeners = append(v.listeners, surface.OnRequestConfigure(func(surf wlr.XwaylandSurface, x, y int16, width, height uint16) {
		if v.isPanel {
			if v == s.panelXway {
				s.repositionPanel()
			} else {
				// Secondary bar — reposition on its target output
				for name, sv := range s.secondaryPanels {
					if sv == v {
						s.repositionSecondaryPanel(name, v)
						break
					}
				}
			}
		} else {
			// Constrain to output bounds (prevent overflow causing off-screen buttons).
			// Use the view's current position if already mapped — the request's (x,y)
			// may be (0,0) which resolves to the primary output even when the window
			// is visually on a secondary screen.
			lookupX, lookupY := float64(x), float64(y)
			if v.mapped && (v.x != 0 || v.y != 0) {
				lookupX, lookupY = v.x, v.y
			}
			outGeo := s.getOutputGeoForView(lookupX, lookupY)
			_, _, cw, ch := s.contentBounds(outGeo)
			if int(width) > cw {
				width = uint16(cw)
			}
			if int(height) > ch {
				height = uint16(ch)
			}
			// Track position for hit-testing (especially for popups)
			v.x = float64(x)
			v.y = float64(y)
			surface.Configure(x, y, width, height)
			setXwayScenePos(v)
			if v.decorated {
				s.updateXwayViewDecorations(v)
			}
		}
	}))

	// Handle client-initiated move request (titlebar drag)
	v.listeners = append(v.listeners, surface.OnRequestMove(func(surf wlr.XwaylandSurface) {
		log.Printf("[MOVE] XWayland OnRequestMove: class=%q title=%q\n",
			getXwaylandSurfaceClass(surf), surf.Title())
		if !v.isPanel {
			restackXwaylandSurfaceAbove(v.surface)
			s.focusXwayView(v)
			s.beginGrabMove(nil, v)
		}
	}))

	// Handle client-initiated resize request (window border drag)
	v.listeners = append(v.listeners, surface.OnRequestResize(func(surf wlr.XwaylandSurface, edges wlr.Edges) {
		if !v.isPanel {
			restackXwaylandSurfaceAbove(v.surface)
			s.focusXwayView(v)
			s.beginGrabResize(nil, v, edges)
		}
	}))

	// Handle client-initiated fullscreen request (e.g. Firefox video fullscreen button)
	// Go wlr bindings lack OnRequestFullscreen, so we use a CGO listener
	if !v.isPanel && !v.overrideRedirect {
		C.listen_xway_request_fullscreen(xwaySurfacePtr(v.surface))
	}

	// Listen for parent changes (X11 transient_for / WM_TRANSIENT_FOR)
	C.listen_xway_set_parent(xwaySurfacePtr(v.surface))
}


func (s *server) fullscreenXwayWindow(v *xwayView, enable bool) {
	if enable == v.fullscreen {
		return
	}

	if enable {
		// Save current geometry
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = v.surface.Width()
		v.savedHeight = v.surface.Height()

		// Fullscreen on the output the window is on
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		outGeo := s.getOutputGeometry(out)

		// Only switch output mode if the surface demands a HIGHER resolution
		// than the output offers (legacy XWayland games). Surfaces smaller
		// than the output will be told to grow to fill it; switching modes
		// for them would churn the panel layout for nothing.
		surfW, surfH := v.surface.Width(), v.surface.Height()
		if surfW > outGeo.width || surfH > outGeo.height {
			outGeo = s.switchModeForFullscreen(out, surfW, surfH)
		}

		v.x = float64(outGeo.x)
		v.y = float64(outGeo.y)
		v.surface.Configure(int16(v.x), int16(v.y), uint16(outGeo.width), uint16(outGeo.height))
		C.xway_surface_set_fullscreen(xwaySurfacePtr(v.surface), 1)
		v.fullscreen = true

		// Reparent to fullscreen layer
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.fullscreenTree))
			C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 1)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
		}
	} else {
		// Restore mode before restoring window geometry
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}

		v.x = v.savedX
		v.y = v.savedY
		v.surface.Configure(int16(v.savedX), int16(v.savedY), uint16(v.savedWidth), uint16(v.savedHeight))
		C.xway_surface_set_fullscreen(xwaySurfacePtr(v.surface), 0)
		v.fullscreen = false

		// Reparent back to windowsTree
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.windowsTree))
			C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 0)
			if v.decorated && v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, C.int(titlebarHeight))
			}
		}

		// Restore output mode after reparenting
		s.restoreModeAfterFullscreen(out)

		// Reset panel hotspot — panel returns to normal z-order
		s.hidePanelHotspot()
	}
	setXwayScenePos(v)
	s.updateXwayViewDecorations(v)
}


func (s *server) debugSetPanelEnabled(enabled bool) {
	if enabled {
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.panelTree).node, 1)
	} else {
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.panelTree).node, 0)
	}
}

func (s *server) dumpSceneOrder(label string) {
	// wlr_scene.tree is the first field
	sceneTree := &(*C.struct_wlr_scene)(s.scene).tree
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.dump_scene_children(sceneTree, cLabel)
}


// xwaySurfacePtr extracts the *C.struct_wlr_xwayland_surface from a wlr.XwaylandSurface.
func xwaySurfacePtr(s wlr.XwaylandSurface) *C.struct_wlr_xwayland_surface {
	return *(**C.struct_wlr_xwayland_surface)(unsafe.Pointer(&s))
}
