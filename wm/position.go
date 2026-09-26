package wm

import (
	"fyshos.com/tyde"
	"fyshos.com/tyde/theme"
)

// PositionForNewWindow returns the suggested position for a new window of the given geometry.
// The screen list hints at available space, but normally list.Active() is the best.
func PositionForNewWindow(win tyde.Window, x, y int, w, h uint, decorated bool,
	screens tyde.ScreenList,
) (int, int, uint, uint) {
	target := screens.Active()
	var offX, offY int
	// A parent that knows its geometry (an X11 window; the interface avoids
	// an import cycle) centres the window; any other, the screen.
	if parent, ok := win.Parent().(interface{ Geometry() (int, int, uint, uint) }); ok && parent != nil {
		wx, wy, ww, wh := parent.Geometry()
		offX, offY = positionInRect(w, h, wx, wy, ww, wh)
	} else {
		offX, offY = positionInRect(w, h, target.X, target.Y, uint(target.Width), uint(target.Height))
	}
	if decorated {
		offX -= ScaleToPixels(theme.BorderWidth, target)
		offY -= ScaleToPixels(theme.TitleHeight, target)
	}

	return offX, offY, w, h
}

func positionInRect(ww, hh uint, x, y int, w, h uint) (int, int) {
	return x + (int(w)-int(ww))/2, y + (int(h)-int(hh))/2
}
