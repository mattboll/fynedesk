package notes

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
)

func init() {
	fynedesk.RegisterModule(notesMeta)
}

var notesMeta = fynedesk.ModuleMetadata{
	Name:        "Notes",
	NewInstance: newNotes,
}

// showOverlay is set by internal/ui to avoid circular imports.
// The ui package calls SetShowOverlay during init.
var showOverlay func(store *Store, onClose func())

// SetShowOverlay registers the UI callback. Called by internal/ui.
func SetShowOverlay(fn func(store *Store, onClose func())) {
	showOverlay = fn
}

// widgetContent is set by internal/ui for the widget panel preview.
var widgetContent func(store *Store, onTap func()) fyne.CanvasObject

// SetWidgetContent registers the widget panel content builder.
func SetWidgetContent(fn func(store *Store, onTap func()) fyne.CanvasObject) {
	widgetContent = fn
}

type notesModule struct {
	store *Store
	btn   *widget.Button
}

func newNotes() fynedesk.Module {
	n := &notesModule{
		store: NewStore(),
	}
	n.btn = widget.NewButtonWithIcon("", theme.DocumentIcon(), func() {
		n.openOverlay()
	})
	n.btn.Importance = widget.LowImportance
	return n
}

func (n *notesModule) Metadata() fynedesk.ModuleMetadata {
	return notesMeta
}

func (n *notesModule) Destroy() {
	n.store.Flush()
}

func (n *notesModule) StatusAreaWidget() fyne.CanvasObject {
	if !fynedesk.Instance().Settings().NarrowWidgetPanel() && widgetContent != nil {
		return widgetContent(n.store, func() { n.openOverlay() })
	}
	return n.btn
}

func (n *notesModule) Shortcuts() map[*fynedesk.Shortcut]func() {
	return map[*fynedesk.Shortcut]func(){
		fynedesk.NewShortcut("Toggle Notes", fyne.KeyN, fynedesk.UserModifier): func() {
			n.openOverlay()
		},
	}
}

func (n *notesModule) openOverlay() {
	if showOverlay != nil {
		showOverlay(n.store, func() {
			// TODO: refresh widget panel after overlay close
		})
	}
}
