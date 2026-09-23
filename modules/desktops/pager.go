package desktops

import (
	"math"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/icon"
)

// pagerWindowIcon is a draggable window preview in the pager
type pagerWindowIcon struct {
	widget.BaseWidget

	win       tyde.Window
	pager     *pager
	content   fyne.CanvasObject
	dragging  bool
	dragStart fyne.Position
}

func newPagerWindowIcon(win tyde.Window, content fyne.CanvasObject, p *pager) *pagerWindowIcon {
	pw := &pagerWindowIcon{win: win, pager: p, content: content}
	pw.ExtendBaseWidget(pw)
	return pw
}

func (pw *pagerWindowIcon) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(pw.content)
}

func (pw *pagerWindowIcon) Dragged(event *fyne.DragEvent) {
	if !pw.dragging {
		pw.dragging = true
		pw.dragStart = pw.Position()
		// Reduce opacity to show drag state
		if img, ok := pw.content.(*canvas.Image); ok {
			img.Translucency = 0.5
		} else if stack, ok := pw.content.(*fyne.Container); ok {
			for _, obj := range stack.Objects {
				if img, ok := obj.(*canvas.Image); ok {
					img.Translucency = 0.5
				}
			}
		}
	}
	pw.Move(fyne.NewPos(
		pw.dragStart.X+event.Position.X-pw.Size().Width/2,
		pw.dragStart.Y+event.Position.Y-pw.Size().Height/2,
	))

	// Highlight target desktop button
	center := pw.Position().Add(fyne.NewPos(pw.Size().Width/2, pw.Size().Height/2))
	for i, btn := range pw.pager.buttons.Objects {
		b := btn.(*widget.Button)
		btnPos := btn.Position()
		btnSize := btn.Size()
		inside := center.X >= btnPos.X && center.X < btnPos.X+btnSize.Width &&
			center.Y >= btnPos.Y && center.Y < btnPos.Y+btnSize.Height
		if inside && i != pw.win.Desktop() {
			b.Importance = widget.HighImportance
		} else if i == tyde.Instance().Desktop() {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.MediumImportance
		}
		b.Refresh()
	}
}

func (pw *pagerWindowIcon) DragEnd() {
	if !pw.dragging {
		return
	}
	pw.dragging = false

	// Determine which desktop button the icon was dropped on
	center := pw.Position().Add(fyne.NewPos(pw.Size().Width/2, pw.Size().Height/2))
	for i, btn := range pw.pager.buttons.Objects {
		btnPos := btn.Position()
		btnSize := btn.Size()
		if center.X >= btnPos.X && center.X < btnPos.X+btnSize.Width &&
			center.Y >= btnPos.Y && center.Y < btnPos.Y+btnSize.Height {
			if i != pw.win.Desktop() {
				pw.win.SetDesktop(i)
			}
			break
		}
	}

	pw.pager.refresh()
}

type pager struct {
	buttons, labels *fyne.Container
	wins            *fyne.Container
	winObjs         map[tyde.Window]*pagerWindowIcon // quick lookup for move updates
}

