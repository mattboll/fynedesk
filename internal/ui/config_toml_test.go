package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLegacySaverLabelRenamed(t *testing.T) {
	// TestMain isolates XDG_CONFIG_HOME: this writes a throwaway config.
	path := configPath()
	t.Cleanup(func() { _ = os.Remove(path) })
	assert.NoError(t, os.WriteFile(path, []byte("[screensaver]\n  label = \"FyneDesk\"\n"), 0o600))

	cfg, _ := loadConfig()
	assert.Equal(t, "Tyde", cfg.ScreenSaver.Label)
	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), `label = "Tyde"`), "the file is updated")

	// A label the user chose stays.
	assert.NoError(t, os.WriteFile(path, []byte("[screensaver]\n  label = \"Bonjour\"\n"), 0o600))
	cfg, _ = loadConfig()
	assert.Equal(t, "Bonjour", cfg.ScreenSaver.Label)
}
