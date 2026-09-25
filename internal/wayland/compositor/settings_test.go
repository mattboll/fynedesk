package compositor

import (
	"os"
	"path/filepath"
	"testing"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/wlipc"
)

func TestReadTOMLAsPrefsPowerAndBlur(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := "[power]\n  lock_timeout_min = 0\n  blank_timeout_min = 12\n  suspend_action = \"hibernate\"\n" +
		"[windows]\n  blur = false\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, err := readTOMLAsPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"power_lock_timeout":   float64(0), // set to never: kept
		"power_blank_timeout":  float64(12),
		"power_suspend_action": "hibernate",
		"blurbehind":           false,
	}
	for k, v := range want {
		if prefs[k] != v {
			t.Errorf("%s = %v, want %v", k, prefs[k], v)
		}
	}
	if _, ok := prefs["power_suspend_timeout"]; ok {
		t.Error("power_suspend_timeout is set although config.toml leaves it out")
	}
}

func TestReadTOMLAsPrefsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, err := readTOMLAsPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if prefs["blurbehind"] != true {
		t.Errorf("blurbehind = %v, want true by default", prefs["blurbehind"])
	}
	if _, ok := prefs["power_blank_timeout"]; ok {
		t.Error("power_blank_timeout is set although config.toml has no [power]")
	}
}

func TestReadTOMLAsPrefsAgentsModule(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		cfg  string
		want any
	}{
		{"[modules]\n  enabled = [\"Notes\", \"Coding Agents\"]\n", true},
		{"[modules]\n  enabled = [\"Notes\"]\n", false},
		{"", nil}, // no list yet: left to the default
	} {
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(tt.cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		prefs, err := readTOMLAsPrefs(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := prefs["agentsmodule"]; got != tt.want {
			t.Errorf("%q: agentsmodule = %v, want %v", tt.cfg, got, tt.want)
		}
	}
}

func TestNextAgentKeyFollowsTheModule(t *testing.T) {
	bound := func(prefs map[string]any) bool {
		s := &server{wmModifier: wlr.KeyboardModifierLogo}
		s.loadKeybindings(prefs)
		for _, action := range s.keybindingMap {
			if action == wlipc.ActionNextAgent {
				return true
			}
		}
		return false
	}
	if !bound(nil) {
		t.Error("next_agent is not bound by default")
	}
	if !bound(map[string]any{"agentsmodule": true}) {
		t.Error("next_agent is not bound with the module on")
	}
	if bound(map[string]any{"agentsmodule": false}) {
		t.Error("next_agent is still bound with the module off")
	}
}
