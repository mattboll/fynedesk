package compositor

/*
#include "restricted_globals.h"
#include <stdlib.h>
#include <string.h>
#include <wayland-server-core.h>
#include <wlr/render/allocator.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/types/wlr_ext_foreign_toplevel_list_v1.h>
#include <wlr/types/wlr_ext_image_capture_source_v1.h>
#include <wlr/types/wlr_ext_image_copy_capture_v1.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_screencopy_v1.h>
#include <wlr/types/wlr_xdg_shell.h>

static struct wlr_ext_foreign_toplevel_list_v1 *capture_toplevel_list;
static struct wlr_ext_foreign_toplevel_image_capture_source_manager_v1 *capture_toplevel_sources;
static struct wl_listener capture_request_listener;
static struct wl_listener capture_destroy_listener;

extern void goCaptureSourceRequest(void *request, void *handle);

static void handle_capture_request(struct wl_listener *listener, void *data) {
	struct wlr_ext_foreign_toplevel_image_capture_source_manager_v1_request *request = data;
	goCaptureSourceRequest(request, request->toplevel_handle);
}

// wlroots asserts that a manager's signals have no listener left when it is
// destroyed (with the display).
static void handle_capture_destroy(struct wl_listener *listener, void *data) {
	wl_list_remove(&capture_request_listener.link);
	wl_list_remove(&capture_destroy_listener.link);
	capture_toplevel_sources = NULL;
}

extern void goCaptureSessionStart(void *session);
extern void goCaptureSessionEnd(void *session);

// Capture sessions are followed from start to end: one that lasts is a
// screen share (a screenshot takes one frame and goes).
struct capture_session_watch {
	struct wl_listener destroy;
	void *session;
};
static struct wl_listener capture_session_listener;
static struct wl_listener capture_display_listener;

static void handle_capture_session_destroy(struct wl_listener *listener, void *data) {
	struct capture_session_watch *watch = wl_container_of(listener, watch, destroy);
	wl_list_remove(&watch->destroy.link);
	goCaptureSessionEnd(watch->session);
	free(watch);
}

static void handle_capture_session(struct wl_listener *listener, void *data) {
	struct wlr_ext_image_copy_capture_session_v1 *session = data;
	struct capture_session_watch *watch = calloc(1, sizeof(*watch));
	if (watch == NULL) {
		return;
	}
	watch->session = session;
	watch->destroy.notify = handle_capture_session_destroy;
	wl_signal_add(&session->events.destroy, &watch->destroy);
	goCaptureSessionStart(session);
}

// wlr-screencopy has no sessions, only frames: the frames of each client
// are followed as they are made and destroyed (a frame waiting for damage
// lives until the screen changes).
extern void goScreencopyFrame(int live);

struct screencopy_client_watch {
	struct wl_listener resource;
	struct wl_listener destroy;
};
static struct wl_listener capture_client_listener;

static void handle_screencopy_frame_destroy(struct wl_listener *listener, void *data) {
	wl_list_remove(&listener->link);
	free(listener);
	goScreencopyFrame(-1);
}

static void handle_client_resource(struct wl_listener *listener, void *data) {
	struct wl_resource *resource = data;
	if (strcmp(wl_resource_get_class(resource), "zwlr_screencopy_frame_v1") != 0) {
		return;
	}
	struct wl_listener *destroy = calloc(1, sizeof(*destroy));
	if (destroy == NULL) {
		return;
	}
	destroy->notify = handle_screencopy_frame_destroy;
	wl_resource_add_destroy_listener(resource, destroy);
	goScreencopyFrame(1);
}

static void handle_screencopy_client_destroy(struct wl_listener *listener, void *data) {
	struct screencopy_client_watch *watch = wl_container_of(listener, watch, destroy);
	wl_list_remove(&watch->resource.link);
	wl_list_remove(&watch->destroy.link);
	free(watch);
}

static void handle_client_created(struct wl_listener *listener, void *data) {
	struct wl_client *client = data;
	struct screencopy_client_watch *watch = calloc(1, sizeof(*watch));
	if (watch == NULL) {
		return;
	}
	watch->resource.notify = handle_client_resource;
	wl_client_add_resource_created_listener(client, &watch->resource);
	watch->destroy.notify = handle_screencopy_client_destroy;
	wl_client_add_destroy_listener(client, &watch->destroy);
}

// The copy manager has no destroy signal: this display listener, added
// before the manager's own, leaves its new_session signal empty in time.
static void handle_capture_display_destroy(struct wl_listener *listener, void *data) {
	wl_list_remove(&capture_session_listener.link);
	wl_list_remove(&capture_client_listener.link);
	wl_list_remove(&capture_display_listener.link);
}

static void setup_capture(struct wl_display *display, struct wlr_screencopy_manager_v1 *screencopy) {
	capture_toplevel_list = wlr_ext_foreign_toplevel_list_v1_create(display, 1);
	capture_display_listener.notify = handle_capture_display_destroy;
	wl_display_add_destroy_listener(display, &capture_display_listener);
	struct wlr_ext_image_copy_capture_manager_v1 *copy = wlr_ext_image_copy_capture_manager_v1_create(display, 1);
	capture_session_listener.notify = handle_capture_session;
	wl_signal_add(&copy->events.new_session, &capture_session_listener);
	capture_client_listener.notify = handle_client_created;
	wl_display_add_client_created_listener(display, &capture_client_listener);
	struct wlr_ext_output_image_capture_source_manager_v1 *outputs =
		wlr_ext_output_image_capture_source_manager_v1_create(display, 1);
	capture_toplevel_sources = wlr_ext_foreign_toplevel_image_capture_source_manager_v1_create(display, 1);

	capture_request_listener.notify = handle_capture_request;
	wl_signal_add(&capture_toplevel_sources->events.new_request, &capture_request_listener);
	capture_destroy_listener.notify = handle_capture_destroy;
	wl_signal_add(&capture_toplevel_sources->events.destroy, &capture_destroy_listener);

	restrict_global(capture_toplevel_list->global);
	restrict_global(copy->global);
	restrict_global(outputs->global);
	restrict_global(capture_toplevel_sources->global);
	if (screencopy) {
		restrict_global(screencopy->global);
	}
}

static struct wlr_ext_foreign_toplevel_handle_v1 *capture_handle_create(const char *title, const char *app_id) {
	struct wlr_ext_foreign_toplevel_handle_v1_state state = { .title = title, .app_id = app_id };
	return wlr_ext_foreign_toplevel_handle_v1_create(capture_toplevel_list, &state);
}

static void capture_handle_update(struct wlr_ext_foreign_toplevel_handle_v1 *handle, const char *title, const char *app_id) {
	struct wlr_ext_foreign_toplevel_handle_v1_state state = { .title = title, .app_id = app_id };
	wlr_ext_foreign_toplevel_handle_v1_update_state(handle, &state);
}

// A capture renders everything of its scene within the node's bounds, so
// each shared window gets a scene of its own holding only its surfaces:
// windows, menus or notifications above it are not shared along with it.
static struct wlr_scene *capture_scene_for_xdg(struct wlr_xdg_surface *xdg_surface) {
	struct wlr_scene *scene = wlr_scene_create();
	if (scene && !wlr_scene_xdg_surface_create(&scene->tree, xdg_surface)) {
		wlr_scene_node_destroy(&scene->tree.node);
		return NULL;
	}
	return scene;
}

static struct wlr_scene *capture_scene_for_surface(struct wlr_surface *surface) {
	struct wlr_scene *scene = wlr_scene_create();
	if (scene && !wlr_scene_subsurface_tree_create(&scene->tree, surface)) {
		wlr_scene_node_destroy(&scene->tree.node);
		return NULL;
	}
	return scene;
}

static void capture_scene_destroy(struct wlr_scene *scene) {
	wlr_scene_node_destroy(&scene->tree.node);
}

static bool capture_accept(void *request, struct wlr_scene *scene, struct wl_display *display,
		struct wlr_allocator *allocator, struct wlr_renderer *renderer) {
	struct wlr_ext_image_capture_source_v1 *source = wlr_ext_image_capture_source_v1_create_with_scene_node(
		&scene->tree.node, wl_display_get_event_loop(display), allocator, renderer);
	if (!source) {
		return false;
	}
	return wlr_ext_foreign_toplevel_image_capture_source_manager_v1_request_accept(request, source);
}
*/
import "C"

