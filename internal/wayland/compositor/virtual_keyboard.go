package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
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

static void setup_virtual_keyboard(struct wl_display *display, struct wlr_backend *backend) {
    vk_manager = wlr_virtual_keyboard_manager_v1_create(display);
    vk_backend = backend;
    vk_new_listener.notify = handle_vk_new;
    wl_signal_add(&vk_manager->events.new_virtual_keyboard, &vk_new_listener);
}
*/
import "C"

import (
	"log"
	"unsafe"
)

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
	type displayPtr struct{ p *C.struct_wl_display }
	type backendPtr struct{ p *C.struct_wlr_backend }

	dp := (*displayPtr)(unsafe.Pointer(&s.display))
	bp := (*backendPtr)(unsafe.Pointer(&s.backend))
	if dp.p == nil || bp.p == nil {
		log.Println("[VKBD] setupVirtualKeyboard: display or backend nil, skipping")
		return
	}
	C.setup_virtual_keyboard(dp.p, bp.p)
	log.Println("[VKBD] wlr_virtual_keyboard_manager_v1 registered")
}
