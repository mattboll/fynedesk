package wlr

/*
#include <stdlib.h>
#include <wlr/interfaces/wlr_keyboard.h>

static const struct wlr_keyboard_impl fd_standalone_keyboard_impl = {
	.name = "standalone-keyboard",
};

static struct wlr_keyboard *fd_standalone_keyboard_new(void) {
	struct wlr_keyboard *kb = calloc(1, sizeof(*kb));
	if (kb != NULL) {
		wlr_keyboard_init(kb, &fd_standalone_keyboard_impl, "standalone-keyboard");
	}
	return kb;
}

static void fd_standalone_keyboard_free(struct wlr_keyboard *kb) {
	wlr_keyboard_finish(kb);
	free(kb);
}
*/
import "C"

// NewStandaloneKeyboard makes a keyboard that belongs to no backend, used to
// exercise keymaps and modifiers without a device (tests). Free it with
// FreeStandaloneKeyboard.
func NewStandaloneKeyboard() Keyboard { return Keyboard{p: C.fd_standalone_keyboard_new()} }

// FreeStandaloneKeyboard frees a keyboard made by NewStandaloneKeyboard.
func FreeStandaloneKeyboard(k Keyboard) { C.fd_standalone_keyboard_free(k.p) }
