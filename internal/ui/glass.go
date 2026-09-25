package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
)

// glassRadius rounds the corners of the frosted glass windows.
const glassRadius = 10

// frostedGlass reports whether the windows of the panel are to be frosted
// glass: the user wants the blur and the compositor can draw it. Without
// it, translucent windows would let everything behind show through.
func frostedGlass(want bool) bool {
	if !want || !wlipc.IsWaylandSession() {
		return false
	}
	state, err := wlipc.ReadCompositorState()
	return err == nil && state.CanBlur
}

// glassOn reports whether the windows of the panel are frosted glass: the
// compositor blurs what lies behind them.
func glassOn() bool {
	return wlipc.IsWaylandSession() && wmtheme.Glass()
}

// glassy makes w frosted glass when the compositor blurs what lies behind
// the windows of the panel: w is transparent and content lies on a
// translucent tint with round corners. It must be called before w is shown.
// Otherwise it returns content as it is.
func glassy(w fyne.Window, content fyne.CanvasObject) fyne.CanvasObject {
	if !glassOn() {
		return content
	}
	w.SetTransparent(true)
	if w.Padded() {
		w.SetPadded(false) // the tint reaches the edges, the content keeps its room
		content = container.NewPadded(content)
	}
	tint := canvas.NewRectangle(wmtheme.Glassy(theme.Color(theme.ColorNameOverlayBackground)))
	tint.CornerRadius = glassRadius
	return container.NewStack(tint, content)
}
