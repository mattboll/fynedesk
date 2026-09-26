// design.go defines the Tyde design system: spacing scale,
// typography hierarchy, and icon sizes for consistent UI across the desktop environment.

package theme

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
