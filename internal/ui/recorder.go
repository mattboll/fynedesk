package ui

import (
	"os/exec"

	"fyshos.com/tyde"
	"fyshos.com/tyde/wlipc"
)

func init() {
	tyde.RegisterModule(recorderModuleMeta)
}

// recorderModuleMeta describes the Screen Recorder module: Super+Shift+R,
// its indicator in the bar and its quick settings. It is offered once
// wf-recorder is found.
var recorderModuleMeta = tyde.ModuleMetadata{
	Name:        wlipc.RecorderModule,
	NewInstance: newRecorderModule,
}

// recorderModule shows the screen recorder while it is enabled.
type recorderModule struct{}

func newRecorderModule() tyde.Module {
	return &recorderModule{}
}

func (m *recorderModule) Metadata() tyde.ModuleMetadata {
	return recorderModuleMeta
}

func (m *recorderModule) Destroy() {}

// recorderInstalled reports whether wf-recorder, which records, is there.
func recorderInstalled() bool {
	_, err := exec.LookPath("wf-recorder")
	return err == nil
}
