package ui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

// Types of the compositor state, shared with it (see wlipc).
type (
	OutputModeInfo        = wlipc.OutputModeInfo
	CompositorOutputState = wlipc.CompositorOutputState
	CompositorState       = wlipc.CompositorState
)

// loadScreensGroup shows the outputs of a Wayland session, as described by
// the compositor, with their resolution, scale and arrangement.
func (d *settingsUI) loadScreensGroup() fyne.CanvasObject {
	var content fyne.CanvasObject = widget.NewLabel(locale.T("screens.notAvail"))
	if resolutionSelect := d.loadResolutionSelector(); resolutionSelect != nil {
		content = resolutionSelect
	}
	return widget.NewCard(locale.T("screens.title"), "", content)
}

func (d *settingsUI) loadResolutionSelector() fyne.CanvasObject {
	outputs := readCompositorOutputs()
	if len(outputs) == 0 {
		return nil
	}

	content := container.NewVBox()

	// Per-output controls container (refreshed when output selector changes)
	outputControls := container.NewVBox()
	buildOutputControls := func(out CompositorOutputState) {
		outputControls.Objects = d.outputControls(out, outputs)
		outputControls.Refresh()
	}

	// Output selector dropdown (only show if multiple outputs)
	if len(outputs) > 1 {
		var outputNames []string
		for _, out := range outputs {
			label := out.OutputName
			switch {
			case out.Primary:
				label += " (" + locale.T("screens.primaryShort") + ")"
			case out.MirrorOf != "":
				label += " (" + locale.T("screens.mirrorShort") + ")"
			case out.Disabled:
				label += " (" + locale.T("screens.offShort") + ")"
			}
			outputNames = append(outputNames, label)
		}
		outputSelect := widget.NewSelect(outputNames, func(selected string) {
			for i, name := range outputNames {
				if name == selected {
					buildOutputControls(outputs[i])
					break
				}
			}
		})
		outputSelect.SetSelectedIndex(0)
		content.Add(settingsRow(locale.T("screens.output"), nil, outputSelect))
	}

	// Build initial controls for first output
	buildOutputControls(outputs[0])
	content.Add(outputControls)

	return content
}

// readCompositorOutputs returns the outputs described in the compositor
// state file.
func readCompositorOutputs() []CompositorOutputState {
	state, err := wlipc.ReadCompositorState()
	if err != nil {
		return nil
	}
	return state.AllOutputs()
}

// settingsRow lays out a bold label before content, with an optional trailing object.
func settingsRow(label string, trailing, content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil,
		widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		trailing, content)
}

// outputControls builds the settings of one output: resolution, scale,
// position among the others, adaptive sync and a description.
func (d *settingsUI) outputControls(out CompositorOutputState, outputs []CompositorOutputState) []fyne.CanvasObject {
	if out.MirrorOf != "" {
		return d.mirrorControls(out)
	}
	if out.Disabled {
		turnOn := widget.NewButton(locale.T("screens.turnOn"), func() {
			d.requestLayoutChange(out.OutputName, "enable", "", false)
		})
		return []fyne.CanvasObject{settingsRow(locale.T("screens.state"), turnOn, widget.NewLabel(locale.T("screens.off")))}
	}
	if len(out.Modes) == 0 {
		return []fyne.CanvasObject{widget.NewLabel(locale.T("screens.noModes"))}
	}

	objects := []fyne.CanvasObject{d.resolutionRow(out), d.scaleRow(out)}
	if len(outputs) > 1 {
		objects = append(objects, d.positionRows(out, outputs)...)
	}

	// Adaptive sync (VRR/FreeSync) toggle
	if out.AdaptiveSyncSupported {
		vrrCheck := widget.NewCheck(locale.T("screens.adaptiveSync"), func(checked bool) {
			d.requestVRRChange(out.OutputName, checked)
		})
		vrrCheck.Checked = out.AdaptiveSyncEnabled
		objects = append(objects, vrrCheck)
	}

	if info := outputDescription(out); info != "" {
		objects = append(objects, widget.NewLabel(info))
	}
	return objects
}

