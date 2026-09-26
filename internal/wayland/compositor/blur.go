package compositor

/*
#include "blur.h"
*/
import "C"

import (
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// Frosted glass: behind the panel, the menus and the notifications, what
// lies below is blurred where the window lets it show through (blur.c). The
// blur is drawn again only when what lies behind, or the window, changes.

// blurRef is the blur behind a window, made on the first frame after the
// window gets one (nil until then).
type blurRef = *C.struct_blur

// canBlur reports whether the renderer can blur (GLES2).
func (s *server) canBlur() bool {
	return bool(C.blur_supported())
}

// addBlur puts the blur behind a panel or overlay window.
func (s *server) addBlur(v *xwayView) {
	if !s.blurBehind || v.sceneTree == nil {
		return
	}
	if s.blurs == nil {
		s.blurs = map[*xwayView]blurRef{}
	}
	if _, ok := s.blurs[v]; !ok {
		s.blurs[v] = nil // made on the next frame, with its output
	}
}

// removeBlur takes the blur away from behind a window.
// dropBlursGL destroys the blurs, made with the old renderer after a GPU
// reset; they are made again on the next frame.
func (s *server) dropBlursGL() {
	for v, b := range s.blurs {
		if b != nil {
			C.blur_destroy(b)
			s.blurs[v] = nil
		}
	}
	C.blur_reset_gl()
}

func (s *server) removeBlur(v *xwayView) {
	if b, ok := s.blurs[v]; ok {
		if b != nil {
			C.blur_destroy(b)
		}
		delete(s.blurs, v)
	}
}

// setBlurBehind turns the blur on or off for the windows that have it.
func (s *server) setBlurBehind(on bool) {
	if on == s.blurBehind {
		return
	}
	s.blurBehind = on
	if !on {
		for v := range s.blurs {
			s.removeBlur(v)
		}
		return
	}
	for _, v := range s.xwayViews {
		if v.mapped && (v.isPanel || v.isOverlay) {
			s.addBlur(v)
		}
	}
	s.scheduleAllOutputFrames()
}

// updateBlurs draws the blurs again where needed, before output is drawn.
func (s *server) updateBlurs(output wlr.Output) {
	if len(s.blurs) == 0 {
		return
	}
	scale := C.float(s.maxOutputScale())
	scene := (*C.struct_wlr_scene)(s.scene)
	for v, b := range s.blurs {
		if b == nil {
			b = C.blur_create((*C.struct_wlr_scene_tree)(v.sceneTree), (*C.struct_wlr_output)(outputPtr(output)))
			if b == nil {
				delete(s.blurs, v) // not GLES2: no blur
				continue
			}
			s.blurs[v] = b
		}
		if !C.blur_alive(b) {
			s.removeBlur(v)
			continue
		}
		if !v.mapped || !v.surface.Surface().Valid() {
			continue
		}
		C.blur_update(b, scene, (*C.struct_wlr_surface)(unsafe.Pointer(surfacePtr(v.surface.Surface()))), scale)
	}
}
