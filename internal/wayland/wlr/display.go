package wlr

/*
#include <wayland-server-core.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Display wraps struct wl_display.
type Display struct {
	p *C.struct_wl_display
}

// CreateDisplay creates a new Wayland display.
func CreateDisplay() Display {
	return Display{p: C.wl_display_create()}
}

// DisplayFromPtr wraps a struct wl_display pointer.
func DisplayFromPtr(p unsafe.Pointer) Display {
	return Display{p: (*C.struct_wl_display)(p)}
}

// Ptr returns the underlying struct wl_display pointer.
func (d Display) Ptr() unsafe.Pointer { return unsafe.Pointer(d.p) }

// Destroy destroys the display and every global still attached to it.
func (d Display) Destroy() { C.wl_display_destroy(d.p) }

// DestroyClients disconnects every client.
func (d Display) DestroyClients() { C.wl_display_destroy_clients(d.p) }

// Run runs the event loop until Terminate is called.
func (d Display) Run() { C.wl_display_run(d.p) }

// Terminate makes Run return.
func (d Display) Terminate() { C.wl_display_terminate(d.p) }

// EventLoop returns the display's event loop.
func (d Display) EventLoop() EventLoop {
	return EventLoop{p: C.wl_display_get_event_loop(d.p)}
}

// AddSocketAuto adds a wayland-N socket and returns its name.
func (d Display) AddSocketAuto() (string, error) {
	socket := C.wl_display_add_socket_auto(d.p)
	if socket == nil {
		return "", errors.New("can't auto add wayland socket")
	}
	return C.GoString(socket), nil
}

// EventLoop wraps struct wl_event_loop.
type EventLoop struct {
	p *C.struct_wl_event_loop
}

// Ptr returns the underlying struct wl_event_loop pointer.
func (l EventLoop) Ptr() unsafe.Pointer { return unsafe.Pointer(l.p) }
