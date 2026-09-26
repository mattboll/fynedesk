package quaketerm

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

// term is the dropdown terminal. Its state is used on the Fyne thread only.
type term struct {
	shown          bool
	running        bool
	content        fyne.CanvasObject
	console        *terminal.Terminal
	bg, over       *canvas.Rectangle
	themeListening bool // true once the per-process theme listener is registered
}

// Destroy puts the terminal away and ends its shell (it outlived the
// module).
func (t *term) Destroy() {
	if t.shown {
		t.shown = false
		tyde.Instance().HideOverlay(t.content)
	}
	if t.running && t.console != nil {
		t.console.Exit()
	}
}

func (t *term) Metadata() tyde.ModuleMetadata {
	return termMeta
}

func (t *term) Shortcuts() map[*tyde.Shortcut]func() {
	return map[*tyde.Shortcut]func(){
		{Name: "Open Terminal Overlay", KeyName: fyne.KeyBackTick, Modifier: tyde.UserModifier}: func() {
			fyne.Do(t.toggle)
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

// hide slides the terminal up and away. Fyne thread.
func (t *term) hide() {
	if !t.shown {
		return
	}
	t.shown = false
	content := t.content
	t.slide(content, 0, -float32(height), func() { tyde.Instance().HideOverlay(content) })
}

// show slides the terminal down, starting its shell the first time. Fyne
// thread.
func (t *term) show() {
	if t.shown {
		return
	}
	t.shown = true
	screen := tyde.Instance().Screens().Primary()
	size := fyne.NewSize(float32(screen.Width)/screen.CanvasScale(), height)

	// Register the overlay at its resting position (0,0), drawn above it.
	tyde.Instance().ShowOverlay(t.content, size, fyne.NewPos(0, 0))
	t.content.Resize(size)
	t.content.Move(fyne.NewPos(0, -float32(height)))

	if !t.running {
		t.running = true
		console := t.console
		go func() {
			if err := console.RunLocalShell(); err != nil {
				fyne.LogError("Failed to open terminal", err)
			}
			fyne.Do(func() {
				t.running = false
				if t.console != console {
					return
				}
				t.hide()
				t.createTerm() // reset for next usage
			})
		}()
	}

	console := t.console
	t.slide(t.content, -float32(height), 0, func() {
		tyde.Instance().Root().Canvas().Focus(console)
	})
}

// slide moves content from one height to another, a step at a time, then
// calls done; only the waiting happens off the Fyne thread.
func (t *term) slide(content fyne.CanvasObject, from, to float32, done func()) {
	go func() {
		dir := float32(step)
		if to < from {
			dir = -dir
		}
		for y := from; (dir > 0 && y < to) || (dir < 0 && y > to); y += dir {
			pos := fyne.NewPos(0, y)
			fyne.Do(func() { content.Move(pos) })
			time.Sleep(delay)
		}
		fyne.Do(func() {
			content.Move(fyne.NewPos(0, to))
			done()
		})
	}()
}

// toggle shows or hides the terminal. Fyne thread.
func (t *term) toggle() {
	if t.content == nil {
		t.createTerm()
	}
	if t.shown {
		t.hide()
	} else {
		t.show()
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
