package theme

import (
	"image/color"
	"sync/atomic"
)

// GlassAlpha is the most opaque the backgrounds of the panel, the menus and
// the notifications are when they are frosted glass.
const GlassAlpha = 0xb8

var glass atomic.Bool

// SetGlass makes the backgrounds of the panel, the menus and the
// notifications translucent, for the compositor blurs what lies behind them.
func SetGlass(on bool) {
	glass.Store(on)
}

// Glass reports whether the backgrounds are frosted glass.
func Glass() bool {
	return glass.Load()
}

// Glassy returns c no more opaque than GlassAlpha when the backgrounds are
// frosted glass, c itself otherwise.
func Glassy(c color.Color) color.Color {
	if !glass.Load() {
		return c
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	if n.A > GlassAlpha {
		n.A = GlassAlpha
	}
	return n
}
