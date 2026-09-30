package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The red disc lets the clicks through to what is below it.
static bool cursor_alert_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
	return false;
}

static struct wlr_scene_buffer *cursor_alert_create(struct wlr_scene_tree *parent,
		struct pixel_buffer *buf, int size) {
	struct wlr_scene_buffer *disc = wlr_scene_buffer_create(parent, &buf->base);
	if (disc) {
		disc->point_accepts_input = cursor_alert_no_input;
		wlr_scene_buffer_set_dest_size(disc, size, size);
	}
	return disc;
}
*/
import "C"

import (
	"image"
	"image/color"
	"math"
	"unsafe"
)

// The red cursor: a red disc under the pointer, on every screen, while
// something waits for the user (Slack's red dot, see wlipc.RequestCursorAlert).
const (
	cursorAlertSize  = 44 // diameter, in layout pixels
	cursorAlertScale = 2  // drawn at twice the size, sharp on HiDPI screens
)

var cursorAlertColor = color.NRGBA{R: 0xFF, G: 0x2D, B: 0x2D, A: 0xFF}

// cursorAlertImage draws the disc: a translucent red fill ringed with
// solid red, its edges smoothed.
func cursorAlertImage() *image.NRGBA {
	n := cursorAlertSize * cursorAlertScale
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	outer := float64(n) / 2
	ring := 3.0 * cursorAlertScale
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)+0.5-outer, float64(y)+0.5-outer)
			a := 0.35 // the fill
			if d > outer-ring-1 {
				a = 0.95 // the ring
			}
			a *= min(max(outer-d, 0), 1) // smoothed outer edge
			c := cursorAlertColor
			c.A = uint8(255 * a)
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// setCursorAlert shows or takes away the red disc under the pointer.
func (s *server) setCursorAlert(on bool) {
	if on == (s.cursorAlert != nil) {
		return
	}
	if !on {
		C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(s.cursorAlert).node)
		C.pixel_buffer_release((*C.struct_pixel_buffer)(s.cursorAlertBuf))
		s.cursorAlert, s.cursorAlertBuf = nil, nil
		return
	}
	img := cursorAlertImage()
	n := img.Bounds().Dx()
	buf := C.pixel_buffer_create(C.int(n), C.int(n))
	if buf == nil {
		return
	}
	C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), C.int(n), C.int(n))
	disc := C.cursor_alert_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree), buf, cursorAlertSize)
	if disc == nil {
		C.pixel_buffer_release(buf)
		return
	}
	s.cursorAlert, s.cursorAlertBuf = unsafe.Pointer(disc), unsafe.Pointer(buf)
	s.moveCursorAlert()
}

// moveCursorAlert keeps the red disc centred on the pointer.
func (s *server) moveCursorAlert() {
	if s.cursorAlert == nil {
		return
	}
	disc := (*C.struct_wlr_scene_buffer)(s.cursorAlert)
	C.wlr_scene_node_set_position(&disc.node,
		C.int(math.Round(s.cursor.X()))-cursorAlertSize/2, C.int(math.Round(s.cursor.Y()))-cursorAlertSize/2)
}
