package main

import (
	"os"
	"testing"

	"fyne.io/fyne/v2/test"

	wmTest "fyshos.com/tyde/test"
)

// TestMain keeps the desktop built by the tests away from the user's session:
// without it the modules write into ~/.config/tyde, open sockets in the
// runtime directory and register on the session bus, and the X11 window
// manager would try the real display.
func TestMain(m *testing.M) {
	cleanup := wmTest.IsolateUserSession()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestNewDesktop(t *testing.T) {
	app := test.NewApp()
	desk := setupDesktop(app)

	desk.Run()
}
