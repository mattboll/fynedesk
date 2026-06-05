package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

// pen_buffer is a CPU-backed wlr_buffer holding the felt-tip ink image.
// It mirrors the pixel_buffer used elsewhere; CGO preambles are per-file
// translation units, so this duplicate definition is local and conflict-free.
struct pen_buffer {
	struct wlr_buffer base;
	void *data;
	uint32_t format;
	size_t stride;
};
static void pen_buffer_destroy(struct wlr_buffer *wlr_buf) {
	struct pen_buffer *buf = (struct pen_buffer *)wlr_buf;
	free(buf->data);
	free(buf);
}
static bool pen_buffer_begin_data_ptr_access(struct wlr_buffer *wlr_buf,
		uint32_t flags, void **data, uint32_t *format, size_t *stride) {
	struct pen_buffer *buf = (struct pen_buffer *)wlr_buf;
	*data = buf->data; *format = buf->format; *stride = buf->stride;
	return true;
}
static void pen_buffer_end_data_ptr_access(struct wlr_buffer *wlr_buf) {}
static const struct wlr_buffer_impl pen_buffer_impl = {
	.destroy = pen_buffer_destroy,
	.begin_data_ptr_access = pen_buffer_begin_data_ptr_access,
	.end_data_ptr_access = pen_buffer_end_data_ptr_access,
};
static struct pen_buffer *pen_buffer_create(int w, int h) {
	struct pen_buffer *buf = calloc(1, sizeof(struct pen_buffer));
	if (!buf) return NULL;
	buf->format = DRM_FORMAT_ABGR8888;
	buf->stride = (size_t)w * 4;
	buf->data = calloc((size_t)h, buf->stride);
	if (!buf->data) { free(buf); return NULL; }
	wlr_buffer_init(&buf->base, &pen_buffer_impl, w, h);
	return buf;
}
static void pen_buffer_update(struct pen_buffer *buf, const void *pixels, int w, int h) {
	size_t new_stride = (size_t)w * 4;
	size_t new_size = new_stride * (size_t)h;
	if (buf->base.width != w || buf->base.height != h) {
		free(buf->data);
		buf->data = malloc(new_size);
		buf->stride = new_stride;
		buf->base.width = w;
		buf->base.height = h;
	}
	memcpy(buf->data, pixels, new_size);
}
static struct wlr_scene_buffer *pen_scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void pen_scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void pen_scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void pen_scene_buffer_set_opacity(struct wlr_scene_buffer *buf, float opacity) {
	wlr_scene_buffer_set_opacity(buf, opacity);
}
static void pen_scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void pen_scene_node_set_enabled(struct wlr_scene_tree *tree, int enabled) {
	wlr_scene_node_set_enabled(&tree->node, enabled != 0);
}
static void pen_scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
*/
import "C"

import (
	"image"
	"image/color"
	"math"
	"time"
	"unsafe"

	"deedles.dev/wlr"
)

// penColor is the felt-tip ink colour — a vivid, opaque marker red.
var penColor = color.NRGBA{R: 0xff, G: 0x35, B: 0x22, A: 0xff}

// btnLeft is the evdev code for the left mouse button (BTN_LEFT).
const btnLeft = 272

// handlePenButton routes left-button events for the felt-tip pen. A press with
// Super held starts (or extends) an annotation; the matching release ends the
// stroke. Returns true when the event was consumed and must not reach a client.
func (s *server) handlePenButton(button wlr.CursorButton, state wlr.ButtonState) bool {
	if button != btnLeft {
		return false
	}
	if state == wlr.ButtonPressed {
		kb := s.seat.Keyboard()
		if !keyboardValid(kb) || kb.GetModifiers()&wlr.KeyboardModifierLogo == 0 {
			return false
		}
		s.startPenStroke()
		return true
	}
	// Release: consume it only if we own the current press.
	if s.penButtonDown {
		s.endPenStroke()
		return true
	}
	return false
}

