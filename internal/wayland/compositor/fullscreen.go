package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <stdio.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/xwayland.h>

static int xdg_toplevel_wants_fullscreen(struct wlr_xdg_toplevel *toplevel) {
	return toplevel->requested.fullscreen;
}

static int xway_surface_wants_fullscreen(struct wlr_xwayland_surface *surface) {
	return surface->fullscreen;
}

static struct wlr_xdg_toplevel *get_xdg_toplevel_parent(struct wlr_xdg_toplevel *toplevel) {
	return toplevel->parent;
}

static struct wlr_xwayland_surface *get_xway_surface_parent(struct wlr_xwayland_surface *surface) {
	return surface->parent;
}

// --- XWayland associate listener (heap-allocated, self-cleaning) ---
// For menus/popups where wlr_surface isn't ready at new_surface time.
// The associate event fires when the inner wlr_surface becomes valid.

struct xway_associate_listener {
	struct wl_listener associate_listener;
	struct wl_listener destroy_listener;
	struct wlr_xwayland_surface *surface;
};

extern void goXwayAssociate(void *surface);

static void handle_xway_assoc_destroy(struct wl_listener *listener, void *data) {
	struct xway_associate_listener *xal = wl_container_of(listener, xal, destroy_listener);
	wl_list_remove(&xal->associate_listener.link);
	wl_list_remove(&xal->destroy_listener.link);
	free(xal);
}

static void handle_xway_associate(struct wl_listener *listener, void *data) {
	struct xway_associate_listener *xal = wl_container_of(listener, xal, associate_listener);
	struct wlr_xwayland_surface *xs = xal->surface;
	goXwayAssociate(xs);
	// Self-clean: one-shot listener
	wl_list_remove(&xal->associate_listener.link);
	wl_list_remove(&xal->destroy_listener.link);
	free(xal);
}

// Returns 1 if the surface is already associated (surface->surface != NULL),
// meaning the associate event already fired and we should call setup directly.
// Returns 0 if not yet associated and a listener was registered.
static int listen_xway_associate(struct wlr_xwayland_surface *surface) {
	if (surface->surface != NULL) {
		// Already associated — no point listening, caller should setup now
		return 1;
	}
	struct xway_associate_listener *xal = calloc(1, sizeof(*xal));
	xal->surface = surface;
	xal->associate_listener.notify = handle_xway_associate;
	wl_signal_add(&surface->events.associate, &xal->associate_listener);
	xal->destroy_listener.notify = handle_xway_assoc_destroy;
	wl_signal_add(&surface->events.destroy, &xal->destroy_listener);
	return 0;
}
*/
import "C"

import (
	"log"
	"unsafe"

	"deedles.dev/wlr"
)

//export goXdgRequestFullscreen
func goXdgRequestFullscreen(toplevel unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	cToplevel := (*C.struct_wlr_xdg_toplevel)(toplevel)
	wantsFS := int(C.xdg_toplevel_wants_fullscreen(cToplevel)) != 0

	// Find the matching xdgView
	for _, v := range s.xdgViews {
		if xdgToplevelPtr(v.xdgToplevel) == cToplevel {
			log.Printf("[FULLSCREEN] XDG request_fullscreen: app_id=%q fullscreen=%v\n",
				getXdgToplevelAppID(v.xdgToplevel), wantsFS)
			log.Printf("[FULLSCREEN] About to call fullscreenXdgWindow(v, %v) — v.fullscreen=%v", wantsFS, v.fullscreen)
			s.fullscreenXdgWindow(v, wantsFS)
			log.Printf("[FULLSCREEN] Returned from fullscreenXdgWindow")
			return
		}
	}
}

//export goXwayRequestFullscreen
func goXwayRequestFullscreen(surface unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	cSurface := (*C.struct_wlr_xwayland_surface)(surface)
	wantsFS := int(C.xway_surface_wants_fullscreen(cSurface)) != 0

	// Find the matching xwayView
	for _, v := range s.xwayViews {
		if xwaySurfacePtr(v.surface) == cSurface {
			log.Printf("[FULLSCREEN] XWayland request_fullscreen: class=%q fullscreen=%v\n",
				getXwaylandSurfaceClass(v.surface), wantsFS)
			s.fullscreenXwayWindow(v, wantsFS)
			return
		}
	}
}

