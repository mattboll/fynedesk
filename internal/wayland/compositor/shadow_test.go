package compositor

import (
	"image"
	"testing"
)

func TestShadowWanted(t *testing.T) {
	s := &server{windowShadows: true}
	tests := []struct {
		name string
		v    *xdgView
		want bool
	}{
		{"free window", &xdgView{decorated: true}, true},
		{"its own decorations", &xdgView{decorated: false}, false},
		{"maximized", &xdgView{decorated: true, maximized: true}, false},
		{"fullscreen", &xdgView{decorated: true, fullscreen: true}, false},
		{"snapped", &xdgView{decorated: true, snapped: snapLeft}, false},
	}
	for _, tt := range tests {
		if got := s.shadowWanted(tt.v); got != tt.want {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	s.windowShadows = false
	if s.shadowWanted(&xdgView{decorated: true}) {
		t.Error("shadows turned off, and still one")
	}
}

func TestHaloBoxesSurroundTheWindow(t *testing.T) {
	p := &glowParts{radius: 16, cornerRadius: 10}
	boxes := haloBoxes(p, 0, 0, 200, 100)
	var all image.Rectangle
	for _, b := range boxes {
		all = all.Union(b)
	}
	if want := image.Rect(-16, -16, 216, 116); all != want {
		t.Errorf("the halo covers %v, want %v", all, want)
	}
	// The edges start where the corners end: no gap, no overlap.
	if top, tl := boxes[4], boxes[0]; top.Min.X != tl.Max.X {
		t.Errorf("top edge starts at %d, the corner ends at %d", top.Min.X, tl.Max.X)
	}
}
