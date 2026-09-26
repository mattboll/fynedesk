package compositor

/*
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

#include "pixel_buffer.h"

static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void scene_buffer_set_opacity_t(struct wlr_scene_buffer *buf, float opacity) {
	wlr_scene_buffer_set_opacity(buf, opacity);
}
static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
*/
import "C"

import (
	"image"
	"image/color"
	"math/rand"
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wallpaper"
)

const (
	transitionDuration = 350 * time.Millisecond
	slideDuration      = 250 * time.Millisecond
)

// startSlideTransition begins a horizontal slide animation for desktop switching.
// A snapshot of the old desktop slides out while the new desktop is revealed beneath.
// The slide direction comes from s.slideDirection (-1=left, +1=right).
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

	// If already transitioning, reset
	if s.transitionActive && s.transitionImg != nil &&
		s.transitionImg.Bounds().Dx() == w && s.transitionImg.Bounds().Dy() == h {
		// Reuse existing overlay — fill with semi-opaque dark
		pix := s.transitionImg.Pix
		for i := 0; i < len(pix); i += 4 {
			pix[i] = 0x10   // R
			pix[i+1] = 0x14 // G
			pix[i+2] = 0x1A // B
			pix[i+3] = 0xE0 // A (high opacity)
		}
		s.transitionStart = time.Now()
		return
	}

	s.endTransition()

	// Create semi-opaque dark overlay that will slide off-screen
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	pix := img.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i] = 0x10   // R
		pix[i+1] = 0x14 // G
		pix[i+2] = 0x1A // B
		pix[i+3] = 0xE0 // A
	}

	pixBuf := C.pixel_buffer_create(C.int(w), C.int(h))
	if pixBuf == nil {
		return
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&pix[0]), C.int(w), C.int(h))

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	sceneBuf := C.scene_buffer_create(ovTree, &pixBuf.base)
	if sceneBuf == nil {
		C.pixel_buffer_release(pixBuf)
		return
	}
	C.scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
	C.scene_node_set_position(&sceneBuf.node, C.int(out.layoutX), C.int(out.layoutY))

	s.transitionBuf = unsafe.Pointer(sceneBuf)
	s.transitionPixBuf = unsafe.Pointer(pixBuf)
	s.transitionImg = img
	s.transitionActive = true
	s.transitionStart = time.Now()
}

// tickTransition advances the desktop transition animation (slide or matrix).
// Returns true if the animation is still running (caller should schedule a frame).
func (s *server) tickTransition() bool {
	if !s.transitionActive {
		return false
	}

	if s.backgroundType == "matrix" {
		return s.tickMatrixTransition()
	}
	return s.tickSlideTransition()
}

// tickSlideTransition moves the overlay off-screen in the slide direction with
// fading opacity. The new desktop is already visible underneath.
func (s *server) tickSlideTransition() bool {
	elapsed := time.Since(s.transitionStart)
	if elapsed >= slideDuration {
		s.slideDirection = 0
		s.endTransition()
		return false
	}

	out := s.primaryOutput()
	if out == nil {
		s.slideDirection = 0
		s.endTransition()
		return false
	}

	progress := dampedSpring(float64(elapsed) / float64(slideDuration))

	w := out.width
	// Slide the overlay off-screen: direction * progress * width
	offsetX := int(float64(s.slideDirection) * progress * float64(w))

	// Fade out the overlay alpha as it slides
	alpha := uint8(float64(0xE0) * (1 - progress))
	if alpha < 2 {
		s.slideDirection = 0
		s.endTransition()
		return false
	}

	// The overlay's alpha is uniform (0xE0): the fade is the buffer's
	// opacity, drawn by the GPU, rather than every pixel rewritten and copied
	// again on the CPU at each frame.
	sceneBuf := (*C.struct_wlr_scene_buffer)(s.transitionBuf)
	// (The spring can overshoot: wlroots asserts 0 <= opacity <= 1.)
	C.scene_buffer_set_opacity_t(sceneBuf, C.float(min(max(1-progress, 0), 1)))
	C.scene_node_set_position(&sceneBuf.node, C.int(out.layoutX+offsetX), C.int(out.layoutY))

	return true
}

const matrixTransitionDuration = 350 * time.Millisecond

