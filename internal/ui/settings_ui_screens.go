package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

// OutputModeInfo matches the compositor's JSON structure
type OutputModeInfo struct {
	Index       int    `json:"index"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	RefreshRate int    `json:"refresh_rate"`
	Current     bool   `json:"current"`
	Custom      bool   `json:"custom,omitempty"`       // true = virtual resolution (scale-based)
	AspectRatio string `json:"aspect_ratio,omitempty"` // e.g. "16:10", "16:9"
}

// CompositorOutputState describes a single output in the compositor state
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
}

// CompositorState matches the compositor's JSON structure
type CompositorState struct {
	Outputs []CompositorOutputState `json:"outputs"`

	// Legacy single-output fields for backward compat
	OutputName string           `json:"output_name"`
	Modes      []OutputModeInfo `json:"modes"`
	PhysWidth  int              `json:"phys_width"`
	PhysHeight int              `json:"phys_height"`
	Scale      float32          `json:"scale"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
}

const randrHelper = "arandr"

func loadScreensTable() fyne.CanvasObject {
	labels1 := container.NewVBox()
	values1 := container.NewVBox()
	labels2 := container.NewVBox()
	values2 := container.NewVBox()

	all := container.NewVBox()
	for _, screen := range fynedesk.Instance().Screens().Screens() {
		all.Add(widget.NewLabelWithStyle(screen.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		labels1.Add(widget.NewLabel(locale.T("screens.width")))
		values1.Add(widget.NewLabel(strconv.Itoa(screen.Width) + "px"))
		labels2.Add(widget.NewLabel(locale.T("screens.height")))
		values2.Add(widget.NewLabel(strconv.Itoa(screen.Height) + "px"))

		labels1.Add(widget.NewLabel(locale.T("screens.scale")))
		values1.Add(widget.NewLabel(strconv.FormatFloat(float64(screen.Scale), 'f', 1, 32)))
		labels2.Add(widget.NewLabel(locale.T("screens.applied")))
		values2.Add(widget.NewLabel(strconv.FormatFloat(float64(screen.CanvasScale()), 'f', 1, 32)))
		all.Add(container.NewHBox(labels1, values1, labels2, values2))
	}

	return all
}

func (d *settingsUI) loadScreensGroup() fyne.CanvasObject {
	userScale := fyne.CurrentApp().Settings().Scale()
	if userScale == 0.0 {
		userScale = 1.0
	}
	content := container.NewVBox(widget.NewLabel(locale.T("screens.userScale") + " " + strconv.FormatFloat(float64(userScale), 'f', 2, 32)))

	// Try to load compositor state for resolution selection
	resolutionSelect := d.loadResolutionSelector()
	if resolutionSelect != nil {
		content.Add(resolutionSelect)
	} else {
		// Fallback to arandr for X11
		if _, err := exec.LookPath(randrHelper); err == nil {
			displays := widget.NewButtonWithIcon(locale.T("screens.manage"), wmtheme.DisplayIcon, func() {
				e := exec.Command(randrHelper).Start()
				if e != nil {
					fyne.LogError("", e)
				}
			})
			content.Add(displays)
		} else {
			content.Add(widget.NewLabel(locale.T("screens.notAvail")))
		}
	}

	content.Add(loadScreensTable())
	screens := widget.NewCard(locale.T("screens.title"), "", content)
	return screens
}

func (d *settingsUI) loadResolutionSelector() fyne.CanvasObject {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	statePath := filepath.Join(home, ".config", "fynedesk", "compositor-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}

	var state CompositorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}

	// Build list of outputs (prefer multi-output, fall back to legacy)
	var outputs []CompositorOutputState
	if len(state.Outputs) > 0 {
		outputs = state.Outputs
	} else if len(state.Modes) > 0 {
		outputs = []CompositorOutputState{{
			OutputName: state.OutputName,
			Modes:      state.Modes,
			PhysWidth:  state.PhysWidth,
			PhysHeight: state.PhysHeight,
			Scale:      state.Scale,
			Width:      state.Width,
			Height:     state.Height,
			Primary:    true,
		}}
	}

	if len(outputs) == 0 {
		return nil
	}

	content := container.NewVBox()

	// Per-output controls container (refreshed when output selector changes)
	outputControls := container.NewVBox()

	// Build controls for a specific output
	buildOutputControls := func(out CompositorOutputState) {
		outputControls.Objects = nil

		if len(out.Modes) == 0 {
			outputControls.Add(widget.NewLabel(locale.T("screens.noModes")))
			outputControls.Refresh()
			return
		}

		// Find native resolution (first EDID mode) for scale computation
		var nativeW int
		for _, m := range out.Modes {
			if !m.Custom {
				nativeW = m.Width
				break
			}
		}

		// Resolution selector
		var options []string
		var currentIndex int
		for i, mode := range out.Modes {
			var option string
			ar := mode.AspectRatio
			if mode.Custom {
				if ar != "" {
					option = fmt.Sprintf("%dx%d (%s) [scaled]", mode.Width, mode.Height, ar)
				} else {
					option = fmt.Sprintf("%dx%d [scaled]", mode.Width, mode.Height)
				}
			} else {
				if ar != "" {
					option = fmt.Sprintf("%dx%d (%s) @%dHz", mode.Width, mode.Height, ar, mode.RefreshRate)
				} else {
					option = fmt.Sprintf("%dx%d @%dHz", mode.Width, mode.Height, mode.RefreshRate)
				}
			}
			options = append(options, option)
			if mode.Current {
				currentIndex = i
			}
		}

		modes := out.Modes
		native := nativeW
		outName := out.OutputName
		resSelect := widget.NewSelect(options, nil)
		resSelect.SetSelectedIndex(currentIndex)
		resSelect.OnChanged = func(selected string) {
			for i, opt := range options {
				if opt == selected {
					d.requestResolutionChange(modes[i], native, outName)
					break
				}
			}
		}
		outputControls.Add(container.NewBorder(nil, nil,
			widget.NewLabelWithStyle(locale.T("screens.resolution"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, resSelect))

		// Scale selector
		scaleOptions := []string{"1", "1.25", "1.5", "1.75", "2", "2.5", "3"}
		currentScaleStr := strconv.FormatFloat(float64(out.Scale), 'f', -1, 32)
		scaleSelect := widget.NewSelect(scaleOptions, nil)
		for _, opt := range scaleOptions {
			if opt == currentScaleStr {
				scaleSelect.SetSelected(opt)
				break
			}
		}
		if scaleSelect.Selected == "" {
			scaleSelect.SetSelected(currentScaleStr)
		}
		scaleSelect.OnChanged = func(selected string) {
			scale, err := strconv.ParseFloat(selected, 32)
			if err != nil {
				return
			}
			d.requestScaleChange(float32(scale), outName)
		}
		outputControls.Add(container.NewBorder(nil, nil,
			widget.NewLabelWithStyle(locale.T("screens.scale"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, scaleSelect))

		// Position and primary controls (only when multiple outputs)
		if len(outputs) > 1 {
			// Primary checkbox
			primaryCheck := widget.NewCheck(locale.T("screens.primary"), func(checked bool) {
				fmt.Printf("[SETTINGS] Primary checkbox: output=%q checked=%v\n", out.OutputName, checked)
				if checked {
					d.requestLayoutChange(out.OutputName, "", "", true)
				}
			})
			primaryCheck.Checked = out.Primary
			outputControls.Add(primaryCheck)

			// Position selector
			positionOptions := []string{
				locale.T("screens.rightOf"),
				locale.T("screens.leftOf"),
				locale.T("screens.above"),
				locale.T("screens.below"),
				locale.T("screens.mirror"),
			}

			// Build list of other output names
			var otherNames []string
			for _, o := range outputs {
				if o.OutputName != out.OutputName {
					otherNames = append(otherNames, o.OutputName)
				}
			}

			if len(otherNames) > 0 {
				posSelect := widget.NewSelect(positionOptions, nil)
				refSelect := widget.NewSelect(otherNames, nil)

				// Pre-select current position relative to first other output
				ref := findOutputByName(outputs, otherNames[0])
				if ref != nil {
					posSelect.SetSelected(detectPosition(out, *ref))
				}
				refSelect.SetSelectedIndex(0)

				applyPos := widget.NewButton(locale.T("screens.applyPos"), func() {
					pos := posSelect.Selected
					refName := refSelect.Selected
					if pos == "" || refName == "" {
						return
					}
					// Convert display text to IPC value
					var ipcPos string
					switch pos {
					case locale.T("screens.rightOf"):
						ipcPos = "right"
					case locale.T("screens.leftOf"):
						ipcPos = "left"
					case locale.T("screens.above"):
						ipcPos = "above"
					case locale.T("screens.below"):
						ipcPos = "below"
					case locale.T("screens.mirror"):
						ipcPos = "mirror"
					}
					d.requestLayoutChange(out.OutputName, ipcPos, refName, false)
				})

				outputControls.Add(container.NewBorder(nil, nil,
					widget.NewLabelWithStyle(locale.T("screens.position"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					applyPos, container.NewGridWithColumns(2, posSelect, refSelect)))
			}
		}

		// Adaptive sync (VRR/FreeSync) toggle
		if out.AdaptiveSyncSupported {
			vrrOutName := out.OutputName
			vrrCheck := widget.NewCheck(locale.T("screens.adaptiveSync"), func(checked bool) {
				d.requestVRRChange(vrrOutName, checked)
			})
			vrrCheck.Checked = out.AdaptiveSyncEnabled
			outputControls.Add(vrrCheck)
		}

		// Display info
		if out.PhysWidth > 0 && out.PhysHeight > 0 {
			dpi := float64(out.Width) * float64(out.Scale) / (float64(out.PhysWidth) / 25.4)
			info := fmt.Sprintf("Physical: %dx%d mm, DPI: %.0f, Logical: %dx%d at (%d,%d)",
				out.PhysWidth, out.PhysHeight, dpi, out.Width, out.Height, out.X, out.Y)
			outputControls.Add(widget.NewLabel(info))
		} else if out.Width > 0 {
			info := fmt.Sprintf("Logical: %dx%d at (%d,%d)", out.Width, out.Height, out.X, out.Y)
			outputControls.Add(widget.NewLabel(info))
		}

		outputControls.Refresh()
	}

	// Output selector dropdown (only show if multiple outputs)
	if len(outputs) > 1 {
		var outputNames []string
		for _, out := range outputs {
			label := out.OutputName
			if out.Primary {
				label += " (primary)"
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
		content.Add(container.NewBorder(nil, nil,
			widget.NewLabelWithStyle(locale.T("screens.output"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			nil, outputSelect))
	}

	// Build initial controls for first output
	buildOutputControls(outputs[0])
	content.Add(outputControls)

	return content
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
	if out.X == ref.X && out.Y == ref.Y {
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
