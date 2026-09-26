package ui

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
)

// IsFirstRun returns true if no config.toml exists yet,
// indicating Tyde has not been configured before.
func IsFirstRun() bool {
	_, err := os.Stat(configPath())
	return err != nil
}

// ShowSetupWizard displays the first-run setup wizard as a blocking dialog.
// It returns after the user completes or skips the wizard.
// Call this after NewPanelDesktop but before ShowAndRun.
func ShowSetupWizard(desk tyde.Desktop) {
	d, ok := desk.(*desktop)
	if !ok {
		return
	}
	settings := d.settings.(*deskSettings)

	win := fyne.CurrentApp().NewWindow(locale.T("wizard.welcome"))
	win.SetFixedSize(true)
	win.Resize(fyne.NewSize(480, 360))
	win.CenterOnScreen()

	// State variables for wizard choices
	chosenLayout := settings.keyboardLayouts
	chosenPosition := settings.barPosition
	chosenScheme := settings.colorScheme
	chosenClock := settings.clockFormatting

	// --- Step 1: Welcome + Keyboard ---
	step1 := makeStep1(chosenLayout, func(layouts []string) {
		chosenLayout = layouts
	})

	// --- Step 2: Bar position ---
	step2 := makeStep2(chosenPosition, func(pos string) {
		chosenPosition = pos
	})

	// --- Step 3: Theme + Clock ---
	step3 := makeStep3(chosenScheme, chosenClock, func(scheme, clock string) {
		chosenScheme = scheme
		chosenClock = clock
	})

	steps := []fyne.CanvasObject{step1, step2, step3}
	currentStep := 0

	content := container.NewStack(steps[0])

	// Progress dots
	dots := makeDots(len(steps), 0)

	var backBtn, nextBtn *widget.Button
	backBtn = widget.NewButton(locale.T("wizard.back"), nil)
	nextBtn = widget.NewButton(locale.T("wizard.next"), nil)
	nextBtn.Importance = widget.HighImportance

	updateButtons := func() {
		backBtn.SetText(locale.T("wizard.back"))
		if currentStep == 0 {
			backBtn.SetText(locale.T("wizard.skip"))
		}
		if currentStep == len(steps)-1 {
			nextBtn.SetText(locale.T("wizard.finish"))
		} else {
			nextBtn.SetText(locale.T("wizard.next"))
		}
		dots = makeDots(len(steps), currentStep)
	}

	backBtn.OnTapped = func() {
		if currentStep == 0 {
			// Skip wizard
			win.Close()
			return
		}
		currentStep--
		content.Objects = []fyne.CanvasObject{steps[currentStep]}
		content.Refresh()
		updateButtons()
	}

	nextBtn.OnTapped = func() {
		if currentStep == len(steps)-1 {
			// Apply settings and close
			settings.beginBatch()
			settings.setKeyboardLayouts(chosenLayout)
			settings.setBarPosition(chosenPosition)
			settings.setColorScheme(chosenScheme)
			settings.setClockFormatting(chosenClock)
			settings.endBatch()
			win.Close()
			return
		}
		currentStep++
		content.Objects = []fyne.CanvasObject{steps[currentStep]}
		content.Refresh()
		updateButtons()
	}

	updateButtons()

	buttons := container.NewHBox(backBtn, layout.NewSpacer(), dots, layout.NewSpacer(), nextBtn)
	win.SetContent(container.NewBorder(nil, buttons, nil, nil, content))
	win.Show()
}

// makeStep1 builds the keyboard layout selection step.
func makeStep1(current []string, onChange func([]string)) fyne.CanvasObject {
	title := widget.NewRichTextFromMarkdown(locale.T("wizard.welcomeMd"))
	subtitle := widget.NewLabel(locale.T("wizard.setup"))
	subtitle.Wrapping = fyne.TextWrapWord

	layoutEntry := widget.NewEntry()
	if len(current) > 0 {
		layoutEntry.SetText(current[0])
	}
	layoutEntry.SetPlaceHolder(locale.T("wizard.kbExample"))
	layoutEntry.OnChanged = func(text string) {
		if text != "" {
			onChange([]string{text})
		}
	}

	hint := widget.NewLabel(locale.T("wizard.kbFormat"))
	hint.TextStyle = fyne.TextStyle{Italic: true}

	return container.NewVBox(
		title,
		subtitle,
		layout.NewSpacer(),
		widget.NewLabel(locale.T("wizard.kbLayout")),
		layoutEntry,
		hint,
		layout.NewSpacer(),
	)
}

// makeStep2 builds the bar position selection step.
func makeStep2(current string, onChange func(string)) fyne.CanvasObject {
	title := widget.NewRichTextFromMarkdown(locale.T("wizard.panelTitle"))
	subtitle := widget.NewLabel(locale.T("wizard.panelDesc"))
	subtitle.Wrapping = fyne.TextWrapWord

	left, bottom := locale.T("wizard.panelLeft"), locale.T("wizard.panelBottom")
	selected := left
	if current == "bottom" {
		selected = bottom
	}

	radio := widget.NewRadioGroup([]string{left, bottom}, func(val string) {
		if val == bottom {
			onChange("bottom")
		} else {
			onChange("left")
		}
	})
	radio.SetSelected(selected)

	return container.NewVBox(
		title,
		subtitle,
		layout.NewSpacer(),
		radio,
		layout.NewSpacer(),
	)
}

// makeStep3 builds the theme and clock format selection step.
func makeStep3(currentScheme, currentClock string, onChange func(scheme, clock string)) fyne.CanvasObject {
	title := widget.NewRichTextFromMarkdown(locale.T("wizard.appearance"))
	subtitle := widget.NewLabel(locale.T("wizard.appearDesc"))
	subtitle.Wrapping = fyne.TextWrapWord

	scheme := currentScheme
	clock := currentClock

	auto, dark, light := locale.T("appearance.auto"), locale.T("appearance.dark"), locale.T("appearance.light")
	schemeRadio := widget.NewRadioGroup([]string{auto, dark, light}, func(val string) {
		switch val {
		case dark:
			scheme = "dark"
		case light:
			scheme = "light"
		default:
			scheme = "auto"
		}
		onChange(scheme, clock)
	})
	switch currentScheme {
	case "dark":
		schemeRadio.SetSelected(dark)
	case "light":
		schemeRadio.SetSelected(light)
	default:
		schemeRadio.SetSelected(auto)
	}
	schemeRadio.Horizontal = true

	clockRadio := widget.NewRadioGroup([]string{"12h", "24h"}, func(val string) {
		clock = val
		onChange(scheme, clock)
	})
	if currentClock == "24h" {
		clockRadio.SetSelected("24h")
	} else {
		clockRadio.SetSelected("12h")
	}
	clockRadio.Horizontal = true

	return container.NewVBox(
		title,
		subtitle,
		layout.NewSpacer(),
		widget.NewLabel(locale.T("wizard.colorScheme")),
		schemeRadio,
		widget.NewLabel(locale.T("wizard.clockFormat")),
		clockRadio,
		layout.NewSpacer(),
	)
}

// makeDots creates a row of dots indicating the current step.
func makeDots(total, current int) *fyne.Container {
	items := make([]fyne.CanvasObject, total)
	for i := 0; i < total; i++ {
		dot := widget.NewIcon(theme.RadioButtonIcon())
		if i == current {
			dot = widget.NewIcon(theme.RadioButtonCheckedIcon())
		}
		items[i] = dot
	}
	return container.NewHBox(items...)
}
