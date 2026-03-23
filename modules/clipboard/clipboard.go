// Package clipboard provides a clipboard manager module with history and quick paste access.
package clipboard

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/ui"
)

func init() {
	fynedesk.RegisterModule(clipMeta)
}

var clipMeta = fynedesk.ModuleMetadata{
	Name:        "Clipboard",
	NewInstance: newClipboard,
}

type clip struct {
	btn *widget.Button
}

func newClipboard() fynedesk.Module {
	c := &clip{}
	c.btn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		ui.ShowClipboardManager()
	})
	c.btn.Importance = widget.LowImportance
	return c
}

func (c *clip) Metadata() fynedesk.ModuleMetadata {
	return clipMeta
}

func (c *clip) StatusAreaWidget() fyne.CanvasObject {
	return c.btn
}

func (c *clip) Destroy() {
}
