package ui

import (
	"fyne.io/fyne/v2"

	"fyshos.com/fynedesk"
)

// NewBarForDesktop creates a new bar widget for the given desktop.
// This is exported for use by the Wayland compositor.
func NewBarForDesktop(desk fynedesk.Desktop) fyne.CanvasObject {
	return newBar(desk)
}

// NewWidgetPanelForDesktop creates a new widget panel for the given desktop.
// This is exported for use by the Wayland compositor.
func NewWidgetPanelForDesktop(desk fynedesk.Desktop) fyne.CanvasObject {
	return newWidgetPanel(desk)
}

// NewBackgroundWidget creates a new background widget.
// This is exported for use by the Wayland compositor.
func NewBackgroundWidget() fyne.CanvasObject {
	return newBackground()
}