// tickMatrixTransition renders a matrix rain sweep transition.
// Columns of matrix glyphs sweep across the screen in the slide direction,
// leaving a trail that fades to transparent, revealing the new desktop.
func (s *server) tickMatrixTransition() bool {
	elapsed := time.Since(s.transitionStart)
	if elapsed >= matrixTransitionDuration {
		s.slideDirection = 0
		s.endTransition()
		return false
	}

	out := s.primaryOutput()
	if out == nil {
		s.slideDirection = 0
		s.endTransition()
		return false
	}

	progress := float64(elapsed) / float64(matrixTransitionDuration)
	eased := easeOutCubic(progress)

	w, h := out.width, out.height
	img := s.transitionImg
	pix := img.Pix
	stride := img.Stride

	// The sweep front moves across the screen
	// direction: -1=left (sweep goes left to right revealing), +1=right (sweep right to left)
	var sweepX int
	if s.slideDirection > 0 {
		sweepX = int(eased * float64(w))
	} else {
		sweepX = w - int(eased*float64(w))
	}

	rng := rand.New(rand.NewSource(int64(elapsed.Milliseconds())))

	// Clear entire image to black transparent
	for i := 0; i < len(pix); i += 4 {
		pix[i] = 0
		pix[i+1] = 0
		pix[i+2] = 0
		pix[i+3] = 0
	}

	// Draw sweep zone: a band of matrix rain columns around the sweep front
	bandWidth := w / 4 // rain band is 25% of screen width
	var bandStart, bandEnd int
	if s.slideDirection > 0 {
		bandStart = max(0, sweepX-bandWidth)
		bandEnd = min(w, sweepX)
	} else {
		bandStart = max(0, sweepX)
		bandEnd = min(w, sweepX+bandWidth)
	}

	// Fill the "already swept" area with semi-opaque dark (fading)
	if s.slideDirection > 0 {
		// Left side already swept — make it opaque (old desktop hidden)
		fadeAlpha := uint8(float64(0xE0) * (1 - eased))
		for y := 0; y < h; y++ {
			off := y * stride
			for x := 0; x < bandStart; x++ {
				idx := off + x*4
				pix[idx] = 0
				pix[idx+1] = 0x08
				pix[idx+2] = 0
				pix[idx+3] = fadeAlpha
			}
		}
	} else {
		fadeAlpha := uint8(float64(0xE0) * (1 - eased))
		for y := 0; y < h; y++ {
			off := y * stride
			for x := bandEnd; x < w; x++ {
				idx := off + x*4
				pix[idx] = 0
				pix[idx+1] = 0x08
				pix[idx+2] = 0
				pix[idx+3] = fadeAlpha
			}
		}
	}

	// Draw matrix rain columns in the sweep band
	numCols := (bandEnd - bandStart) / wallpaper.MatrixGlyphW
	for col := 0; col < numCols; col++ {
		px := bandStart + col*wallpaper.MatrixGlyphW
		// Each column has a random vertical offset and length
		colSeed := rng.Intn(h)
		colLen := 3 + rng.Intn(8) // 3-10 glyphs tall

		for g := 0; g < colLen; g++ {
			py := (colSeed + g*wallpaper.MatrixGlyphH) % h
			glyphIdx := rng.Intn(wallpaper.MatrixNumGlyphs)

			// Head glyph is brightest, trail fades
			var greenVal uint8
			if g == 0 {
				greenVal = 0xFF
			} else {
				greenVal = uint8(max(0x33, 0xCC-g*0x20))
			}
			alpha := uint8(0xFF)
			if g == colLen-1 {
				alpha = 0x80 // trail tip fades
			}

			wallpaper.DrawMatrixGlyph(pix, stride, px, py, w, h, glyphIdx,
				color.NRGBA{R: 0, G: greenVal, B: greenVal / 8, A: alpha})
		}
	}

	// Push pixels
	pixBuf := (*C.struct_pixel_buffer)(s.transitionPixBuf)
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&pix[0]), C.int(w), C.int(h))
	sceneBuf := (*C.struct_wlr_scene_buffer)(s.transitionBuf)
	C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
	// Keep centered (no sliding)
	C.scene_node_set_position(&sceneBuf.node, C.int(out.layoutX), C.int(out.layoutY))

	return true
}

// endTransition cleans up the transition overlay and frees buffers.
func (s *server) endTransition() {
	s.transitionActive = false
	if s.transitionBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.transitionBuf)
		C.scene_node_destroy(&sceneBuf.node)
		s.transitionBuf = nil
	}
	if s.transitionPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(s.transitionPixBuf)
		C.pixel_buffer_release(pixBuf)
		s.transitionPixBuf = nil
	}
	s.transitionImg = nil
}
