package tyde

import (
	"fyne.io/fyne/v2"
	"fyshos.com/tyde/wlipc"
)

// The types of computer that Tyde can be configured for.
const (
	// ComputerDesktop is a machine with no battery and no touch screen.
	ComputerDesktop = "Desktop"
	// ComputerLaptop is a portable computer - it has a battery, but is driven
	// with a keyboard and pointer.
	ComputerLaptop = "Laptop"
	// ComputerTablet is a mobile computer - a battery and a touch screen.
	ComputerTablet = "Tablet"
)

// DeskSettings describes the configuration options available for Fyne desktop
type DeskSettings interface {
	Background() string
	BackgroundFill() string
	BackgroundColor() string
	IconTheme() string
	BorderButtonPosition() string
	ClockFormatting() string
	ClockShowSeconds() bool
	ComputerType() string
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

	PowerLockTimeout() int      // Minutes idle before lock (0 = never)
	PowerBlankTimeout() int     // Minutes idle before display blank (0 = never)
	PowerSuspendTimeout() int   // Minutes idle before auto-suspend (0 = never)
	PowerSuspendAction() string // "suspend", "hibernate", "hybrid-sleep", "nothing"

	AddChangeListener(listener func(DeskSettings))
}
