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
		"[windows]\n  blur = false\n  shadows = false\n"
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
		"windowshadows":        false,
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
	if prefs["blurbehind"] != true || prefs["windowshadows"] != true {
		t.Errorf("blurbehind = %v, windowshadows = %v, want both on by default", prefs["blurbehind"], prefs["windowshadows"])
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

// TestReadTOMLAsPrefsWhole checks the keys the panel used to send in its
// partial snapshot are read from the file itself.
func TestReadTOMLAsPrefsWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := "[display]\n  color_scheme = \"dark\"\n  reduce_motion = true\n  high_contrast = true\n" +
		"[desktops]\n  count = 6\n  names = [\"Mail\", \"Code\"]\n" +
		"[theme]\n  [theme.colors]\n    tyde_titlebar = \"#112233\"\n    tyde_border = \"#445566\"\n" +
		"[[window_rules]]\n  app_id = \"firefox\"\n  workspace = 2\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, err := readTOMLAsPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"colorscheme":  "dark",
		"reducemotion": true,
		"highcontrast": true,
		"desktopcount": float64(6),
		"desktopnames": "Mail|Code",
		"theme_colors": "tyde_border=#445566|tyde_titlebar=#112233",
	} {
		if prefs[key] != want {
			t.Errorf("%s = %v, want %v", key, prefs[key], want)
		}
	}
	if rules, _ := prefs["windowrules"].(string); rules == "" {
		t.Error("window rules not read")
	}
}

func TestRecordingPrefs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := "[modules]\n  enabled = [\"Notes\"]\n[recording]\n  microphone = true\n  format = \"webm\"\n  no_gpu = true\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, err := readTOMLAsPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if prefs["recordermodule"] != false {
		t.Errorf("recordermodule = %v, want false", prefs["recordermodule"])
	}
	s := &server{}
	s.applyRecordingPrefs(prefs)
	r := s.recordSettings
	if !r.Microphone || r.SystemAudio || r.FileExt() != "webm" || !r.NoGPU || r.Camera() != "/dev/video0" {
		t.Errorf("settings %+v", r)
	}

	// Without the module, Super+Shift+R is free.
	s.loadKeybindings(prefs)
	for _, action := range s.keybindingMap {
		if action == wlipc.ActionScreenRecord {
			t.Error("screen_record is bound without its module")
		}
	}
}
