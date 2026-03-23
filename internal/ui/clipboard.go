package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/wlipc"
)

var clipboardPicker *clipboardPickerWindow

// clipboardHistoryCache is the local cache of clipboard history updated via IPC.
var clipboardHistoryCache []wlipc.ClipboardEntry

type clipboardPickerWindow struct {
	win        fyne.Window
	entry      *clipboardSearchEntry
	list       *widget.List
	allEntries []wlipc.ClipboardEntry
	filtered   []wlipc.ClipboardEntry
}

type clipboardSearchEntry struct {
	widget.Entry
	picker *clipboardPickerWindow
}

func (e *clipboardSearchEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		e.picker.close()
		return
	}
	e.Entry.TypedKey(ev)
}

func (p *clipboardPickerWindow) close() {
	clipboardPicker = nil
	if p.win != nil {
		p.win.Close()
	}
}

func (p *clipboardPickerWindow) selectEntry(text string) {
	wlipc.RequestClipboardPaste(text)
	p.close()
}

func (p *clipboardPickerWindow) filter(query string) {
	if query == "" {
		p.filtered = p.allEntries
	} else {
		lower := strings.ToLower(query)
		p.filtered = nil
		for _, e := range p.allEntries {
			if strings.Contains(strings.ToLower(e.Text), lower) {
				p.filtered = append(p.filtered, e)
			}
		}
	}
	p.list.Refresh()
	p.list.ScrollToOffset(0)
}

// ShowClipboardManager opens the clipboard history overlay.
// If already open, it toggles closed.
func ShowClipboardManager() {
	if clipboardPicker != nil {
		clipboardPicker.close()
		clipboardPicker = nil
		return
	}

	var win fyne.Window
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		win = d.CreateSplashWindow()
		win.SetPadded(true)
		win.SetTitle("Clipboard Manager " + SkipTaskbarHint)
	} else {
		win = fyne.CurrentApp().NewWindow("Clipboard Manager " + SkipTaskbarHint)
	}

	// If the cache is empty (panel just started, no broadcast yet),
	// load directly from the persisted file on disk.
	entries := clipboardHistoryCache
	if len(entries) == 0 {
		entries = wlipc.ReadClipboardHistory()
		clipboardHistoryCache = entries
	}

	p := &clipboardPickerWindow{
		win:        win,
		allEntries: entries,
	}
	p.filtered = p.allEntries
	clipboardPicker = p

	// Search entry
	entry := &clipboardSearchEntry{picker: p}
	entry.ExtendBaseWidget(entry)
	entry.SetPlaceHolder("Search clipboard...")
	entry.OnChanged = func(input string) {
		p.filter(input)
	}
	p.entry = entry

	// List of clipboard entries
	p.list = widget.NewList(
		func() int {
			return len(p.filtered)
		},
		func() fyne.CanvasObject {
			textLabel := widget.NewLabel("")
			textLabel.Truncation = fyne.TextTruncateEllipsis
			timeLabel := widget.NewLabel("00m")
			timeLabel.Alignment = fyne.TextAlignTrailing
			return container.NewBorder(nil, nil, nil, timeLabel, textLabel)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(p.filtered) {
				return
			}
			e := p.filtered[id]
			c, ok := obj.(*fyne.Container)
			if !ok || len(c.Objects) < 2 {
				return
			}
			if lbl, ok := c.Objects[0].(*widget.Label); ok {
				lbl.SetText(clipRelativeTime(e.Timestamp))
			}
			if lbl, ok := c.Objects[1].(*widget.Label); ok {
				lbl.SetText(clipFirstLine(e.Text))
			}
		},
	)
	p.list.OnSelected = func(id widget.ListItemID) {
		if id >= len(p.filtered) {
			return
		}
		p.selectEntry(p.filtered[id].Text)
	}

	// Clear button
	clearBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		wlipc.RequestClipboardClear()
		clipboardHistoryCache = nil
		p.close()
	})
	clearBtn.Importance = widget.LowImportance

	header := container.NewBorder(nil, nil, nil, clearBtn, entry)

	// Empty state
	var content fyne.CanvasObject
	if len(p.allEntries) == 0 {
		emptyLabel := widget.NewLabel("Clipboard history is empty.\nCopy some text to see it here.")
		emptyLabel.Alignment = fyne.TextAlignCenter
		content = container.NewBorder(header, nil, nil, nil, emptyLabel)
	} else {
		content = container.NewBorder(header, nil, nil, nil, p.list)
	}

	pickerSize := fyne.NewSize(300, 400)

	fyne.Do(func() {
		win.SetContent(content)

		// Center on screen
		screen := fynedesk.Instance().Screens().Primary()
		centerX := float32(screen.Width)/2 - pickerSize.Width/2
		centerY := float32(screen.Height)/2 - pickerSize.Height/2
		pos := fyne.NewPos(centerX, centerY)
		wm := fynedesk.Instance().WindowManager()
		wm.ShowOverlay(win, pickerSize, pos)
		// Register cleanup so clipboardPicker is cleared when the compositor
		// dismisses the overlay externally (e.g. focus loss).
		if ewm, ok := wm.(*embededWM); ok {
			ewm.onOverlayClosed = func() {
				clipboardPicker = nil
			}
		}

		win.Canvas().Focus(entry)
		go ensureFocused(win, entry)
	})
}

// clipFirstLine extracts the first line of text, truncated for display.
func clipFirstLine(text string) string {
	line := text
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		line = text[:idx]
	}
	if len(line) > 30 {
		line = line[:30] + "…"
	}
	return line
}

// clipRelativeTime formats a timestamp as a relative time string.
func clipRelativeTime(timestamp int64) string {
	t := time.UnixMilli(timestamp)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
