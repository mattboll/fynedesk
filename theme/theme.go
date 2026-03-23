//go:generate fyne bundle -package theme -o bundled.go assets/

// Package theme defines the FyneDesk theme with custom colors, fonts, and bundled assets for the desktop environment.
package theme // import "fyshos.com/fynedesk/theme"

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Desktop-specific theme color names. These can be set in theme.json files
// alongside standard Fyne color names.
const (
	// ColorNamePanelBackground is used in themes to look up the background color
	ColorNamePanelBackground fyne.ThemeColorName = "fynedeskPanelBackground"

	// Notification toast colors
	ColorNameToastBackground fyne.ThemeColorName = "fynedeskToastBackground"
	ColorNameToastTitle      fyne.ThemeColorName = "fynedeskToastTitle"
	ColorNameToastBody       fyne.ThemeColorName = "fynedeskToastBody"
	ColorNameToastBorder     fyne.ThemeColorName = "fynedeskToastBorder"
	ColorNameAccentGlow      fyne.ThemeColorName = "fynedeskAccentGlow"

	// Sidebar colors
	ColorNameSidebarBackground fyne.ThemeColorName = "fynedeskSidebarBackground"
	ColorNameSidebarBorder     fyne.ThemeColorName = "fynedeskSidebarBorder"
	ColorNameSidebarSeparator  fyne.ThemeColorName = "fynedeskSidebarSeparator"
	ColorNameSectionLabel      fyne.ThemeColorName = "fynedeskSectionLabel"

	// Compositor titlebar colors
	ColorNameTitlebarActive   fyne.ThemeColorName = "fynedeskTitlebarActive"
	ColorNameTitlebarInactive fyne.ThemeColorName = "fynedeskTitlebarInactive"
	ColorNameTitlebarText     fyne.ThemeColorName = "fynedeskTitlebarText"

	// General UI accent
	ColorNameBadge fyne.ThemeColorName = "fynedeskBadge"
)

var (
	// PointerDefault is the standard pointer resource
	PointerDefault = resourcePointerPng

	// FyneLogo is the fyne tooklit icon
	FyneLogo = resourceFynePng
	// FyshOSLogo is the fyne tooklit icon
	FyshOSLogo = resourceFishonwhitePng
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
	// InternetIcon is the material design icon for the internet in light and dark theme
	InternetIcon = theme.NewThemedResource(resourceInternetSvg)
	// EthernetIcon is the material design icon for a network connection
	EthernetIcon = theme.NewThemedResource(resourceEthernetSvg)
	// WifiIcon is the material design icon for a wireless network connection
	WifiIcon = theme.NewThemedResource(resourceWifiSvg)
	// WifiOffIcon is the material design icon for a wireless device without a connection
	WifiOffIcon = theme.NewThemedResource(resourceWifioffSvg)
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
	// SoundIcon is the material design icon for sound in light and dark theme
	SoundIcon = theme.NewThemedResource(resourceSoundSvg)
	// MuteIcon is the material design icon for mute in light and dark theme
	MuteIcon = theme.NewThemedResource(resourceMuteSvg)
	// NotificationsIcon is the material design bell icon for notifications
	NotificationsIcon = theme.NewThemedResource(resourceNotificationsSvg)

	// BorderWidth is the width of window frames
	BorderWidth = float32(4)
	// ButtonWidth is the width of window buttons
	ButtonWidth = float32(32)
	// NarrowBarWidth is the size for the bars in narrow layout
	NarrowBarWidth = float32(36)
	// TitleHeight is the height of a frame titleBar
	TitleHeight = float32(28)
	// WidgetPanelWidth defines how wide the large widget panel should be
	WidgetPanelWidth = float32(196)
)

// deskColor looks up a desktop-specific color from the active Fyne theme.
// Returns the fallback if the theme doesn't define the color.
func deskColor(name fyne.ThemeColorName, fallback color.Color) color.Color {
	if th := fyne.CurrentApp().Settings().Theme(); th != nil {
		col := th.Color(name, fyne.CurrentApp().Settings().ThemeVariant())
		if col != nil && col != color.Transparent {
			return col
		}
	}
	return fallback
}

// WidgetPanelBackground returns the semi-transparent background matching the users current theme
func WidgetPanelBackground() color.Color {
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	if th := fyne.CurrentApp().Settings().Theme(); th != nil {
		col := th.Color(ColorNamePanelBackground, variant)
		if col != color.Transparent {
			return col
		}
	}

	if variant == theme.VariantLight {
		return color.RGBA{0xaa, 0xaa, 0xaa, 0xee}
	}
	return color.RGBA{0x24, 0x24, 0x24, 0xee}
}

// ToastBackground returns the notification toast background color.
func ToastBackground() color.Color {
	return deskColor(ColorNameToastBackground, color.NRGBA{R: 10, G: 12, B: 22, A: 235})
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
	return deskColor(ColorNameSidebarBackground, color.NRGBA{R: 18, G: 20, B: 30, A: 240})
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

// TitlebarActive returns the active window titlebar background color.
func TitlebarActive() color.Color {
	return deskColor(ColorNameTitlebarActive, color.NRGBA{R: 0x18, G: 0x1d, B: 0x25, A: 0xff})
}

// TitlebarInactive returns the inactive window titlebar background color.
func TitlebarInactive() color.Color {
	return deskColor(ColorNameTitlebarInactive, color.NRGBA{R: 0x28, G: 0x29, B: 0x2e, A: 0xff})
}

// TitlebarTextColor returns the titlebar text color.
func TitlebarTextColor() color.Color {
	return deskColor(ColorNameTitlebarText, color.NRGBA{R: 0xf3, G: 0xf3, B: 0xf3, A: 0xff})
}

// BadgeColor returns the notification badge dot color.
func BadgeColor() color.Color {
	return deskColor(ColorNameBadge, color.NRGBA{R: 220, G: 40, B: 40, A: 255})
}
