package compositor

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
)

func (s *server) writeCompositorState() {
	if len(s.outputs) == 0 {
		return
	}

	state := CompositorState{}

	pOut := s.primaryOutput()
	for _, out := range s.outputs {
		physW, physH := getOutputPhysSize(out)
		currentScale := out.output.Scale()
		isPrimary := out == pOut

		outState := CompositorOutputState{
			OutputName:           out.output.Name(),
			PhysWidth:            physW,
			PhysHeight:           physH,
			Scale:                currentScale,
			Width:                out.width,
			Height:               out.height,
			X:                    out.layoutX,
			Y:                    out.layoutY,
			Primary:              isPrimary,
			AdaptiveSyncEnabled:  out.vrrEnabled,
			AdaptiveSyncSupported: !s.nestedMode, // DRM backend supports VRR; nested does not
		}

		// EDID modes (real hardware modes)
		for mi, mode := range out.modes {
			w, h := int(mode.Width()), int(mode.Height())
			outState.Modes = append(outState.Modes, OutputModeInfo{
				Index:       mi,
				Width:       w,
				Height:      h,
				RefreshRate: int(mode.RefreshRate() / 1000),
				Current:     mi == out.currentMode && currentScale == 1.0,
				AspectRatio: aspectRatio(w, h),
			})
		}

		// Virtual resolutions (scale-based, keep native DRM mode)
		if len(out.modes) > 0 {
			nativeW := int(out.modes[0].Width())
			nativeH := int(out.modes[0].Height())
			for _, cm := range commonResolutions(nativeW, nativeH) {
				isCurrent := out.width == cm.Width && out.height == cm.Height
				outState.Modes = append(outState.Modes, OutputModeInfo{
					Width:       cm.Width,
					Height:      cm.Height,
					RefreshRate: 0,
					Current:     isCurrent,
					Custom:      true,
					AspectRatio: cm.AspectRatio,
				})
			}
		}

		state.Outputs = append(state.Outputs, outState)

		// Populate legacy fields from primary output for backward compat
		if isPrimary {
			state.OutputName = outState.OutputName
			state.Modes = outState.Modes
			state.PhysWidth = outState.PhysWidth
			state.PhysHeight = outState.PhysHeight
			state.Scale = outState.Scale
			state.Width = outState.Width
			state.Height = outState.Height
		}
	}

	configDir := s.getConfigDir()
	statePath := filepath.Join(configDir, "compositor-state.json")

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		log.Printf("Error marshaling compositor state: %v\n", err)
		return
	}

	if err := atomicWriteFile(statePath, data); err != nil {
		log.Printf("Error writing compositor state: %v\n", err)
		return
	}
}

func (s *server) readLayoutConfig() *OutputLayoutConfig {
	configDir := s.getConfigDir()
	data, err := os.ReadFile(filepath.Join(configDir, "output-layout.json"))
	if err != nil {
		return nil
	}
	var config OutputLayoutConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}
	return &config
}

func (s *server) writeLayoutConfig(config *OutputLayoutConfig) {
	configDir := s.getConfigDir()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("Error marshaling layout config: %v\n", err)
		return
	}
	if err := atomicWriteFile(filepath.Join(configDir, "output-layout.json"), data); err != nil {
		log.Printf("Error writing layout config: %v\n", err)
	}
}

func (s *server) persistCurrentLayout(outputName, position, relativeTo string) {
	config := s.readLayoutConfig()
	if config == nil {
		config = &OutputLayoutConfig{Layouts: map[string]OutputLayoutEntry{}}
	}
	if config.Layouts == nil {
		config.Layouts = map[string]OutputLayoutEntry{}
	}
	config.Layouts[outputName] = OutputLayoutEntry{
		Position:   position,
		RelativeTo: relativeTo,
	}
	// Also store the inverse relationship so either output can connect first
	inverse := map[string]string{
		"left": "right", "right": "left",
		"above": "below", "below": "above",
		"mirror": "mirror",
	}
	if inv, ok := inverse[position]; ok {
		config.Layouts[relativeTo] = OutputLayoutEntry{
			Position:   inv,
			RelativeTo: outputName,
		}
	}
	config.Primary = s.primaryOutputName
	s.writeLayoutConfig(config)
}

