package ui

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/agents"
	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
)

// Colours of the agent statuses.
var (
	agentAskColor  = color.NRGBA{R: 0xF5, G: 0xB0, B: 0x3A, A: 0xFF} // waits for the user
	agentDoneColor = color.NRGBA{R: 0x5C, G: 0xC8, B: 0x7A, A: 0xFF} // finished, not seen yet
	agentIdleColor = color.NRGBA{R: 0x80, G: 0x80, B: 0x88, A: 0xFF}
)

// Icons of the agent notifications (material "check_circle" and "help").
var (
	agentDoneIcon = &fyne.StaticResource{
		StaticName: "agent-done.svg",
		StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#5CC87A" ` +
			`d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"/></svg>`),
	}
	agentAskIcon = &fyne.StaticResource{
		StaticName: "agent-ask.svg",
		StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#F5B03A" ` +
			`d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 17h-2v-2h2v2zm2.07-7.75-.9.92C13.45 12.9 13 13.5 13 15h-2v-.5c0-1.1.45-2.1 1.17-2.83l1.24-1.26c.37-.36.59-.86.59-1.41 0-1.1-.9-2-2-2s-2 .9-2 2H8c0-2.21 1.79-4 4-4s4 1.79 4 4c0 .88-.36 1.68-.93 2.25z"/></svg>`),
	}
)

// maxAgentRows is how many active agents the panel lists before "+N".
const maxAgentRows = 6

// agentsWidget lists the coding agents in the widget panel: those that wait
// for the user, those at work and, folded, the idle ones.
type agentsWidget struct {
	widget.BaseWidget
	hub      *agentHub
	box      *fyne.Container
	showIdle bool
	dots     []*statusDot // the animated ones
	anim     *time.Ticker
	peekGen  int // hovers counted: a late preview of an earlier one is dropped
}

func newAgentsWidget(h *agentHub) *agentsWidget {
	w := &agentsWidget{hub: h, box: container.New(layout.NewCustomPaddedVBoxLayout(0))}
	w.ExtendBaseWidget(w)
	h.onChange(w.rebuild)
	w.rebuild()
	return w
}

func (w *agentsWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.box)
}

// rebuild lays out the list again from the hub.
func (w *agentsWidget) rebuild() {
	list := w.hub.Agents()
	w.box.Objects = nil
	w.dots = nil
	if len(list) == 0 {
		w.Hide()
		w.animate()
		return
	}

	var active, idle []agents.Agent
	for _, a := range list {
		if a.NeedsAttention() || a.Shown() == agents.StatusWorking {
			active = append(active, a)
		} else {
			idle = append(idle, a)
		}
	}

	header := canvas.NewText(locale.T("agents.title"), theme.Color(theme.ColorNameForeground))
	header.TextStyle.Bold = true
	header.TextSize = 12
	summary := canvas.NewText("", secondaryTextColor())
	summary.TextSize = 11
	if working := countWorking(list); working > 0 {
		summary.Text = fmt.Sprintf(locale.T("agents.working"), working)
	}
	w.box.Add(container.NewPadded(container.NewHBox(header, layout.NewSpacer(), summary)))

	rows := active
	if len(rows) > maxAgentRows {
		rows = rows[:maxAgentRows]
	}
	for _, a := range rows {
		w.box.Add(w.agentRow(a))
	}
	if w.showIdle {
		for _, a := range idle {
			w.box.Add(w.agentRow(a))
		}
	}
	if more := len(active) - len(rows); more > 0 {
		w.box.Add(w.foldRow(fmt.Sprintf(locale.T("agents.more"), more), nil))
	}
	if len(idle) > 0 {
		label := fmt.Sprintf(locale.T("agents.idle"), len(idle))
		if len(idle) == 1 {
			label = locale.T("agents.idleOne")
		}
		if w.showIdle {
			label = locale.T("agents.hideIdle")
		}
		w.box.Add(w.foldRow(label, func() {
			w.showIdle = !w.showIdle
			w.rebuild()
		}))
	}
	w.Show()
	w.box.Refresh()
	w.animate()
}

func countWorking(list []agents.Agent) int {
	n := 0
	for _, a := range list {
		if a.Shown() == agents.StatusWorking {
			n++
		}
	}
	return n
}

// agentRow shows one agent: a status dot, what it works on and its folder.
func (w *agentsWidget) agentRow(a agents.Agent) fyne.CanvasObject {
	dot := newStatusDot(a)
	if dot.pulse {
		w.dots = append(w.dots, dot)
	}

	title := canvas.NewText(truncateText(agentLabel(a), 26), theme.Color(theme.ColorNameForeground))
	title.TextSize = 12
	title.TextStyle.Bold = a.NeedsAttention()
	folder := canvas.NewText(truncateText(shortPath(a.Cwd), 30), secondaryTextColor())
	folder.TextSize = 10

	text := container.New(layout.NewCustomPaddedVBoxLayout(1), title, folder)
	dotBox := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(8), dot.circle))
	row := container.NewBorder(nil, nil, dotBox, nil, text)
	pane := a.PaneID
	r := newHoverRow(container.NewPadded(row), func() { focusAgent(pane) })
	r.onHover = func(in bool) { w.peek(r, pane, in) }
	return r
}

// peek shows, while the pointer rests on an agent, the last lines of its
// screen beside the panel.
func (w *agentsWidget) peek(row fyne.CanvasObject, pane string, in bool) {
	w.peekGen++
	gen := w.peekGen
	if !in {
		closePeek()
		return
	}
	time.AfterFunc(350*time.Millisecond, func() {
		screen, err := w.hub.client.Screen(pane, 60)
		if err != nil {
			return
		}
		lines := agents.Preview(screen, peekLines)
		fyne.Do(func() {
			if gen == w.peekGen && len(lines) > 0 {
				showPeek(row, lines)
			}
		})
	})
}

