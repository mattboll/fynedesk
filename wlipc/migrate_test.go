package wlipc

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateLegacyConfig(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "fynedesk", "config.toml"), "a = 1")
	writeFile(t, filepath.Join(base, "fynedesk", "themes", "mine.json"), "{}")
	writeFile(t, filepath.Join(base, "fyne", "com.fyshos.fynedesk", "preferences.json"), `{"x":1}`)

	migrateLegacyConfig(base)

	if got := readFile(t, filepath.Join(base, "tyde", "config.toml")); got != "a = 1" {
		t.Fatalf("config.toml = %q", got)
	}
	if got := readFile(t, filepath.Join(base, "tyde", "themes", "mine.json")); got != "{}" {
		t.Fatalf("theme = %q", got)
	}
	if got := readFile(t, filepath.Join(base, "fyne", "com.fyshos.tyde", "preferences.json")); got != `{"x":1}` {
		t.Fatalf("preferences = %q", got)
	}
	// The old configuration stays for older builds.
	if _, err := os.Stat(filepath.Join(base, "fynedesk", "config.toml")); err != nil {
		t.Fatal("legacy config removed")
	}
	if _, err := os.Stat(filepath.Join(base, "tyde.migrating")); !os.IsNotExist(err) {
		t.Fatal("temporary directory left behind")
	}
}

func TestMigrateLegacyConfigKeepsExistingTyde(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "fynedesk", "config.toml"), "old")
	writeFile(t, filepath.Join(base, "tyde", "config.toml"), "new")

	migrateLegacyConfig(base)

	if got := readFile(t, filepath.Join(base, "tyde", "config.toml")); got != "new" {
		t.Fatalf("existing Tyde config overwritten: %q", got)
	}
}

func TestMigrateLegacyConfigNothingToDo(t *testing.T) {
	base := t.TempDir()
	migrateLegacyConfig(base)
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Fatalf("created %d entries without a legacy configuration", len(entries))
	}
}
