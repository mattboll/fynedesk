package ui

import (
	"strconv"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wm"
)

func (d *settingsUI) populateOrderList(list *fyne.Container, add fyne.CanvasObject) {
	var icons []fyne.CanvasObject
	iconSize := float32(fynedesk.Instance().Settings().LauncherIconSize())
	for i, appName := range d.launcherIcons {
		index := i // capture
		appData := fynedesk.Instance().IconProvider().FindAppFromName(appName)
		if appData == nil {
			continue // uninstalled?
		}
		left := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
			d.launcherIcons[index-1], d.launcherIcons[index] = d.launcherIcons[index], d.launcherIcons[index-1]
			d.populateOrderList(list, add)
		})
		if index <= 0 {
			left.Disable()
		}

		remove := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			if index == 0 {
				d.launcherIcons = d.launcherIcons[1:]
			} else if index == len(d.launcherIcons)-1 {
				d.launcherIcons = d.launcherIcons[:len(d.launcherIcons)-1]
			} else {
				d.launcherIcons = append(d.launcherIcons[:index], d.launcherIcons[index+1])
			}
			d.populateOrderList(list, add)
		})

		right := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
			d.launcherIcons[index+1], d.launcherIcons[index] = d.launcherIcons[index], d.launcherIcons[index+1]
			d.populateOrderList(list, add)
		})
		if index >= len(d.launcherIcons)-1 {
			right.Disable()
		}
		iconRes := appData.Icon(d.settings.IconTheme(), int((d.settings.LauncherIconSize()*d.settings.LauncherZoomScale())*fynedesk.Instance().Screens().Primary().CanvasScale()))
		if iconRes == nil {
			iconRes = wmtheme.BrokenImageIcon
		}
		icon := canvas.NewImageFromResource(iconRes)
		icon.FillMode = canvas.ImageFillContain
		icon.SetMinSize(fyne.NewSize(iconSize, iconSize))
		label := widget.NewLabelWithStyle(appName, fyne.TextAlignCenter, fyne.TextStyle{})
		hbox := container.NewVBox(icon, label, container.NewHBox(left, remove, right))
		icons = append(icons, hbox)
	}

	icons = append(icons, add)
	list.Objects = icons
	list.Refresh()
}

func (d *settingsUI) loadBarScreen() fyne.CanvasObject {
	// --- Layout options ---
	barPosSelect := &widget.Select{Options: []string{locale.T("dock.sideBar"), locale.T("dock.bottomDock")}}
	switch d.settings.BarPosition() {
	case "left":
		barPosSelect.SetSelected(locale.T("dock.sideBar"))
	default:
		barPosSelect.SetSelected(locale.T("dock.bottomDock"))
	}
	narrowWidget := widget.NewCheck(locale.T("dock.narrowWidget"), nil)
	narrowWidget.Checked = d.settings.NarrowWidgetPanel()

	layoutCard := widget.NewCard(locale.T("dock.layout"), "",
		container.NewGridWithColumns(2,
			container.NewHBox(widget.NewLabel(locale.T("dock.position")), barPosSelect),
			narrowWidget))

	// --- Icon order ---
	iconWidth := float32(fynedesk.Instance().Settings().LauncherIconSize())
	addButton := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {})
	addIcon := canvas.NewImageFromResource(theme.ContentAddIcon())
	addIcon.FillMode = canvas.ImageFillContain
	addIcon.SetMinSize(fyne.NewSize(iconWidth, iconWidth))
	addItem := container.NewVBox(addIcon, widget.NewLabel(locale.T("dock.addIcon")), addButton)
	orderList := container.NewHBox()
	d.populateOrderList(orderList, addItem)

	addButton.OnTapped = func() {
		newAppPicker(locale.T("dock.chooseApp"), func(data appie.AppData, _ int) {
			d.launcherIcons = append(d.launcherIcons, data.Name())
			d.populateOrderList(orderList, addItem)
		}).Show()
	}

	bar := container.NewHScroll(orderList)

	iconSize := widget.NewEntry()
	iconSize.Wrapping = fyne.TextWrapOff
	iconSize.SetText(strconv.FormatFloat(float64(d.settings.LauncherIconSize()), 'f', 0, 32))

	zoomScale := widget.NewEntry()
	zoomScale.Wrapping = fyne.TextWrapOff
	zoomScale.SetText(strconv.FormatFloat(float64(d.settings.LauncherZoomScale()), 'f', 2, 64))

	sizeCell := container.NewHBox(widget.NewLabel(locale.T("dock.iconSize")), iconSize)
	zoomCell := container.NewHBox(widget.NewLabel(locale.T("dock.hoverZoom")), zoomScale)

	disableTaskbar := widget.NewCheck(locale.T("dock.showRunning"), nil)
	disableTaskbar.SetChecked(!d.settings.LauncherDisableTaskbar())

	disableZoom := widget.NewCheck(locale.T("dock.hoverEffect"), nil)
	disableZoom.SetChecked(!d.settings.LauncherDisableZoom())

	details := widget.NewCard(locale.T("dock.config"), "",
		container.NewGridWithColumns(2, sizeCell, zoomCell, disableTaskbar, disableZoom))

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			d.settings.beginBatch()

			barPos := "bottom"
			if barPosSelect.Selected == locale.T("dock.sideBar") {
				barPos = "left"
			}
			d.settings.setBarPosition(barPos)
			d.settings.setNarrowWidgetPanel(narrowWidget.Checked)

			size, err := strconv.Atoi(iconSize.Text)
			if err != nil {
				fyne.LogError("error setting launcher icon size", err)
				size = 32
			}
			d.settings.setLauncherIconSize(float32(size))

			scale, err := strconv.ParseFloat(zoomScale.Text, 32)
			if err != nil {
				fyne.LogError("Error setting launcher zoom scale", err)
				scale = 2.0
			}
			d.settings.setLauncherZoomScale(float32(scale))
			d.settings.setLauncherDisableTaskbar(!disableTaskbar.Checked)
			d.settings.setLauncherDisableZoom(!disableZoom.Checked)

			d.settings.setLauncherIcons(d.launcherIcons)

			d.settings.endBatch()
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
		}})

	return container.NewBorder(nil, applyButton, nil, nil,
		container.NewVBox(layoutCard, widget.NewCard(locale.T("dock.icons"), "", container.NewVBox(bar, details))))
}
