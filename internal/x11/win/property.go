//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

package win

import (
	"bytes"
	"math"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/ewmh"
	"github.com/BurntSushi/xgbutil/icccm"
	"github.com/BurntSushi/xgbutil/motif"
	"github.com/BurntSushi/xgbutil/xgraphics"
	"github.com/BurntSushi/xgbutil/xprop"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde"
)

func windowActiveReq(x *xgbutil.XUtil, win xproto.Window) {
	err := ewmh.ActiveWindowReq(x, win)
	if err != nil {
		fyne.LogError("", err)
	}
}

func windowAllowedActionsSet(x *xgbutil.XUtil, win xproto.Window, actions []string) {
	err := ewmh.WmAllowedActionsSet(x, win, actions)
	if err != nil {
		fyne.LogError("", err)
	}
}

func windowBorderless(x *xgbutil.XUtil, win xproto.Window) bool {
	hints, err := motif.WmHintsGet(x, win)
	if err == nil {
		return !motif.Decor(hints)
	}

	return false
}

func windowClass(x *xgbutil.XUtil, win xproto.Window) []string {
	class, err := xprop.PropValStrs(xprop.GetProperty(x, win, "WM_CLASS"))
	if err != nil {
		class, err := xprop.PropValStrs(xprop.GetProperty(x, win, "_NET_WM_CLASS"))
		if err != nil {
			return []string{""}
		}
		return class
	}

	return class
}

func windowCommand(x *xgbutil.XUtil, win xproto.Window) string {
	command, err := xprop.PropValStr(xprop.GetProperty(x, win, "WM_COMMAND"))
	if err != nil {
		command, err := xprop.PropValStr(xprop.GetProperty(x, win, "_NET_WM_COMMAND"))
		if err != nil {
			return ""
		}
		return command
	}

	return command
}

func windowIcon(x *xgbutil.XUtil, win xproto.Window, width int, height int) *bytes.Buffer {
	img, err := xgraphics.FindIcon(x, win, width, height)
	if err != nil {
		return nil
	}

	w := &bytes.Buffer{}
	err = img.WritePng(w)
	if err != nil {
		fyne.LogError("ICON: Could not convert icon to png", err)
		return nil
	}
	return w
}

func windowIconName(x *xgbutil.XUtil, win xproto.Window) string {
	icon, err := icccm.WmIconNameGet(x, win)
	if err != nil {
		icon, err = ewmh.WmIconNameGet(x, win)
		if err != nil {
			return ""
		}
	}

	return icon
}

// sizeCanMaximize reports whether the maximum size of a window, if any,
// lets it cover its screen.
func sizeCanMaximize(c *client) bool {
	screen := tyde.Instance().Screens().ScreenForWindow(c)

	maxWidth, maxHeight := sizeMax(c.sizeHints())
	if maxWidth == -1 && maxHeight == -1 {
		return true
	}
	return maxWidth >= screen.Width && maxHeight >= screen.Height
}

// sizeConstrain keeps a size within the minimum and maximum of the hints.
func sizeConstrain(nh *icccm.NormalHints, width uint16, height uint16) (uint16, uint16) {
	minWidth, minHeight := sizeMin(nh)
	maxWidth, maxHeight := sizeMax(nh)
	if width < uint16(minWidth) {
		width = uint16(minWidth)
	}
	if height < uint16(minHeight) {
		height = uint16(minHeight)
	}
	if maxWidth > -1 && width > uint16(maxWidth) {
		width = uint16(maxWidth)
	}
	if maxHeight > -1 && height > uint16(maxHeight) {
		height = uint16(maxHeight)
	}
	return width, height
}

// sizeFixed reports whether the hints give the window a single size.
func sizeFixed(nh *icccm.NormalHints) bool {
	minWidth, minHeight := sizeMin(nh)
	maxWidth, maxHeight := sizeMax(nh)
	return int(minWidth) == maxWidth && int(minHeight) == maxHeight
}

// sizeMax returns the maximum size of the hints, -1 where there is none.
func sizeMax(nh *icccm.NormalHints) (int, int) {
	if nh != nil && nh.Flags&icccm.SizeHintPMaxSize > 0 {
		return int(nh.MaxWidth), int(nh.MaxHeight)
	}
	return -1, -1
}

// sizeMin returns the minimum size of the hints, 0 where there is none.
func sizeMin(nh *icccm.NormalHints) (uint, uint) {
	if nh != nil && nh.Flags&icccm.SizeHintPMinSize > 0 {
		return nh.MinWidth, nh.MinHeight
	}
	return 0, 0
}

// sizeWithIncrement rounds a size to the resize increments of the hints.
func sizeWithIncrement(nh *icccm.NormalHints, width uint16, height uint16) (uint16, uint16) {
	if nh == nil || nh.Flags&icccm.SizeHintPResizeInc == 0 {
		return width, height
	}

	minWidth, minHeight := sizeMin(nh)
	baseWidth, baseHeight := uint16(minWidth), uint16(minHeight)
	if nh.BaseWidth > 0 {
		baseWidth = uint16(nh.BaseWidth)
	}
	if nh.BaseHeight > 0 {
		baseHeight = uint16(nh.BaseHeight)
	}
	// catch uint underflow
	if width < baseWidth {
		width = baseWidth
	}
	if height < baseHeight {
		height = baseHeight
	}

	if nh.WidthInc > 0 {
		width = baseWidth + uint16(math.Round(float64(width-baseWidth)/float64(nh.WidthInc))*float64(nh.WidthInc))
	}
	if nh.HeightInc > 0 {
		height = baseHeight + uint16(math.Round(float64(height-baseHeight)/float64(nh.HeightInc))*float64(nh.HeightInc))
	}
	return width, height
}

func windowStateGet(x *xgbutil.XUtil, win xproto.Window) uint {
	state, err := icccm.WmStateGet(x, win)
	if err != nil {
		return icccm.StateNormal
	}
	return state.State
}

func windowStateSet(x *xgbutil.XUtil, win xproto.Window, state uint) {
	err := icccm.WmStateSet(x, win, &icccm.WmState{State: state})
	if err != nil {
		fyne.LogError("", err)
	}
}
