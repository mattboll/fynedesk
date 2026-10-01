package ui

import (
	"context"
	"errors"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
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
		{Label: locale.T("screenshot.annotate"), OnTap: func() { showAnnotator(path) }},
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

// recordingNotification is the notification of a screen recording saved at
// path: a click opens it, its buttons make a GIF of it or delete it.
func recordingNotification(path string) *wm.Notification {
	n := wm.NewNotification(locale.T("record.saved"), filepath.Base(path))
	n.OnActivate = func() { openCapture(path) }
	n.Buttons = []wm.NotificationButton{
		{Label: locale.T("record.gif"), OnTap: func() { go makeGIF(path) }},
		{Label: locale.T("screenshot.delete"), OnTap: func() {
			if err := os.Remove(path); err != nil {
				log.Printf("[RECORD] %v", err)
			}
			wm.RemoveNotification(n.ID)
		}},
	}
	return n
}

// gifFilter makes a GIF light enough to share: 12 frames a second, at
// most 960 pixels wide, with a palette of its own.
const gifFilter = "fps=12,scale='min(960,iw)':-1:flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse"

// makeGIF converts the recording at path to a GIF beside it, with ffmpeg,
// and tells the user. Not on the Fyne thread.
func makeGIF(path string) {
	gif := strings.TrimSuffix(path, filepath.Ext(path)) + ".gif"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffmpeg", "-y", "-loglevel", "error", "-i", path, "-vf", gifFilter, gif).CombinedOutput()
	var n *wm.Notification
	if err != nil {
		log.Printf("[RECORD] gif: %v: %s", err, out)
		os.Remove(gif)
		n = wm.NewNotification(locale.T("record.gifFailed"), strings.TrimSpace(string(out)))
	} else {
		n = wm.NewNotification(locale.T("record.gifSaved"), filepath.Base(gif))
		n.OnActivate = func() { openCapture(gif) }
	}
	fyne.Do(func() { wm.SendNotification(n) })
}
