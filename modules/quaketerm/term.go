package launcher

import (
	_ "embed"
	"image/color"
	"time"

	"fyshos.com/tyde"
	wmTheme "fyshos.com/tyde/theme"
	"github.com/fyne-io/terminal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

const (
	delay  = time.Second / 25
	height = 240
	step   = 40
)

var termMeta = tyde.ModuleMetadata{
	Name:        "Terminal Overlay",
	NewInstance: newTerm,
}

//go:embed terminal.svg
var resourceTerminalSvgData []byte

var resourceTerminal = &fyne.StaticResource{
	StaticName:    "terminal.svg",
	StaticContent: resourceTerminalSvgData,
}

type term struct {
	shown          bool
	running        bool
	content        fyne.CanvasObject
	console        *terminal.Terminal
	bg, over       *canvas.Rectangle
	themeListening bool // true once the per-process theme listener is registered
}

func (t *term) Destroy() {
}

func (t *term) Metadata() tyde.ModuleMetadata {
	return termMeta
}

func (t *term) Shortcuts() map[*tyde.Shortcut]func() {
	return map[*tyde.Shortcut]func(){
		{Name: "Open Terminal Overlay", KeyName: fyne.KeyBackTick, Modifier: tyde.UserModifier}: func() {
			t.toggle()
		},
	}
}

func (t *term) createTerm() {
	bg := canvas.NewRectangle(withTransparency(theme.Color(theme.ColorNameOverlayBackground)))
	img := canvas.NewImageFromResource(theme.NewThemedResource(resourceTerminal))
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(200, 200))
	over := canvas.NewRectangle(wmTheme.WidgetPanelBackground())
	t.bg = bg
	t.over = over
	t.startThemeListenerOnce()

	t.console = terminal.New()
	t.content = container.NewStack(img, bg, over, t.console)
}

func (t *term) hide() {
	var y float32
	end := -float32(height)
	for y > end {
		currY := y
		fyne.Do(func() {
			t.content.Move(fyne.NewPos(0, currY))
		})
		time.Sleep(delay)
		y -= step
	}

	t.shown = false
	tyde.Instance().HideOverlay(t.content)
}

func (t *term) show() {
	screen := tyde.Instance().Screens().Primary()
	scale := screen.CanvasScale()
	w := float32(screen.Width) / scale
	y := -float32(height)
	var end float32
	size := fyne.NewSize(w, height)

	// Register the overlay at its resting position (0,0).
	tyde.Instance().ShowOverlay(t.content, size, fyne.NewPos(0, 0))

	if !t.running {
		t.running = true
		go func() {
			err := t.console.RunLocalShell()
			if err != nil {
				fyne.LogError("Failed to open terminal", err)
			}
			t.running = false
			if t.shown {
				t.hide()
			}
			t.createTerm() // reset for next usage
		}()
	}

	for y < end {
		currY := y
		fyne.Do(func() {
			t.content.Resize(size)
			t.content.Move(fyne.NewPos(0, currY))
		})
		time.Sleep(delay)
		y += step
	}
	fyne.Do(func() {
		t.content.Move(fyne.NewPos(0, end))
		tyde.Instance().Root().Canvas().Focus(t.console)
	})
	t.shown = true
}

func (t *term) toggle() {
	if t.content == nil {
		t.createTerm()
	}

	if !t.shown {
		go t.show()
	} else {
		go t.hide()
	}
}

// startThemeListenerOnce registers a single theme listener for this term's
// lifetime. Fyne's AddListener has no remove counterpart, so we must avoid
// adding a fresh closure every time createTerm runs (it runs after each
// shell exit). The listener references t.bg / t.over, which are reassigned
// by every createTerm — so the latest rectangles always get repainted.
func (t *term) startThemeListenerOnce() {
	if t.themeListening {
		return
	}
	t.themeListening = true
	fyne.CurrentApp().Settings().AddListener(func(_ fyne.Settings) {
		if t.bg != nil {
			t.bg.FillColor = withTransparency(theme.Color(theme.ColorNameOverlayBackground))
			t.bg.Refresh()
		}
		if t.over != nil {
			t.over.FillColor = wmTheme.WidgetPanelBackground()
			t.over.Refresh()
		}
	})
}

func withTransparency(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0x99}
}

func newTerm() tyde.Module {
	return &term{}
}
