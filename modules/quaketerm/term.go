package launcher

import (
	"time"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/ui"
	wmTheme "fyshos.com/fynedesk/theme"
	"github.com/fyne-io/terminal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

const (
	delay     = time.Second / 25
	termTitle = "Terminal Overlay " + ui.SkipTaskbarHint
	height    = 240
	step      = 40
)

var termMeta = fynedesk.ModuleMetadata{
	Name:        "Terminal Overlay",
	NewInstance: newTerm,
}

type term struct {
	shown          bool
	win            fynedesk.Window
	ui             fyne.Window
	bg, over       *canvas.Rectangle
	themeListening bool // true once the per-process theme listener is registered
}

func (t *term) Destroy() {
}

func (t *term) Metadata() fynedesk.ModuleMetadata {
	return termMeta
}

func (t *term) Shortcuts() map[*fynedesk.Shortcut]func() {
	return map[*fynedesk.Shortcut]func(){
		&fynedesk.Shortcut{Name: "Open Terminal Overlay", KeyName: fyne.KeyBackTick, Modifier: fynedesk.UserModifier}: func() {
			t.toggle()
		}}
}

func (t *term) createTerm() {
	win := fyne.CurrentApp().Driver().(desktop.Driver).CreateSplashWindow()
	win.SetTitle(termTitle)

	bg := canvas.NewRectangle(theme.Color(theme.ColorNameBackground))
	img := canvas.NewImageFromResource(theme.NewDisabledResource(theme.ComputerIcon()))
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(200, 200))
	over := canvas.NewRectangle(wmTheme.WidgetPanelBackground())
	t.bg = bg
	t.over = over
	t.startThemeListenerOnce()

	console := terminal.New()
	win.SetContent(container.NewStack(bg, img, over, console))
	win.Canvas().Focus(console)
	t.ui = win

	go func() {
		err := console.RunLocalShell()
		if err != nil {
			fyne.LogError("Failed to open terminal", err)
		}
		t.hide() // terminal exited

		t.createTerm() // reset for next usage
	}()
}

func (t *term) getHandle() fynedesk.Window {
	// TODO a better way to capture window frame without showing it and waiting...
	//t.ui.Resize(fyne.NewSize(0, 0))
	fyne.Do(t.ui.Show)

	i := 0
	for {
		time.Sleep(time.Second / 50)

		for _, w := range fynedesk.Instance().WindowManager().Windows() {
			if w.Properties().Title() == termTitle {
				return w
			}
		}

		i++
		if i > 50 {
			return nil // something went wrong
		}
	}
}

func (t *term) hide() {
	screen := fynedesk.Instance().Screens().Primary()
	left := float32(screen.X) / screen.Scale
	y := float32(screen.Y) / screen.Scale
	end := float32(screen.Y)/screen.Scale - height
	for y > end {
		t.win.Move(fyne.NewPos(left, y))
		time.Sleep(delay)
		y -= step
	}
	t.win.Move(fyne.NewPos(left, end))

	t.ui.Hide()
	t.shown = false
}

func (t *term) show() {
	screen := fynedesk.Instance().Screens().Primary()
	t.win.Resize(fyne.NewSize(float32(screen.Width)/screen.Scale, height))
	//	t.ui.Show()
	t.win.RaiseToTop()

	left := float32(screen.X) / screen.Scale
	y := float32(screen.Y)/screen.Scale - height
	end := float32(screen.Y) / screen.Scale
	for y < end {
		t.win.Resize(fyne.NewSize(float32(screen.Width)/screen.Scale, height)) // force it ASAP
		t.win.Move(fyne.NewPos(left, y))
		time.Sleep(delay)
		y += step
	}
	t.win.Move(fyne.NewPos(left, end))
	t.shown = true
}

func (t *term) toggle() {
	if t.ui == nil {
		t.createTerm() // lazy load UI
	}

	if !t.shown {
		go func() {
			t.win = t.getHandle()

			if t.win != nil {
				fyne.Do(func() {
					t.win.Pin()
					t.show()
				})
			}
		}()
	} else {
		t.hide()
		t.win = nil
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
			t.bg.FillColor = theme.Color(theme.ColorNameBackground)
			t.bg.Refresh()
		}
		if t.over != nil {
			t.over.FillColor = wmTheme.WidgetPanelBackground()
			t.over.Refresh()
		}
	})
}

func newTerm() fynedesk.Module {
	// don't load UI until it is first called on
	return &term{}
}
