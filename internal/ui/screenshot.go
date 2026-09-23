package ui

import (
	"image"
	"image/draw"
	"image/png"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/wlipc"
)

func (l *desktop) screenshot() {
	// In Wayland compositor mode, delegate to compositor via IPC
	if client := wlipc.DefaultClient(); client != nil {
		client.SendRequest(wlipc.ReqCompositorAction, struct {
			Action string `json:"action"`
		}{Action: wlipc.ActionScreenshotFull})
		return
	}
	if len(l.screenWindows) == 1 {
		l.showCaptureSave(l.screenWindows[0].win.Canvas().Capture())
		return
	}

	// Multi-screen: stitch captures together based on screen geometry.
	minX, minY, maxX, maxY := 0, 0, 0, 0
	for _, sw := range l.screenWindows {
		s := sw.screen
		if s.X < minX {
			minX = s.X
		}
		if s.Y < minY {
			minY = s.Y
		}
		if s.X+s.Width > maxX {
			maxX = s.X + s.Width
		}
		if s.Y+s.Height > maxY {
			maxY = s.Y + s.Height
		}
	}

	composite := image.NewNRGBA(image.Rect(0, 0, maxX-minX, maxY-minY))
	for _, sw := range l.screenWindows {
		img := sw.win.Canvas().Capture()
		if img == nil {
			continue
		}
		dp := image.Pt(sw.screen.X-minX, sw.screen.Y-minY)
		draw.Draw(composite, image.Rectangle{Min: dp, Max: dp.Add(img.Bounds().Size())},
			img, image.Point{}, draw.Src)
	}
	l.showCaptureSave(composite)
}

func (l *desktop) screenshotWindow() {
	// In Wayland compositor mode, delegate to compositor via IPC
	if client := wlipc.DefaultClient(); client != nil {
		client.SendRequest(wlipc.ReqCompositorAction, struct {
			Action string `json:"action"`
		}{Action: wlipc.ActionScreenshotWindow})
		return
	}
	if l.primaryWin == nil || l.primaryWin.compositor == nil {
		fyne.LogError("Unable to print window with no compositor", nil)
		return
	}

	img := l.primaryWin.compositor.TopImage()
	if img == nil {
		fyne.LogError("Unable to print window with no window visible", nil)
		return
	}
	l.showCaptureSave(img)
}

func (l *desktop) showCaptureSave(img image.Image) {
	w := fyne.CurrentApp().NewWindow("Screenshot")

	save := &widget.Button{
		Text:       "Save...",
		Importance: widget.HighImportance,
		OnTapped: func() {
			saveImage(img, w)
		},
	}

	buttons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("Cancel", w.Close),
		save,
	)

	preview := canvas.NewImageFromImage(img)
	preview.FillMode = canvas.ImageFillContain

	w.SetContent(container.NewBorder(nil, buttons, nil, nil, preview))
	w.Resize(fyne.NewSize(480, 360))
	w.Show()
}

func saveImage(pix image.Image, w fyne.Window) {
	d := dialog.NewFileSave(func(write fyne.URIWriteCloser, err error) {
		if write == nil { // cancelled
			return
		}

		if err != nil {
			dialog.ShowError(err, w)
		} else if err = png.Encode(write, pix); err != nil {
			dialog.ShowError(err, w)
		}

		err = write.Close()
		if err != nil {
			dialog.ShowError(err, w)
		}

		w.Close()
	}, w)

	d.SetFilter(storage.NewMimeTypeFileFilter([]string{"image/png"}))

	now := time.Now().Format("20060102T150405") // YYYYMMDD"T"HHMMSS
	d.SetFileName("screenshot-" + now + ".png")

	if dir, err := getPicturesDir(); err == nil {
		d.SetLocation(dir)
	} else {
		fyne.LogError("error finding pictures dir, falling back to home directory", err)
	}

	d.Show()
}