// mirrorControls shows which output a mirrored one duplicates, with a button
// to extend the desktop onto it again.
func (d *settingsUI) mirrorControls(out CompositorOutputState) []fyne.CanvasObject {
	extend := widget.NewButton(locale.T("screens.extend"), func() {
		d.requestLayoutChange(out.OutputName, "right", out.MirrorOf, false)
	})
	return []fyne.CanvasObject{
		settingsRow(locale.T("screens.position"), extend,
			widget.NewLabel(fmt.Sprintf(locale.T("screens.mirroring"), out.MirrorOf))),
	}
}

// modeLabel describes a mode in the resolution list.
func modeLabel(mode OutputModeInfo) string {
	res := fmt.Sprintf("%dx%d", mode.Width, mode.Height)
	if mode.AspectRatio != "" {
		res += " (" + mode.AspectRatio + ")"
	}
	if mode.Custom {
		return res + " [" + locale.T("screens.scaled") + "]"
	}
	return fmt.Sprintf("%s @%dHz", res, mode.RefreshRate)
}

func (d *settingsUI) resolutionRow(out CompositorOutputState) fyne.CanvasObject {
	// Native resolution (first EDID mode) for scale computation
	var nativeW int
	for _, m := range out.Modes {
		if !m.Custom {
			nativeW = m.Width
			break
		}
	}

	var options []string
	var currentIndex int
	for i, mode := range out.Modes {
		options = append(options, modeLabel(mode))
		if mode.Current {
			currentIndex = i
		}
	}

	resSelect := widget.NewSelect(options, nil)
	resSelect.SetSelectedIndex(currentIndex)
	resSelect.OnChanged = func(string) {
		d.requestResolutionChange(out.Modes[resSelect.SelectedIndex()], nativeW, out.OutputName)
	}
	return settingsRow(locale.T("screens.resolution"), nil, resSelect)
}

func (d *settingsUI) scaleRow(out CompositorOutputState) fyne.CanvasObject {
	scaleOptions := []string{"1", "1.25", "1.5", "1.75", "2", "2.5", "3"}
	scaleSelect := widget.NewSelect(scaleOptions, nil)
	scaleSelect.SetSelected(strconv.FormatFloat(float64(out.Scale), 'f', -1, 32))
	scaleSelect.OnChanged = func(selected string) {
		scale, err := strconv.ParseFloat(selected, 32)
		if err != nil {
			return
		}
		d.requestScaleChange(float32(scale), out.OutputName)
	}
	return settingsRow(locale.T("screens.scale"), nil, scaleSelect)
}

// positionRows builds the primary checkbox and the position of out relative
// to another output.
func (d *settingsUI) positionRows(out CompositorOutputState, outputs []CompositorOutputState) []fyne.CanvasObject {
	primaryCheck := widget.NewCheck(locale.T("screens.primary"), func(checked bool) {
		if checked {
			d.requestLayoutChange(out.OutputName, "", "", true)
		}
	})
	primaryCheck.Checked = out.Primary
	var objects []fyne.CanvasObject
	if out.Primary {
		objects = append(objects, primaryCheck)
	} else {
		// Another output is primary: this one can be turned off.
		turnOff := widget.NewButton(locale.T("screens.turnOff"), func() {
			d.requestLayoutChange(out.OutputName, "disable", "", false)
		})
		turnOff.Importance = widget.LowImportance
		objects = append(objects, container.NewBorder(nil, nil, nil, turnOff, primaryCheck))
	}

	var otherNames []string
	for _, o := range outputs {
		if o.OutputName != out.OutputName && o.MirrorOf == "" && !o.Disabled {
			otherNames = append(otherNames, o.OutputName)
		}
	}
	if len(otherNames) == 0 {
		return objects
	}

	// Display text and the IPC value of each position
	positions := []struct{ label, ipc string }{
		{locale.T("screens.rightOf"), "right"},
		{locale.T("screens.leftOf"), "left"},
		{locale.T("screens.above"), "above"},
		{locale.T("screens.below"), "below"},
		{locale.T("screens.mirror"), "mirror"},
	}
	var positionOptions []string
	for _, p := range positions {
		positionOptions = append(positionOptions, p.label)
	}

	posSelect := widget.NewSelect(positionOptions, nil)
	refSelect := widget.NewSelect(otherNames, nil)
	// Pre-select current position relative to first other output
	if ref := findOutputByName(outputs, otherNames[0]); ref != nil {
		posSelect.SetSelected(detectPosition(out, *ref))
	}
	refSelect.SetSelectedIndex(0)

	applyPos := widget.NewButton(locale.T("screens.applyPos"), func() {
		index := posSelect.SelectedIndex()
		if index < 0 || refSelect.Selected == "" {
			return
		}
		d.requestLayoutChange(out.OutputName, positions[index].ipc, refSelect.Selected, false)
	})
	return append(objects, settingsRow(locale.T("screens.position"), applyPos,
		container.NewGridWithColumns(2, posSelect, refSelect)))
}