// startPenStroke begins a new ink stroke at the current cursor position.
// Called when Super+Left is pressed. Any pending fade is cancelled so the
// existing ink (if any) persists and is drawn over.
func (s *server) startPenStroke() {
	s.cancelPenFade()

	// Bare-Super press+release opens the overview (exposé); a pen click means
	// Super was used in combination, so suppress that gesture.
	s.superAlonePressed = false

	if !s.ensurePenImage() {
		return
	}

	s.penDrawing = true
	s.penButtonDown = true
	s.penInkActive = true
	s.penLastX = s.cursor.X()
	s.penLastY = s.cursor.Y()
	s.cursor.SetXCursor(s.cursorMgr, "crosshair")

	s.stampPenDot(s.cursor.X(), s.cursor.Y())
	s.commitPenInk()
}

// extendPenStroke draws ink from the last stamped point to the current cursor.
// Called on cursor motion while a stroke is active.
func (s *server) extendPenStroke() {
	if !s.penDrawing || s.penImg == nil {
		return
	}
	s.stampPenLine(s.penLastX, s.penLastY, s.cursor.X(), s.cursor.Y())
	s.penLastX = s.cursor.X()
	s.penLastY = s.cursor.Y()
	s.commitPenInk()
}

// endPenStroke finishes the current stroke. The ink stays on screen until the
// fade is triggered by releasing Super.
func (s *server) endPenStroke() {
	s.penDrawing = false
	s.penButtonDown = false
	s.cursor.SetXCursor(s.cursorMgr, "default")
}

// penHandleSuperRelease starts the hold-then-fade countdown when Super is
// released while ink is present. The stroke stops accumulating ink, but the
// left button (if still held) keeps its press owned by us so the eventual
// release is consumed rather than leaking to a client.
func (s *server) penHandleSuperRelease() {
	if !s.penInkActive {
		return
	}
	s.penDrawing = false
	s.schedulePenFade()
}

// ensurePenImage (re)allocates the ink image to span the full output layout.
// Returns false if there is no usable layout (no outputs).
func (s *server) ensurePenImage() bool {
	minX, minY, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return false
	}
	if s.penImg != nil && s.penImg.Bounds().Dx() == w && s.penImg.Bounds().Dy() == h &&
		s.penOriginX == minX && s.penOriginY == minY {
		return true
	}
	// Layout changed (or first use) — start with a fresh transparent canvas.
	s.discardPenScene()
	s.penImg = image.NewNRGBA(image.Rect(0, 0, w, h))
	s.penOriginX = minX
	s.penOriginY = minY
	return true
}

// stampPenDot paints a single round brush stamp at a layout coordinate.
func (s *server) stampPenDot(x, y float64) {
	if s.penImg == nil {
		return
	}
	drawFilledCircle(s.penImg, int(x)-s.penOriginX, int(y)-s.penOriginY, penBrushRadius, penColor)
}

// stampPenLine paints a continuous stroke between two layout coordinates by
// stamping overlapping brush dots roughly one pixel apart.
func (s *server) stampPenLine(x0, y0, x1, y1 float64) {
	if s.penImg == nil {
		return
	}
	dx := x1 - x0
	dy := y1 - y0
	dist := math.Hypot(dx, dy)
	steps := int(dist) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		drawFilledCircle(s.penImg,
			int(x0+dx*t)-s.penOriginX,
			int(y0+dy*t)-s.penOriginY,
			penBrushRadius, penColor)
	}
}

