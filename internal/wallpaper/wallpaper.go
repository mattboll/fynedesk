// Package wallpaper implements wallpaper utilities including dynamic time-based wallpapers and accent color extraction.
package wallpaper

import (
	"image"
	"image/color"
	"math/rand"
)

// AnimatedWallpaper is a frame-driven animation that mutates an NRGBA buffer in-place.
type AnimatedWallpaper interface {
	Init(w, h int, seed int64)
	Tick(img *image.NRGBA)
}

// --- Decay lookup tables ---
// Pre-computed: decayLUT[factor][x] = x * factor / 256
// Matrix uses factor 217 (≈0.85), starfield uses factor 192 (≈0.75).
var matrixDecayLUT [256]uint8
var starDecayLUT [256]uint8

func init() {
	for i := range matrixDecayLUT {
		matrixDecayLUT[i] = uint8(uint16(i) * 217 >> 8)
		starDecayLUT[i] = uint8(uint16(i) * 192 >> 8)
	}
}

// --- Matrix rain effect ---

const (
	matrixGlyphW    = 7
	matrixGlyphH    = 14
	matrixNumGlyphs = 20

	// MatrixGlyphW and MatrixGlyphH are the exported glyph dimensions for use by other packages.
	MatrixGlyphW = matrixGlyphW
	MatrixGlyphH = matrixGlyphH
	// MatrixNumGlyphs is the total number of pre-computed glyph patterns.
	MatrixNumGlyphs = matrixNumGlyphs
)

// Pre-computed 7x14 1-bit glyph bitmaps (katakana-like geometric shapes)
var matrixGlyphs [matrixNumGlyphs][matrixGlyphH][matrixGlyphW]bool

