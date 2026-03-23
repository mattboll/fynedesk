package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/emoji"
	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wlipc"
)

var emojiPicker *emojiPickerWindow

// emojiCategories references the shared emoji categories.
var emojiCategories = emoji.Categories

type emojiPickerWindow struct {
	win       fyne.Window
	entry     *emojiEntry_
	grid      *widget.GridWrap
	catBtns   []*widget.Button
	activeCat int
	current   []emoji.Entry
}

type emojiEntry_ struct {
	widget.Entry
	picker *emojiPickerWindow
}

func (e *emojiEntry_) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		e.picker.close()
		return
	}
	e.Entry.TypedKey(ev)
}

func (p *emojiPickerWindow) close() {
	emojiPicker = nil
	if p.win != nil {
		p.win.Close()
	}
}

func (p *emojiPickerWindow) selectEmoji(emoji string) {
	// Save to recents
	saveEmojiRecent(emoji)

	// Copy to clipboard (panel stays alive so clipboard ownership is retained)
	fyne.CurrentApp().Clipboard().SetContent(emoji)

	// Ask compositor to paste the emoji into the previously focused window.
	wlipc.RequestEmojiPaste(emoji)

	// Close picker — compositor detects overlay unmap and handles the paste
	p.close()
}

func (p *emojiPickerWindow) setEmojis(emojis []emoji.Entry) {
	p.current = emojis
	p.grid.Refresh()
	p.grid.ScrollToOffset(0)
}

func (p *emojiPickerWindow) selectCategory(idx int) {
	if idx < 0 || idx >= len(emojiCategories) {
		return
	}
	// Update button emphasis
	if p.activeCat >= 0 && p.activeCat < len(p.catBtns) {
		p.catBtns[p.activeCat].Importance = widget.LowImportance
		p.catBtns[p.activeCat].Refresh()
	}
	p.activeCat = idx
	p.catBtns[idx].Importance = widget.HighImportance
	p.catBtns[idx].Refresh()

	name := emojiCategories[idx].Name
	if name == "recent" {
		p.setEmojis(getRecentEmojis())
		return
	}
	p.setEmojis(emoji.ForCategory(name))
}

// ShowEmojiPicker opens the emoji picker near the given cursor position.
// If already open, it toggles closed.
func ShowEmojiPicker(cursorX, cursorY float32) {
	if emojiPicker != nil {
		emojiPicker.close()
		return
	}

	var win fyne.Window
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		win = d.CreateSplashWindow()
		win.SetPadded(true)
		win.SetTitle("Emoji Picker " + SkipTaskbarHint)
	} else {
		win = fyne.CurrentApp().NewWindow("Emoji Picker " + SkipTaskbarHint)
	}

	p := &emojiPickerWindow{
		win:       win,
		activeCat: -1,
	}
	emojiPicker = p

	// Search entry
	entry := &emojiEntry_{picker: p}
	entry.ExtendBaseWidget(entry)
	entry.SetPlaceHolder(locale.T("emoji.search"))
	entry.OnChanged = func(input string) {
		if input == "" {
			p.selectCategory(p.activeCat)
			return
		}
		p.setEmojis(emoji.Search(input))
	}
	p.entry = entry

	// Start with smileys (or recent if available)
	p.current = getRecentEmojis()
	startCat := 0 // "recent"
	if len(p.current) == 0 {
		p.current = emoji.ForCategory("smileys")
		startCat = 1 // "smileys"
	}

	// Emoji grid — template with emoji-sized button for proper cell sizing
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
				btn.SetText(emoji.Emoji)
				btn.OnTapped = func() {
					p.selectEmoji(emoji.Emoji)
				}
			}
		},
	)

	// Category buttons — simple horizontal row (no AppTabs content area overhead)
	var catButtons []fyne.CanvasObject
	for i, cat := range emojiCategories {
		idx := i
		btn := widget.NewButton(cat.Icon, func() {
			entry.SetText("")
			p.selectCategory(idx)
		})
		btn.Importance = widget.LowImportance
		p.catBtns = append(p.catBtns, btn)
		catButtons = append(catButtons, btn)
	}
	// Highlight the starting category
	p.activeCat = startCat
	p.catBtns[startCat].Importance = widget.HighImportance

	catRow := container.New(layout.NewHBoxLayout(), catButtons...)

	content := container.NewBorder(
		container.NewVBox(entry, catRow),
		nil, nil, nil,
		p.grid,
	)

	pickerSize := fyne.NewSize(350, 400)

	// Ensure emojiPicker is cleared when the window is closed externally
	// (e.g. compositor dismiss, focus loss) — not just via our close() method.
	win.SetOnClosed(func() {
		emojiPicker = nil
	})

	fyne.Do(func() {
		win.SetContent(content)

		// Position near cursor via ShowOverlay (handles IPC positioning + clamping)
		pos := fyne.NewPos(cursorX, cursorY)
		fynedesk.Instance().WindowManager().ShowOverlay(win, pickerSize, pos)

		// Focus search entry after window is shown
		win.Canvas().Focus(entry)
		go ensureFocused(win, entry)
	})
}

// getRecentEmojis returns the recently used emojis from preferences.
func getRecentEmojis() []emoji.Entry {
	prefs := fyne.CurrentApp().Preferences()
	data := prefs.String("emoji_recents")
	if data == "" {
		return nil
	}

	parts := strings.Split(data, ",")
	var results []emoji.Entry
	for _, em := range parts {
		if em == "" {
			continue
		}
		name := emoji.FindName(em)
		results = append(results, emoji.Entry{Emoji: em, Name: name, Category: "recent"})
	}
	return results
}

// saveEmojiRecent adds an emoji to the front of the recents list.
func saveEmojiRecent(em string) {
	prefs := fyne.CurrentApp().Preferences()
	data := prefs.String("emoji_recents")

	var recents []string
	if data != "" {
		recents = strings.Split(data, ",")
	}

	// Remove if already present
	filtered := make([]string, 0, len(recents))
	for _, r := range recents {
		if r != em {
			filtered = append(filtered, r)
		}
	}

	// Prepend
	filtered = append([]string{em}, filtered...)

	// Limit to 32
	if len(filtered) > 32 {
		filtered = filtered[:32]
	}

	prefs.SetString("emoji_recents", strings.Join(filtered, ","))
}
