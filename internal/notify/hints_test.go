package notify

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestParseHints(t *testing.T) {
	for _, tc := range []struct {
		hints map[string]dbus.Variant
		want  Hints
	}{
		{nil, Hints{}},
		{map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(0))}, Hints{Urgency: "low"}},
		{map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(1))}, Hints{}},
		{map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(2))}, Hints{Urgency: "critical"}},
		{map[string]dbus.Variant{"urgency": dbus.MakeVariant("2")}, Hints{}},
		{map[string]dbus.Variant{"x-dunst-stack-tag": dbus.MakeVariant("vol")}, Hints{Tag: "vol"}},
		{map[string]dbus.Variant{"synchronous": dbus.MakeVariant("vol")}, Hints{Tag: "vol"}},
		{map[string]dbus.Variant{"transient": dbus.MakeVariant(true)}, Hints{Transient: true}},
		{map[string]dbus.Variant{"transient": dbus.MakeVariant(int32(1))}, Hints{Transient: true}},
		{map[string]dbus.Variant{
			"category":      dbus.MakeVariant("email.arrived"),
			"desktop-entry": dbus.MakeVariant("thunderbird"),
		}, Hints{Category: "email.arrived", DesktopEntry: "thunderbird"}},
	} {
		if got := ParseHints(tc.hints); got != tc.want {
			t.Errorf("ParseHints(%v) = %+v, want %+v", tc.hints, got, tc.want)
		}
	}
}
