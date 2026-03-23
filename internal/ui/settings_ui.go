package ui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/cmd/fyne_settings/settings"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

type settingsUI struct {
	settings *deskSettings
	win      fyne.Window

	launcherIcons []string
}

func (w *widgetPanel) showSettings() {
	if w.settings != nil {
		w.settings.CenterOnScreen()
		w.settings.Show()
		wlipc.RequestRaiseByTitle(locale.T("settings.title"))
		return
	}

	deskSettings := w.desk.Settings().(*deskSettings)
	ui := &settingsUI{
		settings:      deskSettings,
		launcherIcons: deskSettings.LauncherIcons(),
	}

	win := fyne.CurrentApp().NewWindow(locale.T("settings.title"))
	ui.win = win
	fyneSettings := settings.NewSettings()

	allTabs := []*container.TabItem{
		{Text: locale.T("settings.interface"), Icon: wmtheme.FyneLogo,
			Content: fyneSettings.LoadAppearanceScreen(win)},
		{Text: locale.T("settings.appearance"), Icon: fyneSettings.AppearanceIcon(),
			Content: ui.loadAppearanceScreen()},
		{Text: locale.T("settings.colorScheme"), Icon: theme.ColorPaletteIcon(), Content: ui.loadThemeScreen()},
		{Text: locale.T("settings.dock"), Icon: wmtheme.IconifyIcon, Content: ui.loadBarScreen()},
		{Text: locale.T("settings.desktops"), Icon: wmtheme.DisplayIcon, Content: ui.loadDesktopsScreen()},
		{Text: locale.T("settings.keyboard"), Icon: wmtheme.KeyboardIcon, Content: ui.loadKeyboardScreen()},
		{Text: locale.T("settings.windowRules"), Icon: theme.ListIcon(), Content: ui.loadWindowRulesScreen()},
		{Text: locale.T("settings.power"), Icon: wmtheme.BatteryIcon, Content: ui.loadPowerScreen()},
		{Text: locale.T("settings.advanced"), Icon: theme.SettingsIcon(),
			Content: ui.loadAdvancedScreen()},
	}

	tabs := container.NewAppTabs(allTabs...)
	tabs.SetTabLocation(container.TabLocationLeading)

	// Search bar: filter tabs by keyword
	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder(locale.T("settings.search"))
	searchEntry.OnChanged = func(query string) {
		settingsSearchFilter(tabs, allTabs, query)
	}

	resetButton := widget.NewButtonWithIcon(locale.T("settings.resetDefaults"), theme.ViewRefreshIcon(), func() {
		dialog.ShowConfirm(locale.T("settings.resetDefaults"),
			locale.T("settings.resetConfirm"),
			func(ok bool) {
				if !ok {
					return
				}
				deskSettings.beginBatch()

				// Reset launcher icons to defaults from icon provider
				defaultApps := fynedesk.Instance().IconProvider().DefaultApps()
				var defaultIcons []string
				for _, appData := range defaultApps {
					defaultIcons = append(defaultIcons, appData.Name())
				}
				deskSettings.setLauncherIcons(defaultIcons)

				// Reset icon theme
				deskSettings.setIconTheme("")

				// Reset zoom
				deskSettings.setLauncherDisableZoom(false)

				// Reset panel layout
				deskSettings.setNarrowLeftLauncher(true)

				deskSettings.endBatch()
				wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("settings.defaultsRestored")))

				// Close and reopen settings to refresh all UI
				win.Close()
				w.settings = nil
			}, win)
	})
	resetButton.Importance = widget.DangerImportance

	bottomBar := container.NewHBox(resetButton, layout.NewSpacer())
	content := container.NewBorder(searchEntry, bottomBar, nil, nil, tabs)
	win.SetContent(content)
	win.Resize(fyne.NewSize(480, 320))

	win.SetCloseIntercept(func() {
		win.Close()
		w.settings = nil
	})
	w.settings = win
	win.Show()

	// Focus search on Ctrl+F (if Fyne supports it, otherwise search is always visible)

	// Raise the settings window above other windows after it maps.
	// Poll until the window appears in the compositor's window list,
	// then raise it. This avoids the race where a fixed sleep is too
	// short and the raise-by-title finds nothing.
	go func() {
		title := locale.T("settings.title")
		for i := 0; i < 20; i++ { // up to 2 seconds
			time.Sleep(100 * time.Millisecond)
			if state, err := wlipc.GetWindowsState(); err == nil {
				for _, w := range state.Windows {
					if w.Title == title {
						wlipc.RequestRaiseByTitle(title)
						return
					}
				}
			}
		}
		// Fallback: try anyway
		wlipc.RequestRaiseByTitle(title)
	}()
}

// settingsTabKeywords maps tab names to additional search keywords so that
// users can type related terms (e.g. "wallpaper") and find the right tab.
var settingsTabKeywords = map[string][]string{
	"Interface":    {"fyne", "theme", "scale", "font", "language"},
	"Appearance":   {"wallpaper", "background", "icon", "launcher", "image"},
	"Color Scheme": {"color", "primary", "accent", "dark", "light", "palette"},
	"Dock":         {"dock", "bar", "taskbar", "launcher", "icon size", "panel"},
	"Desktops":     {"desktop", "virtual", "workspace", "screen", "monitor"},
	"Keyboard":     {"keyboard", "shortcut", "keybinding", "hotkey", "modifier", "key"},
	"Window Rules": {"window", "rule", "float", "maximize", "workspace", "title"},
	"Power":        {"power", "suspend", "hibernate", "sleep", "idle", "timeout", "lock", "blank", "battery"},
	"Advanced":     {"night light", "session", "restore", "reduce motion", "animation", "compositor"},
}

// settingsSearchFilter shows only the tabs whose name or keywords match the query.
func settingsSearchFilter(tabs *container.AppTabs, allTabs []*container.TabItem, query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		tabs.Items = allTabs
		tabs.SelectIndex(0)
		tabs.Refresh()
		return
	}

	var filtered []*container.TabItem
	for _, tab := range allTabs {
		if strings.Contains(strings.ToLower(tab.Text), query) {
			filtered = append(filtered, tab)
			continue
		}
		if keywords, ok := settingsTabKeywords[tab.Text]; ok {
			for _, kw := range keywords {
				if strings.Contains(kw, query) {
					filtered = append(filtered, tab)
					break
				}
			}
		}
	}

	if len(filtered) == 0 {
		filtered = allTabs // show all when nothing matches
	}

	tabs.Items = filtered
	tabs.SelectIndex(0)
	tabs.Refresh()
}
