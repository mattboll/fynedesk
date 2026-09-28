package emoji

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	emojidata "fyshos.com/tyde/internal/emoji"
	"fyshos.com/tyde/locale"
	wmTheme "fyshos.com/tyde/theme"
)

// Preference keys. Recents are the single most-used part of any emoji picker -
// most people reach for the same dozen - so they are persisted between sessions,
// "|"-separated in the same style as the desktop's own module list.
const (
	prefRecent    = "emoji.recent"
	recentSep     = "|"
	maxRecent     = 30 // a little over two rows, so the page is always full
	pickerColumns = 10
	pickerRows    = 7
)

// recentPage is the label of the pseudo-group holding recently used emoji.
const recentPage = "Recent"

// picker is the emoji grid overlay. One instance is reused for the life of the
// module: it is built on first show and then hidden and re-shown, so the recents
// and the selected page survive between uses.
type picker struct {
	overlay fyne.CanvasObject
	entry   *searchEntry
	grid    *widget.GridWrap
	pages   *fyne.Container
	status  *widget.Label

	items []emojidata.Emoji // what the grid is currently showing
	page  string            // selected group, or recentPage
	shown bool
}

// show builds the picker if needed and puts it on screen.
func (p *picker) show() {
	if p.shown {
		return
	}

	if p.overlay == nil {
		p.build()
	}

	desk := tyde.Instance()
	screen := desk.Screens().Primary()
	scale := screen.CanvasScale()
	size := p.size()
	pos := fyne.NewPos(
		(float32(screen.Width)/scale-size.Width)/2,
		(float32(screen.Height)/scale-size.Height)/2,
	)

	p.shown = true
	desk.ShowOverlay(p.overlay, size, pos)
	fyne.Do(func() {
		p.entry.SetText("")
		p.showPage(p.page)
		desk.Root().Canvas().Focus(p.entry)
	})
}

// hide takes the picker off screen. The overlay object is kept for next time.
func (p *picker) hide() {
	if !p.shown {
		return
	}
	p.shown = false
	tyde.Instance().HideOverlay(p.overlay)
}

// toggle is what the keyboard shortcut and the status area icon both call, so
// the same gesture that opens the picker closes it again.
func (p *picker) toggle() {
	if p.shown {
		p.hide()
		return
	}
	p.show()
}

// size is the picker's on-screen size: a fixed grid of cells plus room for the
// search box, the group buttons and the status line.
func (p *picker) size() fyne.Size {
	pad := theme.Padding()
	width := emojidata.CellSize*pickerColumns + pad*4
	height := emojidata.CellSize*pickerRows + theme.TextSize()*5 + pad*8
	return fyne.NewSize(width, height)
}

func (p *picker) build() {
	p.entry = newSearchEntry()
	p.entry.SetPlaceHolder(locale.T("emoji.search"))
	p.entry.OnChanged = func(s string) { p.search(s) }
	p.entry.onEscape = p.hide
	// Enter picks the top match, so a known emoji is three keystrokes away
	// without ever leaving the keyboard.
	p.entry.OnSubmitted = func(string) {
		if len(p.items) > 0 {
			p.pick(p.items[0])
		}
	}

	p.grid = widget.NewGridWrap(
		func() int { return len(p.items) },
		func() fyne.CanvasObject { return emojidata.NewCell() },
		func(id widget.GridWrapItemID, o fyne.CanvasObject) {
			cell, ok := o.(*emojidata.Cell)
			if !ok || id >= len(p.items) {
				return
			}
			item := p.items[id]
			cell.SetEmoji(item,
				func() { p.pick(item) },
				func(in bool) { p.hover(item, in) })
		},
	)

	p.status = widget.NewLabel("")
	p.status.Truncation = fyne.TextTruncateEllipsis

	p.pages = container.NewHBox()
	p.rebuildPageButtons()

	if p.page == "" {
		p.page = p.defaultPage()
	}

	close := &widget.Button{
		Icon: theme.CancelIcon(), Importance: widget.LowImportance,
		OnTapped: p.hide,
	}
	top := container.NewBorder(nil, nil, nil, close, p.entry)
	bottom := container.NewVBox(p.pages, p.status)

	bg := canvas.NewRectangle(wmTheme.WidgetPanelBackground())
	bg.CornerRadius = theme.Size(theme.SizeNameInputRadius) + theme.Padding()

	p.overlay = container.NewStack(bg,
		container.NewPadded(container.NewBorder(top, bottom, nil, nil, p.grid)))
}

