package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyshos.com/tyde/internal/ocr"
	"fyshos.com/tyde/locale"
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
	if len(n.Buttons) != 3 {
		t.Fatalf("%d buttons, want Annotate, Copy text and Delete", len(n.Buttons))
	}
	n.Buttons[2].OnTap() // Delete
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the capture should be deleted, stat: %v", err)
	}
}

func TestTextNotification(t *testing.T) {
	n := textNotification("", ocr.ErrMissing)
	if n.Body != ocr.InstallCommand || len(n.Buttons) != 1 {
		t.Errorf("missing tesseract: %q, %d buttons", n.Body, len(n.Buttons))
	}
	if n := textNotification("", nil); n.Title != locale.T("ocr.empty") {
		t.Errorf("no text: %q", n.Title)
	}
	if n := textNotification("Bonjour\n\nle   monde", nil); n.Title != locale.T("ocr.copied") || n.Body != "Bonjour le monde" {
		t.Errorf("text: %q / %q", n.Title, n.Body)
	}
}

func TestRecordingNotification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recording.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := recordingNotification(path)
	if n.OnActivate == nil || len(n.Buttons) != 3 {
		t.Fatalf("a click should open it, with Edit, Copy and Delete: %d buttons", len(n.Buttons))
	}
	n.Buttons[2].OnTap() // Delete
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the recording should be deleted, stat: %v", err)
	}
}
