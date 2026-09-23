package wlr

/*
#include <stdlib.h>
#include <wayland-server-core.h>

static struct wl_signal *fd_signal_new(void) {
	struct wl_signal *sig = calloc(1, sizeof(*sig));
	if (sig != NULL) {
		wl_signal_init(sig);
	}
	return sig;
}

static int fd_signal_empty(struct wl_signal *sig) {
	return wl_list_empty(&sig->listener_list);
}
*/
import "C"

import "unsafe"

// signal is a standalone wl_signal, used to exercise listeners without a
// Wayland display (tests).
type signal struct {
	p *C.struct_wl_signal
}

func newSignal() signal { return signal{p: C.fd_signal_new()} }

func (s signal) listen(cb func()) Listener {
	return newListener(s.p, func(unsafe.Pointer) { cb() })
}

// emit emits the signal the way wlroots does (listeners may remove
// themselves while it runs).
func (s signal) emit() { C.wl_signal_emit_mutable(s.p, nil) }

func (s signal) empty() bool { return C.fd_signal_empty(s.p) != 0 }

func (s signal) free() { C.free(unsafe.Pointer(s.p)) }