func newPager(d *desktops) *pager {
	p := &pager{wins: container.NewWithoutLayout()}

	count := deskCount()
	names := desktopNames()
	buttons := make([]fyne.CanvasObject, count)
	labels := make([]fyne.CanvasObject, count)
	for i := range count {
		label := desktopLabel(i, names)
		deskID := i
		buttons[i] = widget.NewButton("", func() {
			d.setDesktop(deskID)
		})
		labels[i] = widget.NewLabelWithStyle(label, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	}

	cols := count
	if tyde.Instance() != nil && tyde.Instance().Settings().NarrowWidgetPanel() {
		cols = 1
	}
	p.buttons = container.NewGridWithColumns(cols, buttons...)
	p.labels = container.NewGridWithColumns(cols, labels...)
	p.refresh()
	if desk := tyde.Instance(); desk != nil {
		desk.WindowManager().AddStackListener(p)
	}

	return p
}

// desktopNames returns the workspace names from settings.
func desktopNames() []string {
	if tyde.Instance() != nil {
		return tyde.Instance().Settings().DesktopNames()
	}
	return nil
}

// desktopLabel returns the display label for desktop i.
// Uses the name from settings if available, otherwise the number.
func desktopLabel(i int, names []string) string {
	if i < len(names) && names[i] != "" {
		return names[i]
	}
	return strconv.Itoa(i + 1)
}

func (p *pager) WindowAdded(_ tyde.Window) {
	p.refresh()
}

func (p *pager) WindowMoved(win tyde.Window) {
	fyne.Do(func() {
		obj, ok := p.winObjs[win]
		if !ok {
			p.refreshUI()
			return
		}
		if obj.dragging {
			return
		}
		p.place(obj)
		p.wins.Refresh()
	})
}

func (p *pager) WindowOrderChanged() {
	p.refresh()
}

func (p *pager) WindowRemoved(_ tyde.Window) {
	p.refresh()
}

func (p *pager) WindowStateChanged(_ tyde.Window) {
	p.refresh()
}

func (p *pager) refresh() {
	fyne.Do(func() {
		p.refreshUI()
	})
}

func (p *pager) refreshUI() {
	desk := tyde.Instance()
	if desk == nil {
		return
	}
	currentDesk := desk.Desktop()
	wins := desk.WindowManager().Windows()

	// Update button highlighting
	for i, b := range p.buttons.Objects {
		l := p.labels.Objects[i]
		if i == currentDesk {
			b.(*widget.Button).Importance = widget.HighImportance
			l.(*widget.Label).Importance = widget.LowImportance
		} else {
			b.(*widget.Button).Importance = widget.MediumImportance
			l.(*widget.Label).Importance = widget.MediumImportance
		}
		b.Refresh()
		l.Refresh()
	}

	// Place windows on their respective desktop buttons
	var rects []fyne.CanvasObject
	winObjs := make(map[tyde.Window]*pagerWindowIcon, len(wins))
	for j := len(wins) - 1; j >= 0; j-- {
		win := wins[j]
		if win.Iconic() || win.Properties().SkipTaskbar() {
			continue
		}

		deskID := win.Desktop()
		if win.Pinned() {
			deskID = currentDesk
		}
		if deskID < 0 || deskID >= len(p.buttons.Objects) {
			continue
		}

		var content fyne.CanvasObject
		content = canvas.NewRectangle(theme.Color(theme.ColorNameDisabled))

		// Try window-provided icon first, then look up via FDO app matching
		winIcon := win.Properties().Icon()
		if winIcon == nil {
			app := icon.FindAppFromWinInfo(win, desk.IconProvider())
			if app != nil {
				winIcon = app.Icon(desk.Settings().IconTheme(), 64)
			}
		}
		if winIcon != nil {
			content = container.NewStack(content,
				canvas.NewImageFromResource(winIcon))
		}

		obj := newPagerWindowIcon(win, content, p)
		rects = append(rects, obj)

		winObjs[win] = obj
		p.place(obj)
	}

	p.winObjs = winObjs
	p.wins.Objects = rects
	p.wins.Refresh()
}

// place sizes and moves a window preview over the button of its desktop.
func (p *pager) place(obj *pagerWindowIcon) {
	desk := tyde.Instance()
	win := obj.win
	deskID := win.Desktop()
	if win.Pinned() {
		deskID = desk.Desktop()
	}
	if deskID < 0 || deskID >= len(p.buttons.Objects) {
		return
	}
	button := p.buttons.Objects[deskID]

	// Compute total virtual desktop area across all screens, in pixels
	totalW, totalH := desk.RootSizePixels()
	if totalW == 0 || totalH == 0 {
		screen := desk.Screens().Primary()
		totalW = uint32(screen.Width)
		totalH = uint32(screen.Height)
	}
	if totalW == 0 {
		totalW = 1
	}
	if totalH == 0 {
		totalH = 1
	}

	scale := desk.Screens().ScreenForWindow(win).CanvasScale()
	px, py := win.Position().X*scale, win.Position().Y*scale
	// The X11 window manager parks windows of other desktops a whole screen
	// height above or below the visible one: fold them back onto the screen.
	py = float32(math.Mod(float64(py), float64(totalH)))
	if py < 0 {
		py += float32(totalH)
	}

	// Map pixel coordinates to button area using total virtual desktop size
	x := px / float32(totalW) * button.Size().Width
	y := py / float32(totalH) * button.Size().Height
	w := win.Size().Width * scale / float32(totalW) * button.Size().Width
	h := win.Size().Height * scale / float32(totalH) * button.Size().Height

	// Clamp to button bounds to prevent overflow
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x+w > button.Size().Width {
		w = button.Size().Width - x
	}
	if y+h > button.Size().Height {
		h = button.Size().Height - y
	}

	obj.Resize(fyne.NewSize(w, h))
	obj.Move(button.Position().Add(fyne.NewPos(x, y)))
}
