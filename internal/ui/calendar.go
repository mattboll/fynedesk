package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/locale"
)

// calendarPopup builds a navigable month calendar view with week numbers.
func calendarPopup(now time.Time) fyne.CanvasObject {
	today := now
	viewYear, viewMonth, _ := now.Date()

	var headerLabel *widget.Label
	var gridContainer *fyne.Container

	updateGrid := func() {
		headerLabel.SetText(fmt.Sprintf("%s %d", locale.MonthName(int(viewMonth)), viewYear))
		gridContainer.Objects = buildCalendarGrid(viewYear, viewMonth, today)
		gridContainer.Refresh()
	}

	headerLabel = widget.NewLabelWithStyle(
		fmt.Sprintf("%s %d", locale.MonthName(int(viewMonth)), viewYear),
		fyne.TextAlignCenter, fyne.TextStyle{Bold: true},
	)

	prevBtn := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		viewMonth--
		if viewMonth < 1 {
			viewMonth = 12
			viewYear--
		}
		updateGrid()
	})
	prevBtn.Importance = widget.LowImportance

	nextBtn := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		viewMonth++
		if viewMonth > 12 {
			viewMonth = 1
			viewYear++
		}
		updateGrid()
	})
	nextBtn.Importance = widget.LowImportance

	todayBtn := widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		viewYear, viewMonth, _ = today.Date()
		updateGrid()
	})
	todayBtn.Importance = widget.LowImportance

	header := container.NewBorder(nil, nil, prevBtn, container.NewHBox(todayBtn, nextBtn), headerLabel)

	gridContainer = container.New(layout.NewGridWrapLayout(fyne.NewSize(38, 30)))
	gridContainer.Objects = buildCalendarGrid(viewYear, viewMonth, today)

	sep := canvas.NewRectangle(color.NRGBA{R: 128, G: 128, B: 128, A: 64})
	sep.SetMinSize(fyne.NewSize(0, 1))

	return container.NewVBox(header, sep, gridContainer)
}

// buildCalendarGrid returns the grid cells for a given month.
func buildCalendarGrid(year int, month time.Month, today time.Time) []fyne.CanvasObject {
	loc := today.Location()
	todayYear, todayMonth, todayDay := today.Date()

	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	weekday := int(first.Weekday())
	if weekday == 0 {
		weekday = 7 // Monday-start
	}
	weekday-- // 0=Mon, 6=Sun

	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()

	// Day-of-week header row (W# | Mo Tu We Th Fr Sa Su)
	dayNames := []string{
		locale.T("cal.week"),
		locale.T("cal.mon"), locale.T("cal.tue"), locale.T("cal.wed"),
		locale.T("cal.thu"), locale.T("cal.fri"), locale.T("cal.sat"),
		locale.T("cal.sun"),
	}

	var objects []fyne.CanvasObject
	for i, name := range dayNames {
		style := fyne.TextStyle{}
		if i == 0 {
			style.Italic = true
		}
		label := widget.NewLabelWithStyle(name, fyne.TextAlignCenter, style)
		objects = append(objects, label)
	}

	// Fill calendar rows
	cellIdx := 0
	d := 1
	for d <= daysInMonth {
		dayDate := time.Date(year, month, d, 0, 0, 0, 0, loc)
		if cellIdx%8 == 0 {
			_, wk := dayDate.ISOWeek()
			wkLabel := widget.NewLabelWithStyle(fmt.Sprintf("%d", wk), fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
			wkLabel.Importance = widget.LowImportance
			objects = append(objects, wkLabel)
			cellIdx++
		}

		// Skip empty slots at start of first row
		if d == 1 && cellIdx%8-1 < weekday {
			for cellIdx%8-1 < weekday {
				objects = append(objects, layout.NewSpacer())
				cellIdx++
			}
		}

		label := widget.NewLabel(fmt.Sprintf("%d", d))
		label.Alignment = fyne.TextAlignCenter

		isToday := year == todayYear && month == todayMonth && d == todayDay
		if isToday {
			bg := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
			bg.CornerRadius = 4
			label.Importance = widget.HighImportance
			objects = append(objects, container.NewStack(bg, label))
		} else {
			objects = append(objects, label)
		}
		cellIdx++
		d++
	}

	// Pad remaining cells in last row
	for cellIdx%8 != 0 {
		objects = append(objects, layout.NewSpacer())
		cellIdx++
	}

	return objects
}
