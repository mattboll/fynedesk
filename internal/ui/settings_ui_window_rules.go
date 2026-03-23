package ui

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"fyshos.com/fynedesk/wlipc"
)

func (ui *settingsUI) loadWindowRulesScreen() fyne.CanvasObject {
	rules := ui.settings.WindowRules()
	if rules == nil {
		rules = []wlipc.WindowRule{}
	}

	list := container.NewVBox()
	var rebuildList func()

	makeRuleRow := func(idx int) fyne.CanvasObject {
		r := &rules[idx]

		appIDEntry := widget.NewEntry()
		appIDEntry.SetPlaceHolder("app_id or WM_CLASS")
		if r.AppID != "" {
			appIDEntry.SetText(r.AppID)
		} else if r.Pattern != "" {
			appIDEntry.SetText(r.Pattern)
		}
		appIDEntry.OnChanged = func(s string) {
			// Detect glob pattern
			if containsGlob(s) {
				r.Pattern = s
				r.AppID = ""
			} else {
				r.AppID = s
				r.Pattern = ""
			}
		}

		floatCheck := widget.NewCheck("Float", func(b bool) {
			r.Float = &b
		})
		if r.Float != nil {
			floatCheck.SetChecked(*r.Float)
		}

		maxCheck := widget.NewCheck("Maximize", func(b bool) {
			r.Maximize = &b
		})
		if r.Maximize != nil {
			maxCheck.SetChecked(*r.Maximize)
		}

		pinCheck := widget.NewCheck("Pinned", func(b bool) {
			r.Pinned = &b
		})
		if r.Pinned != nil {
			pinCheck.SetChecked(*r.Pinned)
		}

		wsEntry := widget.NewEntry()
		wsEntry.SetPlaceHolder("Workspace (1-8)")
		if r.Workspace > 0 {
			wsEntry.SetText(strconv.Itoa(r.Workspace))
		}
		wsEntry.OnChanged = func(s string) {
			if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 8 {
				r.Workspace = n
			} else if s == "" {
				r.Workspace = 0
			}
		}

		wEntry := widget.NewEntry()
		wEntry.SetPlaceHolder("Width")
		if r.Width > 0 {
			wEntry.SetText(strconv.Itoa(r.Width))
		}
		wEntry.OnChanged = func(s string) {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				r.Width = n
			} else if s == "" {
				r.Width = 0
			}
		}

		hEntry := widget.NewEntry()
		hEntry.SetPlaceHolder("Height")
		if r.Height > 0 {
			hEntry.SetText(strconv.Itoa(r.Height))
		}
		hEntry.OnChanged = func(s string) {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				r.Height = n
			} else if s == "" {
				r.Height = 0
			}
		}

		removeBtn := widget.NewButton("Remove", func() {
			rules = append(rules[:idx], rules[idx+1:]...)
			rebuildList()
		})

		sizeRow := container.NewGridWithColumns(2, wEntry, hEntry)
		checksRow := container.NewHBox(floatCheck, maxCheck, pinCheck)

		return container.NewVBox(
			container.NewBorder(nil, nil, widget.NewLabel(strconv.Itoa(idx+1)+"."), removeBtn, appIDEntry),
			container.NewGridWithColumns(2, wsEntry, sizeRow),
			checksRow,
			widget.NewSeparator(),
		)
	}

	rebuildList = func() {
		list.RemoveAll()
		for i := range rules {
			list.Add(makeRuleRow(i))
		}
		list.Refresh()
	}
	rebuildList()

	addBtn := widget.NewButton("Add Rule", func() {
		rules = append(rules, wlipc.WindowRule{})
		rebuildList()
	})

	applyBtn := widget.NewButton("Apply", func() {
		// Remove empty rules
		var filtered []wlipc.WindowRule
		for _, r := range rules {
			if r.AppID != "" || r.Pattern != "" {
				filtered = append(filtered, r)
			}
		}
		ui.settings.setWindowRules(filtered)
	})
	applyBtn.Importance = widget.HighImportance

	header := widget.NewRichTextFromMarkdown("Per-app rules matched by `app_id` (exact) or glob `pattern` (e.g. `firefox*`).")

	scrollable := container.NewVScroll(list)
	scrollable.SetMinSize(fyne.NewSize(400, 200))

	return container.NewBorder(
		container.NewVBox(header, addBtn),
		container.New(layout.NewHBoxLayout(), layout.NewSpacer(), applyBtn),
		nil, nil,
		scrollable,
	)
}

func containsGlob(s string) bool {
	for _, c := range s {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}