func (s *server) setResolution(req ModeRequest) {
	// Find target output by name (empty = primary for backward compat)
	out := s.findOutputByName(req.OutputName)
	if out == nil {
		return
	}
	if req.ModeIndex < 0 || req.ModeIndex >= len(out.modes) {
		log.Printf("Invalid mode index: %d\n", req.ModeIndex)
		return
	}

	// Skip if already at this mode
	if out.currentMode == req.ModeIndex {
		return
	}

	out.output.SetMode(out.modes[req.ModeIndex])
	out.currentMode = req.ModeIndex

	// Reset scale to 1.0 when switching EDID modes
	out.output.SetScale(1.0)
	out.output.Enable(true)
	out.output.Commit()

	out.width, out.height = out.output.EffectiveResolution()

	s.writeCompositorState()
	s.loadSettings()

	// Restart panel if this is the primary output (resolution changed)
	if out == s.primaryOutput() {
		s.restartPanel()
	}

	log.Printf("Resolution changed to %dx%d (mode %d) on %s\n", out.width, out.height, req.ModeIndex, out.output.Name())
}

func (s *server) setOutputScale(req ScaleRequest) {
	// Find target output by name (empty = primary for backward compat)
	out := s.findOutputByName(req.OutputName)
	if out == nil {
		return
	}
	if req.Scale < 1.0 || req.Scale > 4.0 {
		log.Printf("Invalid scale: %.2f (must be 1.0-4.0)\n", req.Scale)
		return
	}

	// Skip if already at this scale
	if out.output.Scale() == req.Scale {
		return
	}

	out.output.SetScale(req.Scale)
	out.output.Commit()
	out.width, out.height = out.output.EffectiveResolution()

	// Update compositor state file
	s.writeCompositorState()

	// Reload cursor for new scale
	s.cursorMgr.Load(float64(req.Scale))
	s.cursor.SetXCursor(s.cursorMgr, "default")

	// Restart panel if this is the primary output (scale changed → different logical resolution)
	if out == s.primaryOutput() {
		s.restartPanel()
	}

	// Reload wallpaper at new resolution
	s.loadSettings()

	log.Printf("Scale changed to %.1f, logical resolution: %dx%d on %s\n", req.Scale, out.width, out.height, out.output.Name())
}

// setOutputVRR enables or disables adaptive sync (VRR) on an output and persists the setting.
func (s *server) setOutputVRR(req VRRRequest) {
	out := s.findOutputByName(req.OutputName)
	if out == nil {
		return
	}
	s.enableAdaptiveSync(out, req.Enabled)

	// Persist VRR setting in layout config
	config := s.readLayoutConfig()
	if config == nil {
		config = &OutputLayoutConfig{Layouts: map[string]OutputLayoutEntry{}}
	}
	if config.Layouts == nil {
		config.Layouts = map[string]OutputLayoutEntry{}
	}
	name := out.output.Name()
	entry := config.Layouts[name]
	entry.AdaptiveSync = &req.Enabled
	config.Layouts[name] = entry
	s.writeLayoutConfig(config)

	s.writeCompositorState()
	log.Printf("[VRR] Set adaptive sync=%v on %s\n", req.Enabled, name)
}

// findOutputByName returns the output with the given name, or primary if name is empty
func (s *server) findOutputByName(name string) *outputState {
	if name == "" {
		return s.primaryOutput()
	}
	for _, out := range s.outputs {
		if out.output.Name() == name {
			return out
		}
	}
	log.Printf("Output not found: %s, falling back to primary\n", name)
	return s.primaryOutput()
}

// findBestMode returns the index of the EDID mode that exactly matches wantW×wantH
// on the given output. Prefers the highest refresh rate. Returns -1 if no match or
// if the output is already at that resolution.
func (s *server) findBestMode(out *outputState, wantW, wantH int) int {
	// Already at requested resolution — no switch needed
	if out.width == wantW && out.height == wantH {
		return -1
	}
	bestIdx := -1
	bestRefresh := int32(0)
	for i, mode := range out.modes {
		if int(mode.Width()) == wantW && int(mode.Height()) == wantH {
			if mode.RefreshRate() > bestRefresh {
				bestIdx = i
				bestRefresh = mode.RefreshRate()
			}
		}
	}
	return bestIdx
}

