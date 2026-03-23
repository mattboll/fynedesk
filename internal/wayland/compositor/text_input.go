package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <string.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_text_input_v3.h>
#include <wlr/types/wlr_input_method_v2.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_compositor.h>

// --- Managers ---
static struct wlr_text_input_manager_v3 *ti_manager = NULL;
static struct wlr_input_method_manager_v2 *im_manager = NULL;

// --- Manager-level listeners ---
static struct wl_listener ti_new_listener;
static struct wl_listener im_new_listener;

// --- Per-text-input listeners (heap-allocated) ---
struct ti_listeners {
	struct wl_listener enable;
	struct wl_listener commit;
	struct wl_listener disable;
	struct wl_listener destroy;
	struct wlr_text_input_v3 *ti;
};

// --- Per-input-method listeners (heap-allocated) ---
struct im_listeners {
	struct wl_listener commit;
	struct wl_listener grab_keyboard;
	struct wl_listener destroy;
	struct wlr_input_method_v2 *im;
};

// Forward declarations for Go callbacks
extern void goTextInputNew(void *ti);
extern void goTextInputEnable(void *ti);
extern void goTextInputCommit(void *ti);
extern void goTextInputDisable(void *ti);
extern void goTextInputDestroy(void *ti);
extern void goInputMethodNew(void *im);
extern void goInputMethodCommit(void *im);
extern void goInputMethodGrabKeyboard(void *grab);
extern void goInputMethodDestroy(void *im);

// --- Text input event handlers ---
static void handle_ti_enable(struct wl_listener *listener, void *data) {
	goTextInputEnable(data);
}
static void handle_ti_commit(struct wl_listener *listener, void *data) {
	goTextInputCommit(data);
}
static void handle_ti_disable(struct wl_listener *listener, void *data) {
	goTextInputDisable(data);
}
static void handle_ti_destroy(struct wl_listener *listener, void *data) {
	struct ti_listeners *tl = wl_container_of(listener, tl, destroy);
	wl_list_remove(&tl->enable.link);
	wl_list_remove(&tl->commit.link);
	wl_list_remove(&tl->disable.link);
	wl_list_remove(&tl->destroy.link);
	goTextInputDestroy(data);
	free(tl);
}

// --- Input method event handlers ---
static void handle_im_commit(struct wl_listener *listener, void *data) {
	goInputMethodCommit(data);
}
static void handle_im_grab_keyboard(struct wl_listener *listener, void *data) {
	goInputMethodGrabKeyboard(data);
}
static void handle_im_destroy(struct wl_listener *listener, void *data) {
	struct im_listeners *il = wl_container_of(listener, il, destroy);
	wl_list_remove(&il->commit.link);
	wl_list_remove(&il->grab_keyboard.link);
	wl_list_remove(&il->destroy.link);
	goInputMethodDestroy(data);
	free(il);
}

// --- Manager new-client handlers ---
static void handle_ti_new(struct wl_listener *listener, void *data) {
	struct wlr_text_input_v3 *ti = data;
	struct ti_listeners *tl = calloc(1, sizeof(struct ti_listeners));
	tl->ti = ti;
	tl->enable.notify = handle_ti_enable;
	wl_signal_add(&ti->events.enable, &tl->enable);
	tl->commit.notify = handle_ti_commit;
	wl_signal_add(&ti->events.commit, &tl->commit);
	tl->disable.notify = handle_ti_disable;
	wl_signal_add(&ti->events.disable, &tl->disable);
	tl->destroy.notify = handle_ti_destroy;
	wl_signal_add(&ti->events.destroy, &tl->destroy);
	goTextInputNew(data);
}

static void handle_im_new(struct wl_listener *listener, void *data) {
	struct wlr_input_method_v2 *im = data;
	struct im_listeners *il = calloc(1, sizeof(struct im_listeners));
	il->im = im;
	il->commit.notify = handle_im_commit;
	wl_signal_add(&im->events.commit, &il->commit);
	il->grab_keyboard.notify = handle_im_grab_keyboard;
	wl_signal_add(&im->events.grab_keyboard, &il->grab_keyboard);
	il->destroy.notify = handle_im_destroy;
	wl_signal_add(&im->events.destroy, &il->destroy);
	goInputMethodNew(data);
}

// --- Setup function ---
static void setup_text_input(struct wl_display *display) {
	ti_manager = wlr_text_input_manager_v3_create(display);
	ti_new_listener.notify = handle_ti_new;
	wl_signal_add(&ti_manager->events.text_input, &ti_new_listener);

	im_manager = wlr_input_method_manager_v2_create(display);
	im_new_listener.notify = handle_im_new;
	wl_signal_add(&im_manager->events.input_method, &im_new_listener);
}

