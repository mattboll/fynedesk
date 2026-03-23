package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_xdg_shell.h>

static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
    return wlr_scene_tree_create(parent);
}
static struct wlr_scene_tree *scene_xdg_surface_create(struct wlr_scene_tree *parent, struct wlr_xdg_surface *surface) {
    return wlr_scene_xdg_surface_create(parent, surface);
}

// Store/retrieve scene tree in xdg_surface->data (for popup parent lookup, like TinyWL)
static void set_xdg_surface_data(struct wlr_xdg_surface *surface, void *data) {
    surface->data = data;
}
static struct wlr_scene_tree *get_xdg_popup_parent_tree(struct wlr_xdg_popup *popup) {
    struct wlr_xdg_surface *parent = wlr_xdg_surface_try_from_wlr_surface(popup->parent);
    if (!parent || !parent->data) return NULL;
    return (struct wlr_scene_tree *)parent->data;
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

// Try to map an XDG surface's wlr_surface if it already has a buffer.
// Some Wayland-native clients (e.g. foot) commit a buffer before the
// compositor registers its OnMap listener, so the map event never fires.
static void try_map_xdg_surface(struct wlr_surface *surface) {
    if (!surface) return;
    if (wlr_surface_has_buffer(surface) && !surface->mapped) {
        wlr_surface_map(surface);
    }
}

// --- XDG fullscreen request listener (heap-allocated, self-cleaning) ---

struct xdg_fullscreen_listener {
    struct wl_listener fullscreen_listener;
    struct wl_listener destroy_listener;
    struct wlr_xdg_toplevel *toplevel;
};

extern void goXdgRequestFullscreen(void *toplevel);

static void handle_xdg_fullscreen_destroy(struct wl_listener *listener, void *data) {
    struct xdg_fullscreen_listener *xfl = wl_container_of(listener, xfl, destroy_listener);
    wl_list_remove(&xfl->fullscreen_listener.link);
    wl_list_remove(&xfl->destroy_listener.link);
    free(xfl);
}

static void handle_xdg_request_fullscreen(struct wl_listener *listener, void *data) {
    struct xdg_fullscreen_listener *xfl = wl_container_of(listener, xfl, fullscreen_listener);
    goXdgRequestFullscreen(xfl->toplevel);
}

static void listen_xdg_request_fullscreen(struct wlr_xdg_toplevel *toplevel) {
    struct xdg_fullscreen_listener *xfl = calloc(1, sizeof(*xfl));
    xfl->toplevel = toplevel;
    xfl->fullscreen_listener.notify = handle_xdg_request_fullscreen;
    wl_signal_add(&toplevel->events.request_fullscreen, &xfl->fullscreen_listener);
    xfl->destroy_listener.notify = handle_xdg_fullscreen_destroy;
    wl_signal_add(&toplevel->base->events.destroy, &xfl->destroy_listener);
}

static int xdg_toplevel_wants_fullscreen(struct wlr_xdg_toplevel *toplevel) {
    return toplevel->requested.fullscreen;
}

static void xdg_toplevel_set_fullscreen(struct wlr_xdg_toplevel *toplevel, int fullscreen) {
    wlr_xdg_toplevel_set_fullscreen(toplevel, fullscreen);
}

// --- XDG set_parent listener (heap-allocated, self-cleaning) ---

struct xdg_set_parent_listener {
    struct wl_listener set_parent_listener;
    struct wl_listener destroy_listener;
    struct wlr_xdg_toplevel *toplevel;
};

extern void goXdgSetParent(void *toplevel);

static void handle_xdg_set_parent_destroy(struct wl_listener *listener, void *data) {
    struct xdg_set_parent_listener *xpl = wl_container_of(listener, xpl, destroy_listener);
    wl_list_remove(&xpl->set_parent_listener.link);
    wl_list_remove(&xpl->destroy_listener.link);
    free(xpl);
}

static void handle_xdg_set_parent(struct wl_listener *listener, void *data) {
    struct xdg_set_parent_listener *xpl = wl_container_of(listener, xpl, set_parent_listener);
    goXdgSetParent(xpl->toplevel);
}

static void listen_xdg_set_parent(struct wlr_xdg_toplevel *toplevel) {
    struct xdg_set_parent_listener *xpl = calloc(1, sizeof(*xpl));
    xpl->toplevel = toplevel;
    xpl->set_parent_listener.notify = handle_xdg_set_parent;
    wl_signal_add(&toplevel->events.set_parent, &xpl->set_parent_listener);
    xpl->destroy_listener.notify = handle_xdg_set_parent_destroy;
    wl_signal_add(&toplevel->base->events.destroy, &xpl->destroy_listener);
}

static struct wlr_xdg_toplevel *get_xdg_toplevel_parent(struct wlr_xdg_toplevel *toplevel) {
    return toplevel->parent;
}
*/
import "C"

import (
	"fmt"
	"log"
	"unsafe"

	"deedles.dev/wlr"
)

func (s *server) handleNewXDGSurface(surface wlr.XDGSurface) {
	// Handle popups (menus, tooltips, dropdowns) — same pattern as TinyWL.
	// wlr_scene_xdg_surface_create handles positioning automatically via
	// popup->current.geometry on each commit.
	if surface.Role() == wlr.XDGSurfaceRolePopup {
		popup := surface.Popup()
		parentTree := C.get_xdg_popup_parent_tree(xdgPopupPtr(popup))
		if parentTree != nil {
			popupTree := C.scene_xdg_surface_create(parentTree, xdgSurfacePtr(surface))
			C.set_xdg_surface_data(xdgSurfacePtr(surface), unsafe.Pointer(popupTree))
		}
		return
	}

	if surface.Role() != wlr.XDGSurfaceRoleToplevel {
		return
	}

	toplevel := surface.Toplevel()

	s.nextViewID++
	initGeo := s.getActiveOutputGeo()
	initCx, initCy, _, _ := s.contentBounds(initGeo)
	v := &xdgView{
		id:          fmt.Sprintf("xdg-%d", s.nextViewID),
		xdgToplevel: toplevel,
		x:           float64(initCx) + 20,
		y:           float64(initCy) + 20,
		decorated:   false, // Default CSD; clients binding xdg-decoration get SSD
		opacity:     1.0,
	}
	s.xdgViews = append(s.xdgViews, v)

	// Create scene tree for this view in the windows layer
	windowsTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	if windowsTree == nil {
		log.Printf("[XDG] ERROR: windowsTree is nil, cannot create view for %s", v.id)
		return
	}
	viewTree := C.scene_tree_create(windowsTree)
	if viewTree == nil {
		log.Printf("[XDG] ERROR: scene_tree_create returned nil for %s", v.id)
		return
	}
	v.sceneTree = unsafe.Pointer(viewTree)

	// Create the XDG surface node inside the view tree
	// wlr_scene_xdg_surface_create handles subsurfaces automatically
	xdgSurf := xdgSurfacePtr(surface)
	surfTree := C.scene_xdg_surface_create(viewTree, xdgSurf)
	if surfTree == nil {
		log.Printf("[XDG] ERROR: scene_xdg_surface_create returned nil for %s", v.id)
		C.scene_node_destroy(&viewTree.node)
		v.sceneTree = nil
		return
	}
	v.surfaceTree = unsafe.Pointer(surfTree)
	// Store in xdg_surface->data so popups can find their parent scene tree
	C.set_xdg_surface_data(xdgSurf, unsafe.Pointer(surfTree))

	// Store view pointer in the scene node's data for hit-testing
	viewTree.node.data = unsafe.Pointer(v)

	// Start hidden until mapped
	C.scene_node_set_enabled(&viewTree.node, 0)

	// Handle surface map/unmap
	v.listeners = append(v.listeners, surface.Surface().OnMap(func(Surface wlr.Surface) {
		if v.sceneTree == nil {
			log.Printf("[XDG] WARNING: OnMap fired but sceneTree is nil for %s, skipping", v.id)
			return
		}
		v.mapped = true

		// Assign to current desktop by default
		v.desk = s.currentDesk

		// Read size constraints from toplevel state
		state := toplevel.Current()
		v.minWidth = int(state.MinWidth())
		v.minHeight = int(state.MinHeight())
		v.maxWidth = int(state.MaxWidth())
		v.maxHeight = int(state.MaxHeight())

		// Read protocol-level parent (set_parent signal may have fired before map)
		cParent := C.get_xdg_toplevel_parent(xdgToplevelPtr(toplevel))
		if cParent != nil && v.parent == nil {
			for _, pv := range s.xdgViews {
				if xdgToplevelPtr(pv.xdgToplevel) == cParent {
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

		// Apply per-app window rules before positioning
		appID := getXdgToplevelAppID(toplevel)
		if rule := s.matchWindowRule(appID); rule != nil {
			s.applyWindowRuleXdg(v, rule)
		}

		// Restore session window state (position, desktop, maximize)
		if sw := s.matchSessionWindow(appID); sw != nil {
			s.applySessionWindowXdg(v, sw)
		}

		surfState := Surface.Current()
		log.Printf("[DECO] XDG map: app_id=%q title=%q size=%dx%d decorated=%v parent=%v\n",
			appID, toplevel.Title(), surfState.Width(), surfState.Height(), v.decorated, v.parent != nil)

		s.positionNewXdgWindow(v, surfState.Width(), surfState.Height())

		// Apply maximize geometry if set by window rule (rule only sets flag, not geometry)
		if v.maximized {
			outGeo := s.getActiveOutputGeo()
			cx, cy, cw, ch := s.contentBounds(outGeo)
			topMargin := 0
			if v.decorated {
				topMargin = titlebarHeight
			}
			v.x = float64(cx)
			v.y = float64(cy + topMargin)
			v.xdgToplevel.SetSize(int32(cw), int32(ch-topMargin))
			v.xdgToplevel.SetMaximized(true)
			v.configuredW = cw
			v.configuredH = ch - topMargin
		}

		// Determine if open animation will run
		onCurrentDesk := v.pinned || v.desk == s.currentDesk
		willAnimate := onCurrentDesk && !s.reduceMotion && v.parent == nil && !v.fullscreen

		// Enable scene node only if on current desktop AND no animation
		// (animation keeps it hidden until it finishes)
		if onCurrentDesk && !willAnimate {
			C.scene_node_set_enabled(&viewTree.node, 1)
		}
		// If decorated, offset the surface down by titlebarHeight
		if v.decorated {
			surfT := (*C.struct_wlr_scene_tree)(v.surfaceTree)
			C.scene_node_set_position(&surfT.node, 0, C.int(titlebarHeight))
			// Create decoration nodes
			_, _, v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = s.createDecoNodes(viewTree, surfState.Width(), surfState.Height(), false)
			s.updateXdgViewDecorations(v)
		}
		setXdgScenePos(v)

		// Create modal scrim behind dialog windows (parent != nil)
		if v.parent != nil && onCurrentDesk {
			s.createModalScrimXdg(v)
		}

		if onCurrentDesk {
			s.focusXdgView(v)
			if willAnimate {
				s.startOpenAnimXdg(v)
			}
		}
		s.writeWindowsState()
		s.retile()
	}))

	v.listeners = append(v.listeners, surface.Surface().OnUnmap(func(Surface wlr.Surface) {
		v.mapped = false
		destroyModalScrim(&v.scrimRect)
		C.scene_node_set_enabled(&viewTree.node, 0)
		s.writeWindowsState()
		s.retile()
	}))

	destroyIdx := len(v.listeners) // index where OnDestroy will be stored
	v.listeners = append(v.listeners, surface.OnDestroy(func(XDGSurface wlr.XDGSurface) {
		// Destroy all listeners EXCEPT this OnDestroy listener (currently executing).
		// Failing to do this causes use-after-free: when the wlr_surface is freed,
		// dangling wl_listener nodes corrupt the signal list, leading to SIGSEGV
		// on subsequent scene tree operations.
		for i, lis := range v.listeners {
			if i == destroyIdx {
				continue // skip the currently-executing OnDestroy listener
			}
			lis.Destroy()
		}
		v.listeners = nil

		// Destroy modal scrim if any
		destroyModalScrim(&v.scrimRect)
		// Start close glitch animation before destroying the scene node
		s.startCloseAnimXdg(v)

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

		// Clear parent reference on all children before removing
		for _, cv := range s.xdgViews {
			if cv.parent == v {
				cv.parent = nil
			}
		}
		// Scene node cleanup: wlr_scene_node_destroy recursively destroys children
		if v.sceneTree != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(v.sceneTree).node)
			v.sceneTree = nil
		}
		wasActive := s.activeXdg == v
		for i, view := range s.xdgViews {
			if view == v {
				s.xdgViews = append(s.xdgViews[:i], s.xdgViews[i+1:]...)
				if s.activeXdg == v {
					s.activeXdg = nil
				}
				break
			}
		}
		// Focus next window if this was active
		if wasActive {
			s.focusTopmostOnDesk(s.currentDesk)
		}
		s.writeWindowsState()
		s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
		s.retile()
	}))

	// Handle client-initiated move request (titlebar drag)
	v.listeners = append(v.listeners, toplevel.OnRequestMove(func(t wlr.XDGToplevel, client wlr.SeatClient, serial uint32) {
		log.Printf("[MOVE] XDG OnRequestMove: app_id=%q maximized=%v\n",
			getXdgToplevelAppID(toplevel), v.maximized)
		s.focusXdgView(v)
		if v.maximized {
			// Don't start grab for maximized windows — let client handle
			// double-click (unmaximize). Drag-to-unmaximize can be added later.
			return
		}
		s.beginGrabMove(v, nil)
	}))

	// Handle client-initiated resize request (window border drag)
	v.listeners = append(v.listeners, toplevel.OnRequestResize(func(t wlr.XDGToplevel, client wlr.SeatClient, serial uint32, edges wlr.Edges) {
		log.Printf("[RESIZE] XDG OnRequestResize: app_id=%q edges=%d\n", getXdgToplevelAppID(toplevel), edges)
		s.focusXdgView(v)
		s.beginGrabResize(v, nil, edges)
	}))

	// Handle client-initiated maximize request (e.g. GTK headerbar maximize button)
	v.listeners = append(v.listeners, toplevel.OnRequestMaximize(func(t wlr.XDGToplevel) {
		log.Printf("[MAXIMIZE] XDG OnRequestMaximize: app_id=%q title=%q maximized=%v\n",
			getXdgToplevelAppID(toplevel), toplevel.Title(), v.maximized)
		s.focusXdgView(v)
		s.maximizeXdgWindow(v)
	}))

	// Handle client-initiated fullscreen request (e.g. video player fullscreen button)
	// Go wlr bindings lack OnRequestFullscreen, so we use a CGO listener
	C.listen_xdg_request_fullscreen(xdgToplevelPtr(toplevel))

	// Listen for parent changes (protocol-level transient_for)
	C.listen_xdg_set_parent(xdgToplevelPtr(toplevel))

	// Try to map if the surface already has a buffer committed.
	// Some Wayland-native clients (foot, alacritty) commit their first buffer
	// before the compositor registers the OnMap listener above.
	C.try_map_xdg_surface(surfacePtr(surface.Surface()))
}

func (s *server) positionNewXdgWindow(v *xdgView, winWidth, winHeight int) {
	// Don't reposition maximized windows (maximize already set position/size)
	if v.maximized {
		return
	}
	// Place window on the output under the cursor
	outGeo := s.getActiveOutputGeo()
	cx, cy, cw, ch := s.contentBounds(outGeo)

	contentX := cx + 20
	contentY := cy + 20
	contentWidth := cw - 40
	contentHeight := ch - 40

	// Reserve space for SSD titlebar so it doesn't go off-screen
	if v.decorated {
		contentY += titlebarHeight
		contentHeight -= titlebarHeight
	}

	// Calculate cascade position (per-output)
	outName := s.getActiveOutput().output.Name()
	if s.cascadeOffsets == nil {
		s.cascadeOffsets = map[string]int{}
	}
	offset := s.cascadeOffsets[outName] * cascadeStep

	// Reset cascade if it would put window too far
	if offset > contentWidth/3 || offset > contentHeight/3 {
		s.cascadeOffsets[outName] = 0
		offset = 0
	}

	// Request smaller size if window exceeds content area
	resized := false
	if winWidth > contentWidth {
		winWidth = contentWidth
		resized = true
	}
	if winHeight > contentHeight {
		winHeight = contentHeight
		resized = true
	}
	if resized {
		v.xdgToplevel.SetSize(int32(winWidth), int32(winHeight))
	}

	if winWidth > 0 && winHeight > 0 {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
		if int(v.x)+winWidth > contentX+contentWidth {
			v.x = float64(contentX)
		}
		if int(v.y)+winHeight > contentY+contentHeight {
			v.y = float64(contentY)
		}
	} else {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
	}

	s.cascadeOffsets[outName] = (s.cascadeOffsets[outName] + 1) % maxCascade
}

func (s *server) closeXdgWindow(v *xdgView) {
	v.xdgToplevel.SendClose()
}

func (s *server) maximizeXdgWindow(v *xdgView) {
	oldX, oldY := v.x, v.y

	if v.maximized {
		// Restore
		log.Printf("[MAXIMIZE] Restoring XDG: app_id=%q saved=(%v,%v %dx%d)\n",
			getXdgToplevelAppID(v.xdgToplevel), v.savedX, v.savedY, v.savedWidth, v.savedHeight)

		// If saved size is 0 (maximize before map), use a reasonable default
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)
		restW, restH := v.savedWidth, v.savedHeight
		if restW <= 0 || restH <= 0 {
			restW = cw * 2 / 3
			restH = ch * 2 / 3
		}

		// Validate restored position — if saved position is outside content area
		// (e.g. 0,0 behind the bar), center the window instead
		restX, restY := v.savedX, v.savedY
		if int(restX) < cx || int(restY) < cy ||
			int(restX)+restW > cx+cw || int(restY)+restH > cy+ch {
			restX = float64(cx + (cw-restW)/2)
			restY = float64(cy + (ch-restH)/2)
		}

		v.xdgToplevel.SetSize(int32(restW), int32(restH))
		v.xdgToplevel.SetMaximized(false)
		v.maximized = false
		v.snapped = snapNone
		v.configuredW = restW
		v.configuredH = restH
		s.animateXdgPos(v, oldX, oldY, restX, restY)
	} else {
		// Save current geometry only from normal state (preserve across snap→maximize)
		if v.snapped == snapNone {
			geo := v.xdgToplevel.Base().GetGeometry()
			v.savedX = v.x
			v.savedY = v.y
			v.savedWidth = geo.Dx()
			v.savedHeight = geo.Dy()
		}
		v.snapped = snapNone

		// Maximize to the output the window is on
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)

		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		targetX := float64(cx)
		targetY := float64(cy + topMargin)
		targetW, targetH := cw, ch-topMargin
		v.xdgToplevel.SetSize(int32(targetW), int32(targetH))
		v.xdgToplevel.SetMaximized(true)
		v.maximized = true
		v.configuredW = targetW
		v.configuredH = targetH
		s.animateXdgPos(v, oldX, oldY, targetX, targetY)
	}
}

