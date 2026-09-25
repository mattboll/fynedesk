// Command tyde_chooser lets the user pick the screen or the window to share
// when an application starts a screencast through xdg-desktop-portal-wlr.
//
// It is a dmenu style chooser: the portal writes the choices on stdin, one per
// line ("Monitor: …" or "Window: …"), and the chooser prints the chosen line
// on stdout. Printing nothing declines the screencast.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/BurntSushi/toml"
	"github.com/FyshOS/appie"

	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
)

func main() {
	choices := readChoices(os.Stdin)
	if len(choices) == 0 {
		return // nothing to share: the screencast is declined
	}

	a := app.NewWithID("com.fyshos.tyde")
	applyTydeSettings(a)

	c := newChooser(a, choices)
	c.win.ShowAndRun()
	if c.chosen != nil {
		fmt.Println(c.chosen.line)
	}
}

// applyTydeSettings uses the language and theme of the Tyde desktop.
func applyTydeSettings(a fyne.App) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}

	var cfg struct {
		Display struct {
			Language string `toml:"language"`
		} `toml:"display"`
	}
	if _, err := toml.DecodeFile(filepath.Join(dir, "tyde", "config.toml"), &cfg); err == nil && cfg.Display.Language != "" {
		locale.SetLanguage(cfg.Display.Language)
	}

	if data, err := os.ReadFile(filepath.Join(dir, "fyne", "com.fyshos.tyde", "theme.json")); err == nil {
		if th, err := theme.FromJSON(string(data)); err == nil {
			a.Settings().SetTheme(th)
		}
	}
}

type chooser struct {
	win      fyne.Window
	rows     []*choiceRow
	selected int
	share    *widget.Button
	chosen   *choice
}

func newChooser(a fyne.App, choices []choice) *chooser {
	c := &chooser{win: a.NewWindow(locale.T("chooser.title")), selected: -1}
	icons := appie.NewFDOProvider()

	screens, windows := container.NewVBox(), container.NewVBox()
	for i := range choices {
		ch := &choices[i]
		row := newChoiceRow(ch, choiceIcon(ch, icons), func(r *choiceRow) { c.selectRow(r) }, c.choose)
		c.rows = append(c.rows, row)
		if ch.kind == choiceScreen {
			screens.Add(row)
		} else {
			windows.Add(row)
		}
	}

	sections := container.NewVBox()
	if len(screens.Objects) > 0 {
		sections.Add(sectionTitle(locale.T("chooser.screens")))
		sections.Add(screens)
	}
	if len(windows.Objects) > 0 {
		sections.Add(sectionTitle(locale.T("chooser.windows")))
		sections.Add(windows)
	}

	heading := widget.NewLabelWithStyle(locale.T("chooser.heading"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.SizeName = theme.SizeNameSubHeadingText

	c.share = widget.NewButtonWithIcon(locale.T("chooser.share"), theme.ConfirmIcon(), c.choose)
	c.share.Importance = widget.HighImportance
	c.share.Disable()
	cancel := widget.NewButtonWithIcon(locale.T("chooser.cancel"), theme.CancelIcon(), c.win.Close)
	buttons := container.NewHBox(layout.NewSpacer(), cancel, c.share)

	c.win.SetContent(container.NewBorder(
		container.NewVBox(heading, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), buttons), nil, nil,
		container.NewVScroll(sections)))
	c.win.Canvas().SetOnTypedKey(c.typedKey)
	c.win.Resize(fyne.NewSize(460, 520))
	// A fixed size tells the compositor it is a dialog: Tyde centres it
	// (CenterOnScreen does nothing on Wayland).
	c.win.SetFixedSize(true)
	c.win.CenterOnScreen()

	if len(c.rows) > 0 {
		c.selectRow(c.rows[0])
	}
	return c
}

func (c *chooser) typedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyEscape:
		c.win.Close()
	case fyne.KeyReturn, fyne.KeyEnter:
		c.choose()
	case fyne.KeyUp:
		if c.selected > 0 {
			c.selectRow(c.rows[c.selected-1])
		}
	case fyne.KeyDown:
		if c.selected < len(c.rows)-1 {
			c.selectRow(c.rows[c.selected+1])
		}
	}
}

func (c *chooser) selectRow(row *choiceRow) {
	for i, r := range c.rows {
		r.setSelected(r == row)
		if r == row {
			c.selected = i
		}
	}
	c.share.Enable()
}

// choose shares the selected screen or window and closes the chooser.
func (c *chooser) choose() {
	if c.selected < 0 {
		return
	}
	c.chosen = c.rows[c.selected].choice
	c.win.Close()
}

func sectionTitle(text string) fyne.CanvasObject {
	return widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

// choiceIcon returns the application icon of a window, or a generic icon.
func choiceIcon(c *choice, icons appie.Provider) fyne.Resource {
	if c.kind == choiceScreen {
		return wmtheme.ScreensIcon
	}
	for _, name := range []string{c.sub, strings.ToLower(c.sub)} {
		if name == "" {
			continue
		}
		if apps := icons.FindAppsMatching(name); len(apps) > 0 {
			if res := apps[0].Icon("", 64); res != nil {
				return res
			}
		}
	}
	return theme.FileApplicationIcon()
}

// choiceRow shows one choice: its icon, title and details.
type choiceRow struct {
	widget.BaseWidget
	choice   *choice
	icon     fyne.Resource
	bg       *canvas.Rectangle
	onSelect func(*choiceRow)
	onChoose func()
}

func newChoiceRow(c *choice, icon fyne.Resource, onSelect func(*choiceRow), onChoose func()) *choiceRow {
	r := &choiceRow{choice: c, icon: icon, onSelect: onSelect, onChoose: onChoose}
	r.bg = canvas.NewRectangle(theme.Color(theme.ColorNameSelection))
	r.bg.CornerRadius = theme.Size(theme.SizeNameSelectionRadius)
	r.bg.Hide()
	r.ExtendBaseWidget(r)
	return r
}

func (r *choiceRow) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(r.icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSquareSize(32))

	title := widget.NewLabelWithStyle(r.choice.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	text := container.NewVBox(title)
	if r.choice.sub != "" {
		sub := widget.NewLabel(r.choice.sub)
		sub.Importance = widget.LowImportance
		sub.Truncation = fyne.TextTruncateEllipsis
		text = container.New(layout.NewCustomPaddedVBoxLayout(-theme.Padding()*2), title, sub)
	}

	content := container.NewBorder(nil, nil, container.NewPadded(img), nil, text)
	return widget.NewSimpleRenderer(container.NewStack(r.bg, content))
}

func (r *choiceRow) setSelected(selected bool) {
	r.bg.Hidden = !selected
	r.bg.Refresh()
}

// Tapped selects the row.
func (r *choiceRow) Tapped(*fyne.PointEvent) { r.onSelect(r) }

// DoubleTapped shares the row's screen or window straight away.
func (r *choiceRow) DoubleTapped(*fyne.PointEvent) {
	r.onSelect(r)
	r.onChoose()
}
