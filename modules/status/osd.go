package status

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/wlipc"
)

// osd manages a single on-screen display popup for volume/brightness feedback.
var osd struct {
	mu      sync.Mutex
	win     fyne.Window
	bar     *statusBar
	icon    *widget.Icon
	label   *canvas.Text
	dismiss *time.Timer
}

const (
	osdWidth   float32 = 280
	osdHeight  float32 = 56
	osdTimeout         = 1500 * time.Millisecond
)

// showOSD displays (or updates) a floating OSD popup with the given icon and percentage (0-100).
// It is safe to call from any goroutine; UI work is dispatched via fyne.Do.
func showOSD(icon fyne.Resource, percent float64) {
	if percent < 0 {
		percent = 0
	} else if percent > 100 {
		percent = 100
	}

	fyne.Do(func() {
		osd.mu.Lock()
		defer osd.mu.Unlock()

		if osd.win != nil {
			// Update existing OSD in place.
			osd.icon.SetResource(icon)
			osd.bar.SetValue(percent)
			osd.label.Text = fmt.Sprintf("%.0f%%", percent)
			osd.label.Refresh()
			// Reset dismiss timer.
			if osd.dismiss != nil {
				osd.dismiss.Stop()
			}
			osd.dismiss = time.AfterFunc(osdTimeout, dismissOSD)
			return
		}

		// Create new OSD window.
		osd.icon = widget.NewIcon(icon)
		osd.bar = newStatusBar()
		osd.bar.Max = 100
		osd.bar.Value = percent

		osd.label = canvas.NewText(fmt.Sprintf("%.0f%%", percent), color.White)
		osd.label.TextSize = 13
		osd.label.TextStyle = fyne.TextStyle{Bold: true}
		osd.label.Alignment = fyne.TextAlignTrailing

		// Size constraint for the percentage label so the bar doesn't jump around.
		labelProp := canvas.NewRectangle(color.Transparent)
		labelProp.SetMinSize(fyne.NewSize(44, 0))
		labelBox := container.NewStack(labelProp, osd.label)

		// Icon with some horizontal padding.
		iconProp := canvas.NewRectangle(color.Transparent)
		iconProp.SetMinSize(fyne.NewSize(theme.IconInlineSize()+theme.Padding()*2, theme.IconInlineSize()))
		iconBox := container.NewCenter(iconProp, osd.icon)

		inner := container.NewBorder(nil, nil, iconBox, labelBox, osd.bar)
		padded := container.NewPadded(inner)

		// Semi-transparent dark background with rounded corners.
		bg := canvas.NewRectangle(color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xE0})
		bg.CornerRadius = 12

		// Subtle border.
		border := canvas.NewRectangle(color.Transparent)
		border.CornerRadius = 12
		border.StrokeWidth = 1
		border.StrokeColor = color.NRGBA{R: 0x60, G: 0x60, B: 0x60, A: 0x80}

		styled := container.NewStack(bg, border, padded)

		drv, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
		if !ok {
			return
		}
		win := drv.CreateSplashWindow()
		win.SetTitle("OSD FyneDesk:skip FyneDesk:nofocus")
		win.SetPadded(false)
		win.SetContent(container.New(layout.NewStackLayout(), styled))
		osd.win = win

		win.Resize(fyne.NewSize(osdWidth, osdHeight))

		// Position: centered horizontally, near bottom of screen (above dock bar).
		desk := fynedesk.Instance()
		if desk != nil {
			screen := desk.Screens().Primary()
			screenW := float32(screen.Width) / screen.CanvasScale()
			screenH := float32(screen.Height) / screen.CanvasScale()
			x := (screenW - osdWidth) / 2
			y := screenH - osdHeight - 60 // 60px above bottom edge
			wlipc.RequestOverlayPosition(win.Title(), x, y, osdWidth, osdHeight)
		}

		win.Show()

		osd.dismiss = time.AfterFunc(osdTimeout, dismissOSD)
	})
}

// dismissOSD closes the OSD window. Called from a timer goroutine.
func dismissOSD() {
	fyne.Do(func() {
		osd.mu.Lock()
		defer osd.mu.Unlock()

		if osd.win != nil {
			osd.win.Close()
			osd.win = nil
			osd.bar = nil
			osd.icon = nil
			osd.label = nil
			osd.dismiss = nil
		}
	})
}
