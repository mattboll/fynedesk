package ui

import (
	"encoding/json"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
)

// secondaryBar holds state for a bar window on a non-primary output.
type secondaryBar struct {
	outputName string
	win        fyne.Window
	bar        *bar
}

// secondaryBarManager watches compositor-state.json and creates/destroys
// bar windows for secondary outputs so every screen has a taskbar.
type secondaryBarManager struct {
	mu   sync.Mutex
	desk fynedesk.Desktop
	bars map[string]*secondaryBar // keyed by output name
}

// StartSecondaryBars begins watching for secondary outputs.
// Call this after the primary panel desktop is created.
func StartSecondaryBars(desk fynedesk.Desktop) {
	mgr := &secondaryBarManager{
		desk: desk,
		bars: make(map[string]*secondaryBar),
	}
	go mgr.watch()
}

func (m *secondaryBarManager) watch() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	statePath := filepath.Join(configDir, "fynedesk", "compositor-state.json")

	var lastMod time.Time
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Run reconcile immediately on startup (don't wait 500ms for first tick)
	if data, err := os.ReadFile(statePath); err == nil && len(data) > 0 {
		var state CompositorState
		if json.Unmarshal(data, &state) == nil {
			fyne.Do(func() { m.reconcile(state.Outputs) })
			if info, err := os.Stat(statePath); err == nil {
				lastMod = info.ModTime()
			}
		}
	}

	for range ticker.C {
		info, err := os.Stat(statePath)
		if err != nil || !info.ModTime().After(lastMod) {
			continue
		}
		lastMod = info.ModTime()

		data, err := os.ReadFile(statePath)
		if err != nil || len(data) == 0 {
			continue
		}

		var state CompositorState
		if json.Unmarshal(data, &state) != nil {
			continue
		}

		fyne.Do(func() {
			m.reconcile(state.Outputs)
		})
	}
}

// reconcile creates or removes secondary bar windows to match the current outputs.
func (m *secondaryBarManager) reconcile(outputs []CompositorOutputState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build set of non-primary outputs
	secondary := make(map[string]CompositorOutputState)
	for _, out := range outputs {
		if !out.Primary {
			secondary[out.OutputName] = out
		}
	}

	// Remove bars for outputs that no longer exist
	for name, sb := range m.bars {
		if _, ok := secondary[name]; !ok {
			log.Printf("[secondary-bar] Removing bar for output %s", name)
			sb.win.Close()
			delete(m.bars, name)
		}
	}

	// Create bars for new secondary outputs
	for name, out := range secondary {
		if _, exists := m.bars[name]; exists {
			continue
		}
		log.Printf("[secondary-bar] Creating bar for output %s (%dx%d)", name, out.Width, out.Height)
		m.createBar(name, out)
	}
}

func (m *secondaryBarManager) createBar(outputName string, out CompositorOutputState) {
	// Use the title convention "FyneDesk:Bar:<outputName>" for compositor detection
	title := "FyneDesk:Bar:" + outputName

	win := fyne.CurrentApp().NewWindow(title)
	win.SetPadded(false)
	// Enable transparent framebuffer so the compositor wallpaper shows through,
	// matching the primary panel's SetTransparent(true) in cmd/fynedesk-panel/main.go.
	win.SetTransparent(true)

	// Create a taskbar-only bar (no launchers, no widget panel)
	b := newBar(m.desk)
	// Set output offset so tooltips/previews appear on the correct monitor
	b.outputOffsetX = float32(out.X)
	b.outputOffsetY = float32(out.Y)
	b.outputW = float32(out.Width) / out.Scale
	b.outputH = float32(out.Height) / out.Scale

	// Use transparent background so the compositor's wallpaper shows through.
	bg := canvas.NewRectangle(color.Transparent)

	// Layout: bar fills the window, positioned same as primary
	content := container.New(&secondaryBarLayout{desk: m.desk}, bg, b)
	win.SetContent(content)

	// Size to bar area only (not full screen) to minimize impact if
	// the compositor mis-positions this window on the wrong output.
	pos := m.desk.Settings().BarPosition()
	if pos == "left" {
		win.Resize(fyne.NewSize(wmtheme.NarrowBarWidth, float32(out.Height)))
	} else {
		barH := float32(m.desk.Settings().LauncherIconSize())*float32(m.desk.Settings().LauncherZoomScale()) + 10
		win.Resize(fyne.NewSize(float32(out.Width), barH))
	}

	sb := &secondaryBar{
		outputName: outputName,
		win:        win,
		bar:        b,
	}

	win.SetOnClosed(func() {
		m.mu.Lock()
		delete(m.bars, outputName)
		m.mu.Unlock()
	})

	m.bars[outputName] = sb
	win.Show()
}

// secondaryBarLayout positions a bar on a secondary screen.
// It only shows the taskbar (left bar or bottom dock), no widget panel.
type secondaryBarLayout struct {
	desk fynedesk.Desktop
}

func (l *secondaryBarLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	bg := objects[0]
	b := objects[1]

	pos := l.desk.Settings().BarPosition()
	switch pos {
	case "left":
		barSize := fyne.NewSize(wmtheme.NarrowBarWidth, size.Height)
		bg.Resize(barSize)
		bg.Move(fyne.NewPos(0, 0))
		b.Resize(barSize)
		b.Move(fyne.NewPos(0, 0))
	default: // "bottom"
		// Use zoom-scaled height so Fyne doesn't clip zoomed icons.
		barHeight := float32(l.desk.Settings().LauncherIconSize())*float32(l.desk.Settings().LauncherZoomScale()) + 2
		barSize := fyne.NewSize(size.Width, barHeight+1)
		barPos := fyne.NewPos(0, size.Height-barHeight)
		bg.Resize(barSize)
		bg.Move(barPos)
		b.Resize(barSize)
		b.Move(barPos)
	}
}

func (l *secondaryBarLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(640, 480)
}
