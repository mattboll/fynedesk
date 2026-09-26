package ui

import (
	"image/color"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"github.com/FyshOS/screens/pkg/screenmanager"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/cmd/fyne_settings/settings"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/modules/ai"
	"fyshos.com/tyde/modules/updates"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
	"github.com/godbus/dbus/v5"
)

type settingsUI struct {
	settings *deskSettings
	win      fyne.Window
	panel    *widgetPanel // owning panel, so the Account tab can refresh its avatar

	launcherIcons []string

	netConn *dbus.Conn    // system bus backing the Network tab, closed with the window
	fprint  *fprintClient // fprintd connection backing the Account tab, closed with the window
}

// monitorSurround wraps the given screen image in a simple monitor-shaped frame:
// a dark bezel around the screen area sitting on a small stand. The screen area
// matches the aspect ratio of the primary display so the preview is faithful.
func monitorSurround(screen *canvas.Image, screenColor *canvas.Rectangle) fyne.CanvasObject {
	frameColor := color.NRGBA{R: 0x2b, G: 0x2b, B: 0x2b, A: 0xff}

	const previewHeight = 144
	previewWidth := float32(previewHeight) * 16.0 / 9.0 // default 16:9
	if screens := tyde.Instance().Screens(); screens != nil {
		if primary := screens.Primary(); primary != nil && primary.Height > 0 {
			previewWidth = float32(previewHeight) * float32(primary.Width) / float32(primary.Height)
		}
	}
	screen.SetMinSize(fyne.NewSize(previewWidth, previewHeight))

	bezel := canvas.NewRectangle(frameColor)
	bezel.CornerRadius = theme.Size(theme.SizeNameInputRadius)
	display := container.NewStack(bezel, container.NewPadded(container.NewStack(screenColor, screen)))

	neck := canvas.NewRectangle(frameColor)
	neck.SetMinSize(fyne.NewSize(28, 14))
	base := canvas.NewRectangle(frameColor)
	base.CornerRadius = theme.Size(theme.SizeNameInputRadius)
	base.SetMinSize(fyne.NewSize(96, 8))
	stand := container.NewVBox(container.NewCenter(neck), container.NewCenter(base))

	return container.NewVBox(display, stand)
}

// loadNetworkScreen builds the Wi-Fi management tab from our networks app package.
func (d *settingsUI) loadNetworkScreen() fyne.CanvasObject {
	// NetworkManager systems use its own Wi-Fi list; the other one is for iwd.
	if _, err := exec.LookPath("nmcli"); err == nil {
		_, panel := newWifiPanel(d.win)
		return panel
	}

	nm, conn, err := newWifiNetworks(d.win)
	if err != nil {
		msg := widget.NewLabel("Wi-Fi management is unavailable.\n\n" + err.Error())
		msg.Wrapping = fyne.TextWrapWord
		return container.NewCenter(msg)
	}
	d.netConn = conn
	return nm
}

func (d *settingsUI) loadModulesScreen() fyne.CanvasObject {
	var modules, launchers []fyne.CanvasObject

	applyModules := func() {
		var names []string
		for _, item := range modules {
			check := item.(*widget.Check)
			if check.Checked {
				names = append(names, check.Text)
			}
		}
		for _, item := range launchers {
			check := item.(*widget.Check)
			if check.Checked {
				names = append(names, "Launcher:"+check.Text)
			}
		}

		d.settings.setModuleNames(names)
	}

	for _, mod := range tyde.AvailableModules() {
		name := mod.Name
		enabled := isModuleEnabled(name, d.settings)

		check := widget.NewCheck(name, func(bool) {})
		check.SetChecked(enabled)
		check.OnChanged = func(_ bool) {
			applyModules()
		}

		if strings.Index(name, "Launcher:") == 0 {
			check.SetText(name[9:])
			launchers = append(launchers, check)
		} else {
			modules = append(modules, check)
		}
	}
	return container.NewGridWithColumns(2,
		container.NewBorder(sectionHeading("Modules", ""), nil, nil, nil,
			container.NewVScroll(container.NewVBox(modules...))),
		container.NewBorder(sectionHeading("Launchers", ""), nil, nil, nil,
			container.NewVScroll(container.NewVBox(launchers...))))
}

