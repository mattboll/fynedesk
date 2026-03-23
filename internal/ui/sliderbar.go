package ui

import (
	"fmt"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// sliderBar is a styled interactive bar with a thumb indicator.
// Input methods:
//   - Tap: jump to position (uses Position.X, works despite XWayland offset
//     once the compositor animation fix lands)
//   - Drag vertical: up = increase, down = decrease (only reliable delta under XWayland)
//   - Scroll wheel: fine-grained adjustment
type sliderBar struct {
	widget.BaseWidget

	Value     float64
	Max       float64
	Step      float64
	OnChanged func(float64)

	dragging     bool
	rawDragValue float64 // unsnapped value, accumulates fractional deltas
}

func newSliderBar(max, step float64, onChange func(float64)) *sliderBar {
	b := &sliderBar{Max: max, Step: step, OnChanged: onChange}
	b.ExtendBaseWidget(b)
	return b
}

// --- Input handlers ---

func (b *sliderBar) Tapped(e *fyne.PointEvent) {
	b.setFromPosition(e.Position.X)
}

func (b *sliderBar) Dragged(e *fyne.DragEvent) {
	if !b.dragging {
		// Skip the first event — Fyne reports the accumulated distance from
		// mousedown to drag-threshold which includes the XWayland surface
		// offset (hundreds of bogus pixels). Just record state.
		b.dragging = true
		b.rawDragValue = b.Value
		return
	}
	w := b.Size().Width
	if w <= 0 {
		return
	}
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	// Accumulate raw value without Step snapping — individual DX deltas
	// (1-3px = 0.3-1.0 value units) are smaller than Step (5) and would
	// be rounded to zero if snapped each time.
	b.rawDragValue += float64(e.Dragged.DX) / float64(w) * max
	if b.rawDragValue < 0 {
		b.rawDragValue = 0
	} else if b.rawDragValue > max {
		b.rawDragValue = max
	}
	// Snap only for display/callback
	snapped := b.rawDragValue
	if b.Step > 0 {
		snapped = math.Round(snapped/b.Step) * b.Step
	}
	if math.Abs(b.Value-snapped) < 0.01 {
		return
	}
	b.Value = snapped
	b.Refresh()
	if b.OnChanged != nil {
		b.OnChanged(snapped)
	}
}

func (b *sliderBar) DragEnd() {
	b.dragging = false
}

func (b *sliderBar) Scrolled(e *fyne.ScrollEvent) {
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	step := b.Step
	if step <= 0 {
		step = max / 20
	}
	if e.Scrolled.DY > 0 {
		b.setToValue(b.Value + step)
	} else if e.Scrolled.DY < 0 {
		b.setToValue(b.Value - step)
	}
}

func (b *sliderBar) Cursor() deskDriver.Cursor {
	return deskDriver.PointerCursor
}

// --- Value computation ---

func (b *sliderBar) setFromPosition(x float32) {
	w := b.Size().Width
	if w <= 0 {
		return
	}
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	ratio := float64(x) / float64(w)
	b.setToValue(ratio * max)
}

func (b *sliderBar) setToValue(v float64) {
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	if b.Step > 0 {
		v = math.Round(v/b.Step) * b.Step
	}
	if v < 0 {
		v = 0
	} else if v > max {
		v = max
	}
	if math.Abs(b.Value-v) < 0.01 {
		return
	}
	b.Value = v
	b.Refresh()
	if b.OnChanged != nil {
		b.OnChanged(v)
	}
}

func (b *sliderBar) ratio() float64 {
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	r := b.Value / max
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// --- Renderer ---

func (b *sliderBar) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 6

	fill := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	fill.CornerRadius = 6

	label := canvas.NewText("", color.White)
	label.TextSize = 11
	label.Alignment = fyne.TextAlignCenter
	label.TextStyle = fyne.TextStyle{Bold: true}

	thumb := canvas.NewCircle(color.White)

	return &sliderBarRenderer{
		bar:   b,
		bg:    bg,
		fill:  fill,
		label: label,
		thumb: thumb,
	}
}

type sliderBarRenderer struct {
	bar   *sliderBar
	bg    *canvas.Rectangle
	fill  *canvas.Rectangle
	label *canvas.Text
	thumb *canvas.Circle
}

func (r *sliderBarRenderer) MinSize() fyne.Size {
	return fyne.NewSize(40, 28)
}

func (r *sliderBarRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)

	ratio := float32(r.bar.ratio())
	fillW := size.Width * ratio
	if fillW < r.fill.CornerRadius*2 && fillW > 0 {
		fillW = r.fill.CornerRadius * 2
	}
	r.fill.Resize(fyne.NewSize(fillW, size.Height))

	r.label.Move(fyne.NewPos(0, (size.Height-r.label.MinSize().Height)/2))
	r.label.Resize(fyne.NewSize(size.Width, r.label.MinSize().Height))

	thumbD := size.Height * 0.85
	thumbX := size.Width*ratio - thumbD/2
	if thumbX < 0 {
		thumbX = 0
	}
	if thumbX > size.Width-thumbD {
		thumbX = size.Width - thumbD
	}
	thumbY := (size.Height - thumbD) / 2
	r.thumb.Move(fyne.NewPos(thumbX, thumbY))
	r.thumb.Resize(fyne.NewSize(thumbD, thumbD))
}

func (r *sliderBarRenderer) Refresh() {
	r.fill.FillColor = theme.Color(theme.ColorNamePrimary)
	r.fill.Refresh()

	r.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.bg.Refresh()

	max := r.bar.Max
	if max <= 0 {
		max = 1.0
	}
	r.label.Text = fmt.Sprintf("%.0f%%", r.bar.Value/max*100)
	r.label.Refresh()

	r.thumb.FillColor = color.White
	r.thumb.Refresh()

	r.Layout(r.bar.Size())
}

func (r *sliderBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.fill, r.label, r.thumb}
}

func (r *sliderBarRenderer) Destroy() {}
