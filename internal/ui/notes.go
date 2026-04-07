package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/modules/notes"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

func init() {
	notes.SetShowOverlay(ShowNotesOverlay)
	notes.SetWidgetContent(NotesWidgetPanelContent)
}

var notesOverlay *notesWindow

type notesWindow struct {
	win      fyne.Window
	store    *notes.Store
	list     *widget.List
	editor   *widget.Entry
	titleEnt *widget.Entry
	noteIDs  []string
	selected string
	onClose  func()
}

func (n *notesWindow) close() {
	n.saveCurrentNote()
	notesOverlay = nil
	if n.onClose != nil {
		n.onClose()
	}
	if n.win != nil {
		n.win.Close()
	}
}

func (n *notesWindow) saveCurrentNote() {
	if n.selected == "" {
		return
	}
	n.store.Update(n.selected, n.titleEnt.Text, n.editor.Text)
}

func (n *notesWindow) refreshList() {
	allNotes := n.store.Notes()
	n.noteIDs = make([]string, len(allNotes))
	for i, note := range allNotes {
		n.noteIDs[i] = note.ID
	}
	n.list.Refresh()
}

func (n *notesWindow) selectNote(id string) {
	n.saveCurrentNote()
	note, ok := n.store.Get(id)
	if !ok {
		return
	}
	n.selected = id
	n.titleEnt.SetText(note.Title)
	n.editor.SetText(note.Body)
}

func (n *notesWindow) addNote() {
	n.saveCurrentNote()
	note := n.store.Add("New note")
	n.refreshList()
	n.selectNote(note.ID)
	for i, id := range n.noteIDs {
		if id == note.ID {
			n.list.Select(i)
			break
		}
	}
}

func (n *notesWindow) deleteNote() {
	if n.selected == "" {
		return
	}
	n.store.Delete(n.selected)
	n.selected = ""
	n.editor.SetText("")
	n.titleEnt.SetText("")
	n.refreshList()
	if len(n.noteIDs) > 0 {
		n.list.Select(0)
	}
}

// ShowNotesOverlay opens (or toggles) the notes editor as a real window
// with compositor decorations (title bar, close button, movable, resizable).
func ShowNotesOverlay(store *notes.Store, onClose func()) {
	if notesOverlay != nil {
		notesOverlay.close()
		return
	}

	win := fyne.CurrentApp().NewWindow("Notes")
	win.SetPadded(true)

	n := &notesWindow{
		win:     win,
		store:   store,
		onClose: onClose,
	}
	notesOverlay = n

	// Note list (left panel)
	allNotes := store.Notes()
	n.noteIDs = make([]string, len(allNotes))
	for i, note := range allNotes {
		n.noteIDs[i] = note.ID
	}

	n.list = widget.NewList(
		func() int { return len(n.noteIDs) },
		func() fyne.CanvasObject {
			lbl := widget.NewLabel("")
			lbl.Truncation = fyne.TextTruncateEllipsis
			return lbl
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(n.noteIDs) {
				return
			}
			note, ok := store.Get(n.noteIDs[id])
			if !ok {
				return
			}
			lbl := obj.(*widget.Label)
			title := note.Title
			if title == "" {
				title = firstLine(note.Body)
			}
			if title == "" {
				title = "(empty)"
			}
			done, total := countTodos(note.Body)
			if total > 0 {
				title += " [" + itoa(done) + "/" + itoa(total) + "]"
			}
			lbl.SetText(title)
		},
	)
	n.list.OnSelected = func(id widget.ListItemID) {
		if id < len(n.noteIDs) {
			n.selectNote(n.noteIDs[id])
		}
	}

	// Add/delete buttons above list
	addBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), n.addNote)
	addBtn.Importance = widget.LowImportance
	delBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), n.deleteNote)
	delBtn.Importance = widget.LowImportance
	listToolbar := container.NewHBox(addBtn, delBtn)
	leftPanel := container.NewBorder(listToolbar, nil, nil, nil, n.list)

	// Title entry
	n.titleEnt = widget.NewEntry()
	n.titleEnt.SetPlaceHolder("Title...")
	n.titleEnt.OnChanged = func(_ string) {
		n.saveCurrentNote()
		n.refreshList()
	}

	// Checkbox toggle button
	checkBtn := widget.NewButtonWithIcon("", theme.CheckButtonCheckedIcon(), func() {
		n.toggleCheckboxLines()
	})
	checkBtn.Importance = widget.LowImportance

	titleRow := container.NewBorder(nil, nil, nil, checkBtn, n.titleEnt)

	// Editor
	n.editor = widget.NewMultiLineEntry()
	n.editor.SetPlaceHolder("Write here...\n\nUse [ ] for todos, [x] for done")
	n.editor.Wrapping = fyne.TextWrapWord
	n.editor.OnChanged = func(_ string) {
		n.saveCurrentNote()
		n.refreshList()
	}

	rightPanel := container.NewBorder(titleRow, nil, nil, nil, n.editor)

	// Split layout
	split := container.NewHSplit(leftPanel, rightPanel)
	split.Offset = 0.25

	win.SetContent(split)
	win.Resize(fyne.NewSize(550, 420))
	win.SetOnClosed(func() {
		if notesOverlay != nil {
			notesOverlay.saveCurrentNote()
			notesOverlay = nil
			if onClose != nil {
				onClose()
			}
		}
		refreshNotesPanel()
	})

	// Position to the left of the widget panel
	screen := fynedesk.Instance().Screens().Primary()
	widgetW := float32(196)
	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		widgetW = 36
	}
	winW := float32(550)
	winH := float32(420)
	posX := float32(screen.Width) - widgetW - winW - 10
	posY := float32(screen.Height)/2 - winH/2
	wlipc.RequestOverlayPosition(win.Title(), posX, posY, winW, winH)

	win.Show()

	// Select first note
	if len(n.noteIDs) > 0 {
		n.list.Select(0)
	}
}

