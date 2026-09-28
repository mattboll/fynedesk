package compositor

/*
#include "restricted_globals.h"
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/backend.h>
#include <wlr/types/wlr_virtual_keyboard_v1.h>
#include <wlr/types/wlr_keyboard.h>
#include <wlr/types/wlr_input_device.h>

static struct wlr_virtual_keyboard_manager_v1 *vk_manager = NULL;
static struct wl_listener vk_new_listener;
static struct wlr_backend *vk_backend = NULL;

// When a client creates a virtual keyboard, re-emit it on the backend's
// new_input signal. wlroots' compositor entry point already wires the
// backend signal to handleNewInput, so this single line plugs synthesized
// keyboards into the same key-handling pipeline as physical ones — modulo
// the wlr_seat_keyboard_notify_key path that all keyboards share.
static void handle_vk_new(struct wl_listener *listener, void *data) {
    struct wlr_virtual_keyboard_v1 *vk = data;
    if (!vk || !vk_backend) return;
    struct wlr_input_device *dev = &vk->keyboard.base;
    wl_signal_emit_mutable(&vk_backend->events.new_input, dev);
}

// wlroots asserts that the manager's signals have no listener left when it is
// destroyed (with the display), so detach ours from its destroy event.
static struct wl_listener vk_destroy_listener;

static void handle_vk_manager_destroy(struct wl_listener *listener, void *data) {
    wl_list_remove(&vk_new_listener.link);
    wl_list_remove(&vk_destroy_listener.link);
    vk_manager = NULL;
    vk_backend = NULL;
}

static bool is_virtual_keyboard(struct wlr_keyboard *kb) {
    return wlr_input_device_get_virtual_keyboard(&kb->base) != NULL;
}

static void setup_virtual_keyboard(struct wl_display *display, struct wlr_backend *backend) {
    vk_manager = wlr_virtual_keyboard_manager_v1_create(display);
    restrict_global(vk_manager->global);
    vk_backend = backend;
    vk_new_listener.notify = handle_vk_new;
    wl_signal_add(&vk_manager->events.new_virtual_keyboard, &vk_new_listener);
    vk_destroy_listener.notify = handle_vk_manager_destroy;
    wl_signal_add(&vk_manager->events.destroy, &vk_destroy_listener);
}
*/
import "C"

import (
	"log"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// isVirtualKeyboard reports whether kb is a client's virtual keyboard: its
// client sets its modifiers, locks included.
func isVirtualKeyboard(kb wlr.Keyboard) bool {
	return bool(C.is_virtual_keyboard((*C.struct_wlr_keyboard)(kb.Ptr())))
}

// setupVirtualKeyboard registers the wlr_virtual_keyboard_v1 protocol so
// clients implementing zwp_virtual_keyboard_v1 (e.g. wtype, on-screen
// keyboards, accessibility tools, IME backends, QA automation) can inject
// synthetic key events.
//
// New virtual keyboards are forwarded to the existing handleNewInput path
// by re-emitting on the backend's new_input signal, so they participate
// in normal seat keyboard semantics (focus, modifiers, layouts) without
// any special-case code in the Go side.
func setupVirtualKeyboard(s *server) {
	if s.display.Ptr() == nil || !s.backend.Valid() {
		log.Println("[VKBD] setupVirtualKeyboard: display or backend nil, skipping")
		return
	}
	C.setup_virtual_keyboard(displayPtr(s.display), backendPtr(s.backend))
	log.Println("[VKBD] wlr_virtual_keyboard_manager_v1 registered")
}