// loadAIScreen builds the AI assistant setup: an enable toggle (wired into the
// module enable/disable machinery) above the module's own provider/token panel.
func (d *settingsUI) loadAIScreen() fyne.CanvasObject {
	enable := widget.NewCheck("Enable AI Assistant", nil)
	enable.SetChecked(isModuleEnabled(ai.ModuleName, d.settings))
	enable.OnChanged = func(on bool) {
		names := d.settings.ModuleNames()
		var out []string
		for _, n := range names {
			if n != ai.ModuleName {
				out = append(out, n)
			}
		}
		if on {
			out = append(out, ai.ModuleName)
		}
		d.settings.setModuleNames(out)
	}

	head := container.NewVBox(enable, widget.NewSeparator())
	return container.NewBorder(head, nil, nil, nil, ai.SettingsContent())
}

// showSettings opens the settings window. A non-empty panel title opens that panel directly.
func (w *widgetPanel) showSettings(panel string) {
	if w.settings != nil {
		if panel != "" && w.settingsNav != nil {
			w.settingsNav.showPanel(panel)
		}
		w.settings.CenterOnScreen()
		w.settings.Show()
		w.settings.(deskDriver.Window).RequestAlwaysOnTop()
		wlipc.RequestRaiseByTitle(w.settings.Title())
		return
	}

	deskSettings := w.desk.Settings().(*deskSettings)
	ui := &settingsUI{
		settings:      deskSettings,
		launcherIcons: deskSettings.LauncherIcons(),
		panel:         w,
	}

	win := fyne.CurrentApp().NewWindow("Tyde Settings")
	ui.win = win

	scale := ui.makeScaleGroup(win)
	var screens *screenmanager.Screens // X11 only: the compositor owns the outputs of a Wayland session

	fyneSettings := settings.NewSettings()
	displayPanel := func() fyne.CanvasObject {
		// The compositor owns the outputs of a Wayland session.
		if wlipc.IsWaylandSession() {
			return container.NewVScroll(ui.loadScreensGroup())
		}
		screens = screenmanager.New(win)
		screens.OnConfigurationChanged = w.desk.Screens().RefreshScreens
		screenui := container.NewBorder(sectionHeading("Screens", ""), nil, nil, nil, screens)
		return container.NewBorder(scale, nil, nil, nil, screenui)
	}

	groups := []settingsGroup{
		{title: locale.T("settings.appearance"), panels: []*settingsPanel{
			{title: locale.T("settings.appearance"), icon: fyneSettings.AppearanceIcon(), build: ui.loadAppearanceScreen},
			{title: locale.T("appearance.background"), icon: wmtheme.WallpaperIcon, build: ui.loadBackgroundScreen},
			{title: locale.T("settings.colorScheme"), icon: theme.ColorPaletteIcon(), build: ui.loadThemeScreen},
		}},
		{title: "Desktop", panels: []*settingsPanel{
			{title: locale.T("settings.dock"), icon: dockIcon, build: ui.loadBarScreen},
			{title: locale.T("settings.desktops"), icon: wmtheme.DisplayIcon, build: ui.loadDesktopsScreen},
			{title: locale.T("settings.keyboard"), icon: wmtheme.KeyboardIcon, build: ui.loadKeyboardScreen},
			{title: locale.T("settings.windowRules"), icon: windowRulesIcon, build: ui.loadWindowRulesScreen},
			{title: "Modules", icon: theme.GridIcon(), build: ui.loadModulesScreen},
			{title: "AI", icon: ai.Icon, build: ui.loadAIScreen},
		}},
		{title: "System", panels: []*settingsPanel{
			{title: "Account", icon: wmtheme.UserIcon, build: ui.loadAccountScreen},
			{title: "Display", icon: wmtheme.ScreensIcon, build: displayPanel},
			{title: "Network", icon: wmtheme.WifiIcon, build: ui.loadNetworkScreen},
			{title: "Time/Date", icon: wmtheme.ClockIcon, build: ui.loadTimeScreen},
			{title: locale.T("settings.power"), icon: wmtheme.BatteryIcon, build: ui.loadPowerScreen},
			{title: locale.T("settings.calendar"), icon: theme.CalendarIcon(), build: ui.loadCalendarScreen},
			{title: locale.T("settings.advanced"), icon: tuneIcon, build: ui.loadAdvancedScreen},
		}},
	}

	for _, mod := range tyde.AvailableModules() {
		if mod.Name != updates.ModuleName {
			continue
		}

		if isModuleEnabled(mod.Name, w.desk.Settings()) {
			groups[2].panels = append(groups[2].panels,
				&settingsPanel{title: "Updates", icon: wmtheme.UpdateIcon, build: updates.SettingsContent},
			)
		}
		break
	}

	settingsIcon := theme.SettingsIcon()
	win.SetIcon(settingsIcon)
	nav := newSettingsNav(groups, settingsIcon)
	w.settingsNav = nav
	if panel != "" {
		nav.showPanel(panel)
	}
	// Closing really closes (it used to hide the window, which kept the
	// animation running and the D-Bus connections open): it is built again
	// on the next open.
	win.SetOnClosed(func() {
		if w.settings == win {
			w.settings, w.settingsNav = nil, nil
		}
		nav.waveAnim.Stop()
		if screens != nil {
			screens.Close()
		}
		if ui.netConn != nil {
			_ = ui.netConn.Close()
			ui.netConn = nil
		}
		if ui.fprint != nil {
			ui.fprint.close()
			ui.fprint = nil
		}
	})

	win.SetPadded(false)
	win.SetContent(nav.root)
	win.Resize(fyne.NewSize(760, 640))
	nav.waveAnim.Start()

	w.settings = win
	win.Show()
	raiseSettingsWindow(win.Title())
}