// switchModeForFullscreen adjusts the output for a fullscreen surface that
// renders at wantW×wantH. Strategy:
//  1. Try to find an exact EDID mode match (DRM mode switch, best perf)
//  2. If no mode match, use output.SetScale(nativeW/wantW) to create a
//     virtual resolution matching the surface — GPU upscales, zero CPU cost
//
// Saves the current mode and scale so they can be restored on fullscreen exit.
// Returns the updated output geometry after adjustment.
func (s *server) switchModeForFullscreen(out *outputState, wantW, wantH int) outputGeometry {
	// Already at the right logical resolution
	if out.width == wantW && out.height == wantH {
		return s.getOutputGeometry(out)
	}

	// Save current state (only once — don't overwrite on second fullscreen)
	if out.savedScale == 0 {
		out.savedScale = out.output.Scale()
	}
	if out.savedMode < 0 {
		out.savedMode = out.currentMode
	}

	// Strategy 1: try exact EDID mode match
	modeIdx := s.findBestMode(out, wantW, wantH)
	if modeIdx >= 0 {
		mode := out.modes[modeIdx]
		out.output.SetMode(mode)
		out.output.SetScale(1.0)
		out.output.Commit()
		out.currentMode = modeIdx
		out.width, out.height = out.output.EffectiveResolution()

		log.Printf("[FULLSCREEN] Mode switch: %s → %dx%d @%dHz\n",
			out.output.Name(), out.width, out.height, mode.RefreshRate()/1000)

		s.writeCompositorState()
		return s.getOutputGeometry(out)
	}

	// Strategy 2: scale the output so logical resolution matches the surface.
	// Keep native DRM mode, let the GPU upscale via wlr_scene.
	// Use the native mode dimensions (not current effective, which may be scaled).
	nativeW := out.width
	nativeH := out.height
	if len(out.modes) > 0 && out.currentMode < len(out.modes) {
		nativeW = int(out.modes[out.currentMode].Width())
		nativeH = int(out.modes[out.currentMode].Height())
	}

	scaleX := float32(nativeW) / float32(wantW)
	scaleY := float32(nativeH) / float32(wantH)
	// Use the larger scale to ensure the surface fills the screen.
	// If aspect ratios differ, the game will have black bars on one axis.
	scale := scaleX
	if scaleY > scaleX {
		scale = scaleY
	}
	// Clamp to reasonable range
	if scale < 1.0 {
		scale = 1.0
	}
	if scale > 4.0 {
		scale = 4.0
	}

	out.output.SetScale(scale)
	out.output.Commit()
	out.width, out.height = out.output.EffectiveResolution()

	log.Printf("[FULLSCREEN] Scale switch: %s → scale=%.3f logical=%dx%d (native=%dx%d, game=%dx%d)\n",
		out.output.Name(), scale, out.width, out.height, nativeW, nativeH, wantW, wantH)

	s.writeCompositorState()
	return s.getOutputGeometry(out)
}

// restoreModeAfterFullscreen restores the output mode and scale saved before
// fullscreen. Restarts the panel if this is the primary output.
func (s *server) restoreModeAfterFullscreen(out *outputState) {
	needRestore := false

	// Restore mode if it was changed
	if out.savedMode >= 0 && out.savedMode < len(out.modes) && out.savedMode != out.currentMode {
		out.output.SetMode(out.modes[out.savedMode])
		out.currentMode = out.savedMode
		needRestore = true
	}
	out.savedMode = -1

	// Restore scale if it was changed
	if out.savedScale > 0 && out.savedScale != out.output.Scale() {
		out.output.SetScale(out.savedScale)
		needRestore = true
	}
	out.savedScale = 0

	if !needRestore {
		return
	}

	out.output.Commit()
	out.width, out.height = out.output.EffectiveResolution()

	log.Printf("[FULLSCREEN] Restored: %s → %dx%d scale=%.1f\n",
		out.output.Name(), out.width, out.height, out.output.Scale())

	s.writeCompositorState()
	s.loadWallpaperForNewOutput(out)

	if out == s.primaryOutput() {
		s.restartPanel()
	}
}

