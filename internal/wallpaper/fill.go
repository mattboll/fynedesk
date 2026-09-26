package wallpaper

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// How a wallpaper covers a screen whose shape differs from its own.
const (
	FillStretch = "Stretch" // covers it exactly, out of proportion (the default)
	FillFit     = "Fit"     // shows all of the image, centred, with bars
	FillFill    = "Fill"    // covers it, centred, the overflow cut off
)

// FillModes lists the fill modes in display order.
var FillModes = []string{FillStretch, FillFit, FillFill}

// Rect returns where an image of bounds src goes on a w×h screen for the
// given fill mode (any other mode stretches).
func Rect(src image.Rectangle, w, h int, fill string) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 {
		return image.Rect(0, 0, w, h)
	}

	var scale float64
	switch fill {
	case FillFit:
		scale = math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	case FillFill:
		scale = math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	default:
		return image.Rect(0, 0, w, h)
	}

	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	ox, oy := (w-dw)/2, (h-dh)/2
	return image.Rect(ox, oy, ox+dw, oy+dh)
}

// Draw paints dst with bg, then img scaled by scaler and placed on it
// according to the fill mode.
func Draw(dst draw.Image, img image.Image, fill string, bg color.Color, scaler draw.Scaler) {
	b := dst.Bounds()
	draw.Draw(dst, b, &image.Uniform{C: bg}, image.Point{}, draw.Src)
	r := Rect(img.Bounds(), b.Dx(), b.Dy(), fill).Add(b.Min)
	scaler.Scale(dst, r, img, img.Bounds(), draw.Over, nil)
}