// toggleCheckboxLines converts plain lines to "[ ] line" or removes "[ ] "/"[x] " prefixes.
func (n *notesWindow) toggleCheckboxLines() {
	text := n.editor.Text
	lines := strings.Split(text, "\n")
	hasTodos := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[ ] ") || strings.HasPrefix(trimmed, "[x] ") {
			hasTodos = true
			break
		}
	}

	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if hasTodos {
			trimmed = strings.TrimPrefix(trimmed, "[ ] ")
			trimmed = strings.TrimPrefix(trimmed, "[x] ")
			result = append(result, trimmed)
		} else {
			if trimmed != "" {
				result = append(result, "[ ] "+trimmed)
			} else {
				result = append(result, "")
			}
		}
	}
	n.editor.SetText(strings.Join(result, "\n"))
}

var (
	notesQuickEdit    fyne.Window
	notesPanelWrapper *fyne.Container // stable container returned to the widget panel
	notesPanelStore   *notes.Store
	notesPanelOnTap   func()
)

// refreshNotesPanel rebuilds the preview content inside the stable wrapper
// container, so the widget panel reflects the current note state.
func refreshNotesPanel() {
	if notesPanelWrapper == nil || notesPanelStore == nil {
		return
	}
	content := buildNotesPanelContent(notesPanelStore, notesPanelOnTap)
	fyne.Do(func() {
		notesPanelWrapper.Objects = []fyne.CanvasObject{content}
		notesPanelWrapper.Refresh()
	})
}

// NotesWidgetPanelContent returns a stable container whose content is rebuilt
// dynamically when notes change. The container survives widget panel reloads.
func NotesWidgetPanelContent(store *notes.Store, onTap func()) fyne.CanvasObject {
	notesPanelStore = store
	notesPanelOnTap = onTap
	content := buildNotesPanelContent(store, onTap)
	notesPanelWrapper = container.NewStack(content)
	return notesPanelWrapper
}

