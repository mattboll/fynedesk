package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/locale"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

func (d *settingsUI) loadKeyboardScreen() fyne.CanvasObject {
	return d.loadKeyboardScreenConfigurable()
}

// xkbToDisplayName returns a human-readable label for an XKB key name.
func xkbToDisplayName(xkbKey string) string {
	display := map[string]string{
		"grave": "`", "space": "Space", "Return": "Enter", "BackSpace": "Backspace",
		"Print": "PrintScreen", "minus": "Minus", "equal": "Equal",
		"comma": "Comma", "period": "Period", "slash": "Slash",
		"backslash": "Backslash", "semicolon": "Semicolon",
		"XF86AudioRaiseVolume":  "Vol+",
		"XF86AudioLowerVolume":  "Vol-",
		"XF86AudioMute":         "Mute",
		"XF86MonBrightnessUp":   "Brightness+",
		"XF86MonBrightnessDown": "Brightness-",
		"XF86Calculator":        "Calculator",
	}
	if d, ok := display[xkbKey]; ok {
		return d
	}
	// Single lowercase letter -> uppercase for display
	if len(xkbKey) == 1 && xkbKey[0] >= 'a' && xkbKey[0] <= 'z' {
		return strings.ToUpper(xkbKey)
	}
	return xkbKey
}

// modsToDisplayString converts a modifier list to a display string, resolving "WM".
func modsToDisplayString(mods []string, userMod fyne.KeyModifier) string {
	var parts []string
	for _, m := range mods {
		if m == "WM" {
			if userMod == fyne.KeyModifierAlt {
				parts = append(parts, "Alt")
			} else {
				parts = append(parts, "Super")
			}
		} else {
			parts = append(parts, m)
		}
	}
	return strings.Join(parts, "+")
}

func (d *settingsUI) loadKeyboardScreenConfigurable() fyne.CanvasObject {
	bindings := d.settings.loadKeybindings()
	userMod := d.settings.modifier

	// Build the table content
	actions := wlipc.ActionOrder()
	rows := container.NewVBox()

	for _, action := range actions {
		actionBindings := bindings[action]
		displayName := wlipc.ActionDisplayName(action)

		var bindingStrs []string
		for _, b := range actionBindings {
			modStr := modsToDisplayString(b.Mods, userMod)
			keyStr := xkbToDisplayName(b.Key)
			if modStr != "" {
				bindingStrs = append(bindingStrs, modStr+"+"+keyStr)
			} else {
				bindingStrs = append(bindingStrs, keyStr)
			}
		}
		bindingText := strings.Join(bindingStrs, ", ")

		actionLabel := widget.NewLabel(displayName)
		actionLabel.TextStyle = fyne.TextStyle{}
		bindingLabel := widget.NewLabel(bindingText)

		act := action
		editBtn := widget.NewButton(locale.T("keyboard.edit"), func() {
			d.showKeybindingEditor(act, bindings, userMod, func(updated wlipc.ActionBindings) {
				bindings = updated
				d.settings.saveKeybindings(bindings)
				// Refresh the row
				var newStrs []string
				for _, b := range bindings[act] {
					modStr := modsToDisplayString(b.Mods, userMod)
					keyStr := xkbToDisplayName(b.Key)
					if modStr != "" {
						newStrs = append(newStrs, modStr+"+"+keyStr)
					} else {
						newStrs = append(newStrs, keyStr)
					}
				}
				bindingLabel.SetText(strings.Join(newStrs, ", "))
			})
		})

		resetBtn := widget.NewButton(locale.T("keyboard.reset"), func() {
			defaults := wlipc.DefaultBindings()
			if db, ok := defaults[act]; ok {
				bindings[act] = db
			}
			d.settings.saveKeybindings(bindings)
			var newStrs []string
			for _, b := range bindings[act] {
				modStr := modsToDisplayString(b.Mods, userMod)
				keyStr := xkbToDisplayName(b.Key)
				if modStr != "" {
					newStrs = append(newStrs, modStr+"+"+keyStr)
				} else {
					newStrs = append(newStrs, keyStr)
				}
			}
			bindingLabel.SetText(strings.Join(newStrs, ", "))
		})

		row := container.NewHBox(
			container.NewGridWrap(fyne.NewSize(250, 30), actionLabel),
			container.NewGridWrap(fyne.NewSize(200, 30), bindingLabel),
			editBtn,
			resetBtn,
		)
		rows.Add(row)
	}

	grid := container.NewScroll(rows)

	// Modifier selector at top
	modType := widget.NewRadioGroup([]string{locale.T("keyboard.super"), locale.T("keyboard.alt")}, func(mod string) {
		if mod == locale.T("keyboard.alt") {
			userMod = fyne.KeyModifierAlt
		} else {
			userMod = fyne.KeyModifierSuper
		}
	})
	modType.Horizontal = true
	if d.settings.modifier == fyne.KeyModifierAlt {
		modType.Selected = locale.T("keyboard.alt")
	} else {
		modType.Selected = locale.T("keyboard.super")
	}

	// Keyboard layouts section
	layoutSection := d.loadKeyboardLayoutSection()

	applyButton := container.NewHBox(layout.NewSpacer(),
		&widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
			d.settings.setKeyboardModifier(userMod)
			wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
		}})

	topSection := container.NewVBox(
		container.NewHBox(widget.NewLabel(locale.T("keyboard.preferredMod")+" "), modType),
		widget.NewSeparator(),
		layoutSection,
		widget.NewSeparator(),
	)

	return container.NewBorder(topSection, applyButton, nil, nil, grid)
}

