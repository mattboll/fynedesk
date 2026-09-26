package autostart

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DataDirs are where the applications' .desktop files live:
// $XDG_DATA_HOME, then each of $XDG_DATA_DIRS.
func DataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	dirs := filepath.SplitList(os.Getenv("XDG_DATA_DIRS"))
	if len(dirs) == 0 {
		dirs = []string{"/usr/local/share", "/usr/share"}
	}
	return append([]string{home}, dirs...)
}

// Application finds the installed application a window's app id belongs
// to, and how to start it. The app id comes from the window, so it is only
// ever matched against the .desktop files, never run: by the name of the
// file (org.gnome.Nautilus), then by StartupWMClass, then by the program
// of Exec. Entries that are hidden or run in a terminal are left out.
func Application(appID string, dataDirs []string, lookPath func(string) (string, error)) (Entry, bool) {
	if appID == "" {
		return Entry{}, false
	}
	files := map[string]string{} // id -> path, the first found wins
	for _, dir := range dataDirs {
		if dir == "" {
			continue
		}
		paths, _ := filepath.Glob(filepath.Join(dir, "applications", "*.desktop"))
		for _, p := range paths {
			if id := filepath.Base(p); files[id] == "" {
				files[id] = p
			}
		}
	}
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var byClass, byProgram Entry
	for _, id := range ids {
		keys, err := readDesktopFile(files[id])
		if err != nil {
			continue
		}
		e, ok := applicationFrom(id, keys, lookPath)
		if !ok {
			continue
		}
		switch {
		case strings.EqualFold(strings.TrimSuffix(id, ".desktop"), appID):
			return e, true
		case byClass.ID == "" && strings.EqualFold(keys["StartupWMClass"], appID):
			byClass = e
		case byProgram.ID == "" && strings.EqualFold(e.Program(), appID):
			byProgram = e
		}
	}
	if byClass.ID != "" {
		return byClass, true
	}
	return byProgram, byProgram.ID != ""
}

// applicationFrom tells whether a .desktop file starts an application, and
// how.
func applicationFrom(id string, keys map[string]string, lookPath func(string) (string, error)) (Entry, bool) {
	if t := keys["Type"]; t != "" && t != "Application" {
		return Entry{}, false
	}
	if isTrue(keys["Hidden"]) || isTrue(keys["Terminal"]) {
		return Entry{}, false
	}
	if try := keys["TryExec"]; try != "" {
		if _, err := lookPath(try); err != nil {
			return Entry{}, false
		}
	}
	args, err := ExecArgs(keys["Exec"])
	if err != nil || len(args) == 0 {
		return Entry{}, false
	}
	if _, err := lookPath(args[0]); err != nil {
		return Entry{}, false
	}
	return Entry{ID: id, Name: keys["Name"], Args: args, Dir: keys["Path"]}, true
}
