package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecArgs(t *testing.T) {
	tests := []struct {
		exec string
		want []string
	}{
		{"/usr/bin/kdeconnectd", []string{"/usr/bin/kdeconnectd"}},
		{"env BAMF=/x.desktop /snap/bin/slack %U", []string{"env", "BAMF=/x.desktop", "/snap/bin/slack"}},
		{`sh -c "echo \"hi\" && sleep 1"`, []string{"sh", "-c", `echo "hi" && sleep 1`}},
		{"app --level=100%%", []string{"app", "--level=100%"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
	}
	for _, tt := range tests {
		got, err := ExecArgs(tt.exec)
		if err != nil || strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("ExecArgs(%q) = %q, %v; want %q", tt.exec, got, err, tt.want)
		}
	}
	if _, err := ExecArgs(`sh -c "unterminated`); err == nil {
		t.Error("an unterminated quote is accepted")
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "autostart"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "autostart", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testEnv(t *testing.T) (Env, string, string) {
	home, system := t.TempDir(), t.TempDir()
	installed := map[string]bool{"kdeconnectd": true, "slack": true, "nm-applet": true, "xdg-user-dirs-update": true, "orca": true}
	return Env{
		ConfigHome: home,
		ConfigDirs: []string{system},
		Desktops:   []string{"Tyde", "wlroots"},
		LookPath: func(p string) (string, error) {
			if installed[filepath.Base(p)] {
				return p, nil
			}
			return "", errors.New("not found")
		},
		GSetting: func(schema, key string) (bool, error) { return false, nil },
	}, home, system
}

func ids(entries []Entry) string {
	var names []string
	for _, e := range entries {
		names = append(names, e.ID)
	}
	return strings.Join(names, ",")
}

func TestEntries(t *testing.T) {
	env, home, system := testEnv(t)
	entry := func(name, exec, extra string) string {
		return "[Desktop Entry]\nType=Application\nName=" + name + "\nName[fr]=Traduit\nExec=" + exec + "\n" + extra + "\n"
	}
	write(t, system, "kdeconnect.desktop", entry("KDE Connect", "/usr/bin/kdeconnectd", "X-GNOME-Autostart-enabled=true"))
	write(t, system, "gnome-only.desktop", entry("GNOME thing", "/usr/bin/kdeconnectd", "OnlyShowIn=GNOME;Unity;"))
	write(t, system, "not-here.desktop", entry("Not in Tyde", "/usr/bin/kdeconnectd", "NotShowIn=KDE;Tyde;"))
	write(t, system, "tyde-only.desktop", entry("Ours", "nm-applet", "OnlyShowIn=GNOME;tyde;"))
	write(t, system, "missing.desktop", entry("Not installed", "/usr/bin/nothing", ""))
	write(t, system, "tryexec.desktop", entry("Try", "/usr/bin/kdeconnectd", "TryExec=/usr/bin/nothing"))
	write(t, system, "orca.desktop", entry("Orca", "orca --replace", "AutostartCondition=GSettings org.gnome.desktop.a11y.applications screen-reader-enabled"))
	write(t, system, "dirs.desktop", entry("Dirs", "xdg-user-dirs-update", ""))
	write(t, system, "dirs-kde.desktop", entry("Dirs again", "xdg-user-dirs-update", ""))
	// The user turned one off, and added their own.
	write(t, home, "dirs.desktop", entry("Dirs", "xdg-user-dirs-update", "Hidden=true"))
	write(t, home, "slack.desktop", entry("Slack", "/snap/bin/slack %U", "X-GNOME-Autostart-Delay=5"))
	write(t, home, "link.desktop", "[Desktop Entry]\nType=Link\nURL=https://example.org\n")

	got := Entries(env)
	// dirs-kde runs what the user turned off in dirs: the same command, kept
	// once.
	if ids(got) != "dirs-kde.desktop,kdeconnect.desktop,slack.desktop,tyde-only.desktop" {
		t.Fatalf("entries %s", ids(got))
	}
	slack := got[2]
	if slack.Name != "Slack" || strings.Join(slack.Args, " ") != "/snap/bin/slack" || slack.Delay != 5*time.Second {
		t.Errorf("slack %+v", slack)
	}
}

func TestEntriesRunACommandOnce(t *testing.T) {
	env, _, system := testEnv(t)
	write(t, system, "a.desktop", "[Desktop Entry]\nType=Application\nExec=xdg-user-dirs-update\n")
	write(t, system, "b.desktop", "[Desktop Entry]\nType=Application\nExec=xdg-user-dirs-update\n")
	if got := ids(Entries(env)); got != "a.desktop" {
		t.Errorf("entries %s", got)
	}
}

func TestProgram(t *testing.T) {
	for args, want := range map[string]string{
		"/usr/bin/kdeconnectd":                "kdeconnectd",
		"env BAMF=/x.desktop /snap/bin/slack": "slack",
		"/usr/bin/snap userd --autostart":     "snap",
	} {
		if got := (Entry{Args: strings.Fields(args)}).Program(); got != want {
			t.Errorf("Program(%q) = %q, want %q", args, got, want)
		}
	}
}

func TestConditions(t *testing.T) {
	env, home, _ := testEnv(t)
	if conditionHolds("if-exists tyde-flag", env) || !conditionHolds("unless-exists tyde-flag", env) {
		t.Error("with no file")
	}
	if err := os.WriteFile(filepath.Join(home, "tyde-flag"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !conditionHolds("if-exists tyde-flag", env) || conditionHolds("unless-exists tyde-flag", env) {
		t.Error("with the file")
	}
	env.GSetting = func(schema, key string) (bool, error) { return schema == "org.a11y" && key == "reader", nil }
	if !conditionHolds("GSettings org.a11y reader", env) || conditionHolds("GSettings org.a11y other", env) {
		t.Error("GSettings")
	}
	if !conditionHolds("", env) || !conditionHolds("KDE rc:group:key:true", env) {
		t.Error("conditions of other desktops hold")
	}
}

func TestEscapesOfTheFile(t *testing.T) {
	env, home, _ := testEnv(t)
	env.LookPath = func(p string) (string, error) { return p, nil }
	// As written in a .desktop file: a string escape, then a quoting one.
	write(t, home, "shell.desktop", `[Desktop Entry]
Type=Application
Exec=sh -c "echo \\$HOME\sand \\"quotes\\" > /tmp/x"
`)
	got := Entries(env)
	if len(got) != 1 {
		t.Fatalf("entries %+v", got)
	}
	want := []string{"sh", "-c", `echo $HOME and "quotes" > /tmp/x`}
	if strings.Join(got[0].Args, "|") != strings.Join(want, "|") {
		t.Errorf("args %q, want %q", got[0].Args, want)
	}
}
