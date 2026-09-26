// Command tyde_emoji is an emoji picker application for the Tyde desktop environment.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/internal/emoji"
	"fyshos.com/tyde/wlipc"
)

// emojiPages are the pages of the picker: the recents, then the groups.
var emojiPages = emoji.Pages()

type picker struct {
	app       fyne.App
	win       fyne.Window
	entry     *pickerEntry
	grid      *widget.GridWrap
	catBtns   []*widget.Button
	activeCat int
	current   []emoji.Emoji
}

type pickerEntry struct {
	widget.Entry
	pick *picker
}

func (e *pickerEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		e.pick.quit()
		return
	}
	e.Entry.TypedKey(ev)
}

func (p *picker) quit() {
	p.app.Quit()
}

func (p *picker) selectEmoji(emoji string) {
	saveEmojiRecent(emoji)
	_ = wlipc.RequestEmojiPaste(emoji)
	p.quit()
}

func (p *picker) setEmojis(emojis []emoji.Emoji) {
	p.current = emojis
	p.grid.Refresh()
	p.grid.ScrollToOffset(0)
}

func (p *picker) selectCategory(idx int) {
	if idx < 0 || idx >= len(emojiPages) {
		return
	}
	if p.activeCat >= 0 && p.activeCat < len(p.catBtns) {
		p.catBtns[p.activeCat].Importance = widget.LowImportance
		p.catBtns[p.activeCat].Refresh()
	}
	p.activeCat = idx
	p.catBtns[idx].Importance = widget.HighImportance
	p.catBtns[idx].Refresh()

	name := emojiPages[idx].Name
	if name == emoji.RecentPage {
		p.setEmojis(getRecentEmojis())
		return
	}
	p.setEmojis(emoji.GroupItems(name))
}

func main() {
	// Parse cursor position from args (optional)
	var cursorX, cursorY float32
	if len(os.Args) >= 3 {
		if x, err := strconv.ParseFloat(os.Args[1], 32); err == nil {
			cursorX = float32(x)
		}
		if y, err := strconv.ParseFloat(os.Args[2], 32); err == nil {
			cursorY = float32(y)
		}
	}

	// Ask the compositor to place the window at the cursor
	_ = wlipc.RequestOverlayPositionAbsolute("EmojiPicker", cursorX, cursorY, 350, 400)

	a := app.New()
	// Window title includes "Tyde:EmojiPicker" for compositor detection
	win := a.NewWindow("Tyde:EmojiPicker")
	win.SetPadded(true)

	p := &picker{
		app:       a,
		win:       win,
		activeCat: -1,
	}

	// Search entry
	entry := &pickerEntry{pick: p}
	entry.ExtendBaseWidget(entry)
	entry.SetPlaceHolder("Search emojis...")
	entry.OnChanged = func(input string) {
		if input == "" {
			p.selectCategory(p.activeCat)
			return
		}
		p.setEmojis(emoji.Search(input))
	}
	p.entry = entry

	// Start with recents or smileys
	p.current = getRecentEmojis()
	startCat := 0
	if len(p.current) == 0 {
		p.current = emoji.GroupItems(emojiPages[1].Name)
		startCat = 1
	}

	// Emoji grid
	p.grid = widget.NewGridWrap(
		func() int {
			return len(p.current)
		},
		func() fyne.CanvasObject {
			btn := widget.NewButton("\U0001F600", nil)
			btn.Importance = widget.LowImportance
			return btn
		},
		func(id widget.GridWrapItemID, obj fyne.CanvasObject) {
			btn := obj.(*widget.Button)
			if id < len(p.current) {
				emoji := p.current[id]
				btn.SetText(emoji.Character)
				btn.OnTapped = func() {
					p.selectEmoji(emoji.Character)
				}
			}
		},
	)

	// Category buttons
	var catButtons []fyne.CanvasObject
	for i, cat := range emojiPages {
		idx := i
		btn := widget.NewButton(cat.Icon, func() {
			entry.SetText("")
			p.selectCategory(idx)
		})
		btn.Importance = widget.LowImportance
		p.catBtns = append(p.catBtns, btn)
		catButtons = append(catButtons, btn)
	}
	p.activeCat = startCat
	p.catBtns[startCat].Importance = widget.HighImportance

	catRow := container.New(layout.NewHBoxLayout(), catButtons...)

	content := container.NewBorder(
		container.NewVBox(entry, catRow),
		nil, nil, nil,
		p.grid,
	)

	win.SetContent(content)
	win.Resize(fyne.NewSize(350, 400))
	win.SetFixedSize(true)
	win.Canvas().Focus(entry)

	win.SetCloseIntercept(func() {
		p.quit()
	})

	win.ShowAndRun()
}

// --- Recents, kept in the config directory ---

func getRecentsPath() string {
	return filepath.Join(wlipc.ConfigDir(), "emoji-recents.txt")
}

func getRecentEmojis() []emoji.Emoji {
	data, err := os.ReadFile(getRecentsPath())
	if err != nil || len(data) == 0 {
		return nil
	}

	parts := strings.Split(strings.TrimSpace(string(data)), ",")
	return emoji.FromCharacters(parts)
}

func saveEmojiRecent(em string) {
	data, _ := os.ReadFile(getRecentsPath())
	var recents []string
	if len(data) > 0 {
		recents = strings.Split(strings.TrimSpace(string(data)), ",")
	}

	filtered := make([]string, 0, len(recents))
	for _, r := range recents {
		if r != em {
			filtered = append(filtered, r)
		}
	}
	filtered = append([]string{em}, filtered...)
	if len(filtered) > 32 {
		filtered = filtered[:32]
	}

	os.MkdirAll(wlipc.ConfigDir(), 0o700)
	content := strings.Join(filtered, ",")
	wlipc.WriteFileAtomic(getRecentsPath(), []byte(content))
}
