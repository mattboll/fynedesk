package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

func (d *settingsUI) populateThemeIcons(box *fyne.Container, theme string) {
	box.Objects = nil
	for _, appName := range d.launcherIcons {
		appData := tyde.Instance().IconProvider().FindAppFromName(appName)
		if appData == nil { // if app was removed!
			continue
		}
		iconRes := appData.Icon(theme, int((d.settings.LauncherIconSize()*d.settings.LauncherZoomScale())*tyde.Instance().Screens().Primary().CanvasScale()))
		icon := widget.NewIcon(iconRes)
		box.Add(icon)
	}
	box.Refresh()
}

func (d *settingsUI) loadAppearanceScreen() fyne.CanvasObject {
	colorSchemeLabel := widget.NewLabelWithStyle(locale.T("appearance.colorScheme"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	colorSchemeRadio := &widget.RadioGroup{Options: []string{
		locale.T("appearance.auto"),
		locale.T("appearance.dark"),
		locale.T("appearance.light"),
	}, Required: true, Horizontal: true}
	switch d.settings.ColorScheme() {
	case "dark":
		colorSchemeRadio.SetSelected(locale.T("appearance.dark"))
	case "light":
		colorSchemeRadio.SetSelected(locale.T("appearance.light"))
	default:
		colorSchemeRadio.SetSelected(locale.T("appearance.auto"))
	}
	colorSchemeRow := container.NewBorder(nil, nil, colorSchemeLabel, nil, colorSchemeRadio)

	reduceMotionLabel := widget.NewLabelWithStyle(locale.T("appearance.accessibility"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	reduceMotionCheck := widget.NewCheck(locale.T("appearance.reduceMotion"), nil)
	reduceMotionCheck.Checked = d.settings.cfg.Display.ReduceMotion
	highContrastCheck := widget.NewCheck(locale.T("appearance.highContrast"), nil)
	highContrastCheck.Checked = d.settings.cfg.Display.HighContrast
	accessibilityRow := container.NewBorder(nil, nil, reduceMotionLabel, nil,
		container.NewHBox(reduceMotionCheck, highContrastCheck))

	clockLabel := widget.NewLabelWithStyle(locale.T("appearance.clockFormat"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	clockFormat := &widget.RadioGroup{Options: []string{"12h", "24h"}, Required: true, Horizontal: true}
	clockFormat.SetSelected(d.settings.ClockFormatting())
	clockSeconds := widget.NewCheck(locale.T("appearance.seconds"), nil)
	clockSeconds.Checked = d.settings.ClockShowSeconds()

	borderButtonLabel := widget.NewLabelWithStyle(locale.T("appearance.buttonSide"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	borderButton := &widget.Select{Options: []string{locale.T("appearance.left"), locale.T("appearance.right")}}
	borderButton.SetSelected(d.settings.BorderButtonPosition())

	saverLabel := widget.NewLabelWithStyle(locale.T("appearance.screensaver"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	saverType := &widget.RadioGroup{Options: []string{"FyshOS", "XScreensaver"}, Required: true, Horizontal: true}
	saverType.SetSelected(d.settings.ScreenSaverType())
	saverText := widget.NewEntry()
	saverText.SetText(d.settings.ScreenSaverLabel())
	saverClock := widget.NewCheck(locale.T("appearance.clock"), nil)
	saverClock.Checked = d.settings.ScreenSaverClock()

	themeLabel := widget.NewLabel(d.settings.IconTheme())
	themeIcons := container.NewHBox()
	d.populateThemeIcons(themeIcons, d.settings.IconTheme())
	themeList := container.NewVBox()
	seen := map[string]bool{}
	for _, themeName := range tyde.Instance().IconProvider().AvailableThemes() {
		if seen[themeName] {
			continue
		}
		seen[themeName] = true
		themeButton := widget.NewButton(themeName, nil)
		themeButton.OnTapped = func() {
			themeLabel.SetText(themeButton.Text)

			tyde.Instance().IconProvider().ClearCache()
			d.populateThemeIcons(themeIcons, themeButton.Text)
		}
		themeList.Add(themeButton)
	}

	time := container.NewBorder(nil, nil, clockLabel, container.NewHBox(clockFormat, clockSeconds))
	border := container.NewBorder(nil, nil, borderButtonLabel, borderButton)
	saver := container.NewBorder(nil, nil, container.NewVBox(saverLabel, widget.NewLabel("")),
		container.NewVBox(saverType, container.NewBorder(nil, nil, saverClock, nil, saverText)))
	// Font settings
	fontFamilyEntry := widget.NewEntry()
	fontFamilyEntry.SetPlaceHolder("sans-serif")
	if d.settings.cfg != nil && d.settings.cfg.Theme.FontFamily != "" {
		fontFamilyEntry.SetText(d.settings.cfg.Theme.FontFamily)
	}
	fontSizeSlider := widget.NewSlider(8, 24)
	fontSizeSlider.Step = 1
	if d.settings.cfg != nil && d.settings.cfg.Theme.FontSize > 0 {
		fontSizeSlider.Value = float64(d.settings.cfg.Theme.FontSize)
	} else {
		fontSizeSlider.Value = 13
	}
	fontSizeLabel := widget.NewLabel(fmt.Sprintf("%.0f pt", fontSizeSlider.Value))
	fontSizeSlider.OnChanged = func(v float64) {
		fontSizeLabel.SetText(fmt.Sprintf("%.0f pt", v))
	}
	fontRow := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle(locale.T("appearance.font"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		nil,
		container.NewGridWithColumns(2,
			fontFamilyEntry,
			container.NewBorder(nil, nil, nil, fontSizeLabel, fontSizeSlider),
		))

	// Language selector
	langLabel := widget.NewLabelWithStyle(locale.T("appearance.language"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	langCodes := locale.Languages()
	langLabels := make([]string, len(langCodes))
	for i, code := range langCodes {
		langLabels[i] = locale.LanguageLabel(code)
	}
	langSelect := widget.NewSelect(langLabels, nil)
	currentLang := d.settings.Language()
	langSelect.SetSelected(locale.LanguageLabel(currentLang))
	langRow := container.NewBorder(nil, nil, langLabel, nil, langSelect)

	// The computer type applies straight away: it switches the battery,
	// brightness and virtual keyboard modules and the touch friendly frames.
	computer := newComputerTypeChoice(d.settings.ComputerType(), d.settings.setComputerType)
	top := container.NewVBox(computer, colorSchemeRow, accessibilityRow, langRow, time, border, fontRow, saver)

	themeFormLabel := widget.NewLabelWithStyle(locale.T("appearance.iconTheme"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	themeCurrent := container.NewHBox(layout.NewSpacer(), themeLabel, themeIcons)
	bottom := container.NewBorder(nil, themeCurrent, themeFormLabel, nil, container.NewScroll(themeList))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			d.settings.beginBatch()

			d.settings.setIconTheme(themeLabel.Text)
			d.settings.setClockFormatting(clockFormat.Selected)
			d.settings.setClockShowSeconds(clockSeconds.Checked)
			d.settings.setBorderButtonPosition(borderButton.Selected)
			d.settings.setScreenSaver(saverType.Selected)
			d.settings.setScreenSaverClock(saverClock.Checked)
			d.settings.setScreenSaverLabel(saverText.Text)

			csStr := "auto"
			switch colorSchemeRadio.Selected {
			case locale.T("appearance.dark"):
				csStr = "dark"
			case locale.T("appearance.light"):
				csStr = "light"
			}
			d.settings.setColorScheme(csStr)
			d.settings.setReduceMotion(reduceMotionCheck.Checked)
			d.settings.setHighContrast(highContrastCheck.Checked)
			d.settings.setFont(fontFamilyEntry.Text, int(fontSizeSlider.Value))

			// Language
			selectedLang := "en"
			for _, code := range locale.Languages() {
				if locale.LanguageLabel(code) == langSelect.Selected {
					selectedLang = code
					break
				}
			}
			langChanged := selectedLang != currentLang
			d.settings.setLanguage(selectedLang)
			locale.SetLanguage(selectedLang)

			d.settings.endBatch()
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))

			// Close and reopen settings if language changed so all labels refresh
			if langChanged {
				d.win.Close()
			}
		}})

	return container.NewBorder(top, applyButton, nil, nil, bottom)
}

// readOutputNames reads compositor state to get available output names.
// Returns nil if compositor state is unavailable or has only 1 output.
func readOutputNames() []string {
	state, err := wlipc.ReadCompositorState()
	if err != nil {
		return nil
	}
	var names []string
	for _, out := range state.Outputs {
		if out.MirrorOf != "" || out.Disabled {
			continue // shows another output's wallpaper, or nothing
		}
		names = append(names, out.OutputName)
	}
	return names
}
