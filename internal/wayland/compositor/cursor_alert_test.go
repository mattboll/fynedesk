package compositor

import "testing"

func TestCursorAlertImage(t *testing.T) {
	img := cursorAlertImage()
	n := img.Bounds().Dx()
	if n != cursorAlertSize*cursorAlertScale || img.Bounds().Dy() != n {
		t.Fatalf("size %v", img.Bounds())
	}
	centre, ring, corner := img.NRGBAAt(n/2, n/2), img.NRGBAAt(n/2, 2), img.NRGBAAt(0, 0)
	if centre.R != 0xFF || centre.A < 60 || centre.A > 120 {
		t.Errorf("the fill should be translucent red, got %v", centre)
	}
	if ring.A < 200 {
		t.Errorf("the ring should be nearly opaque, got %v", ring)
	}
	if corner.A != 0 {
		t.Errorf("outside the disc should be clear, got %v", corner)
	}
}
