package emoji

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	emojidata "fyshos.com/tyde/internal/emoji"
)

// cellSize is the width and height of one emoji in the grid, and glyphSize the
// type size drawn inside it. Big enough to read a face at a glance, small enough
// that ten fit across a picker that does not dominate the screen.
const (
	cellSize  = 44
	glyphSize = 26
)

// emojiCell is one tappable emoji in the grid. It is deliberately lighter than a
// widget.Button: no border, no shadow, just the glyph and a highlight under the
// pointer, so a grid of them reads as a sheet of emoji rather than a wall of
// buttons.
type emojiCell struct {
	widget.BaseWidget

	text *canvas.Text
	bg   *canvas.Rectangle

	onTap   func()
	onHover func(bool)
}

func newEmojiCell() *emojiCell {
	c := &emojiCell{
		text: &canvas.Text{TextSize: glyphSize, Alignment: fyne.TextAlignCenter},
		bg:   canvas.NewRectangle(color.Transparent),
	}
	c.bg.CornerRadius = theme.SelectionRadiusSize()
	c.ExtendBaseWidget(c)
	return c
}

// SetEmoji points the cell at a different emoji - called as the grid recycles
// cells while scrolling, so it must reset every piece of per-item state.
func (c *emojiCell) SetEmoji(e emojidata.Emoji, onTap func(), onHover func(bool)) {
	c.onTap = onTap
	c.onHover = onHover
	c.text.Text = e.Character
	c.text.Refresh()
}

func (c *emojiCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(c.bg, container.NewCenter(c.text)))
}

func (c *emojiCell) MinSize() fyne.Size {
	return fyne.NewSquareSize(cellSize)
}

func (c *emojiCell) Tapped(_ *fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *emojiCell) MouseIn(_ *desktop.MouseEvent) {
	c.bg.FillColor = theme.Color(theme.ColorNameHover)
	c.bg.Refresh()
	if c.onHover != nil {
		c.onHover(true)
	}
}

func (c *emojiCell) MouseMoved(_ *desktop.MouseEvent) {}

func (c *emojiCell) MouseOut() {
	c.bg.FillColor = color.Transparent
	c.bg.Refresh()
	if c.onHover != nil {
		c.onHover(false)
	}
}
