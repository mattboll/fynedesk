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
