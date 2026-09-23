package wlipc

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the config and runtime directories at a temporary one so
// the tests (the clipboard key, state files, sockets) never touch the user's
// session.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tyde-wlipc-test-")
	if err != nil {
		panic(err)
	}
	for name, sub := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_RUNTIME_DIR": "run"} {
		path := filepath.Join(dir, sub)
		if err := os.MkdirAll(path, 0o700); err != nil {
			panic(err)
		}
		_ = os.Setenv(name, path)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