// commitPenInk uploads the ink image to its scene buffer (creating the buffer
// on first use), resets opacity to fully visible, enables the pen layer and
// schedules a redraw.
func (s *server) commitPenInk() {
	if s.penImg == nil || s.penTree == nil {
		return
	}
	w := s.penImg.Bounds().Dx()
	h := s.penImg.Bounds().Dy()
	pixels := unsafe.Pointer(&s.penImg.Pix[0])

	if s.penPixBuf != nil {
		pixBuf := (*C.struct_pen_buffer)(s.penPixBuf)
		C.pen_buffer_update(pixBuf, pixels, C.int(w), C.int(h))
		if s.penSceneBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(s.penSceneBuf)
			C.pen_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.pen_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
			C.pen_scene_buffer_set_opacity(sceneBuf, 1.0)
			C.pen_scene_node_set_position(&sceneBuf.node, C.int(s.penOriginX), C.int(s.penOriginY))
		}
	} else {
		pixBuf := C.pen_buffer_create(C.int(w), C.int(h))
		if pixBuf == nil {
			return
		}
		C.pen_buffer_update(pixBuf, pixels, C.int(w), C.int(h))
		penTreeC := (*C.struct_wlr_scene_tree)(s.penTree)
		sceneBuf := C.pen_scene_buffer_create(penTreeC, &pixBuf.base)
		if sceneBuf == nil {
			C.pen_buffer_destroy(&pixBuf.base)
			return
		}
		C.pen_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
		C.pen_scene_node_set_position(&sceneBuf.node, C.int(s.penOriginX), C.int(s.penOriginY))
		s.penPixBuf = unsafe.Pointer(pixBuf)
		s.penSceneBuf = unsafe.Pointer(sceneBuf)
	}

	C.pen_scene_node_set_enabled((*C.struct_wlr_scene_tree)(s.penTree), 1)
	s.scheduleAllOutputFrames()
}

// schedulePenFade (re)starts the hold timer. After penHoldDelay with no new
// strokes, the fade-out animation begins. The timer fires on a goroutine, so it
// bounces the work back onto the main thread via enqueueAction.
func (s *server) schedulePenFade() {
	if s.penFadeActive {
		return // already fading; let it finish
	}
	if s.penFadeTimer != nil {
		s.penFadeTimer.Stop()
	}
	s.penFadeTimer = time.AfterFunc(penHoldDelay, func() {
		s.enqueueAction(func() {
			s.penFadeTimer = nil
			s.beginPenFade()
		})
	})
}

// beginPenFade starts the fade-out animation (advanced by tickPenFade).
func (s *server) beginPenFade() {
	if !s.penInkActive || s.penDrawing {
		return
	}
	s.penFadeActive = true
	s.penFadeStart = time.Now()
	s.scheduleAllOutputFrames()
}

// cancelPenFade aborts a pending or in-progress fade and restores full opacity.
func (s *server) cancelPenFade() {
	if s.penFadeTimer != nil {
		s.penFadeTimer.Stop()
		s.penFadeTimer = nil
	}
	if s.penFadeActive {
		s.penFadeActive = false
		if s.penSceneBuf != nil {
			C.pen_scene_buffer_set_opacity((*C.struct_wlr_scene_buffer)(s.penSceneBuf), 1.0)
		}
	}
}

// tickPenFade advances the pen ink fade-out. Called from the frame callback.
// Returns true while the fade is still running (so frames keep scheduling).
func (s *server) tickPenFade() bool {
	if !s.penFadeActive {
		return false
	}
	elapsed := time.Since(s.penFadeStart)
	if elapsed >= penFadeDur {
		s.clearPenInk()
		return false
	}
	opacity := 1 - float32(elapsed)/float32(penFadeDur)
	if s.penSceneBuf != nil {
		C.pen_scene_buffer_set_opacity((*C.struct_wlr_scene_buffer)(s.penSceneBuf), C.float(opacity))
	}
	return true
}

// clearPenInk removes all ink and tears down the scene buffer.
func (s *server) clearPenInk() {
	s.discardPenScene()
	s.penImg = nil
	s.penInkActive = false
	s.penDrawing = false
	s.penButtonDown = false
	s.penFadeActive = false
}

// discardPenScene destroys the scene buffer node and hides the pen layer,
// leaving s.penImg untouched. The pixel buffer is freed by the scene node's
// destroy (it drops the last buffer reference).
func (s *server) discardPenScene() {
	if s.penSceneBuf != nil {
		C.pen_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(s.penSceneBuf).node)
		s.penSceneBuf = nil
		s.penPixBuf = nil // freed transitively with the scene buffer's last ref
	}
	if s.penTree != nil {
		C.pen_scene_node_set_enabled((*C.struct_wlr_scene_tree)(s.penTree), 0)
	}
}