// restartPanel kills the panel process so it auto-restarts with new primary dimensions.
func (s *server) restartPanel() {
	if s.panelCmd != nil && s.panelCmd.Process != nil {
		log.Printf("[RESTART] Killing panel PID=%d for restart\n", s.panelCmd.Process.Pid)
		s.panelCmd.Process.Kill()
		// The auto-restart goroutine in startPanel() will relaunch with new primary dimensions
	} else {
		log.Println("[RESTART] No panel process to kill")
	}
}

// disableOutput disables an output at the DRM level, stopping all commits.
// This is used to turn off laptop screens (eDP) when an external monitor is primary.
// It disables the output then delegates cleanup to handleOutputDestroy (which lives
// in output.go and has access to CGO symbols).
func (s *server) disableOutput(name string) {
	out := s.findOutputByName(name)
	if out == nil {
		log.Printf("[LAYOUT] disableOutput: output %q not found\n", name)
		return
	}
	if out == s.primaryOutput() {
		log.Printf("[LAYOUT] disableOutput: refusing to disable primary output %q\n", name)
		return
	}

	log.Printf("[LAYOUT] Disabling output %q\n", name)

	// Disable at DRM level — tells kernel to stop scanning out
	out.output.Enable(false)
	out.output.Commit()

	// Reuse handleOutputDestroy for all CGO cleanup (frame listener, wallpaper,
	// scene nodes), window migration, and output list management.
	s.handleOutputDestroy(out)

	// Persist disable in layout config so it stays disabled on restart
	s.persistCurrentLayout(name, "disable", "")
}

// setOutputLayout repositions an output relative to another, or sets primary.
func (s *server) setOutputLayout(req LayoutRequest) {
	log.Printf("[LAYOUT] setOutputLayout: output=%q pos=%q ref=%q primary=%v\n",
		req.OutputName, req.Position, req.RelativeTo, req.Primary)
	// Handle disable request
	if req.Position == "disable" {
		s.disableOutput(req.OutputName)
		return
	}

	// Handle primary-only request (no position change)
	if req.Primary && req.Position == "" {
		log.Printf("[LAYOUT] → primary-only change for %q\n", req.OutputName)
		s.setPrimaryOutput(req.OutputName)
		return
	}

	out := s.findOutputByName(req.OutputName)
	ref := s.findOutputByName(req.RelativeTo)
	if out == nil || ref == nil {
		log.Printf("setOutputLayout: output %q or ref %q not found\n", req.OutputName, req.RelativeTo)
		return
	}

	// Note: primary change is deferred to setPrimaryOutput() below so it can
	// observe the truly-old primary and decide whether to restart vs reposition
	// the panel based on the resolution change.

	// Calculate position relative to reference
	refGeo := s.getOutputGeometry(ref)
	var newX, newY int
	switch req.Position {
	case "right":
		newX = refGeo.x + refGeo.width
		newY = refGeo.y
	case "left":
		newX = refGeo.x - out.width
		newY = refGeo.y
	case "above":
		newX = refGeo.x
		newY = refGeo.y - out.height
	case "below":
		newX = refGeo.x
		newY = refGeo.y + refGeo.height
	case "mirror":
		newX = refGeo.x
		newY = refGeo.y
	default:
		log.Printf("setOutputLayout: unknown position %q\n", req.Position)
		return
	}

	// Reposition in wlr_output_layout (Add replaces existing entry)
	s.outLayout.Add(out.output, newX, newY)

	// Normalize all positions so minimum is (0,0) — avoids negative coordinates
	// which can cause rendering issues with scene outputs
	s.normalizeOutputPositions()

	// Move windows that ended up outside any output's viewport
	s.repositionWindowsAfterLayoutChange()

	// Persist + update IPC
	s.persistCurrentLayout(req.OutputName, req.Position, req.RelativeTo)
	s.writeCompositorState()

	// Reconfigure panel on (possibly new) primary
	if req.Primary {
		s.setPrimaryOutput(req.OutputName) // Handles reposition vs restart
	} else {
		s.repositionPanel() // Just position change, same primary
	}
	s.repositionSecondaryPanels()

	// Re-fit windows to the new layout (after panel reposition so contentBounds
	// reflects the current primary / reserved areas).
	s.refitWindowsToOutputs()

	// Log final state of all outputs
	for _, o := range s.outputs {
		isPrim := o == s.primaryOutput()
		log.Printf("[LAYOUT] Final: %s at (%d,%d) %dx%d primary=%v\n",
			o.output.Name(), o.layoutX, o.layoutY, o.width, o.height, isPrim)
	}
}

