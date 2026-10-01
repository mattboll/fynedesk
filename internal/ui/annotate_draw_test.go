package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

var annotRed = color.NRGBA{R: 0xE5, G: 0x1A, B: 0x1A, A: 0xFF}

// checker is a capture to annotate: black and white squares.
func checker(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(0)
			if (x/4+y/4)%2 == 0 {
				v = 0xFF
			}
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 0xFF})
		}
	}
	return img
}

func TestRenderAnnotations(t *testing.T) {
	base := checker(600, 400)
	before := append([]byte(nil), base.Pix...)
	shapes := []annotShape{
		{tool: toolArrow, from: image.Pt(50, 50), to: image.Pt(300, 200), color: annotRed},
		{tool: toolRect, from: image.Pt(350, 50), to: image.Pt(550, 150), color: annotRed},
		{tool: toolBlur, from: image.Pt(350, 250), to: image.Pt(550, 350)},
		{tool: toolText, from: image.Pt(40, 300), text: "Ici", color: annotRed},
	}
	out := renderAnnotations(base, shapes, annotFace(30))
	if string(base.Pix) != string(before) {
		t.Fatal("the capture itself was changed")
	}

	if c := out.NRGBAAt(175, 125); c != annotRed { // on the arrow's line
		t.Errorf("arrow line: %v", c)
	}
	if c := out.NRGBAAt(296, 198); c != annotRed { // near its tip
		t.Errorf("arrow head: %v", c)
	}
	if c := out.NRGBAAt(450, 51); c != annotRed { // the frame's top side
		t.Errorf("frame: %v", c)
	}
	if out.NRGBAAt(450, 100) != base.NRGBAAt(450, 100) { // inside the frame
		t.Error("the inside of the frame changed")
	}
	// The blurred zone: blocks of one colour (grey, the mean of the
	// checker), not the checker any more.
	if c := out.NRGBAAt(360, 260); c.R < 0x60 || c.R > 0xA0 || out.NRGBAAt(361, 260) != c {
		t.Errorf("blur: %v %v", c, out.NRGBAAt(361, 260))
	}
	if f, err := os.Create(os.Getenv("ANNOT_SAMPLE")); err == nil { // to look at it
		png.Encode(f, out)
		f.Close()
	}
}
