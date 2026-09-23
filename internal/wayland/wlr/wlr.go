// Package wlr is Tyde's thin Go layer over wlroots.
//
// It only wraps what the compositor needs and targets a single wlroots
// release (see the pkg-config directive below): wlroots breaks its API on
// every minor version, so bumping it is meant to be a change contained in
// this package.
//
// Every wrapper type is a small value holding the underlying C pointer.
// Ptr returns it as an unsafe.Pointer and the matching XxxFromPtr function
// wraps a pointer obtained from C, so code with its own cgo preamble can
// interoperate without reaching into the wrapper's fields.
//
// Lifetime rules (wlroots 0.19+ asserts on them):
//   - every Listener must be destroyed before the object emitting the signal
//     is destroyed, typically from that object's OnDestroy callback;
//   - xdg_surface configure events may only be scheduled once the surface is
//     initialized. XDGToplevel setters enforce this and are no-ops before the
//     initial commit, so the compositor must (re)send its state from the
//     initial commit handler.
package wlr

/*
#cgo pkg-config: wlroots-0.20 wayland-server xkbcommon pixman-1
#cgo CFLAGS: -D_GNU_SOURCE -DWLR_USE_UNSTABLE

#include <wlr/util/box.h>
#include <wlr/util/edges.h>
*/
import "C"

import (
	"image"
	"unsafe"
)

// Edges is a bitmask of enum wlr_edges.
type Edges uint32

const (
	EdgeNone   Edges = C.WLR_EDGE_NONE
	EdgeTop    Edges = C.WLR_EDGE_TOP
	EdgeBottom Edges = C.WLR_EDGE_BOTTOM
	EdgeLeft   Edges = C.WLR_EDGE_LEFT
	EdgeRight  Edges = C.WLR_EDGE_RIGHT
)

func boxFromC(box *C.struct_wlr_box) image.Rectangle {
	if box == nil {
		return image.Rectangle{}
	}
	return image.Rect(
		int(box.x),
		int(box.y),
		int(box.x+box.width),
		int(box.y+box.height),
	)
}

func boxToC(r image.Rectangle) C.struct_wlr_box {
	r = r.Canon()
	return C.struct_wlr_box{
		x:      C.int(r.Min.X),
		y:      C.int(r.Min.Y),
		width:  C.int(r.Dx()),
		height: C.int(r.Dy()),
	}
}

// container returns the struct of type T that embeds the wl_list link at the
// given byte offset (the Go counterpart of wl_container_of).
func container[T any](link *C.struct_wl_list, offset uintptr) *T {
	return (*T)(unsafe.Add(unsafe.Pointer(link), -int(offset)))
}
