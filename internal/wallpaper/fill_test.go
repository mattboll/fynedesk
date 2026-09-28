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

func TestMatrixGlyphAtlas(t *testing.T) {
	atlas := MatrixGlyphAtlas()
	w := MatrixNumGlyphs * MatrixGlyphW
	if len(atlas) != w*MatrixGlyphH {
		t.Fatalf("atlas is %d bytes, want %d", len(atlas), w*MatrixGlyphH)
	}
	// The first glyph is a vertical line in its middle column (0x08).
	for row := 0; row < MatrixGlyphH; row++ {
		for col := 0; col < MatrixGlyphW; col++ {
			if on := atlas[row*w+col] == 0xff; on != matrixGlyphs[0][row][col] {
				t.Fatalf("glyph 0 at (%d,%d): atlas %v, glyph %v", col, row, on, !on)
			}
		}
	}
}

func TestMatrixAnimStep(t *testing.T) {
	var cpu, gpu MatrixAnim
	cpu.Init(140, 100, 7)
	gpu.Init(140, 100, 7)
	cols := make([]byte, gpu.Columns()*4)
	for range 50 {
		cpu.Tick(image.NewNRGBA(image.Rect(0, 0, 140, 100)))
		gpu.Step(cols)
	}
	// Both advance the columns alike.
	for i, col := range cpu.columns {
		y := int(cols[i*4])<<8 | int(cols[i*4+1])
		if y-32768 != int(col.headY) || int(cols[i*4+2]) != col.length || int(cols[i*4+3]) != col.glyphIdx {
			t.Fatalf("column %d: step %d/%d/%d, tick %d/%d/%d", i,
				y-32768, cols[i*4+2], cols[i*4+3], int(col.headY), col.length, col.glyphIdx)
		}
	}
}
