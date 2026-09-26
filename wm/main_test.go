package wm

import (
	"os"
	"testing"

	"fyshos.com/tyde/test"
)

// TestMain keeps the tests off the user's session: the notification server
// registers on the session bus, and paths come from HOME and XDG.
func TestMain(m *testing.M) {
	cleanup := test.IsolateUserSession()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
