package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
)

func TestAnnotatorDrag(t *testing.T) {
	a := newAnnotator(checker(400, 200))
	w := test.NewWindow(a)
	defer w.Close()
	w.Resize(fyne.NewSize(400, 200))
	a.Resize(fyne.NewSize(400, 200)) // the capture fills the widget: 1 pixel = 1 unit

	a.tool = toolRect
	test.Drag(w.Canvas(), fyne.NewPos(50, 50), 100, 80)
	if len(a.shapes) != 1 {
		t.Fatalf("%d shapes after a drag", len(a.shapes))
	}
	sh := a.shapes[0]
	if sh.tool != toolRect || sh.to.X <= sh.from.X || sh.to.Y <= sh.from.Y {
		t.Fatalf("shape %+v", sh)
	}
	if c := a.result().NRGBAAt((sh.from.X+sh.to.X)/2, sh.from.Y+1); c != annotColors[0] {
		t.Errorf("the frame is not drawn in the result: %v", c)
	}

	a.undo()
	if len(a.shapes) != 0 || a.result() != a.base {
		t.Error("undo should take the frame away")
	}
}

func TestAnnotatorToImage(t *testing.T) {
	a := newAnnotator(checker(800, 400))
	a.Resize(fyne.NewSize(400, 400)) // shown at half size, centred vertically
	if p := a.toImage(fyne.NewPos(200, 200)); p != image.Pt(400, 200) {
		t.Errorf("centre: %v", p)
	}
	if p := a.toImage(fyne.NewPos(0, 0)); p != image.Pt(0, 0) { // above the picture: clamped
		t.Errorf("corner: %v", p)
	}
}

func TestSaveAnnotated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.png")
	first, err := saveAnnotated(path, checker(10, 10))
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveAnnotated(path, checker(10, 10))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "capture_annotated.png" || filepath.Base(second) != "capture_annotated_2.png" {
		t.Fatalf("names %q, %q", first, second)
	}
	img, err := readNRGBA(second)
	if err != nil || img.Bounds().Dx() != 10 {
		t.Fatalf("read back: %v", err)
	}
	if out := os.Getenv("ANNOT_WINDOW"); out != "" { // a look at the window
		test.NewTempApp(t)
		showAnnotator(second)
		for _, win := range fyne.CurrentApp().Driver().AllWindows() {
			if win.Content() == nil {
				continue
			}
			c := software.NewCanvas()
			c.SetContent(win.Content())
			c.Resize(fyne.NewSize(760, 300))
			if f, err := os.Create(out); err == nil {
				png.Encode(f, c.Capture())
				f.Close()
			}
			win.Close()
		}
	}
}
