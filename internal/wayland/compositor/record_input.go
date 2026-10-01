package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The clicks and keys shown let the clicks through to what is below them.
static bool record_input_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
	return false;
}

static struct wlr_scene_buffer *record_input_create(struct wlr_scene_tree *parent, struct pixel_buffer *buf) {
	struct wlr_scene_buffer *node = wlr_scene_buffer_create(parent, &buf->base);
	if (node) {
		node->point_accepts_input = record_input_no_input;
	}
	return node;
}
*/
import "C"

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/internal/wayland/wlr/xkb"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

// While recording with the input shown, a ring spreads from each click
// and the key combinations show in a bubble at the bottom of the zone, so
// that they are in the video. Characters typed alone never show: no
// password or message ends up in a video.
const (
	rippleSize     = 64 // the ring's picture, in pixels
	rippleDuration = 420 * time.Millisecond
	rippleStep     = 30 * time.Millisecond
	keyBubbleTime  = 1500 * time.Millisecond
	keyBubbleFont  = 20
	keyBubblePad   = 12
	keyBubbleBelow = 28 // from the bottom of the zone
)

// showingInput reports whether the clicks and keys are shown now.
func (s *server) showingInput() bool {
	return s.rec != nil && s.rec.opts.ShowInput && s.rec.state == wlipc.RecordingActive
}

// rippleImage draws the ring a click spreads.
func rippleImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, rippleSize, rippleSize))
	r := float64(rippleSize) / 2
	for y := 0; y < rippleSize; y++ {
		for x := 0; x < rippleSize; x++ {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			a := min(max(r-d, 0), 1) * min(max(d-(r-5), 0), 1) // a 5-pixel ring
			img.SetNRGBA(x, y, color.NRGBA{R: 0xE5, G: 0x3B, B: 0x3B, A: uint8(255 * a)})
		}
	}
	return img
}

// showClick spreads a ring from the pointer, if it is in the recorded zone.
func (s *server) showClick() {
	if !s.showingInput() {
		return
	}
	x, y := s.cursor.X(), s.cursor.Y()
	z := s.rec.zone
	if int(x) < z.x || int(y) < z.y || int(x) >= z.x+z.w || int(y) >= z.y+z.h {
		return
	}
	if s.rippleBuf == nil {
		img := rippleImage()
		buf := C.pixel_buffer_create(rippleSize, rippleSize)
		if buf == nil {
			return
		}
		C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), rippleSize, rippleSize)
		s.rippleBuf = unsafe.Pointer(buf) // kept: every ring shows it
	}
	node := C.record_input_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree), (*C.struct_pixel_buffer)(s.rippleBuf))
	if node == nil {
		return
	}
	s.spreadRipple(node, x, y, time.Now())
}

// spreadRipple draws the ring at its size for now, then again a step later,
// until it has faded.
func (s *server) spreadRipple(node *C.struct_wlr_scene_buffer, x, y float64, start time.Time) {
	p := float64(time.Since(start)) / float64(rippleDuration)
	if p >= 1 {
		C.wlr_scene_node_destroy(&node.node)
		return
	}
	size := 14 + 46*p
	C.wlr_scene_buffer_set_dest_size(node, C.int(size), C.int(size))
	C.wlr_scene_buffer_set_opacity(node, C.float(1-p))
	C.wlr_scene_node_set_position(&node.node, C.int(x-size/2), C.int(y-size/2))
	time.AfterFunc(rippleStep, func() {
		_ = s.enqueueAction(func() { s.spreadRipple(node, x, y, start) })
	})
}

// specialKeys are the keys shown even without Ctrl, Alt or Super, by the
// locale key of their name.
var specialKeys = map[string]string{
	"Return": "keys.enter", "KP_Enter": "keys.enter", "Escape": "keys.escape", "Tab": "keys.tab",
	"ISO_Left_Tab": "keys.tab", "BackSpace": "keys.backspace", "Delete": "keys.delete",
	"Insert": "keys.insert", "Home": "keys.home", "End": "keys.end",
	"Prior": "keys.pageUp", "Next": "keys.pageDown", "Print": "keys.print",
	"Up": "↑", "Down": "↓", "Left": "←", "Right": "→",
}