// buildNotesPanelContent creates the note preview widgets (read-only).
func buildNotesPanelContent(store *notes.Store, onTap func()) fyne.CanvasObject {
	allNotes := store.Notes()
	if len(allNotes) == 0 {
		btn := widget.NewButtonWithIcon("", theme.DocumentIcon(), onTap)
		btn.Importance = widget.LowImportance
		return btn
	}

	// Find most recently updated note
	latest := allNotes[0]
	for _, n := range allNotes[1:] {
		if n.UpdatedAt > latest.UpdatedAt {
			latest = n
		}
	}
	noteID := latest.ID

	// Header: title + expand button to open full multi-note overlay
	expandBtn := widget.NewButtonWithIcon("", theme.ZoomInIcon(), onTap)
	expandBtn.Importance = widget.LowImportance
	title := widget.NewLabel(latest.Title)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Truncation = fyne.TextTruncateEllipsis
	header := container.NewBorder(nil, nil, nil, expandBtn, title)

	// Body: render lines as checkboxes or labels
	lines := strings.Split(latest.Body, "\n")
	var items []fyne.CanvasObject
	maxLines := 8
	shown := 0
	for _, line := range lines {
		if shown >= maxLines {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[ ] ") || strings.HasPrefix(trimmed, "[x] ") {
			checked := strings.HasPrefix(trimmed, "[x] ")
			label := trimmed[4:]
			chk := widget.NewCheck(label, nil)
			chk.Checked = checked
			localLabel := label
			localChecked := checked
			chk.OnChanged = func(_ bool) {
				note, ok := store.Get(noteID)
				if !ok {
					return
				}
				var old, repl string
				if localChecked {
					old = "[x] " + localLabel
					repl = "[ ] " + localLabel
				} else {
					old = "[ ] " + localLabel
					repl = "[x] " + localLabel
				}
				store.Update(noteID, "", strings.Replace(note.Body, old, repl, 1))
			}
			items = append(items, chk)
		} else {
			lbl := widget.NewLabel(trimmed)
			lbl.Wrapping = fyne.TextWrapWord
			lbl.Truncation = fyne.TextTruncateEllipsis
			items = append(items, lbl)
		}
		shown++
	}
	if len(items) == 0 {
		items = append(items, widget.NewLabel("(empty)"))
	}

	body := container.NewVBox(items...)

	openQuickEdit := func() {
		showNotesQuickEdit(store, noteID)
	}
	content := container.NewBorder(header, nil, nil, nil, body)
	return newTappableContainer(content, openQuickEdit)
}

// showNotesQuickEdit opens a small floating editor popup next to the widget
// panel for quick editing. Gets proper keyboard focus from the compositor.
func showNotesQuickEdit(store *notes.Store, noteID string) {
	// Toggle off if already open
	if notesQuickEdit != nil {
		notesQuickEdit.Close()
		notesQuickEdit = nil
		return
	}

	note, ok := store.Get(noteID)
	if !ok {
		return
	}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}
	win := d.CreateSplashWindow()
	win.SetPadded(true)
	win.SetTitle("Quick Notes " + SkipTaskbarHint)

	entry := widget.NewMultiLineEntry()
	entry.SetText(note.Body)
	entry.Wrapping = fyne.TextWrapWord
	entry.SetMinRowsVisible(10)
	entry.OnChanged = func(text string) {
		store.Update(noteID, "", text)
	}

	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		win.Close()
	})
	closeBtn.Importance = widget.LowImportance

	titleLbl := widget.NewLabel(note.Title)
	titleLbl.TextStyle = fyne.TextStyle{Bold: true}
	hdr := container.NewBorder(nil, nil, nil, closeBtn, titleLbl)

	win.SetContent(container.NewBorder(hdr, nil, nil, nil, entry))
	win.SetOnClosed(func() {
		notesQuickEdit = nil
		refreshNotesPanel()
	})

	// Position next to the widget panel
	screen := fynedesk.Instance().Screens().Primary()
	widgetW := wmtheme.WidgetPanelWidth
	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		widgetW = wmtheme.NarrowBarWidth
	}
	popW := float32(300)
	popH := float32(350)
	posX := float32(screen.Width)/screen.CanvasScale() - widgetW - popW - 10
	posY := float32(screen.Height)/screen.CanvasScale()/2 - popH/2

	wlipc.RequestOverlayPosition(win.Title(), posX, posY, popW, popH)
	win.Show()
	notesQuickEdit = win

	win.Canvas().Focus(entry)
	go ensureFocused(win, entry)
}

func countTodos(body string) (done, total int) {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[ ] ") {
			total++
		} else if strings.HasPrefix(trimmed, "[x] ") {
			total++
			done++
		}
	}
	return
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