//export goXwayAssociate
func goXwayAssociate(surface unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	cSurface := (*C.struct_wlr_xwayland_surface)(surface)
	for _, v := range s.xwayViews {
		if xwaySurfacePtr(v.surface) == cSurface {
			if v.pendingSetup != nil {
				v.pendingSetup()
				v.pendingSetup = nil
			}
			return
		}
	}
}

// setupXwayAssociateListener registers a CGO listener for the associate event
// on an XWayland surface. When fired, it calls goXwayAssociate to complete
// the deferred map listener setup. Returns true if the surface was already
// associated (caller should retry setup immediately).
func setupXwayAssociateListener(surface wlr.XwaylandSurface) bool {
	return C.listen_xway_associate(xwaySurfacePtr(surface)) != 0
}

//export goXdgSetParent
func goXdgSetParent(toplevel unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	cToplevel := (*C.struct_wlr_xdg_toplevel)(toplevel)
	cParent := C.get_xdg_toplevel_parent(cToplevel)

	// Find the child view
	var child *xdgView
	for _, v := range s.xdgViews {
		if xdgToplevelPtr(v.xdgToplevel) == cToplevel {
			child = v
			break
		}
	}
	if child == nil {
		return
	}

	if cParent == nil {
		// Unparenting
		child.parent = nil
		return
	}

	// Find the parent view
	for _, v := range s.xdgViews {
		if xdgToplevelPtr(v.xdgToplevel) == cParent {
			child.parent = v
			child.desk = v.desk
			log.Printf("[PARENT] XDG set_parent: child=%s parent=%s\n", child.id, v.id)
			return
		}
	}
}

//export goXwaySetParent
func goXwaySetParent(surface unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	cSurface := (*C.struct_wlr_xwayland_surface)(surface)
	cParent := C.get_xway_surface_parent(cSurface)

	// Find the child view
	var child *xwayView
	for _, v := range s.xwayViews {
		if xwaySurfacePtr(v.surface) == cSurface {
			child = v
			break
		}
	}
	if child == nil {
		return
	}

	// Skip panel and override-redirect windows
	if child.isPanel || child.overrideRedirect {
		return
	}

	if cParent == nil {
		// Unparenting
		child.parent = nil
		return
	}

	// Find the parent view
	for _, v := range s.xwayViews {
		if xwaySurfacePtr(v.surface) == cParent {
			child.parent = v
			child.desk = v.desk
			log.Printf("[PARENT] XWayland set_parent: child=%s parent=%s\n", child.id, v.id)
			return
		}
	}
}

//export goXwaySurfaceResized
func goXwaySurfaceResized(surface unsafe.Pointer, w, h C.int) {
	s := lockServer
	if s == nil {
		return
	}

	cSurface := (*C.struct_wlr_xwayland_surface)(surface)

	width := int(w)
	height := int(h)

	// Find the matching xwayView
	for _, v := range s.xwayViews {
		if xwaySurfacePtr(v.surface) == cSurface {
			// Skip unmapped, panel, overlay, and override-redirect windows
			if !v.mapped || v.isPanel || v.isOverlay || v.overrideRedirect {
				return
			}

			// Update decorations to match new surface dimensions (fixes Steam resize)
			if v.decorated && !v.fullscreen {
				s.updateXwayViewDecorations(v)
			}

			// If fullscreen and surface size differs from output, scale to match
			if v.fullscreen && width > 0 && height > 0 {
				out := s.getOutputForPosition(v.x, v.y)
				if out == nil {
					out = s.primaryOutput()
				}
				if out != nil && (width != out.width || height != out.height) {
					log.Printf("[FULLSCREEN] Surface commit resize: %dx%d (output=%dx%d), adjusting scale\n",
						width, height, out.width, out.height)
					newGeo := s.switchModeForFullscreen(out, width, height)
					v.x = float64(newGeo.x)
					v.y = float64(newGeo.y)
					v.surface.Configure(int16(v.x), int16(v.y), uint16(newGeo.width), uint16(newGeo.height))
					setXwayScenePos(v)
				}
			}
			return
		}
	}
}
