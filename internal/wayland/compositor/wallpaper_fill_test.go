package compositor

import (
	"image"
	"image/color"
	"testing"
)

func TestWallpaperRect(t *testing.T) {
	src := image.Rect(0, 0, 200, 100) // 2:1 on a 4:3 output
	for fill, want := range map[string]image.Rectangle{
		"":        image.Rect(0, 0, 400, 300),
		"Stretch": image.Rect(0, 0, 400, 300),
		"Fit":     image.Rect(0, 50, 400, 250),
		"Fill":    image.Rect(-100, 0, 500, 300),
	} {
		if got := wallpaperRect(src, 400, 300, fill); got != want {
			t.Errorf("%q: got %v, want %v", fill, got, want)
		}
	}
}

func TestRenderWallpaperImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	for i := range src.Pix {
		src.Pix[i] = 0xff // white
	}
	bg := wallpaperBackgroundColor("#102030")

	out := renderWallpaperImage(src, 40, 30, "Fit", bg)
	if got := out.NRGBAAt(20, 2); got != bg {
		t.Errorf("letterbox = %v, want the background %v", got, bg)
	}
	if got := out.NRGBAAt(20, 15); got != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Errorf("centre = %v, want the image", got)
	}
}
