package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplication(t *testing.T) {
	home, system := t.TempDir(), t.TempDir()
	put := func(dir, name, body string) {
		t.Helper()
		apps := filepath.Join(dir, "applications")
		if err := os.MkdirAll(apps, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(apps, name), []byte("[Desktop Entry]\nType=Application\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(system, "org.gnome.Nautilus.desktop", "Exec=nautilus --new-window %U\n")
	put(system, "google-chrome.desktop", "Exec=/opt/google/chrome/google-chrome %U\nStartupWMClass=Google-chrome\n")
	put(system, "firefox.desktop", "Exec=firefox %u\n")
	put(home, "firefox.desktop", "Exec=firefox -P work %u\n") // the user's file wins
	put(system, "htop.desktop", "Exec=htop\nTerminal=true\n")
	put(system, "gone.desktop", "Exec=gone\nHidden=true\n")
	installed := map[string]bool{"nautilus": true, "google-chrome": true, "firefox": true, "htop": true, "gone": true}
	lookPath := func(p string) (string, error) {
		if installed[filepath.Base(p)] {
			return p, nil
		}
		return "", errors.New("not found")
	}

	tests := []struct {
		appID string
		want  string // the command, "" when nothing is found
	}{
		{"org.gnome.Nautilus", "nautilus --new-window"},
		{"google-chrome", "/opt/google/chrome/google-chrome"},
		{"Google-chrome", "/opt/google/chrome/google-chrome"},
		{"firefox", "firefox -P work"},
		{"htop", ""},
		{"gone", ""},
		{"/bin/sh", ""},
		{"sh -c reboot", ""},
		{"", ""},
	}
	for _, tt := range tests {
		e, ok := Application(tt.appID, []string{home, system}, lookPath)
		if got := strings.Join(e.Args, " "); got != tt.want || ok != (tt.want != "") {
			t.Errorf("Application(%q) = %q, %v; want %q", tt.appID, got, ok, tt.want)
		}
	}
}
