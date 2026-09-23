package compositor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemKeyboardLayout(t *testing.T) {
	for name, tc := range map[string]struct {
		content, layout, variant string
	}{
		"debian":   {"XKBMODEL=\"pc105\"\nXKBLAYOUT=\"fr\"\nXKBVARIANT=\"latin9\"\n", "fr", "latin9"},
		"several":  {"XKBLAYOUT=\"fr,us\"\nXKBVARIANT=\"bepo,\"\n", "fr", "bepo"},
		"unquoted": {"# comment\nXKBLAYOUT=de\n", "de", ""},
		"empty":    {"XKBVARIANT=\"nodeadkeys\"\n", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keyboard")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			old := systemKeyboardFile
			systemKeyboardFile = path
			defer func() { systemKeyboardFile = old }()

			layout, variant := systemKeyboardLayout()
			if layout != tc.layout || variant != tc.variant {
				t.Errorf("got %q:%q, want %q:%q", layout, variant, tc.layout, tc.variant)
			}
		})
	}

	old := systemKeyboardFile
	systemKeyboardFile = filepath.Join(t.TempDir(), "missing")
	defer func() { systemKeyboardFile = old }()
	if layout, variant := systemKeyboardLayout(); layout != "" || variant != "" {
		t.Errorf("missing file gave %q:%q", layout, variant)
	}
}
