package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := []byte(`{"hello":"world"}`)
	if err := atomicWriteFile(path, data); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial data
	if err := atomicWriteFile(path, []byte("old")); err != nil {
		t.Fatal(err)
	}

	// Overwrite
	if err := atomicWriteFile(path, []byte("new")); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestAtomicWriteFile_NoTempFileLeftOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := atomicWriteFile(path, []byte("data")); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 1 file, got %d: %v", len(entries), names)
	}
}

func TestWindowInfoRoundTrip(t *testing.T) {
	info := WindowInfo{
		ID:           "xdg-1",
		Title:        "Test Window",
		AppID:        "com.test.app",
		Desktop:      2,
		Output:       "eDP-1",
		Focused:      true,
		Iconic:       false,
		Maximized:    true,
		Fullscreened: false,
		Pinned:       false,
		Urgent:       true,
		IsPanel:      false,
		X:            100.5,
		Y:            200.0,
		Width:        800,
		Height:       600,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var decoded WindowInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != info.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, info.ID)
	}
	if decoded.Urgent != info.Urgent {
		t.Errorf("Urgent: got %v, want %v", decoded.Urgent, info.Urgent)
	}
	if decoded.Output != info.Output {
		t.Errorf("Output: got %q, want %q", decoded.Output, info.Output)
	}
	if decoded.X != info.X {
		t.Errorf("X: got %v, want %v", decoded.X, info.X)
	}
}

func TestWindowInfoUrgentOmitEmpty(t *testing.T) {
	info := WindowInfo{ID: "xdg-1", Title: "Test"}
	data, _ := json.Marshal(info)

	// Urgent defaults to false, should be omitted from JSON
	if string(data) != "" {
		var m map[string]interface{}
		json.Unmarshal(data, &m)
		if _, ok := m["urgent"]; ok {
			t.Error("urgent should be omitted when false (omitempty)")
		}
	}
}

func TestWindowsStateRoundTrip(t *testing.T) {
	state := WindowsState{
		Windows: []WindowInfo{
			{ID: "xdg-1", Title: "Win 1", Focused: true},
			{ID: "xway-2", Title: "Win 2", Iconic: true},
		},
		Timestamp: 1234567890,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	var decoded WindowsState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(decoded.Windows))
	}
	if decoded.Windows[0].Focused != true {
		t.Error("window 0 should be focused")
	}
	if decoded.Windows[1].Iconic != true {
		t.Error("window 1 should be iconic")
	}
	if decoded.Timestamp != 1234567890 {
		t.Errorf("timestamp: got %d, want %d", decoded.Timestamp, 1234567890)
	}
}

func TestDesktopStateRoundTrip(t *testing.T) {
	state := DesktopState{
		Current:  2,
		NumDesks: 4,
		Names:    []string{"Web", "Code", "Chat", "Media"},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	var decoded DesktopState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Current != 2 {
		t.Errorf("Current: got %d, want %d", decoded.Current, 2)
	}
	if decoded.NumDesks != 4 {
		t.Errorf("NumDesks: got %d, want %d", decoded.NumDesks, 4)
	}
	if len(decoded.Names) != 4 || decoded.Names[1] != "Code" {
		t.Errorf("Names: got %v", decoded.Names)
	}
}

func TestWindowRuleSerialize(t *testing.T) {
	boolTrue := true
	boolFalse := false
	rule := WindowRule{
		AppID:     "firefox",
		Float:     &boolTrue,
		Workspace: 1,
		Maximize:  &boolFalse,
		Opacity:   0.9,
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}

	var decoded WindowRule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.AppID != "firefox" {
		t.Errorf("AppID: got %q, want %q", decoded.AppID, "firefox")
	}
	if decoded.Float == nil || *decoded.Float != true {
		t.Error("Float should be true")
	}
	if decoded.Maximize == nil || *decoded.Maximize != false {
		t.Error("Maximize should be false")
	}
	if decoded.Opacity != 0.9 {
		t.Errorf("Opacity: got %v, want 0.9", decoded.Opacity)
	}
}

func TestWindowRulePatternOmitEmpty(t *testing.T) {
	rule := WindowRule{AppID: "firefox"}
	data, _ := json.Marshal(rule)

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	// Fields with omitempty should not appear
	for _, key := range []string{"pattern", "float", "workspace", "width", "height", "maximize", "opacity", "pinned"} {
		if _, ok := m[key]; ok {
			t.Errorf("%q should be omitted when zero/nil", key)
		}
	}
}

func TestSessionStateRoundTrip(t *testing.T) {
	state := SessionState{
		Windows: []SessionWindow{
			{
				AppID:   "foot",
				Desktop: 0,
				X:       100,
				Y:       200,
				Width:   800,
				Height:  600,
			},
		},
		Timestamp: 9999999,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SessionState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if len(decoded.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(decoded.Windows))
	}
	if decoded.Windows[0].AppID != "foot" {
		t.Errorf("AppID: got %q, want %q", decoded.Windows[0].AppID, "foot")
	}
	if decoded.Windows[0].Width != 800 {
		t.Errorf("Width: got %d, want %d", decoded.Windows[0].Width, 800)
	}
}

func TestClipboardEntryRoundTrip(t *testing.T) {
	entry := ClipboardEntry{
		Text:      "Hello clipboard",
		Timestamp: 1234567890,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var decoded ClipboardEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Text != "Hello clipboard" {
		t.Errorf("Text: got %q, want %q", decoded.Text, "Hello clipboard")
	}
}