func init() {
	// Generate simple geometric glyphs (lines, angles, dots)
	patterns := [matrixNumGlyphs][matrixGlyphH]uint8{
		// Vertical line
		{0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08},
		// Horizontal line
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Cross
		{0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x7F, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08},
		// Box
		{0x00, 0x00, 0x7F, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x7F, 0x00, 0x00},
		// Diagonal \
		{0x41, 0x22, 0x14, 0x08, 0x08, 0x14, 0x22, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Triangle up
		{0x00, 0x00, 0x08, 0x08, 0x14, 0x14, 0x22, 0x22, 0x41, 0x41, 0x7F, 0x00, 0x00, 0x00},
		// Triangle down
		{0x00, 0x00, 0x7F, 0x41, 0x41, 0x22, 0x22, 0x14, 0x14, 0x08, 0x08, 0x00, 0x00, 0x00},
		// T shape
		{0x00, 0x00, 0x7F, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x00, 0x00},
		// L shape
		{0x00, 0x00, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x7F, 0x00, 0x00},
		// Dots
		{0x00, 0x00, 0x22, 0x00, 0x22, 0x00, 0x22, 0x00, 0x22, 0x00, 0x22, 0x00, 0x00, 0x00},
		// Plus
		{0x00, 0x00, 0x00, 0x08, 0x08, 0x08, 0x3E, 0x08, 0x08, 0x08, 0x00, 0x00, 0x00, 0x00},
		// Arrow right
		{0x00, 0x00, 0x00, 0x08, 0x04, 0x02, 0x7F, 0x02, 0x04, 0x08, 0x00, 0x00, 0x00, 0x00},
		// Arrow down
		{0x00, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x49, 0x2A, 0x1C, 0x08, 0x00, 0x00, 0x00},
		// Diamond
		{0x00, 0x00, 0x08, 0x14, 0x22, 0x41, 0x22, 0x14, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Pipe fork
		{0x08, 0x08, 0x08, 0x08, 0x08, 0x0F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		// Corner
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0F, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08},
		// Zigzag
		{0x00, 0x00, 0x41, 0x22, 0x14, 0x08, 0x14, 0x22, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00},
		// H shape
		{0x00, 0x00, 0x41, 0x41, 0x41, 0x7F, 0x41, 0x41, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00},
		// U shape
		{0x00, 0x00, 0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0x22, 0x1C, 0x00, 0x00, 0x00, 0x00},
		// Equal
		{0x00, 0x00, 0x00, 0x00, 0x7F, 0x00, 0x00, 0x7F, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
	}
	for g := 0; g < matrixNumGlyphs; g++ {
		for row := 0; row < matrixGlyphH; row++ {
			bits := patterns[g][row]
			for col := 0; col < matrixGlyphW; col++ {
				matrixGlyphs[g][row][col] = (bits & (1 << uint(matrixGlyphW-1-col))) != 0
			}
		}
	}
}

type matrixColumn struct {
	headY    float64 // Current head position (in pixels)
	speed    float64 // Pixels per tick
	length   int     // Trail length in glyphs
	glyphIdx int     // Current glyph index (changes over time)
}

// MatrixAnim implements the matrix digital rain effect.
type MatrixAnim struct {
	w, h    int
	rng     *rand.Rand
	columns []matrixColumn
}

func (m *MatrixAnim) Init(w, h int, seed int64) {
	m.w = w
	m.h = h
	m.rng = rand.New(rand.NewSource(seed))

	numCols := w / matrixGlyphW
	m.columns = make([]matrixColumn, numCols)
	for i := range m.columns {
		m.columns[i] = matrixColumn{
			headY:    -float64(m.rng.Intn(h)), // Stagger start positions
			speed:    2.0 + m.rng.Float64()*6.0,
			length:   8 + m.rng.Intn(20),
			glyphIdx: m.rng.Intn(matrixNumGlyphs),
		}
	}
}

func (m *MatrixAnim) Tick(img *image.NRGBA) {
	w := m.w
	h := m.h
	pix := img.Pix
	stride := img.Stride

	// Decay existing pixels using LUT, 4 pixels (16 bytes) at a time.
	// Check all 4 pixels' RGB at once: if the entire group is black, skip.
	// LUT[0]==0, so decaying black pixels in a mixed group is harmless.
	n := len(pix)
	batch := n &^ 15 // round down to multiple of 16
	for i := 0; i < batch; i += 16 {
		if pix[i]|pix[i+1]|pix[i+2]|
			pix[i+4]|pix[i+5]|pix[i+6]|
			pix[i+8]|pix[i+9]|pix[i+10]|
			pix[i+12]|pix[i+13]|pix[i+14] == 0 {
			continue
		}
		pix[i] = matrixDecayLUT[pix[i]]
		pix[i+1] = matrixDecayLUT[pix[i+1]]
		pix[i+2] = matrixDecayLUT[pix[i+2]]
		pix[i+4] = matrixDecayLUT[pix[i+4]]
		pix[i+5] = matrixDecayLUT[pix[i+5]]
		pix[i+6] = matrixDecayLUT[pix[i+6]]
		pix[i+8] = matrixDecayLUT[pix[i+8]]
		pix[i+9] = matrixDecayLUT[pix[i+9]]
		pix[i+10] = matrixDecayLUT[pix[i+10]]
		pix[i+12] = matrixDecayLUT[pix[i+12]]
		pix[i+13] = matrixDecayLUT[pix[i+13]]
		pix[i+14] = matrixDecayLUT[pix[i+14]]
	}
	for i := batch; i < n; i += 4 {
		if pix[i]|pix[i+1]|pix[i+2] == 0 {
			continue
		}
		pix[i] = matrixDecayLUT[pix[i]]
		pix[i+1] = matrixDecayLUT[pix[i+1]]
		pix[i+2] = matrixDecayLUT[pix[i+2]]
	}

	for ci := range m.columns {
		col := &m.columns[ci]
		col.headY += col.speed
		col.glyphIdx = (col.glyphIdx + 1) % matrixNumGlyphs

		// Reset column when trail is fully off screen
		if int(col.headY)-col.length*matrixGlyphH > h {
			col.headY = -float64(m.rng.Intn(h / 2))
			col.speed = 2.0 + m.rng.Float64()*6.0
			col.length = 8 + m.rng.Intn(20)
			col.glyphIdx = m.rng.Intn(matrixNumGlyphs)
		}

		px := ci * matrixGlyphW
		if px+matrixGlyphW > w {
			continue
		}

		// Draw head glyph (bright white-green)
		headGlyphY := int(col.headY)
		DrawMatrixGlyph(pix, stride, px, headGlyphY, w, h,
			col.glyphIdx, color.NRGBA{R: 0xCC, G: 0xFF, B: 0xCC, A: 0xFF})

		// Draw body glyphs with fading green (integer math, no float64)
		for j := 1; j < col.length; j++ {
			gy := headGlyphY - j*matrixGlyphH
			if gy+matrixGlyphH <= 0 {
				break
			}
			// Fade: 0xCC (204) down to 0x33 (51), delta = 0x99 (153)
			g := uint8(204 - j*153/col.length)
			glyphI := (col.glyphIdx + j*3) % matrixNumGlyphs
			DrawMatrixGlyph(pix, stride, px, gy, w, h,
				glyphI, color.NRGBA{R: 0x00, G: g, B: 0x00, A: 0xFF})
		}
	}
}

// DrawMatrixGlyph renders a single matrix glyph at (px, py) onto an image pixel buffer.
func DrawMatrixGlyph(pix []uint8, stride, px, py, imgW, imgH, glyphIdx int, c color.NRGBA) {
	glyph := &matrixGlyphs[glyphIdx%matrixNumGlyphs]
	for row := 0; row < matrixGlyphH; row++ {
		y := py + row
		if y < 0 || y >= imgH {
			continue
		}
		rowOff := y * stride
		for col := 0; col < matrixGlyphW; col++ {
			if !glyph[row][col] {
				continue
			}
			x := px + col
			if x < 0 || x >= imgW {
				continue
			}
			off := rowOff + x*4
			pix[off] = c.R
			pix[off+1] = c.G
			pix[off+2] = c.B
			pix[off+3] = c.A
		}
	}
}

// --- Starfield fly-through effect ---

type star struct {
	x, y, z float64
}

// StarfieldAnim implements a starfield fly-through effect.
type StarfieldAnim struct {
	w, h    int
	rng     *rand.Rand
	stars   []star
	centerX float64
	centerY float64
	fov     float64
}

func (sf *StarfieldAnim) Init(w, h int, seed int64) {
	sf.w = w
	sf.h = h
	sf.rng = rand.New(rand.NewSource(seed))
	sf.centerX = float64(w) / 2
	sf.centerY = float64(h) / 2
	sf.fov = float64(w) / 2

	numStars := 600
	sf.stars = make([]star, numStars)
	for i := range sf.stars {
		sf.stars[i] = sf.randomStar(true)
	}
}

func (sf *StarfieldAnim) randomStar(randomZ bool) star {
	s := star{}
	if randomZ {
		s.z = sf.rng.Float64()*100 + 1
	} else {
		s.z = 80 + sf.rng.Float64()*20
	}
	s.x = (sf.rng.Float64()*2 - 1) * s.z
	s.y = (sf.rng.Float64()*2 - 1) * s.z
	return s
}

func (sf *StarfieldAnim) Tick(img *image.NRGBA) {
	w := sf.w
	h := sf.h
	pix := img.Pix
	stride := img.Stride

	// Fade existing pixels using LUT, 4 pixels at a time (same pattern as matrix).
	n := len(pix)
	batch := n &^ 15
	for i := 0; i < batch; i += 16 {
		if pix[i]|pix[i+1]|pix[i+2]|
			pix[i+4]|pix[i+5]|pix[i+6]|
			pix[i+8]|pix[i+9]|pix[i+10]|
			pix[i+12]|pix[i+13]|pix[i+14] == 0 {
			continue
		}
		pix[i] = starDecayLUT[pix[i]]
		pix[i+1] = starDecayLUT[pix[i+1]]
		pix[i+2] = starDecayLUT[pix[i+2]]
		pix[i+4] = starDecayLUT[pix[i+4]]
		pix[i+5] = starDecayLUT[pix[i+5]]
		pix[i+6] = starDecayLUT[pix[i+6]]
		pix[i+8] = starDecayLUT[pix[i+8]]
		pix[i+9] = starDecayLUT[pix[i+9]]
		pix[i+10] = starDecayLUT[pix[i+10]]
		pix[i+12] = starDecayLUT[pix[i+12]]
		pix[i+13] = starDecayLUT[pix[i+13]]
		pix[i+14] = starDecayLUT[pix[i+14]]
	}
	for i := batch; i < n; i += 4 {
		if pix[i]|pix[i+1]|pix[i+2] == 0 {
			continue
		}
		pix[i] = starDecayLUT[pix[i]]
		pix[i+1] = starDecayLUT[pix[i+1]]
		pix[i+2] = starDecayLUT[pix[i+2]]
	}

	for i := range sf.stars {
		s := &sf.stars[i]

		// Move star closer
		s.z -= 1.0

		// Reset if too close or off-screen
		if s.z < 0.5 {
			*s = sf.randomStar(false)
			continue
		}

		// Project to 2D
		sx := sf.centerX + s.x/s.z*sf.fov
		sy := sf.centerY + s.y/s.z*sf.fov

		ix := int(sx)
		iy := int(sy)

		if ix < 0 || ix >= w || iy < 0 || iy >= h {
			*s = sf.randomStar(false)
			continue
		}

		// Brightness inversely proportional to z (closer = brighter)
		brightness := 1.0 - s.z/100.0
		if brightness < 0 {
			brightness = 0
		}

		// Distant stars have blue tint, close stars are white
		var c color.NRGBA
		if s.z > 50 {
			// Far: subtle blue
			b := uint8(brightness * 180)
			c = color.NRGBA{R: uint8(float64(b) * 0.7), G: uint8(float64(b) * 0.8), B: b, A: 0xFF}
		} else {
			// Close: white
			b := uint8(brightness * 255)
			c = color.NRGBA{R: b, G: b, B: b, A: 0xFF}
		}

		// Draw star (size depends on distance: close = bigger)
		off := iy*stride + ix*4
		pix[off] = c.R
		pix[off+1] = c.G
		pix[off+2] = c.B
		pix[off+3] = c.A

		// Draw larger dot for close stars
		if s.z < 30 {
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx := ix + dx
					ny := iy + dy
					if nx >= 0 && nx < w && ny >= 0 && ny < h {
						off2 := ny*stride + nx*4
						pix[off2] = c.R
						pix[off2+1] = c.G
						pix[off2+2] = c.B
						pix[off2+3] = c.A
					}
				}
			}
		}
		if s.z < 10 {
			// Even larger for very close stars
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					if dx*dx+dy*dy > 5 {
						continue
					}
					nx := ix + dx
					ny := iy + dy
					if nx >= 0 && nx < w && ny >= 0 && ny < h {
						off2 := ny*stride + nx*4
						pix[off2] = c.R
						pix[off2+1] = c.G
						pix[off2+2] = c.B
						pix[off2+3] = c.A
					}
				}
			}
		}
	}
}
