package fynedesk

import (
	"fyne.io/fyne/v2"
	"fyshos.com/fynedesk/wlipc"
)

// DeskSettings describes the configuration options available for Fyne desktop
type DeskSettings interface {
	Background() string
	IconTheme() string
	BorderButtonPosition() string
	ClockFormatting() string
	ClockShowSeconds() bool
	NarrowWidgetPanel() bool
	NarrowLeftLauncher() bool
	BarPosition() string // "left" (narrow side bar) or "bottom" (full-width taskbar)

	LauncherIcons() []string
	LauncherIconSize() float32
	LauncherDisableTaskbar() bool
	LauncherDisableZoom() bool
	LauncherZoomScale() float32

	DesktopCount() int
	DesktopNames() []string

	ColorScheme() string // "auto", "dark", "light"

	WindowRules() []wlipc.WindowRule

	NaturalScroll() bool
	NightLightEnabled() bool
	NightLightTemperature() int // Kelvin (2700-6500)

	KeyboardLayouts() []string // pipe-separated "layout:variant" pairs, e.g. ["fr:bepo", "us:"]
	KeyboardModifier() fyne.KeyModifier
	ModuleNames() []string
	ScreenSaverType() string
	ScreenSaverClock() bool
	ScreenSaverLabel() string

	ReduceMotion() bool
	Language() string // locale code, e.g. "en", "fr"

	PowerLockTimeout() int    // Minutes idle before lock (0 = never)
	PowerBlankTimeout() int   // Minutes idle before display blank (0 = never)
	PowerSuspendTimeout() int // Minutes idle before auto-suspend (0 = never)
	PowerSuspendAction() string // "suspend", "hibernate", "hybrid-sleep", "nothing"

	AddChangeListener(listener func(DeskSettings))
}
