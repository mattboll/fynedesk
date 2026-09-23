package wlr

/*
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/backend.h>
#include <wlr/backend/session.h>
#include <wlr/render/allocator.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
*/
import "C"

import (
	"errors"
	"image"
	"image/draw"
	"unsafe"
)

// Backend wraps struct wlr_backend.
type Backend struct {
	p *C.struct_wlr_backend
}

// AutocreateBackend picks the most suitable backend for the environment
// (nested Wayland/X11 window, or DRM+libinput with a session).
func AutocreateBackend(loop EventLoop) (Backend, Session) {
	var session *C.struct_wlr_session
	p := C.wlr_backend_autocreate(loop.p, &session)
	return Backend{p: p}, Session{p: session}
}

// Ptr returns the underlying struct wlr_backend pointer.
func (b Backend) Ptr() unsafe.Pointer { return unsafe.Pointer(b.p) }

// Valid reports whether the backend was created.
func (b Backend) Valid() bool { return b.p != nil }

// Start starts the backend, emitting new_output/new_input for existing devices.
func (b Backend) Start() error {
	if !C.wlr_backend_start(b.p) {
		return errors.New("can't start backend")
	}
	return nil
}

// Destroy destroys the backend and its outputs and input devices.
func (b Backend) Destroy() { C.wlr_backend_destroy(b.p) }

// OnNewOutput is emitted for each new output.
func (b Backend) OnNewOutput(cb func(Output)) Listener {
	return newListener(&b.p.events.new_output, func(data unsafe.Pointer) {
		cb(Output{p: (*C.struct_wlr_output)(data)})
	})
}

// OnNewInput is emitted for each new input device.
func (b Backend) OnNewInput(cb func(InputDevice)) Listener {
	return newListener(&b.p.events.new_input, func(data unsafe.Pointer) {
		cb(InputDevice{p: (*C.struct_wlr_input_device)(data)})
	})
}

// Session wraps struct wlr_session (nil when running nested).
type Session struct {
	p *C.struct_wlr_session
}

// Ptr returns the underlying struct wlr_session pointer (may be nil).
func (s Session) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// Valid reports whether a session exists (DRM/libinput backend).
func (s Session) Valid() bool { return s.p != nil }

// Renderer wraps struct wlr_renderer.
type Renderer struct {
	p *C.struct_wlr_renderer
}

// AutocreateRenderer creates the renderer best suited to the backend
// (honours WLR_RENDERER).
func AutocreateRenderer(backend Backend) Renderer {
	return Renderer{p: C.wlr_renderer_autocreate(backend.p)}
}

// Ptr returns the underlying struct wlr_renderer pointer.
func (r Renderer) Ptr() unsafe.Pointer { return unsafe.Pointer(r.p) }

// Valid reports whether the renderer was created.
func (r Renderer) Valid() bool { return r.p != nil }

// InitWLDisplay advertises the renderer's buffer formats (wl_shm,
// linux-dmabuf, ...) on the display.
func (r Renderer) InitWLDisplay(display Display) bool {
	return bool(C.wlr_renderer_init_wl_display(r.p, display.p))
}

// Destroy destroys the renderer.
func (r Renderer) Destroy() { C.wlr_renderer_destroy(r.p) }

// OnLost is emitted when the GPU is reset and the renderer must be recreated.
func (r Renderer) OnLost(cb func()) Listener {
	return newListener(&r.p.events.lost, func(unsafe.Pointer) { cb() })
}

// Allocator wraps struct wlr_allocator.
type Allocator struct {
	p *C.struct_wlr_allocator
}

// AutocreateAllocator creates an allocator suited to backend and renderer.
func AutocreateAllocator(backend Backend, renderer Renderer) Allocator {
	return Allocator{p: C.wlr_allocator_autocreate(backend.p, renderer.p)}
}

// Ptr returns the underlying struct wlr_allocator pointer.
func (a Allocator) Ptr() unsafe.Pointer { return unsafe.Pointer(a.p) }

// Valid reports whether the allocator was created.
func (a Allocator) Valid() bool { return a.p != nil }

// Destroy destroys the allocator.
func (a Allocator) Destroy() { C.wlr_allocator_destroy(a.p) }

// Texture wraps struct wlr_texture.
type Texture struct {
	p *C.struct_wlr_texture
}

// drmFormatABGR8888 is DRM_FORMAT_ABGR8888: R,G,B,A byte order, i.e. the
// memory layout of image.NRGBA.
const drmFormatABGR8888 = uint32('A') | uint32('B')<<8 | uint32('2')<<16 | uint32('4')<<24

// TextureFromImage uploads img (converted to NRGBA if needed) as a texture.
func TextureFromImage(renderer Renderer, img image.Image) Texture {
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		nrgba = image.NewNRGBA(img.Bounds())
		draw.Draw(nrgba, nrgba.Bounds(), img, img.Bounds().Min, draw.Src)
	}
	b := nrgba.Bounds()
	if b.Empty() {
		return Texture{}
	}
	p := C.wlr_texture_from_pixels(renderer.p, C.uint32_t(drmFormatABGR8888),
		C.uint32_t(nrgba.Stride), C.uint32_t(b.Dx()), C.uint32_t(b.Dy()),
		unsafe.Pointer(&nrgba.Pix[0]))
	return Texture{p: p}
}

// Ptr returns the underlying struct wlr_texture pointer.
func (t Texture) Ptr() unsafe.Pointer { return unsafe.Pointer(t.p) }

// Valid reports whether the texture exists.
func (t Texture) Valid() bool { return t.p != nil }

// Destroy destroys the texture.
func (t Texture) Destroy() { C.wlr_texture_destroy(t.p) }
