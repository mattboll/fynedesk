package wlr

/*
#include <time.h>
#include <wlr/types/wlr_compositor.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/xwayland.h>
*/
import "C"

import "unsafe"

// Surface wraps struct wlr_surface.
type Surface struct {
	p *C.struct_wlr_surface
}

// SurfaceFromPtr wraps a struct wlr_surface pointer.
func SurfaceFromPtr(p unsafe.Pointer) Surface {
	return Surface{p: (*C.struct_wlr_surface)(p)}
}

// Ptr returns the underlying struct wlr_surface pointer.
func (s Surface) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// Valid reports whether s wraps a surface.
func (s Surface) Valid() bool { return s.p != nil }

// Mapped reports whether the surface is mapped.
func (s Surface) Mapped() bool { return bool(s.p.mapped) }

// HasBuffer reports whether the surface has a buffer attached.
func (s Surface) HasBuffer() bool { return bool(C.wlr_surface_has_buffer(s.p)) }

// Current returns a snapshot of the committed surface state.
func (s Surface) Current() SurfaceState { return SurfaceState{v: s.p.current} }

// OnMap is emitted when the surface becomes mapped.
func (s Surface) OnMap(cb func(Surface)) Listener {
	return newListener(&s.p.events._map, func(unsafe.Pointer) { cb(s) })
}

// OnUnmap is emitted when the surface becomes unmapped.
func (s Surface) OnUnmap(cb func(Surface)) Listener {
	return newListener(&s.p.events.unmap, func(unsafe.Pointer) { cb(s) })
}

// OnCommit is emitted after a surface commit has been applied.
func (s Surface) OnCommit(cb func(Surface)) Listener {
	return newListener(&s.p.events.commit, func(unsafe.Pointer) { cb(s) })
}

// OnDestroy is emitted when the surface is destroyed.
func (s Surface) OnDestroy(cb func(Surface)) Listener {
	return newListener(&s.p.events.destroy, func(unsafe.Pointer) { cb(s) })
}

// SurfaceState is a copy of struct wlr_surface_state.
type SurfaceState struct {
	v C.struct_wlr_surface_state
}

// Width returns the surface width in surface-local coordinates.
func (s SurfaceState) Width() int { return int(s.v.width) }

// Height returns the surface height in surface-local coordinates.
func (s SurfaceState) Height() int { return int(s.v.height) }
