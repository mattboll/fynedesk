package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OutputModeInfo is a mode an output can take: a real one (EDID) or a
// virtual resolution obtained by scaling (Custom).
type OutputModeInfo struct {
	Index       int    `json:"index"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	RefreshRate int    `json:"refresh_rate"`
	Current     bool   `json:"current"`
	Custom      bool   `json:"custom,omitempty"`       // true = virtual resolution (scale-based)
	AspectRatio string `json:"aspect_ratio,omitempty"` // e.g. "16:10", "16:9"
}

// CompositorOutputState describes an output in the compositor state.
type CompositorOutputState struct {
	OutputName            string           `json:"output_name"`
	Modes                 []OutputModeInfo `json:"modes"`
	PhysWidth             int              `json:"phys_width"`
	PhysHeight            int              `json:"phys_height"`
	Scale                 float32          `json:"scale"`
	Width                 int              `json:"width"`
	Height                int              `json:"height"`
	X                     int              `json:"x"`
	Y                     int              `json:"y"`
	Primary               bool             `json:"primary"`
	AdaptiveSyncEnabled   bool             `json:"adaptive_sync_enabled"`
	AdaptiveSyncSupported bool             `json:"adaptive_sync_supported"`
	// MirrorOf names the output this one shows the picture of. A mirrored
	// output is not part of the desktop: no windows, no position.
	MirrorOf string `json:"mirror_of,omitempty"`
	// Disabled is set for an output turned off: it is not part of the
	// desktop until it is enabled (layout request "enable").
	Disabled bool `json:"disabled,omitempty"`
}

// CompositorState is written by the compositor to compositor-state.json.
type CompositorState struct {
	Outputs []CompositorOutputState `json:"outputs"`
	// CanBlur is set when the compositor can blur what lies behind the
	// translucent windows of the panel (GLES2 renderer): only then are they
	// frosted glass, see-through otherwise.
	CanBlur bool `json:"can_blur,omitempty"`

	// Legacy single-output fields for backward compatibility with older panels
	OutputName string           `json:"output_name"`
	Modes      []OutputModeInfo `json:"modes"`
	PhysWidth  int              `json:"phys_width"`
	PhysHeight int              `json:"phys_height"`
	Scale      float32          `json:"scale"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
}

// AllOutputs returns the outputs, or the single output of an older state
// file (legacy fields), or nil.
func (s *CompositorState) AllOutputs() []CompositorOutputState {
	if len(s.Outputs) > 0 {
		return s.Outputs
	}
	if len(s.Modes) == 0 {
		return nil
	}
	return []CompositorOutputState{{
		OutputName: s.OutputName,
		Modes:      s.Modes,
		PhysWidth:  s.PhysWidth,
		PhysHeight: s.PhysHeight,
		Scale:      s.Scale,
		Width:      s.Width,
		Height:     s.Height,
		Primary:    true,
	}}
}

// CompositorStatePath is where the compositor writes its state.
func CompositorStatePath() string {
	return filepath.Join(ConfigDir(), "compositor-state.json")
}

// ReadCompositorState reads the state the compositor wrote.
func ReadCompositorState() (*CompositorState, error) {
	data, err := os.ReadFile(CompositorStatePath())
	if err != nil {
		return nil, err
	}
	var state CompositorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
