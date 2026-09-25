package test

import "os"

// IsolateUserSession points the XDG config, cache, data and runtime
// directories at a fresh temporary directory and hides the display and
// session bus, so that tests building a desktop never write into the
// developer's home, open sockets beside a running session or register D-Bus
// services on it. The desktops of the tests load a single module that reads
// nothing from the machine (see testConfig). Call it from TestMain; the
// returned function removes the temporary directory.
func IsolateUserSession() func() {
	dir, err := os.MkdirTemp("", "tyde-test-")
	if err != nil {
		panic(err)
	}

	for name, sub := range map[string]string{
		"XDG_CONFIG_HOME": "config",
		"XDG_CACHE_HOME":  "cache",
		"XDG_DATA_HOME":   "data",
		"XDG_RUNTIME_DIR": "run",
	} {
		path := dir + "/" + sub
		if err := os.MkdirAll(path, 0o700); err != nil {
			panic(err)
		}
		_ = os.Setenv(name, path)
	}
	if err := os.MkdirAll(dir+"/config/tyde", 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(dir+"/config/tyde/config.toml", []byte(testConfig), 0o600); err != nil {
		panic(err)
	}
	_ = os.Unsetenv("DISPLAY")
	_ = os.Unsetenv("WAYLAND_DISPLAY")
	_ = os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+dir+"/no-bus")

	return func() { _ = os.RemoveAll(dir) }
}

// testConfig is the Tyde configuration of the tests. The modules of the
// status area read the machine (battery, network, sound, brightness) and
// refresh from goroutines, which the Fyne test driver runs alongside the
// tests; the modules turned on by migration are marked as offered already.
const testConfig = `[modules]
  enabled = ["Virtual Desktops"]
  offered = ["Keyboard Layout", "Power Profile", "Notes", "Next Meeting", "Today's Agenda"]
`
