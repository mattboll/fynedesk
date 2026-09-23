package compositor

// Build flags shared by every cgo preamble of this package. cgo concatenates
// the #cgo directives of all files of a package, so the wlroots version is
// pinned here and in internal/wayland/wlr only.

/*
#cgo pkg-config: wlroots-0.20 wayland-server xkbcommon pixman-1 egl glesv2
#cgo CFLAGS: -DWLR_USE_UNSTABLE
*/
import "C"
