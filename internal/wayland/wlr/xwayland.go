package wlr

/*
#include <stdlib.h>
#include <xcb/xcb.h>
#include <wlr/xcursor.h>
#include <wlr/xwayland.h>
*/
import "C"

import "unsafe"

// XwaylandSurfaceDecorations is a bitmask of enum wlr_xwayland_surface_decorations.
type XwaylandSurfaceDecorations uint32

const (
	XwaylandSurfaceDecorationsAll      XwaylandSurfaceDecorations = C.WLR_XWAYLAND_SURFACE_DECORATIONS_ALL
	XwaylandSurfaceDecorationsNoBorder XwaylandSurfaceDecorations = C.WLR_XWAYLAND_SURFACE_DECORATIONS_NO_BORDER
	XwaylandSurfaceDecorationsNoTitle  XwaylandSurfaceDecorations = C.WLR_XWAYLAND_SURFACE_DECORATIONS_NO_TITLE
)

// Xwayland wraps struct wlr_xwayland.
type Xwayland struct {
	p *C.struct_wlr_xwayland
}

// CreateXwayland starts an Xwayland server (lazily if lazy is true).
func CreateXwayland(display Display, compositor Compositor, lazy bool) Xwayland {
	return Xwayland{p: C.wlr_xwayland_create(display.p, compositor.p, C.bool(lazy))}
}

// Ptr returns the underlying struct wlr_xwayland pointer.
func (x Xwayland) Ptr() unsafe.Pointer { return unsafe.Pointer(x.p) }

// Valid reports whether Xwayland was created.
func (x Xwayland) Valid() bool { return x.p != nil }

// Destroy stops Xwayland.
func (x Xwayland) Destroy() { C.wlr_xwayland_destroy(x.p) }

// DisplayName returns the X11 display name (":1").
func (x Xwayland) DisplayName() string { return C.GoString(x.p.display_name) }

// SetSeat associates Xwayland with seat for input and selections.
func (x Xwayland) SetSeat(seat Seat) { C.wlr_xwayland_set_seat(x.p, seat.p) }

// SetCursor sets the default X11 root cursor.
func (x Xwayland) SetCursor(img XCursorImage) {
	buf := C.wlr_xcursor_image_get_buffer(img.p)
	C.wlr_xwayland_set_cursor(x.p, buf, C.int32_t(img.p.hotspot_x), C.int32_t(img.p.hotspot_y))
}

// OnNewSurface is emitted for each new X11 window.
func (x Xwayland) OnNewSurface(cb func(XwaylandSurface)) Listener {
	return newListener(&x.p.events.new_surface, func(data unsafe.Pointer) {
		cb(XwaylandSurface{p: (*C.struct_wlr_xwayland_surface)(data)})
	})
}

// OnReady is emitted once the X11 window manager is running.
func (x Xwayland) OnReady(cb func()) Listener {
	return newListener(&x.p.events.ready, func(unsafe.Pointer) { cb() })
}

// XwaylandSurface wraps struct wlr_xwayland_surface.
//
// The wl_surface returned by Surface is only valid between the associate and
// dissociate events; X11 unmap dissociates it and a remap may bring a new
// wl_surface.
type XwaylandSurface struct {
	p *C.struct_wlr_xwayland_surface
}

// XwaylandSurfaceFromPtr wraps a struct wlr_xwayland_surface pointer.
func XwaylandSurfaceFromPtr(p unsafe.Pointer) XwaylandSurface {
	return XwaylandSurface{p: (*C.struct_wlr_xwayland_surface)(p)}
}

// XwaylandSurfaceTryFromSurface returns the X11 window behind s, if any.
func XwaylandSurfaceTryFromSurface(s Surface) XwaylandSurface {
	return XwaylandSurface{p: C.wlr_xwayland_surface_try_from_wlr_surface(s.p)}
}

// Ptr returns the underlying struct wlr_xwayland_surface pointer.
func (s XwaylandSurface) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// Valid reports whether s wraps an X11 window.
func (s XwaylandSurface) Valid() bool { return s.p != nil }

// Surface returns the associated wl_surface (invalid when dissociated).
func (s XwaylandSurface) Surface() Surface { return Surface{p: s.p.surface} }

// X returns the X11 x position.
func (s XwaylandSurface) X() int { return int(s.p.x) }

// Y returns the X11 y position.
func (s XwaylandSurface) Y() int { return int(s.p.y) }

// Width returns the X11 width.
func (s XwaylandSurface) Width() int { return int(s.p.width) }

// Height returns the X11 height.
func (s XwaylandSurface) Height() int { return int(s.p.height) }

// Title returns the window title.
func (s XwaylandSurface) Title() string { return C.GoString(s.p.title) }

