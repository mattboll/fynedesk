// Package clipboard provides a clipboard manager module with history and quick paste access.
package clipboard

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/ui"
)

func init() {
	tyde.RegisterModule(clipMeta)
}

var clipMeta = tyde.ModuleMetadata{
	Name:        "Clipboard",
	NewInstance: newClipboard,
}

type clip struct {
	btn *widget.Button
}

func newClipboard() tyde.Module {
	c := &clip{}
	c.btn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		ui.ShowClipboardManager()
	})
	c.btn.Importance = widget.LowImportance
	return c
}

func (c *clip) Metadata() tyde.ModuleMetadata {
	return clipMeta
}

func (c *clip) StatusAreaWidget() fyne.CanvasObject {
	return c.btn
}

func (c *clip) Destroy() {
}