// raiseSettingsWindow raises the settings window of a Wayland session above
// the other windows once the compositor has mapped it. Polling for the window
// avoids the race where a fixed sleep is too short and the raise-by-title
// finds nothing.
func raiseSettingsWindow(title string) {
	if !wlipc.IsWaylandSession() {
		return
	}
	go func() {
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

var (
	picturesDirOnce sync.Once
	picturesDirURI  fyne.ListableURI
	picturesDirErr  error
)

// getPicturesDir resolves the user's Pictures directory. It shells out to
// xdg-user-dir, so the result is cached (it does not change during a session)
// to avoid re-running that on the render thread each time Settings or the
// screenshot dialog is opened.
func getPicturesDir() (fyne.ListableURI, error) {
	picturesDirOnce.Do(func() {
		picturesDirURI, picturesDirErr = resolvePicturesDir()
	})
	return picturesDirURI, picturesDirErr
}

func resolvePicturesDir() (fyne.ListableURI, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	const xdg = "xdg-user-dir"
	if _, err := exec.LookPath(xdg); err == nil {
		out, err := wm.ExecOutput(xdg, "PICTURES")
		location := strings.TrimSpace(string(out)) // empty output must not panic
		if err == nil && location != "" && location != home {
			uri := storage.NewFileURI(location)
			return storage.ListerForURI(uri)
		}
	}

	uri, err := storage.Child(storage.NewFileURI(home), "Pictures")
	if err != nil {
		return nil, err
	}

	return storage.ListerForURI(uri)
}

func (d *settingsUI) makeScaleGroup(w fyne.Window) fyne.CanvasObject {
	s := settings.NewSettings()
	fyneAppearance := s.LoadAppearanceScreen(w)

	preview := fyneAppearance.(*fyne.Container).Objects[0]
	preview.Hide()
	box := fyneAppearance.(*fyne.Container).Objects[1]
	box.(*fyne.Container).Objects[1].Hide() // appearance card

	applyRow := fyneAppearance.(*fyne.Container).Objects[2].(*fyne.Container)
	submit := applyRow.Objects[1].(*widget.Button)
	applyRow.Hide()

	scale := box.(*fyne.Container).Objects[0].(*widget.Card)
	buttons := scale.Content.(*fyne.Container).Objects[1].(*fyne.Container).Objects

	for _, b := range buttons {
		tap := b.(*widget.Button).OnTapped
		b.(*widget.Button).OnTapped = func() {
			tap()
			submit.OnTapped()
		}
	}
	return fyneAppearance
}
