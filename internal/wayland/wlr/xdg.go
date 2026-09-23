package wlr

/*
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/types/wlr_xdg_decoration_v1.h>
*/
import "C"

import (
	"image"
	"unsafe"
)

// XDGShell wraps struct wlr_xdg_shell.
type XDGShell struct {
	p *C.struct_wlr_xdg_shell
}

// CreateXDGShell creates the xdg_wm_base global.
func CreateXDGShell(display Display, version uint32) XDGShell {
	return XDGShell{p: C.wlr_xdg_shell_create(display.p, C.uint32_t(version))}
}

// Ptr returns the underlying struct wlr_xdg_shell pointer.
func (s XDGShell) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// OnNewToplevel is emitted when a client creates an xdg_toplevel. The
// toplevel is not configured yet: wait for its initial commit.
func (s XDGShell) OnNewToplevel(cb func(XDGToplevel)) Listener {
	return newListener(&s.p.events.new_toplevel, func(data unsafe.Pointer) {
		cb(XDGToplevel{p: (*C.struct_wlr_xdg_toplevel)(data)})
	})
}

// OnNewPopup is emitted when a client creates an xdg_popup.
func (s XDGShell) OnNewPopup(cb func(XDGPopup)) Listener {
	return newListener(&s.p.events.new_popup, func(data unsafe.Pointer) {
		cb(XDGPopup{p: (*C.struct_wlr_xdg_popup)(data)})
	})
}

// XDGSurface wraps struct wlr_xdg_surface.
type XDGSurface struct {
	p *C.struct_wlr_xdg_surface
}

// XDGSurfaceFromPtr wraps a struct wlr_xdg_surface pointer.
func XDGSurfaceFromPtr(p unsafe.Pointer) XDGSurface {
	return XDGSurface{p: (*C.struct_wlr_xdg_surface)(p)}
}

// Ptr returns the underlying struct wlr_xdg_surface pointer.
func (s XDGSurface) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// Valid reports whether s wraps an xdg_surface.
func (s XDGSurface) Valid() bool { return s.p != nil }

// Surface returns the underlying wl_surface.
func (s XDGSurface) Surface() Surface { return Surface{p: s.p.surface} }

// Initialized reports whether the surface may receive configure events
// (it has performed its initial commit and has not been unmapped since).
func (s XDGSurface) Initialized() bool { return bool(s.p.initialized) }

// InitialCommit reports whether the commit being processed is the initial one.
func (s XDGSurface) InitialCommit() bool { return bool(s.p.initial_commit) }

// Geometry returns the window geometry in surface-local coordinates.
func (s XDGSurface) Geometry() image.Rectangle { return boxFromC(&s.p.geometry) }

// ScheduleConfigure schedules an empty configure event. It is a no-op until
// the surface is initialized.
func (s XDGSurface) ScheduleConfigure() {
	if s.Initialized() {
		C.wlr_xdg_surface_schedule_configure(s.p)
	}
}

// XDGToplevel wraps struct wlr_xdg_toplevel.
//
// All setters are no-ops until the surface is initialized, because wlroots
// asserts if a configure is scheduled before the initial commit. Compositors
// must send their initial state from the initial commit handler.
type XDGToplevel struct {
	p *C.struct_wlr_xdg_toplevel
}

// XDGToplevelFromPtr wraps a struct wlr_xdg_toplevel pointer.
func XDGToplevelFromPtr(p unsafe.Pointer) XDGToplevel {
	return XDGToplevel{p: (*C.struct_wlr_xdg_toplevel)(p)}
}

// Ptr returns the underlying struct wlr_xdg_toplevel pointer.
func (t XDGToplevel) Ptr() unsafe.Pointer { return unsafe.Pointer(t.p) }

// Valid reports whether t wraps a toplevel.
func (t XDGToplevel) Valid() bool { return t.p != nil }

// Base returns the toplevel's xdg_surface.
func (t XDGToplevel) Base() XDGSurface { return XDGSurface{p: t.p.base} }

// Title returns the window title.
func (t XDGToplevel) Title() string { return C.GoString(t.p.title) }

// AppID returns the application id.
func (t XDGToplevel) AppID() string { return C.GoString(t.p.app_id) }

// Parent returns the parent toplevel (invalid if none).
func (t XDGToplevel) Parent() XDGToplevel { return XDGToplevel{p: t.p.parent} }

// Current returns the committed toplevel state.
func (t XDGToplevel) Current() XDGToplevelState { return XDGToplevelState{v: t.p.current} }

// RequestedMaximized reports whether the client asked to be maximized.
func (t XDGToplevel) RequestedMaximized() bool { return bool(t.p.requested.maximized) }

// RequestedFullscreen reports whether the client asked to be fullscreen.
func (t XDGToplevel) RequestedFullscreen() bool { return bool(t.p.requested.fullscreen) }

func (t XDGToplevel) configurable() bool {
	return t.p != nil && t.p.base != nil && bool(t.p.base.initialized)
}

