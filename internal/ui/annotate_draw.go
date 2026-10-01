package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"fyne.io/fyne/v2/theme"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// annotTool is what a drag (or a tap, for text) draws on a capture.
type annotTool int

const (
	toolArrow annotTool = iota
	toolRect
	toolText
	toolBlur // pixelates, to hide what must not be shared
)

// annotShape is one annotation, in the capture's pixels.
type annotShape struct {
	tool     annotTool
	from, to image.Point
	color    color.NRGBA
	text     string
}

// annotScale is the stroke width for a capture of height h: thin lines
// vanish on a big capture shown small.
func annotScale(h int) float32 {
	return float32(max(3, h/300))
}

// renderAnnotations draws shapes, in order, on a copy of base.
func renderAnnotations(base *image.NRGBA, shapes []annotShape, face font.Face) *image.NRGBA {
	dst := image.NewNRGBA(base.Bounds())
	copy(dst.Pix, base.Pix)
	for _, sh := range shapes {
		drawAnnotation(dst, sh, face)
	}
	return dst
}

// drawAnnotation draws one shape on dst.
func drawAnnotation(dst *image.NRGBA, sh annotShape, face font.Face) {
	w := annotScale(dst.Bounds().Dy())
	switch sh.tool {
	case toolArrow:
		drawArrow(dst, sh.from, sh.to, w, sh.color)
	case toolRect:
		drawFrame(dst, image.Rectangle{Min: sh.from, Max: sh.to}.Canon(), w, sh.color)
	case toolBlur:
		pixelate(dst, image.Rectangle{Min: sh.from, Max: sh.to}.Canon(), max(8, dst.Bounds().Dy()/80))
	case toolText:
		drawLabel(dst, sh.from, sh.text, face, sh.color)
	}
}

// fillPolygon fills the polygon pts with c, rasterizing only its bounding
// box: a rasterizer the size of a big capture would be costly.
func fillPolygon(dst *image.NRGBA, pts []vector2, c color.NRGBA) {
	if len(pts) < 3 {
		return
	}
	minX, minY := pts[0].x, pts[0].y
	maxX, maxY := minX, minY
	for _, p := range pts[1:] {
		minX, minY = min(minX, p.x), min(minY, p.y)
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	box := image.Rect(int(math.Floor(float64(minX))), int(math.Floor(float64(minY))),
		int(math.Ceil(float64(maxX)))+1, int(math.Ceil(float64(maxY)))+1).Intersect(dst.Bounds())
	if box.Empty() {
		return
	}
	z := vector.NewRasterizer(box.Dx(), box.Dy())
	ox, oy := float32(box.Min.X), float32(box.Min.Y)
	z.MoveTo(pts[0].x-ox, pts[0].y-oy)
	for _, p := range pts[1:] {
		z.LineTo(p.x-ox, p.y-oy)
	}
	z.ClosePath()
	z.Draw(dst, box, image.NewUniform(c), image.Point{})
}

type vector2 struct{ x, y float32 }

// segment is the quad of a line from a to b, w wide.
func segment(a, b vector2, w float32) []vector2 {
	dx, dy := b.x-a.x, b.y-a.y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return nil
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	return []vector2{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}}
}

// drawArrow draws an arrow from from to to, its head at to.
func drawArrow(dst *image.NRGBA, from, to image.Point, w float32, c color.NRGBA) {
	a := vector2{float32(from.X), float32(from.Y)}
	b := vector2{float32(to.X), float32(to.Y)}
	dx, dy := b.x-a.x, b.y-a.y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l < 1 {
		return
	}
	ux, uy := dx/l, dy/l
	head := min(w*5, l*0.6)
	base := vector2{b.x - ux*head, b.y - uy*head} // the line stops under the head
	fillPolygon(dst, segment(a, base, w), c)
	fillPolygon(dst, []vector2{
		b,
		{base.x - uy*head*0.55, base.y + ux*head*0.55},
		{base.x + uy*head*0.55, base.y - ux*head*0.55},
	}, c)
}

// drawFrame strokes the rectangle r, w wide, inside it.
func drawFrame(dst *image.NRGBA, r image.Rectangle, w float32, c color.NRGBA) {
	iw := int(w)
	for _, side := range []image.Rectangle{
		{r.Min, image.Pt(r.Max.X, r.Min.Y+iw)},
		{image.Pt(r.Min.X, r.Max.Y-iw), r.Max},
		{r.Min, image.Pt(r.Min.X+iw, r.Max.Y)},
		{image.Pt(r.Max.X-iw, r.Min.Y), r.Max},
	} {
		draw.Draw(dst, side.Intersect(dst.Bounds()), image.NewUniform(c), image.Point{}, draw.Over)
	}
}

// pixelate replaces r by blocks of block×block pixels of their mean colour.
func pixelate(dst *image.NRGBA, r image.Rectangle, block int) {
	r = r.Intersect(dst.Bounds())
	for by := r.Min.Y; by < r.Max.Y; by += block {
		for bx := r.Min.X; bx < r.Max.X; bx += block {
			cell := image.Rect(bx, by, bx+block, by+block).Intersect(r)
			var sr, sg, sb, sa, n int
			for y := cell.Min.Y; y < cell.Max.Y; y++ {
				for x := cell.Min.X; x < cell.Max.X; x++ {
					c := dst.NRGBAAt(x, y)
					sr, sg, sb, sa, n = sr+int(c.R), sg+int(c.G), sb+int(c.B), sa+int(c.A), n+1
				}
			}
			if n == 0 {
				continue
			}
			mean := color.NRGBA{R: uint8(sr / n), G: uint8(sg / n), B: uint8(sb / n), A: uint8(sa / n)}
			draw.Draw(dst, cell, image.NewUniform(mean), image.Point{}, draw.Src)
		}
	}
}

// drawLabel writes text with its top-left corner at at, in c, over a dark
// shadow that keeps it legible on any background.
func drawLabel(dst *image.NRGBA, at image.Point, text string, face font.Face, c color.NRGBA) {
	if face == nil || text == "" {
		return
	}
	baseline := fixed.P(at.X, at.Y).Add(fixed.Point26_6{Y: face.Metrics().Ascent})
	shadow := &font.Drawer{Dst: dst, Src: image.NewUniform(color.NRGBA{A: 0xB0}), Face: face}
	for _, d := range []image.Point{{-2, 0}, {2, 0}, {0, -2}, {0, 2}, {2, 2}} {
		shadow.Dot = baseline.Add(fixed.P(d.X, d.Y))
		shadow.DrawString(text)
	}
	(&font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face, Dot: baseline}).DrawString(text)
}

// annotFace is the font of the text annotations, size pixels high: Fyne's
// bold one, or nil if it cannot be read.
func annotFace(size float64) font.Face {
	f, err := opentype.Parse(theme.TextBoldFont().Content())
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return face
}
