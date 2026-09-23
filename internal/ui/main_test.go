package ui

import (
	"os"
	"testing"

	wmTest "fyshos.com/tyde/test"
)

// TestMain disables the package's background goroutines for tests and keeps
// them away from the user's session.
func TestMain(m *testing.M) {
	cleanup := wmTest.IsolateUserSession()
	runAsync = func(f func()) { f() }
	startClock = func(*widgetPanel) {}
	startAppWatcher = func(*desktop) {}

	code := m.Run()
	cleanup()
	os.Exit(code)
}
