package ui

import (
	"os"
	"path/filepath"
	"testing"

	wmTest "fyshos.com/tyde/test"
)

// TestMain disables the package's background goroutines for tests and keeps
// them away from the user's session.
func TestMain(m *testing.M) {
	cleanup := wmTest.IsolateUserSession()
	// No modules by default: those of the status area read the machine
	// (battery, network, sound) and refresh from goroutines, which the test
	// driver runs alongside the tests.
	if err := writeTestConfig(testModulesConfig); err != nil {
		panic(err)
	}
	runAsync = func(f func()) { f() }
	startClock = func(*widgetPanel) {}
	startAppWatcher = func(*desktop) {}

	code := m.Run()
	cleanup()
	os.Exit(code)
}

// writeTestConfig writes the configuration the desktops of the tests load.
func writeTestConfig(data string) error {
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath(), []byte(data), 0o600)
}

// testModulesConfig loads one module that reads nothing from the machine,
// and marks the modules turned on by migration as offered already.
const testModulesConfig = `[modules]
  enabled = ["Virtual Desktops"]
  offered = ["Keyboard Layout", "Power Profile", "Notes", "Next Meeting", "Today's Agenda"]
`
