package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"
*/
import "C"

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The size of the zone being selected is shown next to its bottom-right
// corner, in a dark label.
const (
	regionSizePad    = 6 // around the text
	regionSizeMargin = 6 // from the zone
)

// regionSizeImage draws the label showing text.
func regionSizeImage(text string) *image.NRGBA {
	face := getTitleFontFace()
	if face == nil {
		return nil
	}
	m := face.Metrics()
	textW := font.MeasureString(face, text).Ceil()
	textH := (m.Ascent + m.Descent).Ceil()
	img := image.NewNRGBA(image.Rect(0, 0, textW+2*regionSizePad, textH+2*regionSizePad))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{A: 0xC8}), image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}),
		Face: face,
		Dot:  fixed.P(regionSizePad, regionSizePad+m.Ascent.Ceil()),
	}
	d.DrawString(text)
	return img
}

// showRegionSize shows w × h next to the corner (x2, y2) of the zone, in
// overlay coordinates, inside the totalW × totalH overlay.
func (s *server) showRegionSize(w, h, x2, y2, totalW, totalH int) {
	if s.regionTree == nil {
		return
	}
	text := fmt.Sprintf("%d × %d", w, h)
	if text != s.regionSizeText {
		img := regionSizeImage(text)
		if img == nil {
			return
		}
		b := img.Bounds()
		if s.regionSizeBuf == nil {
			buf := C.pixel_buffer_create(C.int(b.Dx()), C.int(b.Dy()))
			if buf == nil {
				return
			}
			s.regionSizeBuf = unsafe.Pointer(buf)
			s.regionSizeNode = unsafe.Pointer(C.wlr_scene_buffer_create((*C.struct_wlr_scene_tree)(s.regionTree), &buf.base))
		}
		buf := (*C.struct_pixel_buffer)(s.regionSizeBuf)
		C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), C.int(b.Dx()), C.int(b.Dy()))
		C.wlr_scene_buffer_set_buffer((*C.struct_wlr_scene_buffer)(s.regionSizeNode), &buf.base)
		s.regionSizeText, s.regionSizeW, s.regionSizeH = text, b.Dx(), b.Dy()
	}
	if s.regionSizeNode == nil {
		return
	}
	// Below the corner, right-aligned with it; inside the zone near the
	// bottom of the screens.
	x := max(x2-s.regionSizeW, 0)
	y := y2 + regionSizeMargin
	if y+s.regionSizeH > totalH {
		y = max(y2-s.regionSizeH-regionSizeMargin, 0)
	}
	x = min(x, max(totalW-s.regionSizeW, 0))
	node := &(*C.struct_wlr_scene_buffer)(s.regionSizeNode).node
	C.wlr_scene_node_set_position(node, C.int(x), C.int(y))
	C.wlr_scene_node_set_enabled(node, true)
}

// hideRegionSize hides the size label (no zone to measure).
func (s *server) hideRegionSize() {
	if s.regionSizeNode != nil {
		C.wlr_scene_node_set_enabled(&(*C.struct_wlr_scene_buffer)(s.regionSizeNode).node, false)
	}
}

// forgetRegionSize drops the label, its node going with the overlay.
func (s *server) forgetRegionSize() {
	if s.regionSizeBuf != nil {
		C.pixel_buffer_release((*C.struct_pixel_buffer)(s.regionSizeBuf))
	}
	s.regionSizeBuf, s.regionSizeNode, s.regionSizeText = nil, nil, ""
}
