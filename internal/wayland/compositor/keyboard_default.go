package compositor

import (
	"bufio"
	"os"
	"strings"
)

// systemKeyboardFile is the Debian/Ubuntu keyboard configuration, also used
// by localectl on those distributions.
var systemKeyboardFile = "/etc/default/keyboard"

// systemKeyboardLayout returns the layout and variant configured for the
// system, used when Tyde has no layout of its own. It returns empty strings
// if none is found, leaving xkbcommon to its defaults.
func systemKeyboardLayout() (layout, variant string) {
	f, err := os.Open(systemKeyboardFile)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "XKBLAYOUT":
			layout = value
		case "XKBVARIANT":
			variant = value
		}
	}

	// Multiple layouts ("fr,us") are for the system's own switcher: keep the
	// first, with its matching variant.
	layout, _, _ = strings.Cut(layout, ",")
	variant, _, _ = strings.Cut(variant, ",")
	if layout == "" {
		return "", ""
	}
	return layout, variant
}
