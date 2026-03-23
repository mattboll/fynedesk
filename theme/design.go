// design.go defines the FyneDesk design system: semantic colors, spacing scale,
// typography hierarchy, and icon sizes for consistent UI across the desktop environment.

package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
)

// Spacing scale for consistent layout spacing across all UI components.
const (
	SpaceXXS  = float32(2)
	SpaceXS   = float32(4)
	SpaceSM   = float32(8)
	SpaceMD   = float32(12)
	SpaceLG   = float32(16)
	SpaceXL   = float32(24)
	SpaceXXL  = float32(32)
	SpaceHuge = float32(48)
)

// Typography sizes for a clear text hierarchy.
const (
	TextCaption  = float32(10)
	TextBody     = float32(13)
	TextSubtitle = float32(15)
	TextTitle    = float32(18)
	TextHeadline = float32(24)
)

// Icon sizes for consistent iconography.
const (
	IconSM = float32(16)
	IconMD = float32(24)
	IconLG = float32(32)
	IconXL = float32(48)
)

// isDark returns true when the current application theme variant is dark.
// fyne.ThemeVariant(0) corresponds to theme.VariantDark; we use the literal
// to avoid a circular import with fyne.io/fyne/v2/theme.
func isDark() bool {
	return fyne.CurrentApp().Settings().ThemeVariant() == 0
}

// ColorSuccess returns a green semantic color appropriate for the current theme variant.
func ColorSuccess() color.Color {
	if isDark() {
		return color.NRGBA{R: 102, G: 187, B: 106, A: 255} // green 400
	}
	return color.NRGBA{R: 56, G: 142, B: 60, A: 255} // green 700
}

// ColorWarning returns an amber semantic color appropriate for the current theme variant.
func ColorWarning() color.Color {
	if isDark() {
		return color.NRGBA{R: 255, G: 202, B: 40, A: 255} // amber 400
	}
	return color.NRGBA{R: 255, G: 160, B: 0, A: 255} // amber 800
}

// ColorError returns a red semantic color appropriate for the current theme variant.
func ColorError() color.Color {
	if isDark() {
		return color.NRGBA{R: 239, G: 83, B: 80, A: 255} // red 400
	}
	return color.NRGBA{R: 211, G: 47, B: 47, A: 255} // red 700
}

// ColorInfo returns a blue semantic color appropriate for the current theme variant.
func ColorInfo() color.Color {
	if isDark() {
		return color.NRGBA{R: 66, G: 165, B: 245, A: 255} // blue 400
	}
	return color.NRGBA{R: 25, G: 118, B: 210, A: 255} // blue 700
}

// ColorSurface returns a surface/card background color appropriate for the current theme variant.
func ColorSurface() color.Color {
	if isDark() {
		return color.NRGBA{R: 40, G: 40, B: 40, A: 255}
	}
	return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
}

// ColorOnSurface returns a text-on-surface color appropriate for the current theme variant.
func ColorOnSurface() color.Color {
	if isDark() {
		return color.NRGBA{R: 230, G: 230, B: 230, A: 255}
	}
	return color.NRGBA{R: 33, G: 33, B: 33, A: 255}
}
