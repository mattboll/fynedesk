package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/font"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wm"
)

// annotColors are the colours offered to annotate: red, yellow, green, blue.
var annotColors = []color.NRGBA{
	{R: 0xE5, G: 0x1A, B: 0x1A, A: 0xFF},
	{R: 0xF5, G: 0xC2, B: 0x11, A: 0xFF},
	{R: 0x2E, G: 0xB8, B: 0x4E, A: 0xFF},
	{R: 0x1E, G: 0x6F, B: 0xE8, A: 0xFF},
}

// annotator shows a capture and lets the user draw on it.
type annotator struct {
	widget.BaseWidget
	base      *image.NRGBA
	committed *image.NRGBA // base with the finished shapes
	shapes    []annotShape
	drawing   *annotShape // the shape being dragged
	tool      annotTool
	color     color.NRGBA
	face      font.Face
	view      *canvas.Image
	lastPaint time.Time
	askText   func(at image.Point) // the text tool tapped at
}

func newAnnotator(base *image.NRGBA) *annotator {
	a := &annotator{
		base: base, committed: base, color: annotColors[0],
		face: annotFace(float64(max(18, base.Bounds().Dy()/35))),
	}
	a.view = canvas.NewImageFromImage(base)
	a.view.FillMode = canvas.ImageFillContain
	a.ExtendBaseWidget(a)
	return a
}

func (a *annotator) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(a.view)
}

// toImage converts a position in the widget to the capture's pixels.
func (a *annotator) toImage(pos fyne.Position) image.Point {
	b := a.base.Bounds()
	size := a.Size()
	scale := min(size.Width/float32(b.Dx()), size.Height/float32(b.Dy()))
	if scale <= 0 {
		return image.Point{}
	}
	ox := (size.Width - float32(b.Dx())*scale) / 2
	oy := (size.Height - float32(b.Dy())*scale) / 2
	p := image.Pt(int((pos.X-ox)/scale), int((pos.Y-oy)/scale))
	return image.Pt(min(max(p.X, 0), b.Dx()-1), min(max(p.Y, 0), b.Dy()-1))
}

func (a *annotator) Dragged(e *fyne.DragEvent) {
	if a.tool == toolText {
		return
	}
	p := a.toImage(e.Position)
	if a.drawing == nil {
		a.drawing = &annotShape{tool: a.tool, from: a.toImage(e.Position.Subtract(e.Dragged)), color: a.color}
	}
	a.drawing.to = p
	if time.Since(a.lastPaint) > 33*time.Millisecond { // a big capture is slow to redraw
		a.paint()
	}
}

func (a *annotator) DragEnd() {
	if a.drawing == nil {
		return
	}
	sh := *a.drawing
	a.drawing = nil
	if d := sh.to.Sub(sh.from); d.X*d.X+d.Y*d.Y >= 9 {
		a.add(sh)
		return
	}
	a.paint()
}

func (a *annotator) Tapped(e *fyne.PointEvent) {
	if a.tool == toolText && a.askText != nil {
		a.askText(a.toImage(e.Position))
	}
}

// add keeps a finished shape.
func (a *annotator) add(sh annotShape) {
	a.shapes = append(a.shapes, sh)
	a.committed = renderAnnotations(a.base, a.shapes, a.face)
	a.paint()
}

// addText writes text at (the top-left corner of the text).
func (a *annotator) addText(at image.Point, text string) {
	a.add(annotShape{tool: toolText, from: at, to: at, color: a.color, text: text})
}

// undo takes the last shape away.
func (a *annotator) undo() {
	if len(a.shapes) == 0 {
		return
	}
	a.shapes = a.shapes[:len(a.shapes)-1]
	a.committed = a.base
	if len(a.shapes) > 0 {
		a.committed = renderAnnotations(a.base, a.shapes, a.face)
	}
	a.paint()
}

// result is the annotated capture.
func (a *annotator) result() *image.NRGBA {
	return a.committed
}

// paint shows the capture with its shapes and the one being drawn.
func (a *annotator) paint() {
	img := a.committed
	if a.drawing != nil {
		img = renderAnnotations(a.committed, []annotShape{*a.drawing}, a.face)
	}
	a.lastPaint = time.Now()
	a.view.Image = img
	a.view.Refresh()
}

// colorSwatch picks an annotation colour.
type colorSwatch struct {
	widget.BaseWidget
	color    color.NRGBA
	selected bool
	onTap    func()
}

func newColorSwatch(c color.NRGBA, onTap func()) *colorSwatch {
	s := &colorSwatch{color: c, onTap: onTap}
	s.ExtendBaseWidget(s)
	return s
}

func (s *colorSwatch) Tapped(*fyne.PointEvent) { s.onTap() }

func (s *colorSwatch) MinSize() fyne.Size { return fyne.NewSize(28, 28) }

func (s *colorSwatch) CreateRenderer() fyne.WidgetRenderer {
	dot := canvas.NewCircle(s.color)
	ring := canvas.NewCircle(color.Transparent)
	ring.StrokeColor = theme.Color(theme.ColorNameForeground)
	ring.StrokeWidth = 2
	r := &swatchRenderer{s: s, dot: dot, ring: ring}
	r.Refresh()
	return r
}

