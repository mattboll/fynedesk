package wallpaper

import (
	"image"
	"image/color"
	"testing"
)

func TestExtractAccentColor_SolidColor(t *testing.T) {
	// Create a 10x10 solid red image
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	red := color.NRGBA{R: 200, G: 30, B: 30, A: 255}
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.SetNRGBA(x, y, red)
		}
	}

	result := ExtractAccentColor(img)
	if result.A != 255 {
		t.Errorf("alpha = %d, want 255", result.A)
	}
	// Result should be reddish (R dominant)
	if result.R < 100 {
		t.Errorf("expected reddish accent, got R=%d G=%d B=%d", result.R, result.G, result.B)
	}
}

func TestExtractAccentColor_EmptyImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	result := ExtractAccentColor(img)
	// Should return fallback blue
	expected := color.NRGBA{R: 0x21, G: 0x96, B: 0xf3, A: 0xff}
	if result != expected {
		t.Errorf("empty image: got %v, want fallback %v", result, expected)
	}
}

func TestExtractAccentColor_SinglePixel(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 200, B: 0, A: 255})

	result := ExtractAccentColor(img)
	if result.A != 255 {
		t.Errorf("alpha = %d, want 255", result.A)
	}
}

func TestColorToHex(t *testing.T) {
	tests := []struct {
		c    color.NRGBA
		want string
	}{
		{color.NRGBA{R: 0, G: 0, B: 0, A: 255}, "#000000"},
		{color.NRGBA{R: 255, G: 255, B: 255, A: 255}, "#ffffff"},
		{color.NRGBA{R: 0x21, G: 0x96, B: 0xf3, A: 255}, "#2196f3"},
	}
	for _, tt := range tests {
		got := ColorToHex(tt.c)
		if got != tt.want {
			t.Errorf("ColorToHex(%v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestRgbToHSL_Roundtrip(t *testing.T) {
	// Test that RGB -> HSL -> RGB roundtrips reasonably
	tests := []struct {
		r, g, b uint8
	}{
		{255, 0, 0},     // red
		{0, 255, 0},     // green
		{0, 0, 255},     // blue
		{128, 128, 128}, // gray
		{0, 0, 0},       // black
		{255, 255, 255}, // white
	}
	for _, tt := range tests {
		h, s, l := rgbToHSL(tt.r, tt.g, tt.b)
		r2, g2, b2 := hslToRGB(h, s, l)
		// Allow ±1 for rounding
		if diff(r2, tt.r) > 1 || diff(g2, tt.g) > 1 || diff(b2, tt.b) > 1 {
			t.Errorf("roundtrip(%d,%d,%d): got (%d,%d,%d)", tt.r, tt.g, tt.b, r2, g2, b2)
		}
	}
}

func diff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func TestSamplePixels_TransparentSkipped(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 0}) // transparent
	img.SetNRGBA(0, 1, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 0, G: 0, B: 0, A: 0}) // transparent

	pixels := samplePixels(img, 64)
	if len(pixels) != 2 {
		t.Errorf("got %d pixels, want 2 (transparent should be skipped)", len(pixels))
	}
}