// foldRow is a small link-like row ("+3 idle").
func (w *agentsWidget) foldRow(text string, tapped func()) fyne.CanvasObject {
	label := canvas.NewText(text, secondaryTextColor())
	label.TextSize = 11
	row := container.NewPadded(label)
	if tapped == nil {
		return row
	}
	return newHoverRow(row, tapped)
}

// secondaryTextColor is the foreground, dimmed but readable.
func secondaryTextColor() color.Color {
	r, g, b, _ := theme.Color(theme.ColorNameForeground).RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0x99}
}

// animate runs the pulse of the dots of the agents at work or waiting, at a
// low rate: the panel is a large window.
func (w *agentsWidget) animate() {
	if len(w.dots) == 0 || !w.Visible() {
		if w.anim != nil {
			w.anim.Stop()
			w.anim = nil
		}
		return
	}
	if w.anim != nil {
		return
	}
	w.anim = time.NewTicker(250 * time.Millisecond)
	ticker := w.anim
	start := time.Now()
	go func() {
		for range ticker.C {
			phase := time.Since(start).Seconds()
			fyne.Do(func() {
				for _, d := range w.dots {
					d.step(phase)
				}
			})
		}
	}()
}

// statusDot is the coloured dot before an agent.
type statusDot struct {
	circle *canvas.Circle
	base   color.NRGBA
	pulse  bool
	fast   bool
}

func newStatusDot(a agents.Agent) *statusDot {
	d := &statusDot{}
	switch {
	case a.Shown() == agents.StatusBlocked:
		d.base, d.pulse, d.fast = agentAskColor, true, true
	case a.NeedsAttention():
		d.base = agentDoneColor
	case a.Shown() == agents.StatusWorking:
		r, g, b, _ := wmtheme.AccentGlow().RGBA()
		d.base, d.pulse = color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xFF}, true
	default:
		d.base = agentIdleColor
	}
	d.circle = canvas.NewCircle(d.base)
	if !d.pulse && !a.NeedsAttention() {
		d.circle.FillColor = color.Transparent
		d.circle.StrokeColor = d.base
		d.circle.StrokeWidth = 1.5
	}
	return d
}

// step sets the brightness of a pulsing dot for the given time, in seconds.
func (d *statusDot) step(t float64) {
	period := 2.0
	if d.fast {
		period = 1.0
	}
	a := 0.45 + 0.55*(0.5+0.5*math.Cos(2*math.Pi*t/period))
	c := d.base
	c.A = uint8(a * 255)
	d.circle.FillColor = c
	d.circle.Refresh()
}

// hoverRow highlights on hover and runs an action when tapped.
type hoverRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	bg      *canvas.Rectangle
	tapped  func()
	onHover func(in bool) // optional
}

func newHoverRow(content fyne.CanvasObject, tapped func()) *hoverRow {
	r := &hoverRow{content: content, tapped: tapped}
	r.bg = canvas.NewRectangle(theme.Color(theme.ColorNameHover))
	r.bg.CornerRadius = theme.Size(theme.SizeNameSelectionRadius)
	r.bg.Hide()
	r.ExtendBaseWidget(r)
	return r
}

func (r *hoverRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(r.bg, r.content))
}

// Tapped runs the action of the row.
func (r *hoverRow) Tapped(*fyne.PointEvent) { r.tapped() }

// MouseIn highlights the row.
func (r *hoverRow) MouseIn(*deskDriver.MouseEvent) {
	r.bg.Show()
	r.bg.Refresh()
	if r.onHover != nil {
		r.onHover(true)
	}
}

// MouseMoved is required by desktop.Hoverable.
func (r *hoverRow) MouseMoved(*deskDriver.MouseEvent) {}

// MouseOut removes the highlight.
func (r *hoverRow) MouseOut() {
	r.bg.Hide()
	r.bg.Refresh()
	if r.onHover != nil {
		r.onHover(false)
	}
}

// The preview of an agent's screen.
const (
	peekLines = 12
	peekW     = float32(460)
)

var peekWin fyne.Window

// showPeek shows the lines left of the widget panel, level with row.
func showPeek(row fyne.CanvasObject, lines []string) {
	closePeek()
	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}
	text := container.NewVBox()
	for _, l := range lines {
		t := canvas.NewText(truncateText(l, 64), theme.Color(theme.ColorNameForeground))
		t.TextStyle.Monospace = true
		t.TextSize = 11
		text.Add(t)
	}
	bg := canvas.NewRectangle(wmtheme.WidgetPanelBackground())
	bg.CornerRadius = 8
	bg.StrokeColor = wmtheme.ToastBorder()
	bg.StrokeWidth = 1

	win := d.CreateSplashWindow()
	win.SetTitle("Agent preview " + SkipTaskbarHint + " " + NoFocusHint)
	win.SetPadded(false)
	win.SetContent(container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(8, 8, 10, 10), text)))
	size := fyne.NewSize(peekW, text.MinSize().Height+16)
	win.Resize(size)

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(row)
	screen := tyde.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	x := screenW - wmtheme.WidgetPanelWidth - peekW - 8
	wlipc.RequestOverlayPosition(win.Title(), x, pos.Y, size.Width, size.Height)
	win.Show()
	peekWin = win
}

func closePeek() {
	if peekWin != nil {
		peekWin.Close()
		peekWin = nil
	}
}