type swatchRenderer struct {
	s         *colorSwatch
	dot, ring *canvas.Circle
}

func (r *swatchRenderer) Layout(size fyne.Size) {
	d := min(size.Width, size.Height)
	pos := fyne.NewPos((size.Width-d)/2, (size.Height-d)/2)
	r.ring.Resize(fyne.NewSize(d, d))
	r.ring.Move(pos)
	r.dot.Resize(fyne.NewSize(d-8, d-8))
	r.dot.Move(pos.AddXY(4, 4))
}

func (r *swatchRenderer) MinSize() fyne.Size { return r.s.MinSize() }

func (r *swatchRenderer) Refresh() {
	r.ring.Hidden = !r.s.selected
	r.ring.Refresh()
	r.dot.Refresh()
}

func (r *swatchRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.ring, r.dot} }

func (r *swatchRenderer) Destroy() {}

// showAnnotator opens the capture at path to annotate it. Fyne thread.
func showAnnotator(path string) {
	img, err := readNRGBA(path)
	if err != nil {
		log.Printf("[ANNOTATE] %v", err)
		return
	}
	w := fyne.CurrentApp().NewWindow(locale.T("annotate.title") + " — " + filepath.Base(path))
	a := newAnnotator(img)
	a.askText = func(at image.Point) {
		entry := widget.NewEntry()
		d := dialog.NewForm(locale.T("annotate.text"), locale.T("annotate.ok"), locale.T("annotate.cancel"),
			[]*widget.FormItem{widget.NewFormItem("", entry)}, func(ok bool) {
				if ok && strings.TrimSpace(entry.Text) != "" {
					a.addText(at, entry.Text)
				}
			}, w)
		entry.OnSubmitted = func(string) { d.Submit() }
		d.Show()
		w.Canvas().Focus(entry)
	}

	toolNames := []string{locale.T("annotate.arrow"), locale.T("annotate.rect"), locale.T("annotate.textTool"), locale.T("annotate.blur")}
	tools := widget.NewRadioGroup(toolNames, func(name string) {
		for i, n := range toolNames {
			if n == name {
				a.tool = annotTool(i)
			}
		}
	})
	tools.Horizontal = true
	tools.Required = true
	tools.SetSelected(toolNames[0])

	var swatches []*colorSwatch
	for _, c := range annotColors {
		var s *colorSwatch
		s = newColorSwatch(c, func() {
			a.color = s.color
			for _, o := range swatches {
				o.selected = o == s
				o.Refresh()
			}
		})
		swatches = append(swatches, s)
	}
	swatches[0].selected = true
	colors := container.NewHBox()
	for _, s := range swatches {
		colors.Add(s)
	}

	undo := widget.NewButtonWithIcon(locale.T("annotate.undo"), theme.ContentUndoIcon(), a.undo)
	copyButton := widget.NewButtonWithIcon(locale.T("annotate.copy"), theme.ContentCopyIcon(), func() {
		result := a.result()
		go func() {
			n := wm.NewNotification(locale.T("annotate.copied"), "")
			if err := copyImage(result); err != nil {
				log.Printf("[ANNOTATE] copy: %v", err)
				n = wm.NewNotification(locale.T("annotate.copyFailed"), err.Error())
			}
			fyne.Do(func() { wm.SendNotification(n) })
		}()
	})
	save := widget.NewButtonWithIcon(locale.T("annotate.save"), theme.DocumentSaveIcon(), func() {
		out, err := saveAnnotated(path, a.result())
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		w.Close()
		wm.SendNotification(screenshotNotification(out))
	})
	save.Importance = widget.HighImportance

	bar := container.NewHBox(tools, widget.NewSeparator(), colors, layout.NewSpacer(), undo, copyButton, save)
	w.SetContent(container.NewBorder(container.NewPadded(bar), nil, nil, nil, a))
	w.Canvas().AddShortcut(&deskDriver.CustomShortcut{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) { a.undo() })

	// The capture at its size, or smaller to fit a usual screen.
	b := img.Bounds()
	scale := min(1, 1400/float32(b.Dx()), 860/float32(b.Dy()))
	w.Resize(fyne.NewSize(max(float32(b.Dx())*scale, 760), float32(b.Dy())*scale+60))
	w.CenterOnScreen()
	w.Show()
}

// readNRGBA reads the PNG at path.
func readNRGBA(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if n, ok := src.(*image.NRGBA); ok && n.Bounds().Min == (image.Point{}) {
		return n, nil
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst, nil
}

// saveAnnotated writes img beside the capture at path, as
// <name>_annotated.png (numbered if taken), and returns its path.
func saveAnnotated(path string, img image.Image) (string, error) {
	base := strings.TrimSuffix(path, filepath.Ext(path)) + "_annotated"
	name := base + ".png"
	for i := 2; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			if err := png.Encode(f, img); err != nil {
				f.Close()
				os.Remove(name)
				return "", err
			}
			return name, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		name = fmt.Sprintf("%s_%d.png", base, i)
	}
}

// copyImage puts img in the clipboard, with wl-copy.
func copyImage(img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	cmd := exec.Command("wl-copy", "--type", "image/png")
	cmd.Stdin = &buf
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("wl-copy: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
