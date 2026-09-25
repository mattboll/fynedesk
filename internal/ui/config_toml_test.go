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
	saved, _ := os.ReadFile(path)
	t.Cleanup(func() { _ = os.WriteFile(path, saved, 0o600) })
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

func TestMigrateModulesOnce(t *testing.T) {
	path := configPath()
	saved, _ := os.ReadFile(path)
	t.Cleanup(func() { _ = os.WriteFile(path, saved, 0o600) })

	d := &deskSettings{cfg: defaultConfig()}
	d.moduleNames = []string{"Sound"}
	d.migrateModules("Notes")
	assert.Equal(t, []string{"Sound", "Notes"}, d.moduleNames, "a new module is turned on")

	// The user turns it off: it stays off.
	d.moduleNames = []string{"Sound"}
	d.cfg.Modules.Enabled = d.moduleNames
	d.migrateModules("Notes")
	assert.Equal(t, []string{"Sound"}, d.moduleNames)
	assert.Equal(t, []string{"Notes"}, d.cfg.Modules.Offered)
}
