package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_xdg_activation_v1.h>
#include <wlr/types/wlr_xdg_shell.h>

// --- XDG Activation listeners ---

static struct wlr_xdg_activation_v1 *activation_manager = NULL;
static struct wl_listener activation_request_listener;
static struct wl_listener activation_destroy_listener;

// Forward declaration for Go callback
extern void goActivationRequestActivate(void *event);

static void handle_activation_request(struct wl_listener *listener, void *data) {
	goActivationRequestActivate(data);
}

static void handle_activation_destroy(struct wl_listener *listener, void *data) {
	activation_manager = NULL;
}

static void setup_xdg_activation(struct wl_display *display) {
	activation_manager = wlr_xdg_activation_v1_create(display);
	activation_request_listener.notify = handle_activation_request;
	wl_signal_add(&activation_manager->events.request_activate, &activation_request_listener);
	activation_destroy_listener.notify = handle_activation_destroy;
	wl_signal_add(&activation_manager->events.destroy, &activation_destroy_listener);
}

// Helper to extract fields from the request_activate event
static struct wlr_surface *activation_event_surface(struct wlr_xdg_activation_v1_request_activate_event *ev) {
	return ev->surface;
}

static struct wlr_surface *activation_event_token_surface(struct wlr_xdg_activation_v1_request_activate_event *ev) {
	if (ev->token) return ev->token->surface;
	return NULL;
}

static struct wlr_seat *activation_event_token_seat(struct wlr_xdg_activation_v1_request_activate_event *ev) {
	if (ev->token) return ev->token->seat;
	return NULL;
}
*/
import "C"

import (
	"log"
	"unsafe"
)

var activationServer *server

// setupXDGActivation initializes the xdg-activation-v1 protocol, which allows
// Wayland clients to request focus or signal attention (urgency).
func setupXDGActivation(s *server) {
	activationServer = s
	C.setup_xdg_activation(displayPtr(s.display))
	log.Println("xdg-activation-v1 protocol registered")
}

//export goActivationRequestActivate
func goActivationRequestActivate(event unsafe.Pointer) {
	s := activationServer
	if s == nil {
		return
	}

	ev := (*C.struct_wlr_xdg_activation_v1_request_activate_event)(event)
	requestSurface := C.activation_event_surface(ev)
	if requestSurface == nil {
		return
	}

	tokenSurface := C.activation_event_token_surface(ev)

	// Find the view that is requesting activation
	xdgV, xwayV := s.findViewBySurface(requestSurface)

	// Determine if the token source is the currently focused window.
	// If so, grant focus directly (e.g., a file manager opening a new window).
	// Otherwise, mark as urgent (attention request from background app).
	if tokenSurface != nil && s.isFocusedSurface(tokenSurface) {
		// Token came from the focused window — grant focus (includes raise)
		if xdgV != nil {
			s.focusXdgView(xdgV)
		} else if xwayV != nil {
			s.focusXwayView(xwayV)
		}
	} else {
		// Background activation — mark urgent
		if xdgV != nil {
			if !xdgV.urgent {
				xdgV.urgent = true
				s.writeWindowsState()
			}
		} else if xwayV != nil {
			if !xwayV.urgent {
				xwayV.urgent = true
				s.writeWindowsState()
			}
		}
	}
}

// findViewBySurface locates the xdg or xwayland view that owns a given wlr_surface.
func (s *server) findViewBySurface(surface *C.struct_wlr_surface) (*xdgView, *xwayView) {
	for _, v := range s.xdgViews {
		if surfacePtr(v.xdgToplevel.Base().Surface()) == surface {
			return v, nil
		}
	}
	for _, v := range s.xwayViews {
		surf := v.surface.Surface()
		if surf.Valid() && surfacePtr(surf) == surface {
			return nil, v
		}
	}
	return nil, nil
}

// isFocusedSurface returns true if the given surface belongs to the currently focused view.
func (s *server) isFocusedSurface(surface *C.struct_wlr_surface) bool {
	if s.activeXdg != nil {
		if surfacePtr(s.activeXdg.xdgToplevel.Base().Surface()) == surface {
			return true
		}
	}
	if s.activeXway != nil {
		surf := s.activeXway.surface.Surface()
		if surf.Valid() && surfacePtr(surf) == surface {
			return true
		}
	}
	return false
}