// outputDescription gives the physical size, density and layout of an output.
func outputDescription(out CompositorOutputState) string {
	if out.PhysWidth > 0 && out.PhysHeight > 0 {
		dpi := float64(out.Width) * float64(out.Scale) / (float64(out.PhysWidth) / 25.4)
		return locale.Tf("screens.physicalDesc",
			out.PhysWidth, out.PhysHeight, dpi, out.Width, out.Height, out.X, out.Y)
	}
	if out.Width > 0 {
		return locale.Tf("screens.logicalDesc", out.Width, out.Height, out.X, out.Y)
	}
	return ""
}

// requestResolutionChange sends either a ModeRequest (EDID) or ScaleRequest (virtual).
func (d *settingsUI) requestResolutionChange(mode OutputModeInfo, nativeW int, outputName string) {
	if mode.Custom && nativeW > 0 {
		scale := float32(nativeW) / float32(mode.Width)
		d.requestScaleChange(scale, outputName)
		return
	}
	d.requestModeChange(mode.Index, outputName)
}

func (d *settingsUI) requestModeChange(modeIndex int, outputName string) {
	if err := wlipc.RequestModeChange(modeIndex, outputName); err != nil {
		fyne.LogError("Failed to write mode request", err)
	}
}

func (d *settingsUI) requestScaleChange(scale float32, outputName string) {
	if err := wlipc.RequestScaleChange(scale, outputName); err != nil {
		fyne.LogError("Failed to write scale request", err)
	}
}

func (d *settingsUI) requestVRRChange(outputName string, enabled bool) {
	if err := wlipc.RequestVRRChange(outputName, enabled); err != nil {
		fyne.LogError("Failed to write VRR request", err)
	}
}

func (d *settingsUI) requestLayoutChange(outputName, position, relativeTo string, primary bool) {
	fmt.Printf("[SETTINGS] requestLayoutChange: output=%q pos=%q ref=%q primary=%v\n",
		outputName, position, relativeTo, primary)
	err := wlipc.RequestOutputLayout(wlipc.LayoutRequest{
		OutputName: outputName,
		Position:   position,
		RelativeTo: relativeTo,
		Primary:    primary,
	})
	if err != nil {
		fyne.LogError("Failed to write layout request", err)
	}
}

// detectPosition determines the current position of out relative to ref.
func detectPosition(out, ref CompositorOutputState) string {
	if out.MirrorOf == ref.OutputName {
		return locale.T("screens.mirror")
	}
	if out.X >= ref.X+ref.Width {
		return locale.T("screens.rightOf")
	}
	if out.X+out.Width <= ref.X {
		return locale.T("screens.leftOf")
	}
	if out.Y+out.Height <= ref.Y {
		return locale.T("screens.above")
	}
	if out.Y >= ref.Y+ref.Height {
		return locale.T("screens.below")
	}
	return locale.T("screens.rightOf") // default
}

// findOutputByName finds an output by name in a slice.
func findOutputByName(outputs []CompositorOutputState, name string) *CompositorOutputState {
	for i := range outputs {
		if outputs[i].OutputName == name {
			return &outputs[i]
		}
	}
	return nil
}
