package compositor

import (
	"image/color"
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
