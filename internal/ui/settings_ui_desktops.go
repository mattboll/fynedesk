package ui

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/wm"
)

func (d *settingsUI) loadDesktopsScreen() fyne.CanvasObject {
	currentCount := d.settings.DesktopCount()
	currentNames := d.settings.DesktopNames()

	// Desktop count selector (1-8)
	countLabel := widget.NewLabelWithStyle("Number of Desktops", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	countOptions := []string{"1", "2", "3", "4", "5", "6", "7", "8"}
	countSelect := widget.NewSelect(countOptions, nil)
	countSelect.SetSelected(strconv.Itoa(currentCount))

	// Name entries — one per desktop
	nameEntries := make([]*widget.Entry, 8)
	nameRows := make([]fyne.CanvasObject, 8)
	for i := range 8 {
		entry := widget.NewEntry()
		entry.SetPlaceHolder("Desktop " + strconv.Itoa(i+1))
		if i < len(currentNames) && currentNames[i] != "" {
			entry.SetText(currentNames[i])
		}
		nameEntries[i] = entry
		nameRows[i] = container.NewBorder(nil, nil,
			widget.NewLabel(strconv.Itoa(i+1)+"."), nil, entry)
	}

	namesContainer := container.NewVBox(nameRows...)

	// Show/hide entries based on count
	updateVisible := func(count int) {
		for i, row := range nameRows {
			if i < count {
				row.Show()
			} else {
				row.Hide()
			}
		}
	}
	updateVisible(currentCount)

	countSelect.OnChanged = func(val string) {
		n, _ := strconv.Atoi(val)
		if n > 0 {
			updateVisible(n)
		}
	}

	countRow := container.NewBorder(nil, nil, countLabel, nil, countSelect)

	namesLabel := widget.NewLabelWithStyle("Desktop Names", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: "Apply", Importance: widget.HighImportance, OnTapped: func() {
			d.settings.beginBatch()

			count, _ := strconv.Atoi(countSelect.Selected)
			if count < 1 {
				count = 4
			}
			d.settings.setDesktopCount(count)

			names := make([]string, count)
			for i := range count {
				names[i] = nameEntries[i].Text
			}
			d.settings.setDesktopNames(names)

			d.settings.endBatch()
			wm.SendNotification(wm.NewNotification("Settings", "Changes applied"))
		}})

	return container.NewBorder(
		container.NewVBox(countRow, namesLabel),
		applyButton, nil, nil,
		container.NewScroll(namesContainer),
	)
}