// Class returns WM_CLASS class.
func (s XwaylandSurface) Class() string { return C.GoString(s.p.class) }

// OverrideRedirect reports whether the window bypasses window management.
func (s XwaylandSurface) OverrideRedirect() bool { return bool(s.p.override_redirect) }

// Fullscreen reports the _NET_WM_STATE fullscreen state requested by the client.
func (s XwaylandSurface) Fullscreen() bool { return bool(s.p.fullscreen) }

// Parent returns the transient-for parent (invalid if none).
func (s XwaylandSurface) Parent() XwaylandSurface { return XwaylandSurface{p: s.p.parent} }

// Decorations returns the MOTIF decoration hints.
func (s XwaylandSurface) Decorations() XwaylandSurfaceDecorations {
	return XwaylandSurfaceDecorations(s.p.decorations)
}

// Close asks the client to close the window.
func (s XwaylandSurface) Close() { C.wlr_xwayland_surface_close(s.p) }

// Activate sets the X11 input focus.
func (s XwaylandSurface) Activate(activated bool) {
	C.wlr_xwayland_surface_activate(s.p, C.bool(activated))
}

// SetMinimized sets the _NET_WM_STATE hidden state.
func (s XwaylandSurface) SetMinimized(minimized bool) {
	C.wlr_xwayland_surface_set_minimized(s.p, C.bool(minimized))
}

// SetMaximized sets both _NET_WM_STATE maximized states.
func (s XwaylandSurface) SetMaximized(maximized bool) {
	C.wlr_xwayland_surface_set_maximized(s.p, C.bool(maximized), C.bool(maximized))
}

// SetFullscreen sets the _NET_WM_STATE fullscreen state.
func (s XwaylandSurface) SetFullscreen(fullscreen bool) {
	C.wlr_xwayland_surface_set_fullscreen(s.p, C.bool(fullscreen))
}

// Configure moves and resizes the X11 window.
func (s XwaylandSurface) Configure(x, y int16, width, height uint16) {
	C.wlr_xwayland_surface_configure(s.p, C.int16_t(x), C.int16_t(y), C.uint16_t(width), C.uint16_t(height))
}

// RestackAbove raises the window to the top of the X11 stack.
func (s XwaylandSurface) RestackAbove() {
	C.wlr_xwayland_surface_restack(s.p, nil, C.XCB_STACK_MODE_ABOVE)
}

// OnDestroy is emitted when the X11 window is destroyed.
func (s XwaylandSurface) OnDestroy(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.destroy, func(unsafe.Pointer) { cb(s) })
}

// OnAssociate is emitted when the X11 window gets its wl_surface.
func (s XwaylandSurface) OnAssociate(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.associate, func(unsafe.Pointer) { cb(s) })
}

// OnDissociate is emitted right before the wl_surface is detached (the
// surface has already been unmapped). Remove wl_surface listeners here.
func (s XwaylandSurface) OnDissociate(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.dissociate, func(unsafe.Pointer) { cb(s) })
}

// OnRequestConfigure is emitted when the client asks for a new geometry.
func (s XwaylandSurface) OnRequestConfigure(cb func(s XwaylandSurface, x, y int16, width, height uint16)) Listener {
	return newListener(&s.p.events.request_configure, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_xwayland_surface_configure_event)(data)
		cb(s, int16(event.x), int16(event.y), uint16(event.width), uint16(event.height))
	})
}

// OnRequestMove is emitted when the client starts an interactive move.
func (s XwaylandSurface) OnRequestMove(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.request_move, func(unsafe.Pointer) { cb(s) })
}

// OnRequestResize is emitted when the client starts an interactive resize.
func (s XwaylandSurface) OnRequestResize(cb func(s XwaylandSurface, edges Edges)) Listener {
	return newListener(&s.p.events.request_resize, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_xwayland_resize_event)(data)
		cb(s, Edges(event.edges))
	})
}

// OnRequestFullscreen is emitted when the client toggles fullscreen.
func (s XwaylandSurface) OnRequestFullscreen(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.request_fullscreen, func(unsafe.Pointer) { cb(s) })
}

// OnSetTitle is emitted when the title changes.
func (s XwaylandSurface) OnSetTitle(cb func(s XwaylandSurface, title string)) Listener {
	return newListener(&s.p.events.set_title, func(unsafe.Pointer) { cb(s, s.Title()) })
}

// OnSetParent is emitted when WM_TRANSIENT_FOR changes.
func (s XwaylandSurface) OnSetParent(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.set_parent, func(unsafe.Pointer) { cb(s) })
}

// OnSetDecorations is emitted when the MOTIF hints change.
func (s XwaylandSurface) OnSetDecorations(cb func(XwaylandSurface)) Listener {
	return newListener(&s.p.events.set_decorations, func(unsafe.Pointer) { cb(s) })
}
