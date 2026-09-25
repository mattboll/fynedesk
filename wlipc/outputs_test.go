package wlipc

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReadCompositorState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := ReadCompositorState(); err == nil {
		t.Fatal("no state file: an error is expected")
	}

	want := CompositorState{Outputs: []CompositorOutputState{
		{OutputName: "eDP-1", Width: 1920, Height: 1200, Primary: true},
		{OutputName: "HDMI-A-1", MirrorOf: "eDP-1"},
	}}
	data, _ := json.Marshal(want)
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CompositorStatePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadCompositorState()
	if err != nil {
		t.Fatal(err)
	}
	outs := got.AllOutputs()
	if len(outs) != 2 || outs[1].MirrorOf != "eDP-1" || !outs[0].Primary {
		t.Fatalf("unexpected outputs: %+v", outs)
	}
}

func TestAllOutputsLegacy(t *testing.T) {
	legacy := CompositorState{OutputName: "DP-1", Modes: []OutputModeInfo{{Width: 2560, Height: 1440, Current: true}}, Width: 2560}
	outs := legacy.AllOutputs()
	if len(outs) != 1 || outs[0].OutputName != "DP-1" || !outs[0].Primary {
		t.Fatalf("legacy state: got %+v", outs)
	}
	if (&CompositorState{}).AllOutputs() != nil {
		t.Fatal("an empty state has no outputs")
	}
}
