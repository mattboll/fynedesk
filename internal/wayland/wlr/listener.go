// The listener trampoline is derived from deedles.dev/wlr (itself a fork of
// github.com/swaywm/go-wlroots), MIT licensed, Copyright (c) 2022 DeedleFake.

package wlr

/*
#include <stdlib.h>
#include <wayland-server-core.h>

struct fd_listener {
	struct wl_listener lis;
	uintptr_t handle;
};

extern void fdListenerCallback(struct wl_listener *listener, void *data);

static inline struct fd_listener *fd_listener_from_wl(struct wl_listener *lis) {
	struct fd_listener *l;
	return wl_container_of(lis, l, lis);
}

static inline struct fd_listener *fd_listener_new(uintptr_t handle) {
	struct fd_listener *l = calloc(1, sizeof(*l));
	if (l == NULL) {
		return NULL;
	}
	l->handle = handle;
	l->lis.notify = fdListenerCallback;
	wl_list_init(&l->lis.link);
	return l;
}
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

type listenerFunc func(data unsafe.Pointer)

type listenerState struct {
	p *C.struct_fd_listener
}

// Listener is a Go callback attached to a wl_signal.
//
// Listeners must be destroyed before the object owning the signal goes away:
// wlroots asserts that signal listener lists are empty when it destroys an
// object. Destroy is idempotent and safe to call from inside the listener's
// own callback (wlroots emits every signal with wl_signal_emit_mutable).
type Listener struct {
	s *listenerState
}

func newListener(sig *C.struct_wl_signal, cb listenerFunc) Listener {
	p := C.fd_listener_new(C.uintptr_t(cgo.NewHandle(cb)))
	if p == nil {
		panic("wlr: out of memory allocating listener")
	}
	if sig != nil {
		C.wl_signal_add(sig, &p.lis)
	}
	return Listener{s: &listenerState{p: p}}
}

// Valid reports whether the listener is still attached.
func (l Listener) Valid() bool {
	return l.s != nil && l.s.p != nil
}

// Destroy detaches the listener from its signal and frees it.
func (l Listener) Destroy() {
	if !l.Valid() {
		return
	}
	p := l.s.p
	l.s.p = nil
	cgo.Handle(p.handle).Delete()
	C.wl_list_remove(&p.lis.link)
	C.free(unsafe.Pointer(p))
}

//export fdListenerCallback
func fdListenerCallback(lis *C.struct_wl_listener, data unsafe.Pointer) {
	p := C.fd_listener_from_wl(lis)
	cgo.Handle(p.handle).Value().(listenerFunc)(data)
}

// Listeners is a set of listeners sharing a lifetime (typically the lifetime
// of one wlroots object).
type Listeners []Listener

// Add records lis and returns it.
func (ls *Listeners) Add(lis Listener) Listener {
	*ls = append(*ls, lis)
	return lis
}

// DestroyAll destroys every listener in the set and empties it. It is safe
// to call from one of the set's own callbacks.
func (ls *Listeners) DestroyAll() {
	for _, lis := range *ls {
		lis.Destroy()
	}
	*ls = nil
}
