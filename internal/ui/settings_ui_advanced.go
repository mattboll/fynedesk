package ui

import (
	"fmt"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wm"
)

// moduleCategory returns a display category for a module based on its name.
func moduleCategory(name string) string {
	if strings.HasPrefix(name, "Launcher:") {
		return "Launcher"
	}
	switch name {
	case "Sound", "Battery", "Brightness", "Network", "Power Profile", "Keyboard Layout":
		return "Status Bar"
	case "Virtual Desktops", "Desktop Files", "Compositor":
		return "Desktop"
	default:
		return "System"
	}
}

func (d *settingsUI) loadAdvancedScreen() fyne.CanvasObject {
	// Group modules by category
	categories := map[string][]fyne.CanvasObject{}
	categoryOrder := []string{"Status Bar", "Desktop", "Launcher", "System"}
	var allChecks []*widget.Check

	for _, mod := range fynedesk.AvailableModules() {
		name := mod.Name
		enabled := isModuleEnabled(name, d.settings)

		check := widget.NewCheck(name, func(bool) {})
		check.SetChecked(enabled)
		allChecks = append(allChecks, check)

		cat := moduleCategory(name)
		categories[cat] = append(categories[cat], check)
	}

	categoryLabels := map[string]string{
		"Status Bar": locale.T("advanced.statusBar"),
		"Desktop":    locale.T("advanced.desktop"),
		"Launcher":   locale.T("advanced.launcher"),
		"System":     locale.T("advanced.system"),
	}

	var moduleCards []fyne.CanvasObject
	for _, cat := range categoryOrder {
		checks := categories[cat]
		if len(checks) == 0 {
			continue
		}
		label := categoryLabels[cat]
		if label == "" {
			label = cat
		}
		moduleCards = append(moduleCards, widget.NewCard(label, "", container.NewVBox(checks...)))
	}

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

	windowsCard := widget.NewCard(locale.T("advanced.windows"), "",
		container.NewVBox(innerGapLabel, innerGapSlider, outerGapLabel, outerGapSlider))

	// Power profile (only if powerprofilesctl is available)
	var powerCard fyne.CanvasObject
	if profileOut, profileErr := exec.Command("powerprofilesctl", "get").Output(); profileErr == nil {
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
			go exec.Command("powerprofilesctl", "set", profile).Run()
		}
		powerCard = widget.NewCard(locale.T("advanced.power"), "", powerSelect)
	}

	leftItems := []fyne.CanvasObject{d.loadScreensGroup()}
	rightItems := append(moduleCards, inputCard, windowsCard)
	if powerCard != nil {
		rightItems = append(rightItems, powerCard)
	}
	content := container.NewScroll(container.NewHBox(
		container.NewVBox(leftItems...),
		container.NewVBox(rightItems...),
	))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			var names []string
			for _, check := range allChecks {
				if check.Checked {
					names = append(names, check.Text)
				}
			}

			d.settings.setModuleNames(names)
			d.settings.setNaturalScroll(naturalScroll.Checked)
			d.settings.setWindowGaps(int(innerGapSlider.Value), int(outerGapSlider.Value))
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
		}})

	return container.NewBorder(nil, applyButton, nil, nil, content)
}
