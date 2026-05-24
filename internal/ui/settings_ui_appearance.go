package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wm"
)

func (d *settingsUI) populateThemeIcons(box *fyne.Container, theme string) {
	box.Objects = nil
	for _, appName := range d.launcherIcons {
		appData := fynedesk.Instance().IconProvider().FindAppFromName(appName)
		if appData == nil { // if app was removed!
			continue
		}
		iconRes := appData.Icon(theme, int((d.settings.LauncherIconSize()*d.settings.LauncherZoomScale())*fynedesk.Instance().Screens().Primary().CanvasScale()))
		icon := widget.NewIcon(iconRes)
		box.Add(icon)
	}
	box.Refresh()
}

func (d *settingsUI) loadAppearanceScreen() fyne.CanvasObject {
	var bgPathClear *widget.Button
	bgPath := widget.NewEntry()
	bgPath.SetPlaceHolder(locale.T("appearance.chooseImage"))
	bgPathClear = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		bgPath.SetText("")
		bgPathClear.Disable()
	})

	if fyne.CurrentApp().Preferences().String("background") != "" {
		bgPath.SetText(fyne.CurrentApp().Preferences().String("background"))
	} else {
		bgPathClear.Disable()
	}
	bgLabel := widget.NewLabelWithStyle(locale.T("appearance.background"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	bgDialog := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
		if err != nil || file == nil {
			return
		}

		// not advisable for cross-platform but we are desktop only
		path := file.URI().String()[7:]
		// TODO add a nice preview :)
		_ = file.Close()

		bgPath.SetText(path)
		bgPathClear.Enable()
	}, d.win)
	bgDialog.SetFilter(storage.NewExtensionFileFilter([]string{".jpg", ".jpeg", ".png", ".svg"}))
	if dir, err := getPicturesDir(); err == nil {
		bgDialog.SetLocation(dir)
	} else {
		fyne.LogError("error finding pictures dir, falling back to home directory", err)
	}

	bgButtons := container.NewHBox(bgPathClear,
		widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
			bgDialog.Show()
		}))
	bgImageRow := container.NewBorder(nil, nil, nil, bgButtons, bgPath)

	// Dynamic wallpaper: folder picker row
	bgDirPath := widget.NewEntry()
	bgDirPath.SetPlaceHolder(locale.T("appearance.chooseFolder"))
	var bgDirClear *widget.Button
	bgDirClear = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		bgDirPath.SetText("")
		bgDirClear.Disable()
	})
	bgDirClear.Disable()
	bgDirDialog := dialog.NewFolderOpen(func(dir fyne.ListableURI, err error) {
		if err != nil || dir == nil {
			return
		}
		bgDirPath.SetText(dir.Path())
		bgDirClear.Enable()
	}, d.win)
	if dir, err := getPicturesDir(); err == nil {
		bgDirDialog.SetLocation(dir)
	}
	bgDirButtons := container.NewHBox(bgDirClear,
		widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
			bgDirDialog.Show()
		}))
	bgDirRow := container.NewBorder(nil, nil, nil, bgDirButtons, bgDirPath)

	// Background type selector (Image, Dynamic, Matrix, Starfield)
	bgTypeLabel := widget.NewLabelWithStyle(locale.T("appearance.type"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	bgTypeRadio := &widget.RadioGroup{Options: []string{
		locale.T("appearance.image"),
		locale.T("appearance.dynamic"),
		locale.T("appearance.matrix"),
		locale.T("appearance.starfield"),
	}, Required: true, Horizontal: true}

	currentBgType := fyne.CurrentApp().Preferences().String("background_type")
	switch currentBgType {
	case "dynamic":
		bgTypeRadio.SetSelected(locale.T("appearance.dynamic"))
		bgImageRow.Hide()
		if fyne.CurrentApp().Preferences().String("background") != "" {
			bgDirPath.SetText(fyne.CurrentApp().Preferences().String("background"))
			bgDirClear.Enable()
		}
	case "matrix":
		bgTypeRadio.SetSelected(locale.T("appearance.matrix"))
		bgImageRow.Hide()
		bgDirRow.Hide()
	case "starfield":
		bgTypeRadio.SetSelected(locale.T("appearance.starfield"))
		bgImageRow.Hide()
		bgDirRow.Hide()
	default:
		bgTypeRadio.SetSelected(locale.T("appearance.image"))
		bgDirRow.Hide()
	}

	bgTypeRadio.OnChanged = func(selected string) {
		switch selected {
		case locale.T("appearance.image"):
			bgImageRow.Show()
			bgDirRow.Hide()
		case locale.T("appearance.dynamic"):
			bgImageRow.Hide()
			bgDirRow.Show()
		default:
			bgImageRow.Hide()
			bgDirRow.Hide()
		}
	}

	bgTypeRow := container.NewBorder(nil, nil, bgTypeLabel, nil, bgTypeRadio)

	// Per-monitor selector (only shown when 2+ outputs)
	var monitorSelect *widget.Select
	monitorLabel := widget.NewLabelWithStyle(locale.T("appearance.monitor"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	monitorRow := container.NewBorder(nil, nil, monitorLabel, nil)
	monitorRow.Hide() // hidden by default; shown below if multi-output

	outputNames := readOutputNames()
	if len(outputNames) >= 2 {
		options := append([]string{locale.T("appearance.allMonitors")}, outputNames...)
		monitorSelect = widget.NewSelect(options, func(selected string) {
			if selected == locale.T("appearance.allMonitors") || selected == "" {
				// Show global wallpaper settings
				bgPath.SetText(fyne.CurrentApp().Preferences().String("background"))
				return
			}
			// Show per-monitor wallpaper if configured
			if d.settings.cfg != nil {
				if mw, ok := d.settings.cfg.Display.Monitors[selected]; ok {
					bgPath.SetText(mw.Background)
					return
				}
			}
			bgPath.SetText("") // No per-monitor override
		})
		monitorSelect.SetSelected(locale.T("appearance.allMonitors"))
		monitorRow = container.NewBorder(nil, nil, monitorLabel, nil, monitorSelect)
		monitorRow.Show()
	}

	bgSection := container.NewVBox(
		container.NewBorder(nil, nil, bgLabel, nil, bgTypeRow),
		monitorRow,
		bgImageRow,
		bgDirRow,
	)

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
	for _, themeName := range fynedesk.Instance().IconProvider().AvailableThemes() {
		if seen[themeName] {
			continue
		}
		seen[themeName] = true
		themeButton := widget.NewButton(themeName, nil)
		themeButton.OnTapped = func() {
			themeLabel.SetText(themeButton.Text)

			fynedesk.Instance().IconProvider().ClearCache()
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

	top := container.NewVBox(bgSection, colorSchemeRow, accessibilityRow, langRow, time, border, fontRow, saver)

	themeFormLabel := widget.NewLabelWithStyle(locale.T("appearance.iconTheme"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	themeCurrent := container.NewHBox(layout.NewSpacer(), themeLabel, themeIcons)
	bottom := container.NewBorder(nil, themeCurrent, themeFormLabel, nil, container.NewScroll(themeList))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			d.settings.beginBatch()

			// Save background type
			bgTypeStr := "image"
			switch bgTypeRadio.Selected {
			case locale.T("appearance.dynamic"):
				bgTypeStr = "dynamic"
			case locale.T("appearance.matrix"):
				bgTypeStr = "matrix"
			case locale.T("appearance.starfield"):
				bgTypeStr = "starfield"
			}
			d.settings.setBackgroundType(bgTypeStr)

			// Per-monitor or global wallpaper
			selectedMonitor := ""
			if monitorSelect != nil {
				selectedMonitor = monitorSelect.Selected
			}
			if selectedMonitor != "" && selectedMonitor != locale.T("appearance.allMonitors") {
				// Per-monitor wallpaper
				path := bgPath.Text
				if bgTypeStr == "dynamic" {
					path = bgDirPath.Text
				}
				d.settings.setMonitorBackground(selectedMonitor, path, bgTypeStr)
			} else {
				// Global wallpaper
				if bgTypeStr == "dynamic" {
					d.settings.setBackground(bgDirPath.Text)
				} else {
					d.settings.setBackground(bgPath.Text)
				}
			}
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
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	statePath := filepath.Join(configDir, "fynedesk", "compositor-state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var state CompositorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	var names []string
	for _, out := range state.Outputs {
		names = append(names, out.OutputName)
	}
	return names
}

func getPicturesDir() (fyne.ListableURI, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	const xdg = "xdg-user-dir"
	if _, err := exec.LookPath(xdg); err == nil {
		out, err := wm.ExecOutput(xdg, "PICTURES")
		if err == nil {
			location := strings.TrimRight(string(out), "\n")
			if location != "" && location != home {
				uri := storage.NewFileURI(location)
				return storage.ListerForURI(uri)
			}
		}
	}

	uri, err := storage.Child(storage.NewFileURI(home), "Pictures")
	if err != nil {
		return nil, err
	}

	return storage.ListerForURI(uri)
}