func (s *server) minimizeXdgWindow(v *xdgView) {
	v.minimized = true
	v.mapped = false
	setViewSceneEnabled(v.sceneTree, false)
}

func (s *server) fullscreenXdgWindow(v *xdgView, enable bool) {
	log.Printf("[FULLSCREEN] fullscreenXdgWindow: enable=%v current=%v sceneTree=%v", enable, v.fullscreen, v.sceneTree != nil)
	if enable == v.fullscreen {
		log.Printf("[FULLSCREEN] SKIPPED — already in desired state")
		return
	}

	if enable {
		log.Printf("[FULLSCREEN] enable branch entered")
		// Save current geometry
		geo := v.xdgToplevel.Base().GetGeometry()
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = geo.Dx()
		v.savedHeight = geo.Dy()
		v.savedDecorated = v.decorated
		log.Printf("[FULLSCREEN] saved geo: %dx%d at (%v,%v)", geo.Dx(), geo.Dy(), v.x, v.y)

		// Fullscreen on the output the window is on
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		log.Printf("[FULLSCREEN] output=%v", out != nil)
		outGeo := s.getOutputGeometry(out)
		log.Printf("[FULLSCREEN] outGeo=%dx%d at (%d,%d)", outGeo.width, outGeo.height, outGeo.x, outGeo.y)

		// Check if the client wants a different resolution than the output
		clientW, clientH := geo.Dx(), geo.Dy()
		if clientW > 0 && clientH > 0 && (clientW != outGeo.width || clientH != outGeo.height) {
			outGeo = s.switchModeForFullscreen(out, clientW, clientH)
		}

		v.x = float64(outGeo.x)
		v.y = float64(outGeo.y)
		log.Printf("[FULLSCREEN] Setting XDG fullscreen geometry: %dx%d at (%d,%d)", outGeo.width, outGeo.height, outGeo.x, outGeo.y)
		v.xdgToplevel.SetSize(int32(outGeo.width), int32(outGeo.height))
		C.xdg_toplevel_set_fullscreen(xdgToplevelPtr(v.xdgToplevel), 1)
		v.decorated = false
		v.fullscreen = true

		// Reparent to fullscreen layer and enable it
		log.Printf("[FULLSCREEN] sceneTree=%v for %q", v.sceneTree != nil, getXdgToplevelAppID(v.xdgToplevel))
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.fullscreenTree))
			C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 1)
			// Position the fullscreen view at (0,0) on the output
			C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, C.int(outGeo.x), C.int(outGeo.y))
			log.Printf("[FULLSCREEN] Enabled fullscreenTree for %q, sceneTree pos=(%d,%d)", getXdgToplevelAppID(v.xdgToplevel), outGeo.x, outGeo.y)
			// Hide deco surface offset (CSD shadows)
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
		v.xdgToplevel.SetSize(int32(v.savedWidth), int32(v.savedHeight))
		C.xdg_toplevel_set_fullscreen(xdgToplevelPtr(v.xdgToplevel), 0)
		v.decorated = v.savedDecorated
		v.fullscreen = false
		v.configuredW = v.savedWidth
		v.configuredH = v.savedHeight

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
	setXdgScenePos(v)
	s.updateXdgViewDecorations(v)
}

func (s *server) restoreXdgWindow(v *xdgView) {
	v.minimized = false
	v.mapped = true
	setViewSceneEnabled(v.sceneTree, true)
	s.focusXdgView(v)
}

// xdgToplevelPtr extracts the *C.struct_wlr_xdg_toplevel from a wlr.XDGToplevel.
func xdgToplevelPtr(t wlr.XDGToplevel) *C.struct_wlr_xdg_toplevel {
	return *(**C.struct_wlr_xdg_toplevel)(unsafe.Pointer(&t))
}