// loadKeyboardLayoutSection creates the keyboard layout picker UI
func (d *settingsUI) loadKeyboardLayoutSection() fyne.CanvasObject {
	// Current layouts list
	currentLayouts := make([]string, len(d.settings.keyboardLayouts))
	copy(currentLayouts, d.settings.keyboardLayouts)

	layoutListBox := container.NewVBox()

	var refreshList func()
	refreshList = func() {
		layoutListBox.Objects = nil
		for i, entry := range currentLayouts {
			idx := i
			parsed := wlipc.ParseKeyboardLayoutPref(entry)
			label := entry
			if len(parsed) > 0 {
				if parsed[0].DisplayName != "" {
					label = parsed[0].DisplayName
				} else {
					// Try to make a readable label
					label = strings.ToUpper(parsed[0].Layout)
					if parsed[0].Variant != "" {
						label += " (" + parsed[0].Variant + ")"
					}
				}
			}

			removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				currentLayouts = append(currentLayouts[:idx], currentLayouts[idx+1:]...)
				refreshList()
			})

			upBtn := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
				if idx > 0 {
					currentLayouts[idx-1], currentLayouts[idx] = currentLayouts[idx], currentLayouts[idx-1]
					refreshList()
				}
			})
			if idx == 0 {
				upBtn.Disable()
			}

			downBtn := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
				if idx < len(currentLayouts)-1 {
					currentLayouts[idx+1], currentLayouts[idx] = currentLayouts[idx], currentLayouts[idx+1]
					refreshList()
				}
			})
			if idx >= len(currentLayouts)-1 {
				downBtn.Disable()
			}

			row := container.NewHBox(
				widget.NewLabel(label),
				layout.NewSpacer(),
				upBtn, downBtn, removeBtn,
			)
			layoutListBox.Add(row)
		}
		layoutListBox.Refresh()
	}
	refreshList()

	addBtn := widget.NewButtonWithIcon(locale.T("keyboard.addLayout"), theme.ContentAddIcon(), func() {
		d.showAddLayoutDialog(func(layoutStr string) {
			currentLayouts = append(currentLayouts, layoutStr)
			refreshList()
		})
	})

	applyLayoutsBtn := &widget.Button{Text: locale.T("keyboard.applyLayouts"), Importance: widget.HighImportance, OnTapped: func() {
		d.settings.setKeyboardLayouts(currentLayouts)
	}}

	return container.NewVBox(
		widget.NewLabelWithStyle(locale.T("keyboard.layouts"), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layoutListBox,
		container.NewHBox(addBtn, layout.NewSpacer(), applyLayoutsBtn),
	)
}

