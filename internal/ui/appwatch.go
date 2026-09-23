package ui

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"github.com/fsnotify/fsnotify"
)

// appWatchSettle is how long the application directories must stay quiet
// before the list is reloaded: a package install writes many files.
var appWatchSettle = 2 * time.Second

// startAppWatcher starts watchApplicationDirs. It is a package var so tests
// can leave the directories of the machine alone.
var startAppWatcher = func(l *desktop) { go l.watchApplicationDirs(nil) }

// applicationDirs lists the directories holding .desktop files, following the
// XDG base directory specification plus the Flatpak and Snap exports.
func applicationDirs() []string {
	var dirs []string
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	if dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "applications"),
			filepath.Join(dataHome, "flatpak", "exports", "share", "applications"))
	}

	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	dirs = append(dirs, "/var/lib/flatpak/exports/share/applications", "/var/lib/snapd/desktop/applications")

	seen := make(map[string]bool, len(dirs))
	var unique []string
	for _, d := range dirs {
		d = filepath.Clean(d)
		if !seen[d] {
			seen[d] = true
			unique = append(unique, d)
		}
	}
	return unique
}

// watchApplicationDirs reloads the list of applications when one is installed,
// removed or changed, so the launcher and the dock see it without restarting
// the desktop. It returns when done is closed (nil: never).
func (l *desktop) watchApplicationDirs(done <-chan struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Println("Cannot watch the application directories:", err)
		return
	}
	defer watcher.Close()

	watched := 0
	for _, dir := range applicationDirs() {
		if err := watcher.Add(dir); err == nil {
			watched++
		}
	}
	if watched == 0 {
		return
	}

	delay := appWatchSettle
	settle := time.NewTimer(delay)
	settle.Stop()
	for {
		select {
		case <-done:
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if strings.HasSuffix(ev.Name, ".desktop") {
				settle.Reset(delay)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Application directory watch error:", err)
		case <-settle.C:
			fyne.Do(l.reloadApplications)
		}
	}
}

// reloadApplications drops the cached application list, so it is read again
// on next use, and refreshes the dock.
func (l *desktop) reloadApplications() {
	l.icons.ClearCache()
	if l.bar != nil {
		l.bar.updateIcons()
		l.bar.updateIconOrder()
	}
}
