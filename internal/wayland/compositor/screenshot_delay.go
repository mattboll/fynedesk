package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The countdown lets the clicks through to what is below it.
static bool countdown_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
	return false;
}

static struct wlr_scene_buffer *countdown_create(struct wlr_scene_tree *parent, struct pixel_buffer *buf) {
	struct wlr_scene_buffer *node = wlr_scene_buffer_create(parent, &buf->base);
	if (node) {
		node->point_accepts_input = countdown_no_input;
	}
	return node;
}
*/
import "C"

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// A delayed screenshot counts down beside the pointer, then captures the
// screen under it: time to open a menu or hover what must show.
const (
	screenshotDelay   = 3 // seconds
	countdownSize     = 72
	countdownFontSize = 34
)

// countdownImage draws n in a dark disc.
func countdownImage(n int, face font.Face) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, countdownSize, countdownSize))
	r := float64(countdownSize) / 2
	for y := 0; y < countdownSize; y++ {
		for x := 0; x < countdownSize; x++ {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			a := min(max(r-d, 0), 1) * 0.8
			img.SetNRGBA(x, y, color.NRGBA{R: 0x10, G: 0x14, B: 0x1A, A: uint8(255 * a)})
		}
	}
	if face == nil {
		return img
	}
	text := strconv.Itoa(n)
	m := face.Metrics()
	w := font.MeasureString(face, text)
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.I(countdownSize/2) - w/2,
			Y: fixed.I(countdownSize/2) + (m.Ascent-m.Descent)/2,
		},
	}
	d.DrawString(text)
	return img
}

// startDelayedScreenshot counts down beside the pointer, then captures the
// screen under it.
func (s *server) startDelayedScreenshot() {
	if s.countdownNode != nil {
		return // already counting
	}
	face := fontFaceOfSize(countdownFontSize)
	img := countdownImage(screenshotDelay, face)
	buf := C.pixel_buffer_create(countdownSize, countdownSize)
	if buf == nil {
		return
	}
	C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), countdownSize, countdownSize)
	node := C.countdown_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree), buf)
	if node == nil {
		C.pixel_buffer_release(buf)
		return
	}
	s.countdownNode, s.countdownBuf = unsafe.Pointer(node), unsafe.Pointer(buf)
	s.moveCountdown()
	s.countDown(screenshotDelay-1, face)
}

// countDown shows n a second from now, or captures when n is 0.
func (s *server) countDown(n int, face font.Face) {
	time.AfterFunc(time.Second, func() {
		_ = s.enqueueAction(func() {
			if s.countdownNode == nil {
				return
			}
			if n == 0 {
				s.dropCountdown()
				// The countdown must be gone from the screen first.
				time.AfterFunc(150*time.Millisecond, func() {
					_ = s.enqueueAction(func() { s.captureScreen(grimGeometry(s.getActiveOutputGeo())) })
				})
				return
			}
			img := countdownImage(n, face)
			buf := (*C.struct_pixel_buffer)(s.countdownBuf)
			C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), countdownSize, countdownSize)
			C.wlr_scene_buffer_set_buffer((*C.struct_wlr_scene_buffer)(s.countdownNode), &buf.base)
			s.countDown(n-1, face)
		})
	})
}

// moveCountdown keeps the countdown beside the pointer, below and right
// of it, clear of what it points at.
func (s *server) moveCountdown() {
	if s.countdownNode == nil {
		return
	}
	node := &(*C.struct_wlr_scene_buffer)(s.countdownNode).node
	C.wlr_scene_node_set_position(node, C.int(s.cursor.X())+24, C.int(s.cursor.Y())+24)
}

// dropCountdown takes the countdown away.
func (s *server) dropCountdown() {
	if s.countdownNode == nil {
		return
	}
	C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(s.countdownNode).node)
	C.pixel_buffer_release((*C.struct_pixel_buffer)(s.countdownBuf))
	s.countdownNode, s.countdownBuf = nil, nil
}