// showAddLayoutDialog opens a dialog to add a new keyboard layout
func (d *settingsUI) showAddLayoutDialog(onAdd func(string)) {
	// Parse available layouts
	xkbLayouts, err := wlipc.ParseXKBLayouts()
	if err != nil {
		// Fallback: manual entry
		entry := widget.NewEntry()
		entry.SetPlaceHolder("layout:variant (e.g. us:dvorak)")
		dlg := dialog.NewCustomConfirm(locale.T("keyboard.addTitle"), locale.T("keyboard.add"), locale.T("keyboard.cancel"),
			container.NewVBox(widget.NewLabel("Enter layout:variant"), entry),
			func(ok bool) {
				if ok && entry.Text != "" {
					onAdd(entry.Text)
				}
			}, d.win)
		dlg.Resize(fyne.NewSize(400, 200))
		dlg.Show()
		return
	}

	// Build layout options
	var layoutNames []string
	layoutMap := make(map[string]wlipc.XKBLayoutInfo)
	for _, l := range xkbLayouts {
		displayStr := l.Description + " (" + l.Name + ")"
		layoutNames = append(layoutNames, displayStr)
		layoutMap[displayStr] = l
	}

	layoutSelect := widget.NewSelect(layoutNames, nil)
	layoutSelect.PlaceHolder = "Select layout..."

	variantSelect := widget.NewSelect([]string{locale.T("keyboard.default")}, nil)
	variantSelect.PlaceHolder = "Select variant..."
	variantSelect.SetSelectedIndex(0)

	layoutSelect.OnChanged = func(selected string) {
		info := layoutMap[selected]
		variantOptions := []string{locale.T("keyboard.default")}
		for _, v := range info.Variants {
			variantOptions = append(variantOptions, v.Description+" ("+v.Name+")")
		}
		variantSelect.Options = variantOptions
		variantSelect.SetSelectedIndex(0)
	}

	content := container.NewVBox(
		widget.NewLabel(locale.T("keyboard.layout")),
		layoutSelect,
		widget.NewLabel(locale.T("keyboard.variant")),
		variantSelect,
	)

	dlg := dialog.NewCustomConfirm(locale.T("keyboard.addTitle"), locale.T("keyboard.add"), locale.T("keyboard.cancel"), content, func(ok bool) {
		if !ok || layoutSelect.Selected == "" {
			return
		}

		info := layoutMap[layoutSelect.Selected]
		variant := ""
		if variantSelect.SelectedIndex() > 0 && variantSelect.SelectedIndex()-1 < len(info.Variants) {
			variant = info.Variants[variantSelect.SelectedIndex()-1].Name
		}

		onAdd(info.Name + ":" + variant)
	}, d.win)
	dlg.Resize(fyne.NewSize(450, 300))
	dlg.Show()
}

// showKeybindingEditor shows a dialog to edit the key binding for an action.
func (d *settingsUI) showKeybindingEditor(action string, bindings wlipc.ActionBindings, userMod fyne.KeyModifier, onSave func(wlipc.ActionBindings)) {
	displayName := wlipc.ActionDisplayName(action)
	current := bindings[action]

	// Build key options for the keybinding editor
	keyOptions := []string{
		"Escape", "Tab", "space", "Return", "BackSpace",
		"Up", "Down", "Left", "Right",
		"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12",
		"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
		"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m",
		"n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z",
		"grave", "minus", "equal", "comma", "period", "slash", "backslash", "semicolon",
		"Print", "Delete", "Insert", "Home", "End", "Prior", "Next",
		"XF86AudioRaiseVolume", "XF86AudioLowerVolume", "XF86AudioMute",
		"XF86MonBrightnessUp", "XF86MonBrightnessDown", "XF86Calculator",
	}
	keyDisplayOptions := make([]string, len(keyOptions))
	for i, k := range keyOptions {
		keyDisplayOptions[i] = xkbToDisplayName(k)
	}

	// Modifier checkboxes
	checkShift := widget.NewCheck(locale.T("keyboard.shift"), nil)
	checkCtrl := widget.NewCheck(locale.T("keyboard.ctrl"), nil)
	checkAlt := widget.NewCheck(locale.T("keyboard.alt"), nil)
	checkWM := widget.NewCheck(locale.T("keyboard.wmMod"), nil)

	keySelect := widget.NewSelect(keyDisplayOptions, nil)

	// Pre-fill with current first binding
	if len(current) > 0 {
		b := current[0]
		for _, m := range b.Mods {
			switch m {
			case "Shift":
				checkShift.SetChecked(true)
			case "Ctrl":
				checkCtrl.SetChecked(true)
			case "Alt":
				checkAlt.SetChecked(true)
			case "WM":
				checkWM.SetChecked(true)
			}
		}
		for i, k := range keyOptions {
			if k == b.Key {
				keySelect.SetSelectedIndex(i)
				break
			}
		}
	}

	content := container.NewVBox(
		widget.NewLabel(locale.T("keyboard.action")+" "+displayName),
		widget.NewSeparator(),
		widget.NewLabel(locale.T("keyboard.modifiers")),
		container.NewHBox(checkShift, checkCtrl, checkAlt, checkWM),
		widget.NewLabel(locale.T("keyboard.key")),
		keySelect,
	)

	dlg := dialog.NewCustomConfirm(locale.T("keyboard.editBind"), locale.T("keyboard.save"), locale.T("keyboard.cancel"), content, func(ok bool) {
		if !ok || keySelect.SelectedIndex() < 0 {
			return
		}

		var mods []string
		if checkShift.Checked {
			mods = append(mods, "Shift")
		}
		if checkCtrl.Checked {
			mods = append(mods, "Ctrl")
		}
		if checkAlt.Checked {
			mods = append(mods, "Alt")
		}
		if checkWM.Checked {
			mods = append(mods, "WM")
		}

		b := wlipc.KeyBinding{Key: keyOptions[keySelect.SelectedIndex()], Mods: mods}
		bindings[action] = []wlipc.KeyBinding{b}
		onSave(bindings)
	}, d.win)
	dlg.Resize(fyne.NewSize(400, 350))
	dlg.Show()
}
