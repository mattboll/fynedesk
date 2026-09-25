// Package autostart finds the applications to start with the session, as
// the XDG Autostart specification says: the .desktop files of
// $XDG_CONFIG_HOME/autostart, then of the autostart folder of each of
// $XDG_CONFIG_DIRS (/etc/xdg/autostart); a file of the user hides a system
// file of the same name.
package autostart

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Entry is an application to start with the session.
type Entry struct {
	ID    string   // the name of its .desktop file
	Name  string   // what it is called
	Args  []string // the command
	Dir   string   // where it runs, if said
	Delay time.Duration
}

// Env is what the entries are checked against.
type Env struct {
	ConfigHome string   // $XDG_CONFIG_HOME
	ConfigDirs []string // $XDG_CONFIG_DIRS
	Desktops   []string // $XDG_CURRENT_DESKTOP, split on ':'
	// LookPath finds a program (exec.LookPath).
	LookPath func(string) (string, error)
	// GSetting reads a boolean setting of GNOME, for the entries that start
	// under a condition (AutostartCondition=GSettings schema key).
	GSetting func(schema, key string) (bool, error)
}

// DefaultEnv is the environment of the session.
func DefaultEnv() Env {
	home := os.Getenv("XDG_CONFIG_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".config")
		}
	}
	dirs := filepath.SplitList(os.Getenv("XDG_CONFIG_DIRS"))
	if len(dirs) == 0 {
		dirs = []string{"/etc/xdg"}
	}
	return Env{
		ConfigHome: home,
		ConfigDirs: dirs,
		Desktops:   strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":"),
		LookPath:   exec.LookPath,
		GSetting:   gsetting,
	}
}

func gsetting(schema, key string) (bool, error) {
	out, err := exec.Command("gsettings", "get", schema, key).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// Entries returns the applications to start, by the name of their file.
func Entries(env Env) []Entry {
	files := map[string]string{} // id -> path, the first found wins
	for _, dir := range append([]string{env.ConfigHome}, env.ConfigDirs...) {
		if dir == "" {
			continue
		}
		paths, _ := filepath.Glob(filepath.Join(dir, "autostart", "*.desktop"))
		for _, p := range paths {
			id := filepath.Base(p)
			if _, ok := files[id]; !ok {
				files[id] = p
			}
		}
	}
	var entries []Entry
	for id, path := range files {
		keys, err := readDesktopFile(path)
		if err != nil {
			continue
		}
		if e, ok := entryFrom(id, keys, env); ok {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	// The same command in two files (for two desktops) runs once.
	seen := map[string]bool{}
	unique := entries[:0]
	for _, e := range entries {
		if cmd := strings.Join(e.Args, "\x00"); !seen[cmd] {
			seen[cmd] = true
			unique = append(unique, e)
		}
	}
	return unique
}

// Program returns the name of the program an entry runs, past "env" and
// its variables.
func (e Entry) Program() string {
	for i, a := range e.Args {
		if i == 0 && filepath.Base(a) == "env" {
			continue
		}
		if i > 0 && filepath.Base(e.Args[0]) == "env" && strings.Contains(a, "=") && !strings.HasPrefix(a, "-") {
			continue
		}
		return filepath.Base(a)
	}
	return ""
}

// entryFrom tells whether a .desktop file is to be started, and how.
func entryFrom(id string, keys map[string]string, env Env) (Entry, bool) {
	if t := keys["Type"]; t != "" && t != "Application" {
		return Entry{}, false
	}
	if isTrue(keys["Hidden"]) || keys["X-GNOME-Autostart-enabled"] == "false" {
		return Entry{}, false // turned off
	}
	if only := keys["OnlyShowIn"]; only != "" && !anyIn(only, env.Desktops) {
		return Entry{}, false
	}
	if not := keys["NotShowIn"]; not != "" && anyIn(not, env.Desktops) {
		return Entry{}, false
	}
	if try := keys["TryExec"]; try != "" {
		if _, err := env.LookPath(try); err != nil {
			return Entry{}, false // not installed
		}
	}
	if !conditionHolds(keys["AutostartCondition"], env) {
		return Entry{}, false
	}
	args, err := ExecArgs(keys["Exec"])
	if err != nil || len(args) == 0 {
		return Entry{}, false
	}
	if _, err := env.LookPath(args[0]); err != nil {
		return Entry{}, false
	}
	e := Entry{ID: id, Name: keys["Name"], Args: args, Dir: keys["Path"]}
	if s, err := strconv.Atoi(keys["X-GNOME-Autostart-Delay"]); err == nil && s > 0 {
		e.Delay = time.Duration(s) * time.Second
	}
	return e, true
}

func isTrue(v string) bool {
	return v == "true" || v == "1"
}

// anyIn reports whether one of the desktops is in the ';'-separated list.
func anyIn(list string, desktops []string) bool {
	for _, item := range strings.Split(list, ";") {
		for _, d := range desktops {
			if item != "" && strings.EqualFold(item, d) {
				return true
			}
		}
	}
	return false
}

// conditionHolds checks an AutostartCondition: "if-exists file",
// "unless-exists file" (in $XDG_CONFIG_HOME) or "GSettings schema key".
// Other conditions (of other desktops) hold.
func conditionHolds(cond string, env Env) bool {
	fields := strings.Fields(cond)
	if len(fields) < 2 {
		return true
	}
	exists := func() bool {
		_, err := os.Stat(filepath.Join(env.ConfigHome, fields[1]))
		return err == nil
	}
	switch strings.ToLower(fields[0]) {
	case "if-exists":
		return exists()
	case "unless-exists":
		return !exists()
	case "gsettings":
		if len(fields) < 3 || env.GSetting == nil {
			return false
		}
		on, err := env.GSetting(fields[1], fields[2])
		return err == nil && on
	}
	return true
}

// ExecArgs splits the Exec key of a .desktop file into its arguments: quoted
// arguments keep their spaces, and the field codes (%f, %U…) are dropped, as
// nothing is opened with the application.
func ExecArgs(exec string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	runes := []rune(exec)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quoted && r == '\\' && i+1 < len(runes):
			i++
			cur.WriteRune(runes[i])
		case r == '"':
			quoted = !quoted
			inArg = true
		case !quoted && (r == ' ' || r == '\t'):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case r == '%' && i+1 < len(runes):
			i++
			if runes[i] == '%' {
				cur.WriteRune('%')
				inArg = true
			}
			// A field code: nothing to put in its place.
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	if inArg && cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args, nil
}

// readDesktopFile reads the keys of the [Desktop Entry] group, leaving the
// translated ones (Name[fr]) out.
func readDesktopFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	keys := map[string]string{}
	inEntry := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !inEntry || !ok || strings.Contains(k, "[") {
			continue
		}
		keys[strings.TrimSpace(k)] = unescape(strings.TrimSpace(v))
	}
	return keys, scanner.Err()
}

// unescape reads the escapes of a string value (\s, \n, \t, \r, \\). They
// come before those of the quoted arguments of Exec: "\\$" in the file is
// "\$" in the value, a "$" in the argument.
func unescape(v string) string {
	if !strings.Contains(v, "\\") {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' || i+1 == len(v) {
			b.WriteByte(v[i])
			continue
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default: // not a string escape: kept for the quoting of Exec
			b.WriteByte('\\')
			b.WriteByte(v[i])
		}
	}
	return b.String()
}

// Start starts an entry in its own process group, with env, and lets it
// run on its own.
func Start(e Entry, env []string) error {
	cmd := exec.Command(e.Args[0], e.Args[1:]...)
	cmd.Env = env
	cmd.Dir = e.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
