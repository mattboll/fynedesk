package compositor

/*
#include <wlr/types/wlr_scene.h>

#include "matrix_fx.h"
*/
import "C"

import (
	"math/rand"
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wallpaper"
)

const (
	slideDuration            = 250 * time.Millisecond
	matrixTransitionDuration = 350 * time.Millisecond
)

// slideShade is the veil that slides off the screen when the desktop
// changes: dark, nearly opaque.
var slideShade = [4]float32{0x10 / 255.0, 0x14 / 255.0, 0x1A / 255.0, 0xE0 / 255.0}

// startSlideTransition begins the animation of a desktop switch, in the
// direction of s.slideDirection (-1 = left, +1 = right): with the matrix
// wallpaper, a rain of glyphs sweeps the screen (on the GPU); otherwise a
// dark veil slides off it, uncovering the new desktop.
func (s *server) startSlideTransition() {
	if s.reduceMotion {
		return // Skip slide animation
	}
	out := s.primaryOutput()
	if out == nil {
		return
	}
	w, h := out.width, out.height
	if w <= 0 || h <= 0 {
		return
	}
	s.endTransition()

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	var node *C.struct_wlr_scene_node
	if s.backgroundType == "matrix" {
		atlas := wallpaper.MatrixGlyphAtlas()
		fx := C.matrix_fx_create(ovTree, outputPtr(out.output), C.int(w), C.int(h),
			(*C.uint8_t)(unsafe.Pointer(&atlas[0])),
			C.int(wallpaper.MatrixGlyphW), C.int(wallpaper.MatrixGlyphH), C.int(wallpaper.MatrixNumGlyphs))
		if fx != nil {
			s.matrixFX = unsafe.Pointer(fx)
			node = C.matrix_fx_node(fx)
		}
	}
	if node == nil { // the veil, also for the matrix without GLES2
		rect := C.wlr_scene_rect_create(ovTree, C.int(w), C.int(h), premultiplied(slideShade, 1))
		if rect == nil {
			return
		}
		s.transitionRect = unsafe.Pointer(rect)
		node = &rect.node
	}
	C.wlr_scene_node_set_position(node, C.int(out.layoutX), C.int(out.layoutY))
	s.transitionNode = unsafe.Pointer(node)
	s.transitionActive = true
	s.transitionStart = time.Now()
}

// premultiplied returns a colour faded by opacity, premultiplied as the
// scene wants it.
func premultiplied(c [4]float32, opacity float32) *C.float {
	a := c[3] * opacity
	p := [4]C.float{C.float(c[0] * a), C.float(c[1] * a), C.float(c[2] * a), C.float(a)}
	return &p[0]
}

// tickTransition advances the desktop transition animation (slide or matrix).
// Returns true if the animation is still running (caller should schedule a frame).
func (s *server) tickTransition() bool {
	if !s.transitionActive {
		return false
	}
	if s.matrixFX != nil {
		return s.tickMatrixTransition()
	}
	return s.tickSlideTransition()
}

// tickSlideTransition moves the veil off-screen in the slide direction,
// fading it. The new desktop is already visible underneath.
func (s *server) tickSlideTransition() bool {
	elapsed := time.Since(s.transitionStart)
	out := s.primaryOutput()
	if elapsed >= slideDuration || out == nil {
		s.endTransition()
		return false
	}

	progress := dampedSpring(float64(elapsed) / float64(slideDuration))
	// (The spring can overshoot.)
	opacity := min(max(1-progress, 0), 1)
	if opacity*float64(slideShade[3]) < 2.0/255 {
		s.endTransition()
		return false
	}
	offsetX := int(float64(s.slideDirection) * progress * float64(out.width))

	rect := (*C.struct_wlr_scene_rect)(s.transitionRect)
	C.wlr_scene_rect_set_color(rect, premultiplied(slideShade, float32(opacity)))
	C.wlr_scene_node_set_position(&rect.node, C.int(out.layoutX+offsetX), C.int(out.layoutY))
	return true
}

// tickMatrixTransition draws a frame of the matrix rain: a band of glyph
// columns sweeps across the screen in the slide direction, the part it has
// passed dark and fading, revealing the new desktop.
func (s *server) tickMatrixTransition() bool {
	elapsed := time.Since(s.transitionStart)
	out := s.primaryOutput()
	if elapsed >= matrixTransitionDuration || out == nil {
		s.endTransition()
		return false
	}

	eased := easeOutCubic(float64(elapsed) / float64(matrixTransitionDuration))
	w := float64(out.width)
	band := w / 4 // the rain is a quarter of the screen wide
	var start, end float64
	if s.slideDirection > 0 {
		front := eased * w
		start, end = max(0, front-band), front
	} else {
		front := w - eased*w
		start, end = front, min(w, front+band)
	}

	cols, ncols := matrixColumns(int(end-start)/wallpaper.MatrixGlyphW, out.height, elapsed.Milliseconds())
	fx := (*C.struct_matrix_fx)(s.matrixFX)
	if !C.matrix_fx_draw(fx, C.float(start), C.float(end), C.int(s.slideDirection), C.float(1-eased),
		(*C.uint8_t)(unsafe.Pointer(&cols[0])), C.int(ncols)) {
		s.endTransition()
		return false
	}
	C.wlr_scene_node_set_position(C.matrix_fx_node(fx), C.int(out.layoutX), C.int(out.layoutY))
	return true
}

// matrixColumns draws the rain's columns for a frame, as matrix_fx_draw
// takes them: where each starts (0..h), how many glyphs long (3..10) and
// which glyphs. A new seed each millisecond: the columns flicker.
func matrixColumns(ncols, h int, seed int64) ([]byte, int) {
	rows := C.MATRIX_FX_COL_ROWS
	cols := make([]byte, max(ncols, 1)*rows*4)
	rng := rand.New(rand.NewSource(seed))
	for c := 0; c < ncols; c++ {
		start, length := rng.Intn(h), 3+rng.Intn(8)
		head := c * 4
		cols[head], cols[head+1], cols[head+2] = byte(start>>8), byte(start), byte(length)
		for g := 0; g < length; g++ {
			cols[((g+1)*ncols+c)*4] = byte(rng.Intn(wallpaper.MatrixNumGlyphs))
		}
	}
	return cols, ncols
}

// endTransition removes the veil or the rain.
func (s *server) endTransition() {
	s.transitionActive = false
	s.slideDirection = 0
	if s.matrixFX != nil {
		C.matrix_fx_destroy((*C.struct_matrix_fx)(s.matrixFX))
		s.matrixFX = nil
	} else if s.transitionNode != nil {
		C.wlr_scene_node_destroy((*C.struct_wlr_scene_node)(s.transitionNode))
	}
	s.transitionNode = nil
	s.transitionRect = nil
}

// dropTransitionGL ends the transition and forgets the rain's GL program,
// after a GPU reset: they were made with the old renderer.
func (s *server) dropTransitionGL() {
	s.endTransition()
	C.matrix_fx_reset_gl()
}