import (
	"log"
	"strings"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// captureTarget is a window offered to screen sharing: exactly one of xdg
// and xway is set.
type captureTarget struct {
	xdg  *xdgView
	xway *xwayView
}

// captureHandles maps each ext_foreign_toplevel handle to its window. Only
// touched from the main thread (wlroots callbacks).
var captureHandles = map[*C.struct_wlr_ext_foreign_toplevel_handle_v1]captureTarget{}

// captureServer is the server the capture callbacks from C work on.
var captureServer *server

// setupCapture creates the screen capture protocols used by the portal to
// share an output or a single window (ext-image-copy-capture with output and
// foreign toplevel sources, plus the ext-foreign-toplevel-list of windows),
// and keeps them and screencopy away from sandboxed clients.
func setupCapture(s *server) {
	captureServer = s
	C.setup_capture(displayPtr(s.display), (*C.struct_wlr_screencopy_manager_v1)(s.screencopyMgr.Ptr()))
	log.Println("ext-image-copy-capture and ext-foreign-toplevel-list registered")
}

// captureAddWindow lists a mapped window for screen sharing.
func (s *server) captureAddWindow(t captureTarget, title, appID string) unsafe.Pointer {
	cTitle, cAppID := C.CString(title), C.CString(appID)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cAppID))

	handle := C.capture_handle_create(cTitle, cAppID)
	if handle == nil {
		return nil
	}
	captureHandles[handle] = t
	log.Printf("[CAPTURE] listed %q (%s) as %s", title, appID, C.GoString(handle.identifier))
	return unsafe.Pointer(handle)
}

// captureUpdateWindow updates the title and app id shown for a listed window.
func captureUpdateWindow(handle unsafe.Pointer, title, appID string) {
	if handle == nil {
		return
	}
	cTitle, cAppID := C.CString(title), C.CString(appID)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cAppID))
	C.capture_handle_update((*C.struct_wlr_ext_foreign_toplevel_handle_v1)(handle), cTitle, cAppID)
}

