package compositor

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// wallpaperBackgroundColor parses the colour drawn around the wallpaper,
// opaque black by default.
func wallpaperBackgroundColor(hex string) color.NRGBA {
	if c := parseHexColor(hex); c != nil {
		c.A = 0xff // the output has nothing behind it to blend with
		return *c
	}
	return color.NRGBA{A: 0xff}
}

// wallpaperRect returns where an image of bounds src goes on a w×h output
// for the given fill mode: "Stretch" (the default) covers the output exactly,
// "Fit" shows the whole image and "Fill" covers the output, both keeping the
// aspect ratio and centred.
func wallpaperRect(src image.Rectangle, w, h int, fill string) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 {
		return image.Rect(0, 0, w, h)
	}

	var scale float64
	switch fill {
	case "Fit":
		scale = math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	case "Fill":
		scale = math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	default: // Stretch
		return image.Rect(0, 0, w, h)
	}

	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	ox, oy := (w-dw)/2, (h-dh)/2
	return image.Rect(ox, oy, ox+dw, oy+dh)
}

// renderWallpaperImage scales img onto a w×h image filled with bg, placed
// according to the fill mode.
func renderWallpaperImage(img image.Image, w, h int, fill string, bg color.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(out, wallpaperRect(img.Bounds(), w, h, fill), img, img.Bounds(), draw.Over, nil)
	return out
}
