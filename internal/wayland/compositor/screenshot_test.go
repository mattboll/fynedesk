package compositor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScreenshotPathReservesTheName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seen := map[string]bool{}
	for range 3 {
		name, err := screenshotPath()
		if err != nil {
			t.Fatal(err)
		}
		if seen[name] {
			t.Fatalf("%s given twice", filepath.Base(name))
		}
		seen[name] = true
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s not created: %v", filepath.Base(name), err)
		}
	}
}

func TestDragRect(t *testing.T) {
	want := regionRect{x: 10, y: 20, w: 90, h: 60}
	for _, c := range [][4]float64{{10, 20, 100, 80}, {100, 80, 10, 20}, {100, 20, 10, 80}} {
		if got := dragRect(c[0], c[1], c[2], c[3]); got != want {
			t.Errorf("%v: got %+v, want %+v", c, got, want)
		}
	}
}

func TestCountdownImage(t *testing.T) {
	img := countdownImage(3, fontFaceOfSize(countdownFontSize))
	if b := img.Bounds(); b.Dx() != countdownSize || b.Dy() != countdownSize {
		t.Fatalf("size %v", b)
	}
	if img.NRGBAAt(0, 0).A != 0 {
		t.Error("the corners should be clear")
	}
	light := 0 // the digit, white on the dark disc
	for y := 0; y < countdownSize; y++ {
		for x := 0; x < countdownSize; x++ {
			if c := img.NRGBAAt(x, y); c.R > 200 && c.A > 200 {
				light++
			}
		}
	}
	if fontFaceOfSize(countdownFontSize) != nil && light == 0 {
		t.Error("the digit is not drawn")
	}
}

func TestRecordingPathReservesTheName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := recordingPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := recordingPath()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || filepath.Ext(a) != ".mp4" || filepath.Base(filepath.Dir(a)) != "Videos" {
		t.Fatalf("paths %q, %q", a, b)
	}
}
