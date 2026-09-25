package compositor

import "testing"

func TestZoneRect(t *testing.T) {
	// A 1000x800 content area at (100, 50), 30 px of titlebar, 6 px gaps.
	const cx, cy, cw, ch, top, o, i = 100, 50, 1000, 800, 30, 6, 6
	type rect struct {
		x, y float64
		w, h int
	}
	tests := []struct {
		zone snapZone
		want rect
	}{
		{snapTop, rect{100, 80, 1000, 770}}, // maximized: no gaps
		{snapLeft, rect{106, 86, 491, 758}},
		{snapRight, rect{603, 86, 491, 758}},
		{snapTopLeft, rect{106, 86, 491, 376}},
		{snapBottomRight, rect{603, 468, 491, 376}},
	}
	for _, tt := range tests {
		x, y, w, h := zoneRect(cx, cy, cw, ch, tt.zone, top, o, i)
		if got := (rect{x, y, w, h}); got != tt.want {
			t.Errorf("zone %d: got %+v, want %+v", tt.zone, got, tt.want)
		}
	}

	// The halves meet with the inner gap between them and the outer gap at
	// the edges.
	lx, _, lw, _ := zoneRect(cx, cy, cw, ch, snapLeft, top, o, i)
	rx, _, rw, _ := zoneRect(cx, cy, cw, ch, snapRight, top, o, i)
	if gap := rx - (lx + float64(lw)); gap != i {
		t.Errorf("gap between the halves = %v, want %d", gap, i)
	}
	if right := rx + float64(rw); right != cx+cw-o {
		t.Errorf("right half ends at %v, want %d", right, cx+cw-o)
	}
}