// repositionWindowsAfterLayoutChange moves windows that ended up outside
// any output's viewport back onto the nearest output.
func (s *server) repositionWindowsAfterLayoutChange() {
	if len(s.outputs) == 0 {
		return
	}

	// Build bounding boxes for all outputs
	type rect struct{ x, y, w, h int }
	var rects []rect
	for _, o := range s.outputs {
		rects = append(rects, rect{o.layoutX, o.layoutY, o.width, o.height})
	}

	// Check if a point is inside any output
	pointInAnyOutput := func(px, py float64) bool {
		for _, r := range rects {
			if px >= float64(r.x) && px < float64(r.x+r.w) &&
				py >= float64(r.y) && py < float64(r.y+r.h) {
				return true
			}
		}
		return false
	}

	// Find nearest output for a point (returns its geometry)
	nearestOutput := func(px, py float64) rect {
		best := rects[0]
		bestDist := math.MaxFloat64
		for _, r := range rects {
			// Clamp point to rect and compute distance
			cx := math.Max(float64(r.x), math.Min(px, float64(r.x+r.w-1)))
			cy := math.Max(float64(r.y), math.Min(py, float64(r.y+r.h-1)))
			dx := px - cx
			dy := py - cy
			d := dx*dx + dy*dy
			if d < bestDist {
				bestDist = d
				best = r
			}
		}
		return best
	}

	moved := 0

	// Reposition XDG views
	for _, v := range s.xdgViews {
		if !v.mapped || v.minimized {
			continue
		}
		if pointInAnyOutput(v.x+50, v.y+20) {
			continue // Titlebar area is visible
		}
		r := nearestOutput(v.x, v.y)
		v.x = float64(r.x) + 50
		v.y = float64(r.y) + 50
		setXdgScenePos(v)
		moved++
		log.Printf("[LAYOUT] Repositioned XDG %s to (%.0f,%.0f)\n", v.id, v.x, v.y)
	}

	// Reposition XWayland views
	for _, v := range s.xwayViews {
		if !v.mapped || v.minimized || v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		if pointInAnyOutput(v.x+50, v.y+20) {
			continue
		}
		r := nearestOutput(v.x, v.y)
		v.x = float64(r.x) + 50
		v.y = float64(r.y) + 50
		v.surface.Configure(int16(v.x), int16(v.y), uint16(v.surface.Width()), uint16(v.surface.Height()))
		setXwayScenePos(v)
		moved++
		log.Printf("[LAYOUT] Repositioned XWay %s to (%.0f,%.0f)\n", v.id, v.x, v.y)
	}

	if moved > 0 {
		log.Printf("[LAYOUT] Repositioned %d windows after layout change\n", moved)
	}
}

// setPrimaryOutput changes which output is primary (gets the panel).
func (s *server) setPrimaryOutput(name string) {
	oldPrimary := s.primaryOutput()
	s.primaryOutputName = name
	config := s.readLayoutConfig()
	if config == nil {
		config = &OutputLayoutConfig{Layouts: map[string]OutputLayoutEntry{}}
	}
	config.Primary = name
	s.writeLayoutConfig(config)
	s.writeCompositorState()

	newPrimary := s.primaryOutput()
	if newPrimary == nil {
		log.Printf("[PRIMARY] Primary set to %s but output not found!\n", name)
		return
	}

	newGeo := s.getOutputGeometry(newPrimary)
	log.Printf("[PRIMARY] Primary set to %s → geo=(%d,%d %dx%d)\n",
		name, newGeo.x, newGeo.y, newGeo.width, newGeo.height)

	// If resolution changed, restart panel (Fyne can't resize externally)
	if oldPrimary != nil && (oldPrimary.width != newPrimary.width || oldPrimary.height != newPrimary.height) {
		log.Printf("[PRIMARY] Resolution changed (%dx%d → %dx%d), restarting panel\n",
			oldPrimary.width, oldPrimary.height, newPrimary.width, newPrimary.height)
		s.restartPanel()
	} else {
		// Same resolution, just move the panel
		log.Println("[PRIMARY] Same resolution, repositioning panel")
		s.repositionPanel()
	}
	s.repositionSecondaryPanels()
}
