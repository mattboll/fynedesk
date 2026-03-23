package wlipc

import (
	"testing"
)

func TestKeyboardLayout_ShortName(t *testing.T) {
	tests := []struct {
		name     string
		layout   KeyboardLayout
		expected string
	}{
		{
			name:     "simple layout",
			layout:   KeyboardLayout{Layout: "us"},
			expected: "US",
		},
		{
			name:     "layout with variant",
			layout:   KeyboardLayout{Layout: "fr", Variant: "bepo"},
			expected: "FR-BEPO",
		},
		{
			name:     "layout with empty variant",
			layout:   KeyboardLayout{Layout: "de", Variant: ""},
			expected: "DE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.layout.ShortName()
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseKeyboardLayoutPref_Empty(t *testing.T) {
	result := ParseKeyboardLayoutPref("")
	if result != nil {
		t.Errorf("empty pref should return nil, got %v", result)
	}
}

func TestParseKeyboardLayoutPref_Single(t *testing.T) {
	result := ParseKeyboardLayoutPref("us:")
	if len(result) != 1 {
		t.Fatalf("expected 1 layout, got %d", len(result))
	}
	if result[0].Layout != "us" {
		t.Errorf("layout: got %q, want %q", result[0].Layout, "us")
	}
	if result[0].Variant != "" {
		t.Errorf("variant: got %q, want empty", result[0].Variant)
	}
}

func TestParseKeyboardLayoutPref_Multiple(t *testing.T) {
	result := ParseKeyboardLayoutPref("us:|fr:bepo_afnor")
	if len(result) != 2 {
		t.Fatalf("expected 2 layouts, got %d", len(result))
	}
	if result[0].Layout != "us" {
		t.Errorf("layout 0: got %q, want %q", result[0].Layout, "us")
	}
	if result[1].Layout != "fr" || result[1].Variant != "bepo_afnor" {
		t.Errorf("layout 1: got %q:%q, want fr:bepo_afnor", result[1].Layout, result[1].Variant)
	}
}

func TestParseKeyboardLayoutPref_NoColon(t *testing.T) {
	result := ParseKeyboardLayoutPref("us")
	if len(result) != 1 {
		t.Fatalf("expected 1 layout, got %d", len(result))
	}
	if result[0].Layout != "us" {
		t.Errorf("layout: got %q, want %q", result[0].Layout, "us")
	}
	if result[0].Variant != "" {
		t.Errorf("variant: got %q, want empty", result[0].Variant)
	}
}

func TestFormatKeyboardLayoutPref(t *testing.T) {
	layouts := []KeyboardLayout{
		{Layout: "us", Variant: ""},
		{Layout: "fr", Variant: "bepo_afnor"},
	}

	result := FormatKeyboardLayoutPref(layouts)
	expected := "us:|fr:bepo_afnor"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestKeyboardLayoutPref_RoundTrip(t *testing.T) {
	original := []KeyboardLayout{
		{Layout: "de", Variant: "dvorak"},
		{Layout: "us", Variant: ""},
	}

	formatted := FormatKeyboardLayoutPref(original)
	parsed := ParseKeyboardLayoutPref(formatted)

	if len(parsed) != len(original) {
		t.Fatalf("round-trip: expected %d layouts, got %d", len(original), len(parsed))
	}
	for i := range original {
		if parsed[i].Layout != original[i].Layout || parsed[i].Variant != original[i].Variant {
			t.Errorf("round-trip layout %d: got %s:%s, want %s:%s",
				i, parsed[i].Layout, parsed[i].Variant, original[i].Layout, original[i].Variant)
		}
	}
}
