package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wm"
)

// loadAdvancedScreen gathers the settings that fit no other panel: pointer
// input, the gaps between windows and the power profile.
func (d *settingsUI) loadAdvancedScreen() fyne.CanvasObject {
	// Input settings
	naturalScroll := widget.NewCheck(locale.T("advanced.naturalScroll"), nil)
	naturalScroll.Checked = d.settings.NaturalScroll()

	inputCard := widget.NewCard(locale.T("advanced.input"), "", container.NewVBox(naturalScroll))

	// Window gaps
	innerGapLabel := widget.NewLabel(fmt.Sprintf("%s %d px", locale.T("advanced.innerGap"), d.settings.InnerGap()))
	innerGapSlider := widget.NewSlider(0, 24)
	innerGapSlider.Step = 1
	innerGapSlider.Value = float64(d.settings.InnerGap())
	innerGapSlider.OnChanged = func(v float64) {
		innerGapLabel.SetText(fmt.Sprintf("%s %d px", locale.T("advanced.innerGap"), int(v)))
	}

	outerGapLabel := widget.NewLabel(fmt.Sprintf("%s %d px", locale.T("advanced.outerGap"), d.settings.OuterGap()))
	outerGapSlider := widget.NewSlider(0, 24)
	outerGapSlider.Step = 1
	outerGapSlider.Value = float64(d.settings.OuterGap())
	outerGapSlider.OnChanged = func(v float64) {
		outerGapLabel.SetText(fmt.Sprintf("%s %d px", locale.T("advanced.outerGap"), int(v)))
	}

	wobbly := widget.NewCheck(locale.T("advanced.wobbly"), nil)
	wobbly.Checked = d.settings.cfg.Windows.WobblyWindows()
	blur := widget.NewCheck(locale.T("advanced.blur"), nil)
	blur.Checked = d.settings.cfg.Windows.BlurBehind()
	shadows := widget.NewCheck(locale.T("advanced.shadows"), nil)
	shadows.Checked = d.settings.cfg.Windows.WindowShadows()

	windowsCard := widget.NewCard(locale.T("advanced.windows"), "",
		container.NewVBox(innerGapLabel, innerGapSlider, outerGapLabel, outerGapSlider, wobbly, blur, shadows))

	// Power profile (only if powerprofilesctl is available)
	var powerCard fyne.CanvasObject
	if profileOut, profileErr := wm.ExecOutput("powerprofilesctl", "get"); profileErr == nil {
		powerSelect := &widget.Select{Options: []string{
			locale.T("advanced.performance"),
			locale.T("advanced.balanced"),
			locale.T("advanced.powerSaver"),
		}}
		cur := strings.TrimSpace(string(profileOut))
		switch cur {
		case "performance":
			powerSelect.SetSelected(locale.T("advanced.performance"))
		case "power-saver":
			powerSelect.SetSelected(locale.T("advanced.powerSaver"))
		default:
			powerSelect.SetSelected(locale.T("advanced.balanced"))
		}
		powerSelect.OnChanged = func(selected string) {
			var profile string
			switch selected {
			case locale.T("advanced.performance"):
				profile = "performance"
			case locale.T("advanced.powerSaver"):
				profile = "power-saver"
			default:
				profile = "balanced"
			}
			go wm.ExecRun("powerprofilesctl", "set", profile) //nolint:errcheck
		}
		powerCard = widget.NewCard(locale.T("advanced.power"), "", powerSelect)
	}

	items := []fyne.CanvasObject{inputCard, windowsCard}
	if powerCard != nil {
		items = append(items, powerCard)
	}
	content := container.NewVScroll(container.NewVBox(items...))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			d.settings.setNaturalScroll(naturalScroll.Checked)
			d.settings.setWindowGaps(int(innerGapSlider.Value), int(outerGapSlider.Value))
			d.settings.setWobblyWindows(wobbly.Checked)
			d.settings.setBlurBehind(blur.Checked)
			d.settings.setWindowShadows(shadows.Checked)
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
		}})

	return container.NewBorder(nil, applyButton, nil, nil, content)
}
