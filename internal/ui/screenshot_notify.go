package ui

import (
	"context"
	"errors"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"golang.org/x/image/draw"

	"fyshos.com/tyde/internal/ocr"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

// The preview of a capture in its notification fits in this box.
const (
	capturePreviewW = 272
	capturePreviewH = 150
)

// screenshotNotification is the notification of a capture saved at path:
// it shows the picture, a click opens it, and its buttons act on it.
func screenshotNotification(path string) *wm.Notification {
	n := wm.NewNotification(locale.T("screenshot.saved"), filepath.Base(path))
	n.Preview = capturePreview(path)
	n.OnActivate = func() { openCapture(path) }
	n.Buttons = []wm.NotificationButton{
		{Label: locale.T("screenshot.copyText"), OnTap: func() { go copyCaptureText(path, false) }},
		{Label: locale.T("screenshot.delete"), OnTap: func() {
			if err := os.Remove(path); err != nil {
				log.Printf("[SCREENSHOT] %v", err)
			}
			wm.RemoveNotification(n.ID)
		}},
	}
	return n
}

// capturePreview reads the capture at path, scaled down to fit the preview
// box (never up), or nil if it cannot be read.
func capturePreview(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		log.Printf("[SCREENSHOT] preview of %s: %v", path, err)
		return nil
	}
	return fitInside(src, capturePreviewW, capturePreviewH)
}

// fitInside scales img down to fit in w×h, keeping its proportions.
func fitInside(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if b.Dx() <= w && b.Dy() <= h {
		return img
	}
	scale := min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
	dst := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// openCapture opens a capture in the image viewer.
func openCapture(path string) {
	if err := wm.StartDetached("xdg-open", path); err != nil {
		log.Printf("[SCREENSHOT] open %s: %v", path, err)
	}
}

// copyCaptureText reads the text of the capture at path, puts it in the
// clipboard and tells the user. With remove, the capture is a temporary
// file, deleted once read. Not on the Fyne thread: reading takes a moment.
func copyCaptureText(path string, remove bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	text, err := ocr.Text(ctx, path)
	if remove {
		os.Remove(path)
	}
	n := textNotification(text, err)
	n.Tag = "ocr" // a new reading replaces the previous one
	fyne.Do(func() { wm.SendNotification(n) })
}

// textNotification tells what reading the text of a capture gave, and puts
// the text in the clipboard.
func textNotification(text string, err error) *wm.Notification {
	switch {
	case errors.Is(err, ocr.ErrMissing):
		n := wm.NewNotification(locale.T("ocr.missing"), ocr.InstallCommand)
		n.Buttons = []wm.NotificationButton{{Label: locale.T("ocr.copyCommand"), OnTap: func() {
			_ = wlipc.RequestClipboardPaste(ocr.InstallCommand)
		}}}
		return n
	case err != nil:
		log.Printf("[OCR] %v", err)
		return wm.NewNotification(locale.T("ocr.failed"), err.Error())
	case text == "":
		return wm.NewNotification(locale.T("ocr.empty"), "")
	}
	if err := wlipc.RequestClipboardPaste(text); err != nil {
		log.Printf("[OCR] clipboard: %v", err)
		return wm.NewNotification(locale.T("ocr.failed"), err.Error())
	}
	return wm.NewNotification(locale.T("ocr.copied"), strings.Join(strings.Fields(text), " "))
}
