package ui

import (
	"testing"

	"fyne.io/fyne/v2/widget"
)

func TestAudioDeviceNamesUnique(t *testing.T) {
	objs := audioDeviceSelectorObjects("sink", []audioDevice{
		{id: "41", name: "USB Headset"},
		{id: "42", name: "USB Headset"},
		{id: "43", name: "Speakers"},
	}, 2)
	var sel *widget.Select
	for _, o := range objs {
		if s, ok := o.(*widget.Select); ok {
			sel = s
		}
	}
	if sel == nil {
		t.Fatal("no selector")
	}
	want := []string{"USB Headset", "USB Headset (2)", "Speakers"}
	for i, name := range want {
		if sel.Options[i] != name {
			t.Errorf("option %d = %q, want %q", i, sel.Options[i], name)
		}
	}
	if sel.Selected != "Speakers" {
		t.Errorf("selected %q", sel.Selected)
	}
}
