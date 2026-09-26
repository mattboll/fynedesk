//go:generate fyne bundle -package theme -o bundled.go assets/

// Package theme defines the Tyde theme with custom colors, fonts, and bundled assets for the desktop environment.
package theme // import "fyshos.com/tyde/theme"

import (
	"image/color"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// Desktop-specific theme color names. These can be set in theme.json files
// alongside standard Fyne color names.
const (
	// ColorNamePanelBackground is used in themes to look up the background color
	ColorNamePanelBackground fyne.ThemeColorName = "tydePanelBackground"

	// Notification toast colors
	ColorNameToastBackground fyne.ThemeColorName = "tydeToastBackground"
	ColorNameToastTitle      fyne.ThemeColorName = "tydeToastTitle"
	ColorNameToastBody       fyne.ThemeColorName = "tydeToastBody"
	ColorNameToastBorder     fyne.ThemeColorName = "tydeToastBorder"
	ColorNameAccentGlow      fyne.ThemeColorName = "tydeAccentGlow"

	// Sidebar colors
	ColorNameSidebarBackground fyne.ThemeColorName = "tydeSidebarBackground"
	ColorNameSidebarBorder     fyne.ThemeColorName = "tydeSidebarBorder"
	ColorNameSidebarSeparator  fyne.ThemeColorName = "tydeSidebarSeparator"
	ColorNameSectionLabel      fyne.ThemeColorName = "tydeSectionLabel"

	// Compositor titlebar colors
	ColorNameTitlebarActive   fyne.ThemeColorName = "tydeTitlebarActive"
	ColorNameTitlebarInactive fyne.ThemeColorName = "tydeTitlebarInactive"
	ColorNameTitlebarText     fyne.ThemeColorName = "tydeTitlebarText"

	// General UI accent
	ColorNameBadge fyne.ThemeColorName = "tydeBadge"
)

var (
	// PointerDefault is the standard pointer resource
	PointerDefault = resourcePointerPng

	// FyneLogo is the fyne tooklit icon
	FyneLogo = resourceFynePng
	// FyshOSLogo is the fyne tooklit icon
	FyshOSLogo = resourceFishLogoPng
	// LogoFade is the logo with a semi-transparent background (faded).
	LogoFade = resourceLogoFadePng
	// AppIcon is the image for this application icon
	AppIcon = resourceIconPng

	// BatteryIcon is the material design icon for battery in light and dark theme
	BatteryIcon = theme.NewThemedResource(resourceBatterySvg)
	// BrightnessIcon is the material design icon for brightness in light and dark theme
	BrightnessIcon = theme.NewThemedResource(resourceBrightnessSvg)
	// CalculateIcon is the material design icon for a calculator in light and dark theme
	CalculateIcon = theme.NewThemedResource(resourceCalculateSvg)
	// DisplayIcon is the material design icon for computer displays in light and dark theme
	DisplayIcon = theme.NewThemedResource(resourceDisplaySvg)
	// LaptopIcon is the material design icon for a portable computer
	LaptopIcon = theme.NewThemedResource(resourceLaptopSvg)
	// TabletIcon is the material design icon for a tablet computer
	TabletIcon = theme.NewThemedResource(resourceTabletSvg)
	// InternetIcon is the material design icon for the internet in light and dark theme
	InternetIcon = theme.NewThemedResource(resourceInternetSvg)
	// EthernetIcon is the material design icon for a network connection
	EthernetIcon = theme.NewThemedResource(resourceEthernetSvg)
	// WifiIcon is the material design icon for a wireless network connection
	WifiIcon = theme.NewThemedResource(resourceWifiSvg)
	// WifiOffIcon is the material design icon for a wireless device without a connection
	WifiOffIcon = theme.NewThemedResource(resourceWifioffSvg)
	// AirplaneIcon is the material design icon for a wireless device in airplane mode
	AirplaneIcon = theme.NewThemedResource(resourceAirplaneSvg)
	// PowerIcon is the material design icon for a power connection in light and dark theme
	PowerIcon = theme.NewThemedResource(resourcePowerSvg)
	// UserIcon is the material design icon for a user in light and dark theme
	UserIcon = theme.NewThemedResource(resourcePersonSvg)

	// BrokenImageIcon is the material design icon for a broken image
	BrokenImageIcon = theme.NewThemedResource(resourceBrokenimageSvg)
	// MaximizeIcon is the material design icon for maximizing a window
	MaximizeIcon = theme.NewThemedResource(resourceMaximizeSvg)
	// IconifyIcon is the material design icon for minimizing a window
	IconifyIcon = theme.NewThemedResource(resourceMinimizeSvg)
	// KeyboardIcon is the material design icon for the keyboard settings
	KeyboardIcon = theme.NewThemedResource(resourceKeyboardSvg)
	// LockIcon is the material design icon for the screen lock icon
	LockIcon = theme.NewThemedResource(resourceLockSvg)
	// ScreensIcon is the material design icon for multiple screens
	ScreensIcon = theme.NewThemedResource(resourceScreensSvg)
	// SoundHighIcon is the material design icon for sound in light and dark theme
	SoundHighIcon = theme.NewThemedResource(resourceSoundHighSvg)
	// SoundMidIcon is the material design icon for sound in light and dark theme
	SoundMidIcon = theme.NewThemedResource(resourceSoundMidSvg)
	// SoundLowIcon is the material design icon for sound in light and dark theme
	SoundLowIcon = theme.NewThemedResource(resourceSoundLowSvg)
	// MuteIcon is the material design icon for mute in light and dark theme
	MuteIcon = theme.NewThemedResource(resourceMuteSvg)
	// WallpaperIcon is the material design icon for a desktop wallpaper
	WallpaperIcon = theme.NewThemedResource(resourceWallpaperSvg)
	// ClockIcon is the material design icon for time and date settings
	ClockIcon = theme.NewThemedResource(resourceClockSvg)
	// UpdateIcon is the material design icon for available system updates
	UpdateIcon = theme.NewThemedResource(resourceUpdateSvg)
	// NotificationsIcon is the material design bell icon for notifications
	NotificationsIcon = theme.NewThemedResource(resourceNotificationsSvg)

	// BorderWidth is the width of window frames
	BorderWidth = float32(4)
	// NarrowBarWidth is the size for the bars in narrow layout
	NarrowBarWidth = float32(36)
	// WidgetPanelWidth defines how wide the large widget panel should be
	WidgetPanelWidth = float32(196)
)

// The frame metrics that SetTouchScreen chooses between. A finger needs a much
// bigger target than a pointer, so a touch screen gets a taller title bar with
// bigger buttons in it. The buttons are sized apart from the bar as they are
// centred in it, so growing the bar alone would leave them small.
const (
	buttonWidth      = float32(32)
	buttonWidthTouch = float32(44)

	titleHeight      = float32(28)
	titleHeightTouch = float32(48)

	titleButtonHeight      = float32(16)
	titleButtonHeightTouch = float32(36)

	titleButtonIconSize      = float32(14)
	titleButtonIconSizeTouch = float32(22)
)

// touchScreen is whether the frames are sized for fingers: set by the
// settings, read by the frames as they draw (from other goroutines on X11).
var touchScreen atomic.Bool

// SetTouchScreen configures the window frame metrics for the input the user
// has. On a touch screen the title bar and the window buttons drawn in it grow
// big enough to tap; anywhere else they return to the pointer sizes.
func SetTouchScreen(touch bool) {
	touchScreen.Store(touch)
}

// ButtonWidth is the width of window buttons.
func ButtonWidth() float32 {
	if touchScreen.Load() {
		return buttonWidthTouch
	}
	return buttonWidth
}

// TitleHeight is the height of a frame titleBar.
func TitleHeight() float32 {
	if touchScreen.Load() {
		return titleHeightTouch
	}
	return titleHeight
}

// TitleButtonHeight is the size of the buttons drawn in a frame titleBar.
func TitleButtonHeight() float32 {
	if touchScreen.Load() {
		return titleButtonHeightTouch
	}
	return titleButtonHeight
}

// TitleButtonIconSize is the size of the icon inside a titleBar button.
func TitleButtonIconSize() float32 {
	if touchScreen.Load() {
		return titleButtonIconSizeTouch
	}
	return titleButtonIconSize
}

// WindowShadow returns the shadow drawn beneath a window frame - deeper when
// the window is active - so that overlay dialogs (app switcher, launcher,
// menus) float the same way as a focused window. The values match the shadows
// of a Fyne container.InnerWindow.
func WindowShadow(active bool) canvas.Shadow {
	shadow := canvas.Shadow{Color: theme.Color(theme.ColorNameShadow)}
	if active {
		shadow.Offset = fyne.NewPos(2, 5)
		shadow.BlurRadius = 20
		shadow.Spread = 10
	} else {
		shadow.Offset = fyne.NewPos(1, 2)
		shadow.BlurRadius = 8
		shadow.Spread = 3
	}
	return shadow
}

// deskColor looks up a desktop-specific color from the active Fyne theme.
// Returns the fallback if the theme doesn't define the color.
func deskColor(name fyne.ThemeColorName, fallback color.Color) color.Color {
	if th := fyne.CurrentApp().Settings().Theme(); th != nil {
		variant := fyne.CurrentApp().Settings().ThemeVariant()
		// Themes written before the move to Tyde use the "fynedesk" prefix.
		legacy := fyne.ThemeColorName("fynedesk" + strings.TrimPrefix(string(name), "tyde"))
		for _, n := range []fyne.ThemeColorName{name, legacy} {
			if col := th.Color(n, variant); col != nil && col != color.Transparent {
				return col
			}
		}
	}
	return fallback
}

// WidgetPanelBackground returns the semi-transparent background matching the users current theme
func WidgetPanelBackground() color.Color {
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	if th := fyne.CurrentApp().Settings().Theme(); th != nil {
		// Respond on known colours and legacy names too
		for _, name := range []fyne.ThemeColorName{ColorNamePanelBackground, "fynedeskPanelBackground"} {
			if col := th.Color(name, variant); col != color.Transparent { // non-transparent means found
				return Glassy(col)
			}
		}
	}

	if variant == theme.VariantLight {
		return Glassy(color.RGBA{0xaa, 0xaa, 0xaa, 0xee})
	}
	return Glassy(color.RGBA{0x24, 0x24, 0x24, 0xee})
}

// ToastBackground returns the notification toast background color.
func ToastBackground() color.Color {
	return Glassy(deskColor(ColorNameToastBackground, color.NRGBA{R: 10, G: 12, B: 22, A: 235}))
}

// ToastTitle returns the notification toast title text color.
func ToastTitle() color.Color {
	return deskColor(ColorNameToastTitle, color.NRGBA{R: 235, G: 240, B: 255, A: 255})
}

// ToastBody returns the notification toast body text color.
func ToastBody() color.Color {
	return deskColor(ColorNameToastBody, color.NRGBA{R: 150, G: 165, B: 190, A: 255})
}

// ToastBorder returns the notification toast border color.
func ToastBorder() color.Color {
	return deskColor(ColorNameToastBorder, color.NRGBA{R: 50, G: 65, B: 100, A: 80})
}

// AccentGlow returns the accent glow color (used on notification bar, highlights).
func AccentGlow() color.Color {
	return deskColor(ColorNameAccentGlow, color.NRGBA{R: 0, G: 180, B: 255, A: 220})
}

// SidebarBackground returns the sidebar panel background color.
func SidebarBackground() color.Color {
	return Glassy(deskColor(ColorNameSidebarBackground, color.NRGBA{R: 18, G: 20, B: 30, A: 240}))
}

// SidebarBorder returns the sidebar border accent color.
func SidebarBorder() color.Color {
	return deskColor(ColorNameSidebarBorder, color.NRGBA{R: 50, G: 65, B: 100, A: 80})
}

// SidebarSeparator returns the sidebar separator line color.
func SidebarSeparator() color.Color {
	return deskColor(ColorNameSidebarSeparator, color.NRGBA{R: 50, G: 65, B: 100, A: 60})
}

// SectionLabelColor returns the section label text color.
func SectionLabelColor() color.Color {
	return deskColor(ColorNameSectionLabel, color.NRGBA{R: 120, G: 140, B: 180, A: 255})
}

// BadgeColor returns the notification badge dot color.
func BadgeColor() color.Color {
	return deskColor(ColorNameBadge, color.NRGBA{R: 220, G: 40, B: 40, A: 255})
}