// --- Relay helpers (called from Go, operate on C pointers) ---

static void relay_ti_to_im(
	struct wlr_text_input_v3 *ti,
	struct wlr_input_method_v2 *im
) {
	uint32_t features = ti->current.features;

	if (features & WLR_TEXT_INPUT_V3_FEATURE_SURROUNDING_TEXT) {
		const char *text = ti->current.surrounding.text;
		if (text) {
			wlr_input_method_v2_send_surrounding_text(im,
				text, ti->current.surrounding.cursor, ti->current.surrounding.anchor);
		}
	}
	wlr_input_method_v2_send_text_change_cause(im, ti->current.text_change_cause);
	if (features & WLR_TEXT_INPUT_V3_FEATURE_CONTENT_TYPE) {
		wlr_input_method_v2_send_content_type(im,
			ti->current.content_type.hint, ti->current.content_type.purpose);
	}
}

static void relay_im_to_ti(
	struct wlr_input_method_v2 *im,
	struct wlr_text_input_v3 *ti
) {
	// Preedit
	if (im->current.preedit.text) {
		wlr_text_input_v3_send_preedit_string(ti,
			im->current.preedit.text,
			im->current.preedit.cursor_begin,
			im->current.preedit.cursor_end);
	} else {
		wlr_text_input_v3_send_preedit_string(ti, NULL, 0, 0);
	}
	// Commit text
	if (im->current.commit_text) {
		wlr_text_input_v3_send_commit_string(ti, im->current.commit_text);
	}
	// Delete surrounding
	if (im->current.delete.before_length || im->current.delete.after_length) {
		wlr_text_input_v3_send_delete_surrounding_text(ti,
			im->current.delete.before_length, im->current.delete.after_length);
	}
	wlr_text_input_v3_send_done(ti);
}

static void im_activate_relay(struct wlr_input_method_v2 *im, struct wlr_text_input_v3 *ti) {
	wlr_input_method_v2_send_activate(im);
	relay_ti_to_im(ti, im);
	wlr_input_method_v2_send_done(im);
}

static void im_deactivate_done(struct wlr_input_method_v2 *im) {
	wlr_input_method_v2_send_deactivate(im);
	wlr_input_method_v2_send_done(im);
}

// Check if text_input client matches surface client (required by protocol).
static int ti_client_matches_surface(struct wlr_text_input_v3 *ti, struct wlr_surface *surface) {
	if (!ti || !surface) return 0;
	return wl_resource_get_client(ti->resource) == wl_resource_get_client(surface->resource);
}

static void ti_enter(struct wlr_text_input_v3 *ti, struct wlr_surface *surface) {
	wlr_text_input_v3_send_enter(ti, surface);
}

static void ti_leave(struct wlr_text_input_v3 *ti) {
	wlr_text_input_v3_send_leave(ti);
}

static void ti_clear_preedit(struct wlr_text_input_v3 *ti) {
	wlr_text_input_v3_send_preedit_string(ti, NULL, 0, 0);
	wlr_text_input_v3_send_done(ti);
}

static void im_send_unavailable(struct wlr_input_method_v2 *im) {
	wlr_input_method_v2_send_unavailable(im);
}

static struct wlr_surface *ti_get_focused_surface(struct wlr_text_input_v3 *ti) {
	return ti->focused_surface;
}

static void im_grab_set_keyboard(struct wlr_input_method_keyboard_grab_v2 *grab,
	struct wlr_keyboard *keyboard) {
	wlr_input_method_keyboard_grab_v2_set_keyboard(grab, keyboard);
}
*/
import "C"

import (
	"log"
	"unsafe"
)

var textInputServer *server

// setupTextInput initializes the text-input-v3 and input-method-v2 protocols.
// This enables IME (Input Method Editor) support for CJK and complex script input
// via IBus, Fcitx5, or other Wayland IME clients.
func setupTextInput(s *server) {
	textInputServer = s
	C.setup_text_input(displayPtr(s.display))
	log.Println("text-input-v3 and input-method-v2 protocols registered")
}

func tiPtr(p unsafe.Pointer) *C.struct_wlr_text_input_v3 {
	return (*C.struct_wlr_text_input_v3)(p)
}

func imPtr(p unsafe.Pointer) *C.struct_wlr_input_method_v2 {
	return (*C.struct_wlr_input_method_v2)(p)
}

//export goTextInputNew
func goTextInputNew(tiRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil {
		return
	}
	s.textInputs = append(s.textInputs, tiRaw)
	log.Printf("[IME] New text input client registered (total: %d)", len(s.textInputs))
}

