package ui

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FyshOS/appie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wmTest "fyshos.com/tyde/test"
)

func TestApplicationDirs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/home/me/.local/share")
	t.Setenv("XDG_DATA_DIRS", "/usr/local/share:/usr/share/:/usr/share")

	assert.Equal(t, []string{
		"/home/me/.local/share/applications",
		"/home/me/.local/share/flatpak/exports/share/applications",
		"/usr/local/share/applications",
		"/usr/share/applications",
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
	}, applicationDirs())
}

// countingProvider records how often the application cache is dropped.
type countingProvider struct {
	appie.Provider
	cleared atomic.Int32
}

func (p *countingProvider) ClearCache() { p.cleared.Add(1) }

func TestWatchApplicationDirs(t *testing.T) {
	dataHome := t.TempDir()
	apps := filepath.Join(dataHome, "applications")
	require.NoError(t, os.MkdirAll(apps, 0o700))
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", t.TempDir())

	old := appWatchSettle
	appWatchSettle = 50 * time.Millisecond

	provider := &countingProvider{Provider: wmTest.NewAppProvider()}
	l := &desktop{icons: provider}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		l.watchApplicationDirs(done)
		close(stopped)
	}()
	defer func() {
		close(done)
		<-stopped
		appWatchSettle = old
	}()
	time.Sleep(100 * time.Millisecond) // let the watcher start

	// Unrelated files are ignored, a new .desktop file reloads the list once.
	require.NoError(t, os.WriteFile(filepath.Join(apps, "notes.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(apps, "kitty.desktop"), []byte("[Desktop Entry]\n"), 0o600))
	assert.Eventually(t, func() bool { return provider.cleared.Load() == 1 }, 2*time.Second, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), provider.cleared.Load())
}
