package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFitInside(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{2560, 1600, 240, 150}, // limited by the height
		{3440, 1440, 272, 113}, // limited by the width
		{100, 50, 100, 50},     // small enough: left as is
	} {
		got := fitInside(image.NewNRGBA(image.Rect(0, 0, c.w, c.h)), capturePreviewW, capturePreviewH).Bounds()
		if got.Dx() != c.wantW || got.Dy() != c.wantH {
			t.Errorf("%dx%d: got %dx%d, want %dx%d", c.w, c.h, got.Dx(), got.Dy(), c.wantW, c.wantH)
		}
	}
}

func TestScreenshotNotification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 1280, 720))); err != nil {
		t.Fatal(err)
	}
	f.Close()

	n := screenshotNotification(path)
	if n.Preview == nil {
		t.Fatal("no preview")
	}
	if b := n.Preview.Bounds(); b.Dx() != 266 || b.Dy() != capturePreviewH {
		t.Fatalf("preview %dx%d, want 266x%d", b.Dx(), b.Dy(), capturePreviewH)
	}
	if n.OnActivate == nil {
		t.Fatal("a click should open the capture")
	}
	if len(n.Buttons) != 1 {
		t.Fatalf("buttons %v", n.Buttons)
	}
	n.Buttons[0].OnTap() // Delete
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the capture should be deleted, stat: %v", err)
	}
}
