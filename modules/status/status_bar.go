package status

import (
	"fmt"
	"image/color"
	"math"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
)

// statusBar is a styled progress bar with rounded corners and optional semantic colors.
// When OnChanged is set, the bar becomes interactive (tappable + draggable) and
// shows a thumb indicator.
type statusBar struct {
	widget.BaseWidget

	Value float64 // 0.0 – 1.0
	Max   float64 // upper bound (e.g. 100 for volume); 0 means 1.0
	Step  float64 // snap increment (0 = continuous)

	// SemanticColor, when non-nil, returns a fill color based on current value.
	SemanticColor func(value float64) color.Color

	// TextFormat, when non-nil, returns the text to display inside the bar.
	TextFormat func(value, max float64) string

	// OnChanged, when non-nil, makes the bar interactive (tappable + draggable).
	// Called with the new value after each change.
	OnChanged func(float64)

	animGen atomic.Int64 // generation counter to cancel stale animations
}

func newStatusBar() *statusBar {
	b := &statusBar{Max: 1.0}
	b.ExtendBaseWidget(b)
	return b
}

// Cursor returns a pointer cursor when the bar is interactive.
func (b *statusBar) Cursor() desktop.Cursor {
	if b.interactive() {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (b *statusBar) SetValue(v float64) {
	max := b.Max
	if max <= 0 {
		max = 1.0
	}
	if v < 0 {
		v = 0
	} else if v > max {
		v = max
	}

	// Skip animation if reduce motion is enabled or change is tiny
	if reduceMotion() || math.Abs(b.Value-v) < 0.5 {
		b.animGen.Add(1) // cancel any running animation
		b.Value = v
		b.Refresh()
		return
	}

	// Cancel previous animation and start a new one
	from := b.Value
	gen := b.animGen.Add(1)
	go func() {
		dur := 150 * time.Millisecond
		start := time.Now()
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if b.animGen.Load() != gen {
				return // newer animation started, bail out
			}
			t := float64(time.Since(start)) / float64(dur)
			if t >= 1 {
				b.Value = v
				fyne.Do(func() { b.Refresh() })
				return
			}
			ease := 1 - math.Pow(1-t, 3) // easeOutCubic
			b.Value = from + (v-from)*ease
			fyne.Do(func() { b.Refresh() })
		}
	}()
}

// interactive returns true when OnChanged is set.
func (b *statusBar) interactive() bool {
	return b.OnChanged != nil
}

// Tapped handles click-to-seek on the bar.
func (b *statusBar) Tapped(e *fyne.PointEvent) {
	if !b.interactive() {
		return
	}
	b.setFromPosition(e.Position.X)
}

// Dragged handles drag-to-seek on the bar.
func (b *statusBar) Dragged(e *fyne.DragEvent) {
	if !b.interactive() {
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
	// Use drag delta rather than absolute position — DragEvent.Position
	// can be in canvas coordinates under XWayland, not widget-local.
	delta := float64(e.Dragged.DX) / float64(w) * max
	b.setToValue(b.Value + delta)
}

// DragEnd is called when the drag ends.
func (b *statusBar) DragEnd() {}

// setFromPosition maps a horizontal pixel position to a value and fires OnChanged.
func (b *statusBar) setFromPosition(x float32) {
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

func (b *statusBar) setToValue(v float64) {
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

	b.animGen.Add(1) // cancel running animations
	b.Value = v
	b.Refresh()
	if b.OnChanged != nil {
		b.OnChanged(v)
	}
}

func (b *statusBar) fillColor() color.Color {
	if b.SemanticColor != nil {
		return b.SemanticColor(b.ratio())
	}
	return theme.Color(theme.ColorNamePrimary)
}

func (b *statusBar) ratio() float64 {
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

func (b *statusBar) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 6

	fill := canvas.NewRectangle(b.fillColor())
	fill.CornerRadius = 6

	label := canvas.NewText("", color.White)
	label.TextSize = 11
	label.Alignment = fyne.TextAlignCenter
	label.TextStyle = fyne.TextStyle{Bold: true}

	thumb := canvas.NewCircle(color.White)
	if !b.interactive() {
		thumb.Hide()
	}

	return &statusBarRenderer{
		bar:   b,
		bg:    bg,
		fill:  fill,
		label: label,
		thumb: thumb,
	}
}

type statusBarRenderer struct {
	bar   *statusBar
	bg    *canvas.Rectangle
	fill  *canvas.Rectangle
	label *canvas.Text
	thumb *canvas.Circle
}

func (r *statusBarRenderer) MinSize() fyne.Size {
	if r.bar.interactive() {
		return fyne.NewSize(40, 28)
	}
	return fyne.NewSize(40, 20)
}

func (r *statusBarRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)

	ratio := float32(r.bar.ratio())
	fillW := size.Width * ratio
	if fillW < r.fill.CornerRadius*2 && fillW > 0 {
		fillW = r.fill.CornerRadius * 2
	}
	r.fill.Move(fyne.NewPos(0, 0))
	r.fill.Resize(fyne.NewSize(fillW, size.Height))

	r.label.Move(fyne.NewPos(0, (size.Height-r.label.MinSize().Height)/2))
	r.label.Resize(fyne.NewSize(size.Width, r.label.MinSize().Height))

	if r.bar.interactive() {
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
		r.thumb.Show()
	}
}

func (r *statusBarRenderer) Refresh() {
	r.fill.FillColor = r.bar.fillColor()
	r.fill.Refresh()

	r.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.bg.Refresh()

	if r.bar.TextFormat != nil {
		r.label.Text = r.bar.TextFormat(r.bar.Value, r.bar.Max)
	} else {
		max := r.bar.Max
		if max <= 0 {
			max = 1.0
		}
		r.label.Text = fmt.Sprintf("%.0f%%", r.bar.Value/max*100)
	}
	r.label.Refresh()

	if r.bar.interactive() {
		r.thumb.FillColor = color.White
		r.thumb.Refresh()
	}

	r.Layout(r.bar.Size())
}

func (r *statusBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.fill, r.label, r.thumb}
}

func (r *statusBarRenderer) Destroy() {}

// reduceMotion returns true if the user has enabled Reduce Motion accessibility setting.
func reduceMotion() bool {
	desk := fynedesk.Instance()
	if desk == nil {
		return true // no desktop instance (e.g. tests) — skip animations
	}
	return desk.Settings().ReduceMotion()
}

// --- Semantic color helpers ---

// batteryColor returns green (>50%), yellow (20-50%), or red (<20%).
func batteryColor(ratio float64) color.Color {
	switch {
	case ratio > 0.5:
		return color.NRGBA{R: 0x4c, G: 0xaf, B: 0x50, A: 0xff} // green
	case ratio > 0.2:
		return color.NRGBA{R: 0xff, G: 0xb3, B: 0x00, A: 0xff} // amber
	default:
		return color.NRGBA{R: 0xf4, G: 0x43, B: 0x36, A: 0xff} // red
	}
}