// SetSize requests a new window geometry size (0 = client's choice).
func (t XDGToplevel) SetSize(width, height int32) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_size(t.p, C.int32_t(width), C.int32_t(height))
	}
}

// SetActivated toggles the activated (focused) state.
func (t XDGToplevel) SetActivated(activated bool) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_activated(t.p, C.bool(activated))
	}
}

// SetMaximized toggles the maximized state.
func (t XDGToplevel) SetMaximized(maximized bool) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_maximized(t.p, C.bool(maximized))
	}
}

// SetFullscreen toggles the fullscreen state.
func (t XDGToplevel) SetFullscreen(fullscreen bool) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_fullscreen(t.p, C.bool(fullscreen))
	}
}

// SetResizing toggles the resizing state.
func (t XDGToplevel) SetResizing(resizing bool) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_resizing(t.p, C.bool(resizing))
	}
}

// SetTiled sets the edges the window is tiled against.
func (t XDGToplevel) SetTiled(edges Edges) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_tiled(t.p, C.uint32_t(edges))
	}
}

// SetBounds hints the maximum useful window size (0 = unknown).
func (t XDGToplevel) SetBounds(width, height int32) {
	if t.configurable() {
		C.wlr_xdg_toplevel_set_bounds(t.p, C.int32_t(width), C.int32_t(height))
	}
}

// SendClose asks the client to close the window.
func (t XDGToplevel) SendClose() { C.wlr_xdg_toplevel_send_close(t.p) }

// OnDestroy is emitted when the xdg_toplevel role object is destroyed. This
// happens before the xdg_surface and wl_surface are destroyed, and all
// toplevel listeners must be removed at this point.
func (t XDGToplevel) OnDestroy(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.destroy, func(unsafe.Pointer) { cb(t) })
}

// OnRequestMove is emitted when the client starts an interactive move.
func (t XDGToplevel) OnRequestMove(cb func(t XDGToplevel, client SeatClient, serial uint32)) Listener {
	return newListener(&t.p.events.request_move, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_xdg_toplevel_move_event)(data)
		cb(t, SeatClient{p: event.seat}, uint32(event.serial))
	})
}

// OnRequestResize is emitted when the client starts an interactive resize.
func (t XDGToplevel) OnRequestResize(cb func(t XDGToplevel, client SeatClient, serial uint32, edges Edges)) Listener {
	return newListener(&t.p.events.request_resize, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_xdg_toplevel_resize_event)(data)
		cb(t, SeatClient{p: event.seat}, uint32(event.serial), Edges(event.edges))
	})
}

// OnRequestMaximize is emitted when the client toggles maximization. The
// compositor must answer with a configure once the surface is initialized.
func (t XDGToplevel) OnRequestMaximize(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.request_maximize, func(unsafe.Pointer) { cb(t) })
}

// OnRequestFullscreen is emitted when the client toggles fullscreen. The
// compositor must answer with a configure once the surface is initialized.
func (t XDGToplevel) OnRequestFullscreen(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.request_fullscreen, func(unsafe.Pointer) { cb(t) })
}

// OnRequestMinimize is emitted when the client asks to be minimized.
func (t XDGToplevel) OnRequestMinimize(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.request_minimize, func(unsafe.Pointer) { cb(t) })
}

// OnSetParent is emitted when the client changes the parent toplevel.
func (t XDGToplevel) OnSetParent(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.set_parent, func(unsafe.Pointer) { cb(t) })
}

// OnSetTitle is emitted when the title changes.
func (t XDGToplevel) OnSetTitle(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.set_title, func(unsafe.Pointer) { cb(t) })
}

// OnSetAppID is emitted when the app id changes.
func (t XDGToplevel) OnSetAppID(cb func(XDGToplevel)) Listener {
	return newListener(&t.p.events.set_app_id, func(unsafe.Pointer) { cb(t) })
}

// XDGToplevelState is a copy of struct wlr_xdg_toplevel_state.
type XDGToplevelState struct {
	v C.struct_wlr_xdg_toplevel_state
}

// Activated reports the activated state.
func (s XDGToplevelState) Activated() bool { return bool(s.v.activated) }

// Width returns the configured width.
func (s XDGToplevelState) Width() uint32 { return uint32(s.v.width) }

// Height returns the configured height.
func (s XDGToplevelState) Height() uint32 { return uint32(s.v.height) }

// MinWidth returns the client's minimum width (0 = none).
func (s XDGToplevelState) MinWidth() uint32 { return uint32(s.v.min_width) }

// MinHeight returns the client's minimum height (0 = none).
func (s XDGToplevelState) MinHeight() uint32 { return uint32(s.v.min_height) }

// MaxWidth returns the client's maximum width (0 = none).
func (s XDGToplevelState) MaxWidth() uint32 { return uint32(s.v.max_width) }

// MaxHeight returns the client's maximum height (0 = none).
func (s XDGToplevelState) MaxHeight() uint32 { return uint32(s.v.max_height) }