// rebuildPageButtons lays out the group selector row. Recent only earns a button
// once there is something in it.
func (p *picker) rebuildPageButtons() {
	p.pages.RemoveAll()
	for _, name := range p.pageNames() {
		page := name
		icon := emojidata.GroupIcon(page)
		if page == recentPage {
			icon = emojidata.RecentIcon
		}
		btn := &widget.Button{
			Text: icon, Importance: widget.LowImportance,
			OnTapped: func() {
				p.entry.SetText("") // leaving search behind for a group page
				p.showPage(page)
			},
		}
		p.pages.Add(btn)
	}
	p.pages.Refresh()
}

// pageNames is the ordered list of selectable pages.
func (p *picker) pageNames() []string {
	var names []string
	if len(p.recent()) > 0 {
		names = append(names, recentPage)
	}
	for _, g := range emojidata.Groups() {
		names = append(names, g.Name)
	}
	return names
}

// defaultPage opens on the user's recents when they have some, and on the first
// group (smileys) otherwise.
func (p *picker) defaultPage() string {
	if len(p.recent()) > 0 {
		return recentPage
	}
	return firstGroup()
}

// firstGroup is the page shown when there is nothing better to show.
func firstGroup() string {
	if gs := emojidata.Groups(); len(gs) > 0 {
		return gs[0].Name
	}
	return ""
}

// showPage swaps the grid to a group (or the recents) and scrolls back to the
// top so the page always starts where the eye expects.
func (p *picker) showPage(name string) {
	if name == recentPage && len(p.recent()) == 0 {
		name = firstGroup()
	}
	p.page = name

	if name == recentPage {
		p.setItems(p.recent())
		return
	}
	for _, g := range emojidata.Groups() {
		if g.Name == name {
			p.setItems(g.Items)
			return
		}
	}
	p.setItems(nil)
}

// search filters across every group. An empty box falls back to the selected
// page rather than showing nothing.
func (p *picker) search(query string) {
	if strings.TrimSpace(query) == "" {
		p.showPage(p.page)
		return
	}
	p.setItems(emojidata.Search(query))
}

// setItems replaces the grid contents.
func (p *picker) setItems(items []emojidata.Emoji) {
	p.items = items
	if p.grid == nil {
		return
	}
	p.grid.Refresh()
	p.grid.ScrollToTop()
}

// hover names the emoji under the pointer, which is how anyone learns what the
// less obvious ones are called - and what to type next time.
func (p *picker) hover(e emojidata.Emoji, in bool) {
	if !in {
		p.setStatus("")
		return
	}
	p.setStatus(e.Name)
}

func (p *picker) setStatus(text string) {
	if p.status == nil || p.status.Text == text {
		return
	}
	p.status.SetText(text)
}

// pick delivers the emoji by copying it to the clipboard, then closes the
// picker: with nothing typed for you, the next thing you want is your own
// window back so you can paste.
func (p *picker) pick(e emojidata.Emoji) {
	fyne.CurrentApp().Clipboard().SetContent(e.Character)
	p.remember(e)
	p.hide()
}

// recent reads the stored recents back into emoji, newest first. Characters that
// no longer resolve (a dataset update, a hand-edited preference) are skipped.
func (p *picker) recent() []emojidata.Emoji {
	stored := fyne.CurrentApp().Preferences().String(prefRecent)
	if stored == "" {
		return nil
	}

	var out []emojidata.Emoji
	for _, char := range strings.Split(stored, recentSep) {
		if e, ok := emojidata.Find(char); ok {
			out = append(out, e)
		}
	}
	return out
}

// remember moves an emoji to the front of the recents, trimming the tail.
func (p *picker) remember(e emojidata.Emoji) {
	chars := []string{e.Character}
	for _, old := range p.recent() {
		if old.Character == e.Character {
			continue
		}
		chars = append(chars, old.Character)
		if len(chars) == maxRecent {
			break
		}
	}
	fyne.CurrentApp().Preferences().SetString(prefRecent, strings.Join(chars, recentSep))

	// A first-ever pick earns the Recent page its button.
	if p.pages != nil && len(p.pages.Objects) < len(p.pageNames()) {
		p.rebuildPageButtons()
	}
}

// searchEntry is the picker's search box, extended so Escape closes the picker
// instead of being swallowed by the entry.
type searchEntry struct {
	widget.Entry

	onEscape func()
}

func newSearchEntry() *searchEntry {
	e := &searchEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *searchEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if e.onEscape != nil {
			e.onEscape()
		}
		return
	}
	e.Entry.TypedKey(ev)
}
