package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/backgrounds"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wm"
)

// Background types, as stored in the "background_type" setting.
const (
	bgTypeImage     = "image"
	bgTypeDynamic   = "dynamic"
	bgTypeMatrix    = "matrix"
	bgTypeStarfield = "starfield"
)

// loadBackgroundScreen builds the wallpaper settings: the kind of background
// (an image, a folder of time-of-day images or an animation), the image and
// how it fills the screen, the colour drawn around it and, with several
// monitors, a per-monitor override.
func (d *settingsUI) loadBackgroundScreen() fyne.CanvasObject {
	prefs := fyne.CurrentApp().Preferences()
	currentType := prefs.String("background_type")
	if currentType == "" {
		currentType = bgTypeImage
	}

	// Image row
	var bgPathClear *widget.Button
	bgPath := widget.NewEntry()
	bgPath.SetPlaceHolder(locale.T("appearance.chooseImage"))
	bgPathClear = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		bgPath.SetText("")
		bgPathClear.Disable()
	})
	bgDialog := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
		if err != nil || file == nil {
			return
		}
		_ = file.Close()

		bgPath.SetText(file.URI().Path())
		bgPathClear.Enable()
	}, d.win)
	bgDialog.SetFilter(storage.NewExtensionFileFilter([]string{".jpg", ".jpeg", ".png", ".svg"}))
	if dir, err := getPicturesDir(); err == nil {
		bgDialog.SetLocation(dir)
	} else {
		fyne.LogError("error finding pictures dir, falling back to home directory", err)
	}
	bgImageRow := container.NewBorder(nil, nil, nil, container.NewHBox(bgPathClear,
		widget.NewButtonWithIcon("", theme.FolderOpenIcon(), bgDialog.Show)), bgPath)

	// Dynamic wallpaper: folder row
	var bgDirClear *widget.Button
	bgDirPath := widget.NewEntry()
	bgDirPath.SetPlaceHolder(locale.T("appearance.chooseFolder"))
	bgDirClear = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		bgDirPath.SetText("")
		bgDirClear.Disable()
	})
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
	bgDirRow := container.NewBorder(nil, nil, nil, container.NewHBox(bgDirClear,
		widget.NewButtonWithIcon("", theme.FolderOpenIcon(), bgDirDialog.Show)), bgDirPath)

	// Live preview of the chosen image in a monitor surround, over the
	// colour drawn wherever the image does not cover the screen.
	screen := canvas.NewImageFromFile("")
	screen.ScaleMode = canvas.ImageScaleFastest
	screenColor := canvas.NewRectangle(ParseHexColor(d.settings.BackgroundColor()))
	preview := container.NewCenter(monitorSurround(screen, screenColor))

	set := fyne.CurrentApp().Settings()
	fillSelect := widget.NewSelect(backgroundFillModes, nil)
	refreshPreview := func() {
		if bgPath.Text == "" {
			// The default wallpaper used by the desktop when no image is configured.
			screen.File = ""
			screen.Resource = backgrounds.Default().Load(set.Theme(), set.ThemeVariant()).(*canvas.Image).Resource
			screen.FillMode = canvas.ImageFillCover
		} else {
			screen.Resource = nil
			screen.File = bgPath.Text
			screen.FillMode = backgroundFillMode(fillSelect.Selected)
		}
		screen.Refresh()
	}
	fillSelect.OnChanged = func(string) { refreshPreview() }
	fillSelect.SetSelected(d.settings.BackgroundFill())
	bgPath.OnChanged = func(string) { refreshPreview() }

	colorSwatch := canvas.NewRectangle(screenColor.FillColor)
	colorSwatch.CornerRadius = theme.Size(theme.SizeNameInputRadius)
	colorSwatch.SetMinSize(fyne.NewSize(24, 24))
	colorButton := widget.NewButtonWithIcon(locale.T("appearance.colour"), theme.ColorChromaticIcon(), func() {
		picker := dialog.NewColorPicker(locale.T("appearance.colourTitle"), locale.T("appearance.colourHint"),
			func(c color.Color) {
				screenColor.FillColor = c
				screenColor.Refresh()
				colorSwatch.FillColor = c
				colorSwatch.Refresh()
			}, d.win)
		picker.Advanced = true
		picker.SetColor(screenColor.FillColor)
		picker.Show()
	})
	fillRow := container.NewBorder(nil, nil, widget.NewLabel(locale.T("appearance.fill")),
		container.NewHBox(container.NewCenter(colorSwatch), colorButton), fillSelect)
	imageSettings := container.NewVBox(bgImageRow, fillRow, preview)

	// Shows the global setting, or a monitor's override, in the path rows.
	showPath := func(path, bgType string) {
		bgPath.SetText("")
		bgDirPath.SetText("")
		if bgType == bgTypeDynamic {
			bgDirPath.SetText(path)
		} else {
			bgPath.SetText(path)
		}
		if bgPath.Text == "" {
			bgPathClear.Disable()
		} else {
			bgPathClear.Enable()
		}
		if bgDirPath.Text == "" {
			bgDirClear.Disable()
		} else {
			bgDirClear.Enable()
		}
	}
	showPath(prefs.String("background"), currentType)

	// Background type
	typeNames := map[string]string{
		bgTypeImage:     locale.T("appearance.image"),
		bgTypeDynamic:   locale.T("appearance.dynamic"),
		bgTypeMatrix:    locale.T("appearance.matrix"),
		bgTypeStarfield: locale.T("appearance.starfield"),
	}
	types := []string{bgTypeImage, bgTypeDynamic, bgTypeMatrix, bgTypeStarfield}
	var options []string
	for _, t := range types {
		options = append(options, typeNames[t])
	}
	selectedType := func(label string) string {
		for _, t := range types {
			if typeNames[t] == label {
				return t
			}
		}
		return bgTypeImage
	}
	showType := func(bgType string) {
		imageSettings.Hide()
		bgDirRow.Hide()
		switch bgType {
		case bgTypeImage:
			imageSettings.Show()
		case bgTypeDynamic:
			bgDirRow.Show()
		}
	}
	typeRadio := &widget.RadioGroup{Options: options, Required: true, Horizontal: true}
	typeRadio.SetSelected(typeNames[currentType])
	typeRadio.OnChanged = func(label string) { showType(selectedType(label)) }
	showType(currentType)
	typeRow := container.NewBorder(nil, nil, widget.NewLabel(locale.T("appearance.type")), nil, typeRadio)

	// Per-monitor selector, only with two outputs or more
	var monitorSelect *widget.Select
	var monitorRow fyne.CanvasObject = layout.NewSpacer()
	if outputs := readOutputNames(); len(outputs) >= 2 {
		all := locale.T("appearance.allMonitors")
		monitorSelect = widget.NewSelect(append([]string{all}, outputs...), func(selected string) {
			if selected == all || selected == "" {
				showPath(prefs.String("background"), prefs.String("background_type"))
				return
			}
			if d.settings.cfg != nil {
				if mw, ok := d.settings.cfg.Display.Monitors[selected]; ok {
					showPath(mw.Background, mw.BackgroundType)
					return
				}
			}
			showPath("", "") // no override for this monitor
		})
		monitorSelect.SetSelected(all)
		monitorRow = container.NewBorder(nil, nil, widget.NewLabel(locale.T("appearance.monitor")), nil, monitorSelect)
	}

	apply := func() {
		bgType := selectedType(typeRadio.Selected)
		path := bgPath.Text
		if bgType == bgTypeDynamic {
			path = bgDirPath.Text
		}

		d.settings.beginBatch()
		if monitorSelect != nil && monitorSelect.Selected != "" && monitorSelect.SelectedIndex() > 0 {
			d.settings.setMonitorBackground(monitorSelect.Selected, path, bgType)
		} else {
			d.settings.setBackgroundType(bgType)
			d.settings.setBackground(path)
		}
		d.settings.setBackgroundFill(fillSelect.Selected)
		d.settings.setBackgroundColor(HexColor(screenColor.FillColor))
		d.settings.endBatch()
		wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
	}
	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: apply})

	return container.NewBorder(container.NewVBox(typeRow, monitorRow, bgDirRow), applyButton, nil, nil,
		imageSettings)
}
