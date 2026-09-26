package compositor

import (
	"image"
	"image/color"
	"testing"
)

// TestBandShiftAlternates checks that the shifted bands go one way, then the
// other.
func TestBandShiftAlternates(t *testing.T) {
	const w, h = 64, 96 // bands of 8 rows: 0-7 shifted, 16-23 shifted...
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		img.SetNRGBA(w/2, y, color.NRGBA{255, 255, 255, 255}) // a vertical line
	}
	applyBandShift(img, 5)
	lineX := func(y int) int {
		for x := 0; x < w; x++ {
			if img.NRGBAAt(x, y).R == 255 {
				return x
			}
		}
		return -1
	}
	first, second := lineX(2), lineX(18)
	if first == w/2 || second == w/2 || (first-w/2)*(second-w/2) >= 0 {
		t.Errorf("band 0 moved the line to %d, band 2 to %d (from %d): not in opposite directions", first, second, w/2)
	}
}
