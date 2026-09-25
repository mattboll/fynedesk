package compositor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTOMLAsPrefsPower(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := "[power]\n  lock_timeout_min = 0\n  blank_timeout_min = 12\n  suspend_action = \"hibernate\"\n"
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
