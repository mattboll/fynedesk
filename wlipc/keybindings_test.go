package wlipc

import (
	"testing"
)

func TestDefaultBindings_HasRequiredActions(t *testing.T) {
	defaults := DefaultBindings()

	required := []string{
		ActionQuit,
		ActionSwitchAppNext,
		ActionCloseWindow,
		ActionOpenTerminal,
		ActionMaximize,
		ActionMinimize,
		ActionToggleFullscreen,
		ActionPrevDesktop,
		ActionNextDesktop,
		ActionLockScreen,
		ActionShowLauncher,
	}

	for _, action := range required {
		bindings, ok := defaults[action]
		if !ok || len(bindings) == 0 {
			t.Errorf("DefaultBindings missing required action %q", action)
		}
	}
}

func TestMergeWithDefaults_NilUser(t *testing.T) {
	result := MergeWithDefaults(nil)
	defaults := DefaultBindings()

	if len(result) != len(defaults) {
		t.Errorf("nil user should return defaults: got %d actions, want %d", len(result), len(defaults))
	}
}

func TestMergeWithDefaults_UserOverride(t *testing.T) {
	user := ActionBindings{
		ActionQuit: {
			{Key: "q", Mods: []string{"Ctrl"}},
		},
	}

	result := MergeWithDefaults(user)

	// User override should replace default quit binding
	quitBindings := result[ActionQuit]
	if len(quitBindings) != 1 || quitBindings[0].Key != "q" {
		t.Errorf("user override not applied: got %v", quitBindings)
	}

	// Other defaults should still be present
	if _, ok := result[ActionCloseWindow]; !ok {
		t.Error("default close_window binding should still be present")
	}
}

func TestBindingToString(t *testing.T) {
	tests := []struct {
		name     string
		binding  KeyBinding
		expected string
	}{
		{
			name:     "single mod + key",
			binding:  KeyBinding{Key: "Tab", Mods: []string{"WM"}},
			expected: "WM+Tab",
		},
		{
			name:     "multiple mods",
			binding:  KeyBinding{Key: "Tab", Mods: []string{"WM", "Shift"}},
			expected: "WM+Shift+Tab",
		},
		{
			name:     "key only",
			binding:  KeyBinding{Key: "F11", Mods: nil},
			expected: "F11",
		},
		{
			name:     "grave key",
			binding:  KeyBinding{Key: "grave", Mods: []string{"WM"}},
			expected: "WM+`",
		},
		{
			name:     "volume key",
			binding:  KeyBinding{Key: "XF86AudioRaiseVolume"},
			expected: "Vol+",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BindingToString(tt.binding)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseBindingsJSON_Empty(t *testing.T) {
	bindings, err := ParseBindingsJSON("")
	if err != nil {
		t.Fatal(err)
	}
	if bindings != nil {
		t.Error("empty string should return nil")
	}
}

func TestParseBindingsJSON_RoundTrip(t *testing.T) {
	original := ActionBindings{
		ActionQuit:         {{Key: "Escape", Mods: []string{"Alt"}}},
		ActionOpenTerminal: {{Key: "t", Mods: []string{"WM"}}},
	}

	jsonStr, err := BindingsToJSON(original)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParseBindingsJSON(jsonStr)
	if err != nil {
		t.Fatal(err)
	}

	if len(parsed[ActionQuit]) != 1 || parsed[ActionQuit][0].Key != "Escape" {
		t.Errorf("quit binding round-trip failed: %v", parsed[ActionQuit])
	}
	if len(parsed[ActionOpenTerminal]) != 1 || parsed[ActionOpenTerminal][0].Key != "t" {
		t.Errorf("terminal binding round-trip failed: %v", parsed[ActionOpenTerminal])
	}
}

func TestActionDisplayName(t *testing.T) {
	// Verify a few known display names
	if name := ActionDisplayName(ActionQuit); name == "" {
		t.Error("quit should have a display name")
	}
	if name := ActionDisplayName(ActionShowLauncher); name == "" {
		t.Error("show_launcher should have a display name")
	}
	// Unknown action should return the action string itself or empty
	name := ActionDisplayName("unknown_action")
	if name != "" && name != "unknown_action" {
		t.Errorf("unexpected display name for unknown action: %q", name)
	}
}

func TestProtectedActionsCannotBeRemoved(t *testing.T) {
	user := ActionBindings{
		ActionQuit:            {}, // empty = user tried to remove
		ActionEmergencyLogout: {}, // empty = user tried to remove
	}
	merged := MergeWithDefaults(user)

	for _, action := range ProtectedActions() {
		if bindings, ok := merged[action]; !ok || len(bindings) == 0 {
			t.Errorf("protected action %q should have bindings after merge, got empty", action)
		}
	}
}

func TestActionOrder_ContainsAllDefaults(t *testing.T) {
	order := ActionOrder()
	defaults := DefaultBindings()

	orderSet := make(map[string]bool)
	for _, a := range order {
		orderSet[a] = true
	}

	for action := range defaults {
		if !orderSet[action] {
			t.Errorf("ActionOrder missing default action %q", action)
		}
	}
}
