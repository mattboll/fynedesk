//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd
// +build !linux,!darwin,!freebsd,!openbsd,!netbsd

package main

import (
	"log"
	"runtime"

	"fyne.io/fyne/v2"

	"github.com/FyshOS/appie"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/ui"
)

func setupDesktop(a fyne.App) tyde.Desktop {
	log.Println("Full desktop not possible on", runtime.GOOS)
	return ui.NewEmbeddedDesktop(a, appie.NewFDOProvider())
}