//export goTextInputEnable
func goTextInputEnable(tiRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil || s.activeInputMethod == nil {
		return
	}
	s.activeTextInput = tiRaw

	// Activate the input method and send initial state
	C.im_activate_relay(imPtr(s.activeInputMethod), tiPtr(tiRaw))
	log.Println("[IME] Text input enabled, IME activated")
}

//export goTextInputCommit
func goTextInputCommit(tiRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil || s.activeInputMethod == nil {
		return
	}
	if tiRaw != s.activeTextInput {
		return
	}

	// Relay updated state to IME
	C.relay_ti_to_im(tiPtr(tiRaw), imPtr(s.activeInputMethod))
	C.wlr_input_method_v2_send_done(imPtr(s.activeInputMethod))
}

//export goTextInputDisable
func goTextInputDisable(tiRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil || s.activeInputMethod == nil {
		return
	}
	if tiRaw != s.activeTextInput {
		return
	}

	s.activeTextInput = nil
	C.im_deactivate_done(imPtr(s.activeInputMethod))
	log.Println("[IME] Text input disabled, IME deactivated")
}

//export goTextInputDestroy
func goTextInputDestroy(tiRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil {
		return
	}

	// Remove from list
	for i, t := range s.textInputs {
		if t == tiRaw {
			s.textInputs = append(s.textInputs[:i], s.textInputs[i+1:]...)
			break
		}
	}

	if s.activeTextInput == tiRaw {
		s.activeTextInput = nil
		if s.activeInputMethod != nil {
			C.im_deactivate_done(imPtr(s.activeInputMethod))
		}
	}
	log.Printf("[IME] Text input client destroyed (remaining: %d)", len(s.textInputs))
}

//export goInputMethodNew
func goInputMethodNew(imRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil {
		return
	}

	// If there's already an active IME, mark the old one unavailable
	if s.activeInputMethod != nil {
		C.im_send_unavailable(imPtr(s.activeInputMethod))
	}

	s.activeInputMethod = imRaw
	log.Println("[IME] Input method (IME) connected")

	// If there's an active text input, notify the new IME
	if s.activeTextInput != nil {
		C.im_activate_relay(imPtr(imRaw), tiPtr(s.activeTextInput))
	}
}

//export goInputMethodCommit
func goInputMethodCommit(imRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil || s.activeTextInput == nil {
		return
	}
	if imRaw != s.activeInputMethod {
		return
	}

	// Relay IME output back to the text input client
	C.relay_im_to_ti(imPtr(imRaw), tiPtr(s.activeTextInput))
}

//export goInputMethodGrabKeyboard
func goInputMethodGrabKeyboard(grabPtr unsafe.Pointer) {
	s := textInputServer
	if s == nil {
		return
	}
	grab := (*C.struct_wlr_input_method_keyboard_grab_v2)(grabPtr)

	// Set the keyboard on the grab so IME receives key events
	keyboard := s.seat.Keyboard()
	if keyboardValid(keyboard) {
		kbPtr := (*C.struct_wlr_keyboard)(unsafe.Pointer(uintptr(*(*unsafe.Pointer)(unsafe.Pointer(&keyboard)))))
		C.im_grab_set_keyboard(grab, kbPtr)
	}
	log.Println("[IME] Input method grabbed keyboard")
}

//export goInputMethodDestroy
func goInputMethodDestroy(imRaw unsafe.Pointer) {
	s := textInputServer
	if s == nil {
		return
	}
	if s.activeInputMethod == imRaw {
		s.activeInputMethod = nil

		// Clear any active preedit on the text input
		if s.activeTextInput != nil {
			C.ti_clear_preedit(tiPtr(s.activeTextInput))
		}
	}
	log.Println("[IME] Input method (IME) disconnected")
}

// handleTextInputFocusChange updates text input focus when keyboard focus changes.
// Call this from focusXdgView and focusXwayView after setting keyboard enter.
func (s *server) handleTextInputFocusChange(newSurface *C.struct_wlr_surface) {
	if len(s.textInputs) == 0 {
		return
	}

	for _, tiRaw := range s.textInputs {
		ti := tiPtr(tiRaw)
		focused := C.ti_get_focused_surface(ti)
		if focused != nil && focused != newSurface {
			// Leave old surface
			C.ti_leave(ti)
			// Deactivate IME for this text input
			if tiRaw == s.activeTextInput && s.activeInputMethod != nil {
				s.activeTextInput = nil
				C.im_deactivate_done(imPtr(s.activeInputMethod))
			}
		}
		if newSurface != nil && focused != newSurface {
			// Only send enter if the text_input client owns the surface
			// (XWayland surfaces belong to XWayland, not the text_input client)
			if C.ti_client_matches_surface(ti, newSurface) != 0 {
				C.ti_enter(ti, newSurface)
			}
		}
	}
}
