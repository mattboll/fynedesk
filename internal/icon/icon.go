// Package icon provides icon resolution for application windows using FDO icon themes and macOS icon providers.
package icon

import (
	"runtime"
	"strings"

	"fyshos.com/fynedesk"
	"github.com/FyshOS/appie"
)

// findMatch tries exact then lowercase matching against the provider.
func findMatch(name string, provider appie.Provider) appie.AppData {
	if name == "" {
		return nil
	}
	apps := provider.FindAppsMatching(name)
	if len(apps) > 0 {
		return apps[0]
	}
	lower := strings.ToLower(name)
	if lower != name {
		apps = provider.FindAppsMatching(lower)
		if len(apps) > 0 {
			return apps[0]
		}
	}
	return nil
}

// FindAppByName searches for an application by name using the provider.
func FindAppByName(name string, provider appie.Provider) appie.AppData {
	return findMatch(name, provider)
}

// FindAppFromWinInfo searches the known applications and tries to find one
// based on the properties of an open window.
func FindAppFromWinInfo(win fynedesk.Window, provider appie.Provider) appie.AppData {
	if runtime.GOOS == "darwin" { // simpler handling when we are not the desktop environment
		title := win.Properties().Title()
		if title != "" {
			apps := provider.FindAppsMatching(title)
			if len(apps) > 0 {
				return apps[0]
			}
		}
		return nil
	}

	if app := findMatch(win.Properties().Command(), provider); app != nil {
		return app
	}

	for _, class := range win.Properties().Class() {
		if app := findMatch(class, provider); app != nil {
			return app
		}
	}

	if app := findMatch(win.Properties().IconName(), provider); app != nil {
		return app
	}

	// Last resort: try window title
	title := win.Properties().Title()
	if title != "" {
		apps := provider.FindAppsMatching(title)
		if len(apps) > 0 {
			return apps[0]
		}
	}

	return nil
}
