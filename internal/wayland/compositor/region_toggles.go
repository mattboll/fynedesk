package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"
*/
import "C"

import (
	"image"
	"image/color"
	"image/draw"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"fyshos.com/tyde/internal/wayland/wlr/xkb"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

// While the zone of a recording is chosen, a bar at the top of the screen
// shows what it will record: the defaults, which a click or a key turns on
// or off for this recording only.

// recordToggle is an option of a recording shown in the bar.
type recordToggle struct {
	key   xkb.KeySym
	label string // locale key
	opt   func(*wlipc.RecordingSettings) *bool
}

var recordToggles = []recordToggle{
	{'m', "record.microphone", func(r *wlipc.RecordingSettings) *bool { return &r.Microphone }},
	{'s', "record.systemAudio", func(r *wlipc.RecordingSettings) *bool { return &r.SystemAudio }},
	{'w', "record.webcam", func(r *wlipc.RecordingSettings) *bool { return &r.Webcam }},
	{'k', "record.showInput", func(r *wlipc.RecordingSettings) *bool { return &r.ShowInput }},
}

const (
	togglePad    = 10 // inside a pill
	toggleGap    = 8  // between pills
	toggleTop    = 24 // from the top of the screen
	toggleHintUp = 8  // between the pills and the hint
)

var (
	toggleOn   = color.NRGBA{R: 0xE5, G: 0x3B, B: 0x3B, A: 0xE6}
	toggleOff  = color.NRGBA{R: 0x26, G: 0x29, B: 0x2E, A: 0xE6}
	toggleText = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	toggleDim  = color.NRGBA{R: 0xB0, G: 0xB4, B: 0xBA, A: 0xFF}
)

// toggleBarImage draws the bar for opts, and returns where each toggle is
// in it.
func toggleBarImage(opts wlipc.RecordingSettings, face font.Face) (*image.NRGBA, []image.Rectangle) {
	m := face.Metrics()
	lineH := (m.Ascent + m.Descent).Ceil()
	pillH := lineH + 2*togglePad
	labels := make([]string, len(recordToggles))
	width := 0
	for i, t := range recordToggles {
		labels[i] = string(rune(t.key-'a'+'A')) + "  " + locale.T(t.label)
		width += font.MeasureString(face, labels[i]).Ceil() + 2*togglePad
	}
	width += toggleGap * (len(recordToggles) - 1)
	hint := locale.T("record.selectHint")
	hintW := font.MeasureString(face, hint).Ceil() + 2*togglePad
	totalW := max(width, hintW)
	img := image.NewNRGBA(image.Rect(0, 0, totalW, 2*pillH+toggleHintUp))

	rects := make([]image.Rectangle, len(recordToggles))
	x := (totalW - width) / 2
	for i, t := range recordToggles {
		w := font.MeasureString(face, labels[i]).Ceil() + 2*togglePad
		r := image.Rect(x, 0, x+w, pillH)
		bg, fg := toggleOff, toggleDim
		if *t.opt(&opts) {
			bg, fg = toggleOn, toggleText
		}
		draw.Draw(img, r, image.NewUniform(bg), image.Point{}, draw.Src)
		(&font.Drawer{
			Dst: img, Src: image.NewUniform(fg), Face: face,
			Dot: fixed.P(x+togglePad, togglePad+m.Ascent.Ceil()),
		}).DrawString(labels[i])
		rects[i] = r
		x += w + toggleGap
	}
	hr := image.Rect((totalW-hintW)/2, pillH+toggleHintUp, (totalW+hintW)/2, 2*pillH+toggleHintUp)
	draw.Draw(img, hr, image.NewUniform(toggleOff), image.Point{}, draw.Src)
	(&font.Drawer{
		Dst: img, Src: image.NewUniform(toggleText), Face: face,
		Dot: fixed.P(hr.Min.X+togglePad, hr.Min.Y+togglePad+m.Ascent.Ceil()),
	}).DrawString(hint)
	return img, rects
}

// showRecordToggles shows the bar of the recording about to be chosen,
// at the top of the screen under the pointer.
func (s *server) showRecordToggles() {
	if s.regionTree == nil || s.recordOverride == nil {
		return
	}
	face := getTitleFontFace()
	if face == nil {
		return
	}
	img, rects := toggleBarImage(*s.recordOverride, face)
	b := img.Bounds()
	if s.toggleBuf == nil {
		buf := C.pixel_buffer_create(C.int(b.Dx()), C.int(b.Dy()))
		if buf == nil {
			return
		}
		s.toggleBuf = unsafe.Pointer(buf)
		s.toggleNode = unsafe.Pointer(C.wlr_scene_buffer_create((*C.struct_wlr_scene_tree)(s.regionTree), &buf.base))
	}
	buf := (*C.struct_pixel_buffer)(s.toggleBuf)
	C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), C.int(b.Dx()), C.int(b.Dy()))
	C.wlr_scene_buffer_set_buffer((*C.struct_wlr_scene_buffer)(s.toggleNode), &buf.base)

	out := s.getActiveOutputGeo()
	minX, minY, _, _ := s.fullLayoutBounds()
	x, y := out.x+(out.width-b.Dx())/2, out.y+toggleTop
	C.wlr_scene_node_set_position(&(*C.struct_wlr_scene_buffer)(s.toggleNode).node, C.int(x-minX), C.int(y-minY))
	s.toggleRects = s.toggleRects[:0]
	for _, r := range rects {
		s.toggleRects = append(s.toggleRects, r.Add(image.Pt(x, y)))
	}
}

// flipRecordToggle turns option i of the recording on or off.
func (s *server) flipRecordToggle(i int) {
	if s.recordOverride == nil || i < 0 || i >= len(recordToggles) {
		return
	}
	opt := recordToggles[i].opt(s.recordOverride)
	*opt = !*opt
	s.showRecordToggles()
}

// recordToggleKey flips the toggle of sym, and reports whether there is one.
func (s *server) recordToggleKey(sym xkb.KeySym) bool {
	if s.recordOverride == nil {
		return false
	}
	for i, t := range recordToggles {
		if sym == t.key || sym == t.key-'a'+'A' {
			s.flipRecordToggle(i)
			return true
		}
	}
	return false
}

// recordToggleAt is the toggle at (x, y), layout coordinates, or -1.
func (s *server) recordToggleAt(x, y float64) int {
	p := image.Pt(int(x), int(y))
	for i, r := range s.toggleRects {
		if p.In(r) {
			return i
		}
	}
	return -1
}

// forgetRecordToggles drops the bar, its node going with the overlay.
func (s *server) forgetRecordToggles() {
	if s.toggleBuf != nil {
		C.pixel_buffer_release((*C.struct_pixel_buffer)(s.toggleBuf))
	}
	s.toggleBuf, s.toggleNode, s.toggleRects = nil, nil, nil
}
