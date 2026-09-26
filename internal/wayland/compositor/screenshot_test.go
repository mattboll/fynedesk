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
