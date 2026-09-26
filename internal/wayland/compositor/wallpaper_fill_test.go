package compositor

import (
	"image/color"
	"testing"
)

func TestWallpaperBackgroundColor(t *testing.T) {
	if got, want := wallpaperBackgroundColor("#10203080"), (color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}); got != want {
		t.Errorf("got %v, want %v: the output has nothing to blend with", got, want)
	}
	if got, want := wallpaperBackgroundColor("nonsense"), (color.NRGBA{A: 0xff}); got != want {
		t.Errorf("got %v, want opaque black", got)
	}
}