// captureRemoveWindow withdraws a window from screen sharing; an ongoing
// capture of it ends.
func captureRemoveWindow(handle unsafe.Pointer) {
	if handle == nil {
		return
	}
	h := (*C.struct_wlr_ext_foreign_toplevel_handle_v1)(handle)
	delete(captureHandles, h)
	C.wlr_ext_foreign_toplevel_handle_v1_destroy(h)
}

//export goCaptureSessionStart
func goCaptureSessionStart(session unsafe.Pointer) {
	if s := captureServer; s != nil {
		s.captureSessionStarted(uintptr(session))
	}
}

//export goCaptureSessionEnd
func goCaptureSessionEnd(session unsafe.Pointer) {
	if s := captureServer; s != nil {
		s.captureSessionEnded(uintptr(session))
	}
}

//export goScreencopyFrame
func goScreencopyFrame(live C.int) {
	if s := captureServer; s != nil {
		s.screencopyFrame(int(live))
	}
}

//export goCaptureSourceRequest
func goCaptureSourceRequest(request, handle unsafe.Pointer) {
	s := captureServer
	if s == nil {
		return
	}
	t, ok := captureHandles[(*C.struct_wlr_ext_foreign_toplevel_handle_v1)(handle)]
	if !ok {
		return
	}

	var scene *C.struct_wlr_scene
	switch {
	case t.xdg != nil:
		if t.xdg.captureScene == nil {
			t.xdg.captureScene = unsafe.Pointer(C.capture_scene_for_xdg(
				(*C.struct_wlr_xdg_surface)(t.xdg.xdgToplevel.Base().Ptr())))
		}
		scene = (*C.struct_wlr_scene)(t.xdg.captureScene)
	case t.xway != nil:
		if surface := t.xway.surface.Surface(); t.xway.captureScene == nil && surface.Valid() {
			t.xway.captureScene = unsafe.Pointer(C.capture_scene_for_surface((*C.struct_wlr_surface)(surface.Ptr())))
		}
		scene = (*C.struct_wlr_scene)(t.xway.captureScene)
	}
	if scene == nil {
		log.Println("[CAPTURE] cannot build a capture scene for the window")
		return
	}

	if !C.capture_accept(request, scene, displayPtr(s.display),
		(*C.struct_wlr_allocator)(s.allocator.Ptr()), (*C.struct_wlr_renderer)(s.renderer.Ptr())) {
		log.Println("[CAPTURE] cannot create a capture source for the window")
	}
}

// captureShowXdg lists a mapped XDG window for screen sharing and follows its
// title and app id.
func (s *server) captureShowXdg(v *xdgView) {
	if v.captureHandle != nil {
		return
	}
	v.captureHandle = s.captureAddWindow(captureTarget{xdg: v}, v.xdgToplevel.Title(), v.xdgToplevel.AppID())
	update := func(t wlr.XDGToplevel) { captureUpdateWindow(v.captureHandle, t.Title(), t.AppID()) }
	v.captureListeners.Add(v.xdgToplevel.OnSetTitle(update))
	v.captureListeners.Add(v.xdgToplevel.OnSetAppID(update))
}

// captureHideXdg withdraws an XDG window from screen sharing.
func (s *server) captureHideXdg(v *xdgView) {
	v.captureListeners.DestroyAll()
	captureRemoveWindow(v.captureHandle)
	v.captureHandle = nil
	captureDestroyScene(&v.captureScene)
}

// captureShareable reports whether an XWayland window is an application
// window: not a popup nor one of Tyde's own panels, bars, menus or dialogs.
func captureShareable(v *xwayView) bool {
	return !v.isPanel && !v.isOverlay && !v.overrideRedirect &&
		!strings.Contains(v.surface.Title(), "Tyde:")
}

// captureShowXway lists a mapped XWayland window for screen sharing.
func (s *server) captureShowXway(v *xwayView) {
	if v.captureHandle != nil || !captureShareable(v) {
		return
	}
	v.captureHandle = s.captureAddWindow(captureTarget{xway: v}, v.surface.Title(), v.surface.Class())
}

// captureUpdateXway follows the title of an XWayland window.
func (s *server) captureUpdateXway(v *xwayView) {
	if v.captureHandle == nil {
		return
	}
	if !captureShareable(v) {
		s.captureHideXway(v)
		return
	}
	captureUpdateWindow(v.captureHandle, v.surface.Title(), v.surface.Class())
}

// captureHideXway withdraws an XWayland window from screen sharing.
func (s *server) captureHideXway(v *xwayView) {
	captureRemoveWindow(v.captureHandle)
	v.captureHandle = nil
	captureDestroyScene(&v.captureScene)
}

// captureDestroyScene destroys a window's capture scene, ending its captures.
func captureDestroyScene(scene *unsafe.Pointer) {
	if *scene != nil {
		C.capture_scene_destroy((*C.struct_wlr_scene)(*scene))
		*scene = nil
	}
}
