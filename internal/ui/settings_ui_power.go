package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wm"
)

func (d *settingsUI) loadPowerScreen() fyne.CanvasObject {
	// Lock timeout
	lockVal := d.settings.PowerLockTimeout()
	lockLabel := widget.NewLabel(formatTimeout(locale.T("power.lockScreen"), lockVal))
	lockSlider := widget.NewSlider(0, 30)
	lockSlider.Step = 1
	lockSlider.Value = float64(lockVal)
	lockSlider.OnChanged = func(v float64) {
		lockLabel.SetText(formatTimeout(locale.T("power.lockScreen"), int(v)))
	}

	// Blank timeout
	blankVal := d.settings.PowerBlankTimeout()
	blankLabel := widget.NewLabel(formatTimeout(locale.T("power.blankDisplay"), blankVal))
	blankSlider := widget.NewSlider(0, 30)
	blankSlider.Step = 1
	blankSlider.Value = float64(blankVal)
	blankSlider.OnChanged = func(v float64) {
		blankLabel.SetText(formatTimeout(locale.T("power.blankDisplay"), int(v)))
	}

	// Suspend timeout
	suspendVal := d.settings.PowerSuspendTimeout()
	suspendLabel := widget.NewLabel(formatTimeout(locale.T("power.autoSuspend"), suspendVal))
	suspendSlider := widget.NewSlider(0, 60)
	suspendSlider.Step = 1
	suspendSlider.Value = float64(suspendVal)
	suspendSlider.OnChanged = func(v float64) {
		suspendLabel.SetText(formatTimeout(locale.T("power.autoSuspend"), int(v)))
	}

	// Suspend action
	actionOptions := []string{
		locale.T("power.suspend"),
		locale.T("power.hibernate"),
		locale.T("power.hybridSleep"),
		locale.T("power.nothing"),
	}
	actionSelect := widget.NewSelect(actionOptions, nil)
	switch d.settings.PowerSuspendAction() {
	case "hibernate":
		actionSelect.SetSelected(locale.T("power.hibernate"))
	case "hybrid-sleep":
		actionSelect.SetSelected(locale.T("power.hybridSleep"))
	case "nothing":
		actionSelect.SetSelected(locale.T("power.nothing"))
	default:
		actionSelect.SetSelected(locale.T("power.suspend"))
	}

	idleCard := widget.NewCard(locale.T("power.idleTimeouts"), locale.T("power.disableHint"), container.NewVBox(
		lockLabel, lockSlider,
		blankLabel, blankSlider,
		suspendLabel, suspendSlider,
	))

	actionCard := widget.NewCard(locale.T("power.suspendAction"), locale.T("power.suspendActionDesc"), container.NewVBox(
		actionSelect,
	))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			var action string
			switch actionSelect.Selected {
			case locale.T("power.hibernate"):
				action = "hibernate"
			case locale.T("power.hybridSleep"):
				action = "hybrid-sleep"
			case locale.T("power.nothing"):
				action = "nothing"
			default:
				action = "suspend"
			}
			d.settings.setPowerSettings(
				int(lockSlider.Value),
				int(blankSlider.Value),
				int(suspendSlider.Value),
				action,
			)
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("settings.powerApplied")))
		}})

	content := container.NewScroll(container.NewVBox(idleCard, actionCard))
	return container.NewBorder(nil, applyButton, nil, nil, content)
}

func formatTimeout(prefix string, minutes int) string {
	if minutes == 0 {
		return prefix + ": " + locale.T("power.never")
	}
	return fmt.Sprintf("%s: %d min", prefix, minutes)
}
