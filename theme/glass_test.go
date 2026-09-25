package theme

import (
	"image/color"
	"testing"
)

func TestGlassy(t *testing.T) {
	opaque := color.NRGBA{R: 10, G: 20, B: 30, A: 0xff}
	faint := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x28}

	SetGlass(false)
	if got := Glassy(opaque); got != opaque {
		t.Errorf("without glass: got %v, want %v", got, opaque)
	}

	SetGlass(true)
	defer SetGlass(false)
	if got := Glassy(opaque); got != (color.NRGBA{R: 10, G: 20, B: 30, A: GlassAlpha}) {
		t.Errorf("opaque with glass: got %v", got)
	}
	if got := Glassy(faint); got != faint {
		t.Errorf("a fainter colour is kept: got %v, want %v", got, faint)
	}
}
