package wallpaper

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/draw"
)

func TestRect(t *testing.T) {
	src := image.Rect(0, 0, 200, 100) // 2:1 on a 4:3 screen
	for fill, want := range map[string]image.Rectangle{
		"":          image.Rect(0, 0, 400, 300),
		FillStretch: image.Rect(0, 0, 400, 300),
		FillFit:     image.Rect(0, 50, 400, 250),
		FillFill:    image.Rect(-100, 0, 500, 300),
	} {
		if got := Rect(src, 400, 300, fill); got != want {
			t.Errorf("%q: got %v, want %v", fill, got, want)
		}
	}
}

func TestDraw(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	for i := range src.Pix {
		src.Pix[i] = 0xff // white
	}
	bg := color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}

	out := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	Draw(out, src, FillFit, bg, draw.ApproxBiLinear)
	if got := out.NRGBAAt(20, 2); got != bg {
		t.Errorf("letterbox = %v, want the background %v", got, bg)
	}
	if got := out.NRGBAAt(20, 15); got != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("centre = %v, want the image", got)
	}
}