// XDGPopup wraps struct wlr_xdg_popup.
type XDGPopup struct {
	p *C.struct_wlr_xdg_popup
}

// Ptr returns the underlying struct wlr_xdg_popup pointer.
func (p XDGPopup) Ptr() unsafe.Pointer { return unsafe.Pointer(p.p) }

// Base returns the popup's xdg_surface.
func (p XDGPopup) Base() XDGSurface { return XDGSurface{p: p.p.base} }

// Parent returns the parent wl_surface (invalid if not set yet).
func (p XDGPopup) Parent() Surface { return Surface{p: p.p.parent} }

// UnconstrainFromBox repositions the popup so it fits box, expressed in the
// coordinate space of the popup's toplevel.
func (p XDGPopup) UnconstrainFromBox(box image.Rectangle) {
	cb := boxToC(box)
	C.wlr_xdg_popup_unconstrain_from_box(p.p, &cb)
}

// OnDestroy is emitted when the popup is destroyed.
func (p XDGPopup) OnDestroy(cb func(XDGPopup)) Listener {
	return newListener(&p.p.events.destroy, func(unsafe.Pointer) { cb(p) })
}

// XDGToplevelDecorationV1Mode mirrors enum wlr_xdg_toplevel_decoration_v1_mode.
type XDGToplevelDecorationV1Mode uint32

const (
	XDGToplevelDecorationV1ModeNone       XDGToplevelDecorationV1Mode = C.WLR_XDG_TOPLEVEL_DECORATION_V1_MODE_NONE
	XDGToplevelDecorationV1ModeClientSide XDGToplevelDecorationV1Mode = C.WLR_XDG_TOPLEVEL_DECORATION_V1_MODE_CLIENT_SIDE
	XDGToplevelDecorationV1ModeServerSide XDGToplevelDecorationV1Mode = C.WLR_XDG_TOPLEVEL_DECORATION_V1_MODE_SERVER_SIDE
)

// XDGDecorationManagerV1 wraps struct wlr_xdg_decoration_manager_v1.
type XDGDecorationManagerV1 struct {
	p *C.struct_wlr_xdg_decoration_manager_v1
}

// CreateXDGDecorationManagerV1 creates the zxdg_decoration_manager_v1 global.
func CreateXDGDecorationManagerV1(display Display) XDGDecorationManagerV1 {
	return XDGDecorationManagerV1{p: C.wlr_xdg_decoration_manager_v1_create(display.p)}
}

// OnNewToplevelDecoration is emitted when a client binds a decoration object
// to one of its toplevels (usually before the toplevel's initial commit).
func (m XDGDecorationManagerV1) OnNewToplevelDecoration(cb func(XDGToplevelDecorationV1)) Listener {
	return newListener(&m.p.events.new_toplevel_decoration, func(data unsafe.Pointer) {
		cb(XDGToplevelDecorationV1{p: (*C.struct_wlr_xdg_toplevel_decoration_v1)(data)})
	})
}

// XDGToplevelDecorationV1 wraps struct wlr_xdg_toplevel_decoration_v1.
type XDGToplevelDecorationV1 struct {
	p *C.struct_wlr_xdg_toplevel_decoration_v1
}

// Valid reports whether d wraps a decoration object.
func (d XDGToplevelDecorationV1) Valid() bool { return d.p != nil }

// Toplevel returns the decorated toplevel.
func (d XDGToplevelDecorationV1) Toplevel() XDGToplevel { return XDGToplevel{p: d.p.toplevel} }

// RequestedMode returns the mode requested by the client (None = no preference).
func (d XDGToplevelDecorationV1) RequestedMode() XDGToplevelDecorationV1Mode {
	return XDGToplevelDecorationV1Mode(d.p.requested_mode)
}

// SetMode schedules the decoration mode. It returns false (and does nothing)
// while the toplevel is not initialized yet; send it again from the initial
// commit handler in that case.
func (d XDGToplevelDecorationV1) SetMode(mode XDGToplevelDecorationV1Mode) bool {
	if mode == XDGToplevelDecorationV1ModeNone || d.p.toplevel == nil ||
		d.p.toplevel.base == nil || !bool(d.p.toplevel.base.initialized) {
		return false
	}
	C.wlr_xdg_toplevel_decoration_v1_set_mode(d.p, C.enum_wlr_xdg_toplevel_decoration_v1_mode(mode))
	return true
}

// OnRequestMode is emitted when the client changes its preferred mode.
func (d XDGToplevelDecorationV1) OnRequestMode(cb func(XDGToplevelDecorationV1)) Listener {
	return newListener(&d.p.events.request_mode, func(unsafe.Pointer) { cb(d) })
}

// OnDestroy is emitted when the decoration object is destroyed.
func (d XDGToplevelDecorationV1) OnDestroy(cb func(XDGToplevelDecorationV1)) Listener {
	return newListener(&d.p.events.destroy, func(unsafe.Pointer) { cb(d) })
}
