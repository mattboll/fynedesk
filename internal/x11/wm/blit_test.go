//go:build linux || openbsd || freebsd || netbsd

package wm

import (
	"image"
	"image/color"
	"testing"

	"github.com/BurntSushi/xgbutil/xgraphics"
)

func TestBlitToRoot(t *testing.T) {
	root := &xgraphics.Image{Rect: image.Rect(0, 0, 4, 2), Stride: 16, Pix: make([]uint8, 32)}
	src := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			src.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	blitToRoot(root, src, 2, 1) // only its top-left 2×1 fits
	if got := root.Pix[root.PixOffset(2, 1):][:4]; got[0] != 30 || got[1] != 20 || got[2] != 10 || got[3] != 255 {
		t.Errorf("pixel (2,1) = %v, want BGRA 30 20 10 255", got)
	}
	if got := root.Pix[root.PixOffset(1, 1)]; got != 0 {
		t.Error("a pixel left of the image was drawn")
	}
}
