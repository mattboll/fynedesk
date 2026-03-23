package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
)

func (l *desktop) newDesktopWindowEmbedWithTitle(title string) fyne.Window {
	win := l.app.NewWindow(title)
	win.SetPadded(false)
	win.Resize(fyne.NewSize(1024, 576)) // grow a little from the minimum for testing
	win.SetMaster()
	return win
}

func (l *desktop) runEmbed() {
	l.root.ShowAndRun()
}

func (l *desktop) showMenuEmbed(menu *fyne.Menu, pos fyne.Position) {
	wid := widget.NewPopUpMenu(menu, l.root.Canvas())
	finalW := wmtheme.WidgetPanelWidth
	finalH := wid.MinSize().Height

	// Flip menu upward if it would extend below the canvas
	canvasH := l.root.Canvas().Size().Height
	if pos.Y+finalH > canvasH {
		pos.Y = pos.Y - finalH
		if pos.Y < 0 {
			pos.Y = 0
		}
	}

	if fynedesk.Instance().Settings().ReduceMotion() {
		wid.Resize(fyne.NewSize(finalW, finalH))
		wid.ShowAtPosition(pos)
		return
	}

	wid.Resize(fyne.NewSize(finalW, 0))
	wid.ShowAtPosition(pos)
	fyne.NewAnimation(canvas.DurationStandard, func(f float32) {
		fyne.Do(func() {
			wid.Resize(fyne.NewSize(finalW, finalH*f))
		})
	}).Start()
}
