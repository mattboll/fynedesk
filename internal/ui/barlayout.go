package ui

import (
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
)

// Declare conformity with Layout interface
var _ fyne.Layout = (*barLayout)(nil)

const (
	iconZoomDistance = 2.5
	separatorWidth   = 2
)

// barLayout returns a layout used for zooming linear groups of icons
type barLayout struct {
	bar *bar

	mouseInside   bool          // Is the mouse inside of the layout?
	mousePosition fyne.Position // Current coordinates of the mouse cursor
}

// setPointerInside tells the barLayout that the mouse is inside of the Layout.
func (bl *barLayout) setPointerInside(inside bool) {
	bl.mouseInside = inside
}

// setPointerPosition tells the barLayout that the mouse position has been updated.
func (bl *barLayout) setPointerPosition(position fyne.Position) {
	bl.mousePosition = position
}

// Layout is called to pack all icons into a specified size.  It also handles the zooming effect of the icons.
func (bl *barLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	narrow := fynedesk.Instance().Settings().BarPosition() == "left"
	bg := objects[0]
	objects = objects[1:]
	x := theme.Padding()
	if narrow {
		bl.layoutNarrowBar(objects)
	} else {
		x, _ = bl.layoutFullBar(size, objects)
	}

	zoomLeft := x
	for _, child := range objects {
		width := child.Size().Width
		height := child.Size().Height

		if narrow {
			child.Move(fyne.NewPos(theme.Padding(), x))
			x += height + theme.Padding()
		} else {
			// Bottom-aligned: all icons anchor at the bar's bottom edge.
			// Zoomed icons extend upward (negative y) above the bar — macOS dock behavior.
			child.Move(fyne.NewPos(x, size.Height-height))
			x += width + theme.Padding()
		}
	}
	if narrow {
		bg.Move(fyne.NewPos(0, 0))
		bg.Resize(fyne.NewSize(wmtheme.NarrowBarWidth, size.Height))
	} else {
		// Add extra padding around the pill for visual breathing room
		pillPad := theme.Padding()
		bgW := x - zoomLeft + theme.Padding() + pillPad*2
		bgH := bl.bar.iconSize + pillPad*2
		bg.Move(fyne.NewPos(zoomLeft-theme.Padding()-pillPad, size.Height-bl.bar.iconSize-pillPad))
		bg.Resize(fyne.NewSize(bgW, bgH))
	}
}

// MinSize finds the smallest size that satisfies all the child objects.
// For a barLayout this is the width of the widest item and the height is
// the sum of of all children combined with padding between each.
func (bl *barLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	barWidth := bl.calculateBarWidth(objects)

	if fynedesk.Instance().Settings().BarPosition() == "left" {
		return fyne.NewSize(wmtheme.NarrowBarWidth, barWidth)
	}

	// Always report the base icon size, never the zoomed size. The parent
	// layout (desk.go, secondary_bar.go) positions the bar using fixed
	// coordinates. If MinSize returns the zoomed height, Fyne's layout
	// engine overrides the bar's position, causing the 20px offset bug.
	// The zoom effect is handled internally by barLayout.Layout().
	return fyne.NewSize(barWidth, bl.bar.iconSize)
}

func (bl *barLayout) calculateBarWidth(objects []fyne.CanvasObject) float32 {
	iconCount := float32(len(objects))
	if !bl.bar.disableTaskbar {
		iconCount--
		return (iconCount * (bl.bar.iconSize + theme.Padding())) + separatorWidth
	}

	return iconCount * (bl.bar.iconSize + theme.Padding())
}

// gaussianScale returns a scale factor based on gaussian distribution.
// dist is the distance from cursor to icon center, sigma controls the spread.
// Returns a value between 1.0 and maxScale.
func gaussianScale(dist float64, sigma float64, maxScale float32) float32 {
	g := float32(math.Exp(-dist * dist / (2 * sigma * sigma)))
	return 1.0 + (maxScale-1.0)*g
}

func (bl *barLayout) layoutFullBar(size fyne.Size, icons []fyne.CanvasObject) (x float32, zoom bool) {
	offset := float32(0.0)
	barWidth := bl.calculateBarWidth(icons)
	barLeft := (size.Width - barWidth) / 2
	iconLeft := barLeft

	mouseX := bl.mousePosition.X
	zoom = !bl.bar.disableZoom && bl.mouseInside && mouseX >= barLeft && mouseX < barLeft+barWidth
	sigma := float64(bl.bar.iconSize * iconZoomDistance)
	for _, child := range icons {
		if zoom {
			if _, ok := child.(*canvas.Rectangle); ok {
				child.Resize(fyne.NewSize(separatorWidth, bl.bar.iconSize))
				if iconLeft+separatorWidth+theme.Padding() < mouseX {
					offset += separatorWidth
				} else if iconLeft < mouseX {
					offset += separatorWidth + theme.Padding()
				}
			} else {
				iconCenter := iconLeft + bl.bar.iconSize/2
				dist := float64(mouseX - iconCenter)

				scale := gaussianScale(dist, sigma, bl.bar.iconScale)
				newSize := max(bl.bar.iconSize*scale, bl.bar.iconSize)
				child.Resize(fyne.NewSize(newSize, newSize))

				if iconLeft+bl.bar.iconSize+theme.Padding() < mouseX {
					offset += newSize - bl.bar.iconSize
				} else if iconLeft < mouseX {
					ratio := (mouseX - iconLeft) / (bl.bar.iconSize + theme.Padding())
					offset += (newSize-bl.bar.iconSize)*ratio + theme.Padding()
				}
			}
		} else {
			if _, ok := child.(*canvas.Rectangle); ok {
				child.Resize(fyne.NewSize(separatorWidth, bl.bar.iconSize))
			} else {
				child.Resize(fyne.NewSize(bl.bar.iconSize, bl.bar.iconSize))
			}
		}
		if _, ok := child.(*canvas.Rectangle); ok {
			iconLeft += separatorWidth
		} else {
			iconLeft += bl.bar.iconSize
		}
		iconLeft += theme.Padding()
	}

	return barLeft - offset, zoom
}

func (bl *barLayout) layoutNarrowBar(icons []fyne.CanvasObject) {
	iconSize := wmtheme.NarrowBarWidth - theme.Padding()*2
	iconLeft := theme.Padding()

	for _, child := range icons {
		if _, ok := child.(*canvas.Rectangle); ok {
			child.Resize(fyne.NewSize(iconSize, separatorWidth))
		} else {
			child.Resize(fyne.NewSize(iconSize, iconSize))
		}

		if _, ok := child.(*canvas.Rectangle); ok {
			iconLeft += separatorWidth
		} else {
			iconLeft += iconSize
		}
		iconLeft += theme.Padding()
	}
}

// newBarLayout returns a horizontal icon bar
func newBarLayout(bar *bar) barLayout {
	return barLayout{bar, false, fyne.NewPos(0, 0)}
}