// keyLabel is what shows for sym pressed with mods: a combination with
// Ctrl, Alt or Super, or a special key; false for a character typed alone,
// and for a modifier itself.
func keyLabel(mods wlr.KeyboardModifier, sym xkb.KeySym) (string, bool) {
	name := sym.Name()
	if name == "" || isModifierName(name) {
		return "", false
	}
	label := ""
	special := false
	switch {
	case specialKeys[name] != "":
		label, special = specialKeys[name], true
		if strings.HasPrefix(label, "keys.") {
			label = locale.T(label)
		}
	case len(name) > 1 && name[0] == 'F' && strings.Trim(name[1:], "0123456789") == "":
		label, special = name, true // F1…F24
	case name == "space":
		label = locale.T("keys.space")
	default:
		r := sym.Rune()
		if r == 0 || !unicode.IsPrint(r) {
			return "", false
		}
		label = strings.ToUpper(string(r))
	}
	combo := mods&(wlr.KeyboardModifierCtrl|wlr.KeyboardModifierAlt|wlr.KeyboardModifierLogo) != 0
	if !combo && !special {
		return "", false // a character typed: never shown
	}
	var parts []string
	for _, m := range []struct {
		mod wlr.KeyboardModifier
		key string
	}{
		{wlr.KeyboardModifierCtrl, "keys.ctrl"},
		{wlr.KeyboardModifierAlt, "keys.alt"},
		{wlr.KeyboardModifierLogo, "keys.super"},
		{wlr.KeyboardModifierShift, "keys.shift"},
	} {
		if mods&m.mod != 0 {
			parts = append(parts, locale.T(m.key))
		}
	}
	return strings.Join(append(parts, label), " + "), true
}

// isModifierName reports whether a keysym name is a modifier key's.
func isModifierName(name string) bool {
	for _, p := range []string{"Shift_", "Control_", "Alt_", "Super_", "Meta_", "Hyper_", "ISO_Level3", "ISO_Level5", "Caps_Lock", "Num_Lock"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// showKeys shows the combination of syms pressed with mods, if there is one
// to show.
func (s *server) showKeys(mods wlr.KeyboardModifier, syms []xkb.KeySym) {
	if !s.showingInput() {
		return
	}
	for _, sym := range syms {
		if label, ok := keyLabel(mods, sym); ok {
			s.showKeyBubble(label)
			return
		}
	}
}

// keyBubbleImage draws label in a dark bubble.
func keyBubbleImage(label string, face font.Face) *image.NRGBA {
	m := face.Metrics()
	w := font.MeasureString(face, label).Ceil() + 2*keyBubblePad
	h := (m.Ascent + m.Descent).Ceil() + 2*keyBubblePad
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{R: 0x10, G: 0x14, B: 0x1A, A: 0xE0}), image.Point{}, draw.Src)
	(&font.Drawer{
		Dst: img, Src: image.NewUniform(color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}), Face: face,
		Dot: fixed.P(keyBubblePad, keyBubblePad+m.Ascent.Ceil()),
	}).DrawString(label)
	return img
}

// showKeyBubble shows label at the bottom of the zone for a moment; a new
// one takes its place.
func (s *server) showKeyBubble(label string) {
	if s.keyFace == nil {
		s.keyFace = fontFaceOfSize(keyBubbleFont)
		if s.keyFace == nil {
			return
		}
	}
	img := keyBubbleImage(label, s.keyFace)
	b := img.Bounds()
	s.dropKeyBubble()
	buf := C.pixel_buffer_create(C.int(b.Dx()), C.int(b.Dy()))
	if buf == nil {
		return
	}
	C.pixel_buffer_update(buf, unsafe.Pointer(&img.Pix[0]), C.int(b.Dx()), C.int(b.Dy()))
	node := C.record_input_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree), buf)
	if node == nil {
		C.pixel_buffer_release(buf)
		return
	}
	z := s.rec.zone
	C.wlr_scene_node_set_position(&node.node, C.int(z.x+(z.w-b.Dx())/2), C.int(z.y+z.h-b.Dy()-keyBubbleBelow))
	s.keyBubble, s.keyBubbleBuf = unsafe.Pointer(node), unsafe.Pointer(buf)
	s.keyBubbleGen++
	gen := s.keyBubbleGen
	time.AfterFunc(keyBubbleTime, func() {
		_ = s.enqueueAction(func() {
			if s.keyBubbleGen == gen {
				s.dropKeyBubble()
			}
		})
	})
}

// dropKeyBubble takes the key bubble away.
func (s *server) dropKeyBubble() {
	if s.keyBubble == nil {
		return
	}
	C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(s.keyBubble).node)
	C.pixel_buffer_release((*C.struct_pixel_buffer)(s.keyBubbleBuf))
	s.keyBubble, s.keyBubbleBuf = nil, nil
}
