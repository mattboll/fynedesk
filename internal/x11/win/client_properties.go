//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

// Package win provides X11 window wrapper implementation with client properties, theming, and frame management.
package win

import (
	"fyne.io/fyne/v2"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/icon"
	"fyshos.com/tyde/internal/x11"
)

type clientProperties struct {
	c         *client
	decorated bool
	iconCache fyne.Resource
}

func (c *client) Properties() tyde.WindowProperties {
	if c.props == nil {
		c.props = &clientProperties{c: c}
		c.props.refreshCache()
	}

	return c.props
}

func (c *clientProperties) Class() []string {
	return windowClass(c.c.wm.X(), c.c.win)
}

func (c *clientProperties) Command() string {
	return windowCommand(c.c.wm.X(), c.c.win)
}

func (c *clientProperties) Decorated() bool {
	return c.decorated
}

func (c *clientProperties) lookupDecorated() bool {
	return !windowBorderless(c.c.wm.X(), c.c.win)
}

func (c *clientProperties) Icon() fyne.Resource {
	if c.iconCache != nil {
		return c.iconCache
	}

	xIcon := windowIcon(c.c.wm.X(), c.c.win, 64, 64)
	if xIcon != nil {
		c.iconCache = fyne.NewStaticResource(c.Title(), xIcon.Bytes())
		return c.iconCache
	}

	// The window provides no icon of its own so fall back to the app icon.
	c.iconCache = c.appIcon()
	return c.iconCache
}

// appIcon resolves the application icon for this window from the desktop's icon
// provider, returning nil when no matching application can be found.
func (c *clientProperties) appIcon() fyne.Resource {
	provider := tyde.Instance().IconProvider()
	if provider == nil {
		return nil
	}

	app := icon.FindAppFromWinInfo(c.c, provider)
	if app == nil {
		return nil
	}

	return app.Icon(tyde.Instance().Settings().IconTheme(), 64)
}

func (c *clientProperties) IconName() string {
	return windowIconName(c.c.wm.X(), c.c.win)
}

func (c *clientProperties) SkipTaskbar() bool {
	extendedHints := x11.WindowExtendedHintsGet(c.c.wm.X(), c.c.win)
	if extendedHints == nil {
		return false
	}
	for _, hint := range extendedHints {
		if hint == "_NET_WM_STATE_SKIP_TASKBAR" {
			return true
		}
	}
	return false
}

func (c *clientProperties) Title() string {
	return x11.WindowName(c.c.wm.X(), c.c.win)
}

func (c *clientProperties) refreshCache() {
	c.iconCache = nil
	c.decorated = c.lookupDecorated()
}

// refreshIconCache clears only the cached window icon so the next lookup
// re-reads the client's icon property (e.g. _NET_WM_ICON).
func (c *clientProperties) refreshIconCache() {
	c.iconCache = nil
}
