// Command fynedesk-emoji is an emoji picker application for the FyneDesk desktop environment.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/internal/emoji"
)

type picker struct {
	app       fyne.App
	win       fyne.Window
	entry     *pickerEntry
	grid      *widget.GridWrap
	catBtns   []*widget.Button
	activeCat int
	current   []emoji.Entry
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
	requestEmojiPaste(emoji)
	p.quit()
}

func (p *picker) setEmojis(emojis []emoji.Entry) {
	p.current = emojis
	p.grid.Refresh()
	p.grid.ScrollToOffset(0)
}

func (p *picker) selectCategory(idx int) {
	if idx < 0 || idx >= len(emoji.Categories) {
		return
	}
	if p.activeCat >= 0 && p.activeCat < len(p.catBtns) {
		p.catBtns[p.activeCat].Importance = widget.LowImportance
		p.catBtns[p.activeCat].Refresh()
	}
	p.activeCat = idx
	p.catBtns[idx].Importance = widget.HighImportance
	p.catBtns[idx].Refresh()

	name := emoji.Categories[idx].Name
	if name == "recent" {
		p.setEmojis(getRecentEmojis())
		return
	}
	p.setEmojis(emoji.ForCategory(name))
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

	// Write overlay-request.json so compositor can position the window
	writeOverlayRequest(cursorX, cursorY)

	a := app.New()
	// Window title includes "FyneDesk:EmojiPicker" for compositor detection
	win := a.NewWindow("FyneDesk:EmojiPicker")
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
		p.current = emoji.ForCategory("smileys")
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
				btn.SetText(emoji.Emoji)
				btn.OnTapped = func() {
					p.selectEmoji(emoji.Emoji)
				}
			}
		},
	)

	// Category buttons
	var catButtons []fyne.CanvasObject
	for i, cat := range emoji.Categories {
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

// --- IPC helpers ---

func getConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, "fynedesk")
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeOverlayRequest(x, y float32) {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)

	req := struct {
		Title  string  `json:"title"`
		X      float32 `json:"x"`
		Y      float32 `json:"y"`
		Width  float32 `json:"width"`
		Height float32 `json:"height"`
	}{
		Title:  "EmojiPicker",
		X:      x,
		Y:      y,
		Width:  350,
		Height: 400,
	}
	data, _ := json.Marshal(req)
	path := filepath.Join(configDir, "overlay-request.json")
	atomicWriteFile(path, data)
}

func requestEmojiPaste(emoji string) {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)

	req := struct {
		Emoji     string `json:"emoji"`
		Timestamp int64  `json:"timestamp"`
	}{
		Emoji:     emoji,
		Timestamp: 0, // compositor doesn't check timestamp
	}
	data, _ := json.Marshal(req)
	path := filepath.Join(configDir, "emoji-paste.json")
	atomicWriteFile(path, data)
}

// --- Recents (file-based, shared with panel) ---

func getRecentsPath() string {
	return filepath.Join(getConfigDir(), "emoji-recents.txt")
}

func getRecentEmojis() []emoji.Entry {
	data, err := os.ReadFile(getRecentsPath())
	if err != nil || len(data) == 0 {
		return nil
	}

	parts := strings.Split(strings.TrimSpace(string(data)), ",")
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

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0755)
	content := strings.Join(filtered, ",")
	atomicWriteFile(getRecentsPath(), []byte(content))
}
