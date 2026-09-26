//go:build !wayland && (linux || openbsd || freebsd || netbsd)

package main

import (
	"log"
	"os"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/ui"
	"fyshos.com/tyde/internal/x11/composit"
	"fyshos.com/tyde/internal/x11/wm"
)

// exitNoX tells tyde_runner that the X server cannot be reached.
const exitNoX = 3

func setupDesktop(a fyne.App) tyde.Desktop {
	icons := appie.NewFDOProvider()
	mgr, err := wm.NewX11WindowManager(a)
	if err != nil {
		log.Println("Could not create window manager:", err)
		if os.Getenv("FYNE_DESK_RUNNER") == "1" {
			os.Exit(exitNoX) // the session's X server is gone: the runner stops
		}
		return ui.NewEmbeddedDesktop(a, icons)
	}
	return ui.NewDesktop(a, mgr, icons, wm.NewX11ScreensProvider(mgr), composit.Run)
}
