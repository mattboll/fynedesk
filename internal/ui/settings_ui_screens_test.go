package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"fyshos.com/tyde/locale"
)

func TestDetectPosition(t *testing.T) {
	ref := CompositorOutputState{OutputName: "eDP-1", Width: 1920, Height: 1200}

	right := CompositorOutputState{OutputName: "HDMI-A-1", X: 1920, Width: 2560, Height: 1440}
	assert.Equal(t, locale.T("screens.rightOf"), detectPosition(right, ref))

	above := CompositorOutputState{OutputName: "HDMI-A-1", Y: -1440, Width: 2560, Height: 1440}
	assert.Equal(t, locale.T("screens.above"), detectPosition(above, ref))

	mirror := CompositorOutputState{OutputName: "HDMI-A-1", MirrorOf: "eDP-1", Width: 2560, Height: 1440}
	assert.Equal(t, locale.T("screens.mirror"), detectPosition(mirror, ref))
}

func TestMirroredOutputControls(t *testing.T) {
	d := &settingsUI{}
	objs := d.outputControls(CompositorOutputState{OutputName: "HDMI-A-1", MirrorOf: "eDP-1"}, nil)
	assert.Len(t, objs, 1, "a mirrored output only offers to extend the desktop again")
}

func TestDisabledOutputControls(t *testing.T) {
	d := &settingsUI{}
	objs := d.outputControls(CompositorOutputState{OutputName: "eDP-1", Disabled: true}, nil)
	assert.Len(t, objs, 1, "an output that is off only offers to turn it on")
}
