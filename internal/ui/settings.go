package ui

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/wlipc"
	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
)

type deskSettings struct {
	background             string
	iconTheme              string
	launcherIcons          []string
	launcherIconSize       float32
	launcherDisableTaskbar bool
	launcherDisableZoom    bool
	launcherZoomScale      float32
	borderButtonPosition   string
	clockFormatting        string
	clockShowSeconds       bool

	modifier    fyne.KeyModifier
	moduleNames []string

	narrowPanel, narrowLeftLauncher bool
	barPosition                     string // "left" or "bottom"
	naturalScroll                   bool
	desktopCount                    int
	desktopNames                    []string
	keyboardLayouts                 []string
	screenSaverClock                bool
	screenSaver, screenSaverLabel   string
	colorScheme                     string // "auto", "dark", "light"
	nightLightEnabled               bool
	nightLightTemperature           int

	listenerLock    sync.Mutex
	changeListeners []func(fynedesk.DeskSettings)

	batching bool    // when true, apply() is deferred until endBatch()
	cfg      *Config // TOML config (source of truth)
}

func (d *deskSettings) Background() string {
	return d.background
}

func (d *deskSettings) IconTheme() string {
	return d.iconTheme
}

func (d *deskSettings) LauncherIcons() []string {
	return d.launcherIcons
}

func (d *deskSettings) LauncherIconSize() float32 {
	return d.launcherIconSize
}

func (d *deskSettings) LauncherDisableTaskbar() bool {
	return d.launcherDisableTaskbar
}

func (d *deskSettings) LauncherDisableZoom() bool {
	return d.launcherDisableZoom
}

func (d *deskSettings) LauncherZoomScale() float32 {
	return d.launcherZoomScale
}

func (d *deskSettings) KeyboardModifier() fyne.KeyModifier {
	return d.modifier
}

func (d *deskSettings) ModuleNames() []string {
	return d.moduleNames
}

func (d *deskSettings) KeyboardLayouts() []string {
	return d.keyboardLayouts
}

func (d *deskSettings) NaturalScroll() bool {
	return d.naturalScroll
}

func (d *deskSettings) InnerGap() int {
	if d.cfg != nil {
		return d.cfg.Windows.InnerGap
	}
	return 6
}

func (d *deskSettings) OuterGap() int {
	if d.cfg != nil {
		return d.cfg.Windows.OuterGap
	}
	return 6
}

func (d *deskSettings) ColorScheme() string {
	if d.colorScheme == "" {
		return "auto"
	}
	return d.colorScheme
}

func (d *deskSettings) WindowRules() []wlipc.WindowRule {
	if d.cfg == nil {
		return nil
	}
	return d.cfg.WindowRules
}

func (d *deskSettings) setWindowRules(rules []wlipc.WindowRule) {
	d.cfg.WindowRules = rules
	d.saveAndApply()
}

func (d *deskSettings) DesktopCount() int {
	if d.desktopCount < 2 {
		return 4
	}
	return d.desktopCount
}

func (d *deskSettings) DesktopNames() []string {
	return d.desktopNames
}

func (d *deskSettings) NightLightEnabled() bool {
	return d.nightLightEnabled
}

func (d *deskSettings) NightLightTemperature() int {
	if d.nightLightTemperature == 0 {
		return 4500
	}
	return d.nightLightTemperature
}

func (d *deskSettings) NarrowWidgetPanel() bool {
	return d.narrowPanel
}

func (d *deskSettings) NarrowLeftLauncher() bool {
	return d.narrowLeftLauncher
}

func (d *deskSettings) BarPosition() string {
	if d.barPosition == "" {
		if d.narrowLeftLauncher {
			return "left"
		}
		return "bottom"
	}
	return d.barPosition
}

func (d *deskSettings) ScreenSaverClock() bool {
	return d.screenSaverClock
}

func (d *deskSettings) ScreenSaverType() string {
	return d.screenSaver
}

func (d *deskSettings) ScreenSaverLabel() string {
	return d.screenSaverLabel
}

func (d *deskSettings) BorderButtonPosition() string {
	return d.borderButtonPosition
}

func (d *deskSettings) ClockFormatting() string {
	return d.clockFormatting
}

func (d *deskSettings) ClockShowSeconds() bool {
	return d.clockShowSeconds
}

func (d *deskSettings) AddChangeListener(listener func(fynedesk.DeskSettings)) {
	d.listenerLock.Lock()
	defer d.listenerLock.Unlock()
	d.changeListeners = append(d.changeListeners, listener)
}

func (d *deskSettings) beginBatch() {
	d.batching = true
}

func (d *deskSettings) endBatch() {
	d.batching = false
	d.apply()
}

func (d *deskSettings) apply() {
	if d.batching {
		return
	}

	d.listenerLock.Lock()
	listeners := d.changeListeners
	defer d.listenerLock.Unlock()

	fyne.Do(func() {
		for _, listener := range listeners {
			listener(d)
		}
	})

	// Notify compositor to reload settings with a prefs snapshot
	// (Fyne may not have flushed to disk yet)
	if wlipc.IsWaylandSession() {
		_ = wlipc.NotifySettingsChanged(d.prefsSnapshot())
	}
}

func isModuleEnabled(name string, settings fynedesk.DeskSettings) bool {
	for _, mod := range settings.ModuleNames() {
		if mod == name {
			return true
		}
	}

	return false
}

func (d *deskSettings) setBackgroundType(bgType string) {
	d.cfg.Display.BackgroundType = bgType
	fyne.CurrentApp().Preferences().SetString("background_type", bgType)
	// No apply() here — setBackground is always called next and will apply.
}

func (d *deskSettings) setBackground(name string) {
	d.background = name
	d.cfg.Display.Background = name
	d.saveAndApply()
}

// setMonitorBackground sets a per-monitor wallpaper override.
// If path is empty, the override is removed (falls back to global).
func (d *deskSettings) setMonitorBackground(outputName, path, bgType string) {
	if d.cfg.Display.Monitors == nil {
		d.cfg.Display.Monitors = make(map[string]MonitorWallpaper)
	}
	if path == "" {
		delete(d.cfg.Display.Monitors, outputName)
	} else {
		d.cfg.Display.Monitors[outputName] = MonitorWallpaper{
			Background:     path,
			BackgroundType: bgType,
		}
	}
	d.saveAndApply()
}

func (d *deskSettings) setIconTheme(name string) {
	d.iconTheme = name
	d.cfg.Display.IconTheme = name
	d.saveAndApply()
}

func (d *deskSettings) setLauncherIcons(defaultApps []string) {
	d.launcherIcons = defaultApps
	d.cfg.Launcher.Icons = defaultApps
	d.saveAndApply()
}

func (d *deskSettings) setLauncherIconSize(size float32) {
	d.launcherIconSize = size
	d.cfg.Launcher.IconSize = int(size)
	d.saveAndApply()
}

func (d *deskSettings) setLauncherDisableTaskbar(taskbar bool) {
	d.launcherDisableTaskbar = taskbar
	d.cfg.Launcher.DisableTaskbar = taskbar
	d.saveAndApply()
}

func (d *deskSettings) setLauncherDisableZoom(zoom bool) {
	d.launcherDisableZoom = zoom
	d.cfg.Launcher.DisableZoom = zoom
	d.saveAndApply()
}

func (d *deskSettings) setLauncherZoomScale(scale float32) {
	d.launcherZoomScale = scale
	d.cfg.Launcher.ZoomScale = float64(scale)
	d.saveAndApply()
}

func (d *deskSettings) setKeyboardModifier(mod fyne.KeyModifier) {
	d.modifier = mod
	if mod == fyne.KeyModifierAlt {
		d.cfg.Input.KeyboardModifier = "Alt"
	} else {
		d.cfg.Input.KeyboardModifier = "Super"
	}
	d.saveAndApply()
}

func (d *deskSettings) setModuleNames(names []string) {
	d.moduleNames = names
	d.cfg.Modules.Enabled = names
	d.saveAndApply()
}

func (d *deskSettings) setKeyboardLayouts(layouts []string) {
	d.keyboardLayouts = layouts
	d.cfg.Input.KeyboardLayouts = layouts
	d.saveAndApply()
}

func (d *deskSettings) setNaturalScroll(natural bool) {
	d.naturalScroll = natural
	d.cfg.Input.NaturalScroll = natural
	d.saveAndApply()
}

func (d *deskSettings) setWindowGaps(inner, outer int) {
	d.cfg.Windows.InnerGap = inner
	d.cfg.Windows.OuterGap = outer
	d.saveAndApply()
}

func (d *deskSettings) setNarrowLeftLauncher(narrow bool) {
	d.narrowLeftLauncher = narrow
	d.cfg.Launcher.NarrowLeft = narrow
	d.saveAndApply()
}

func (d *deskSettings) setBarPosition(pos string) {
	d.barPosition = pos
	d.cfg.Launcher.BarPosition = pos
	d.narrowLeftLauncher = pos == "left"
	d.cfg.Launcher.NarrowLeft = d.narrowLeftLauncher
	d.saveAndApply()
}

func (d *deskSettings) setNarrowWidgetPanel(narrow bool) {
	d.narrowPanel = narrow
	d.cfg.Panel.NarrowWidget = narrow
	d.saveAndApply()
}

func (d *deskSettings) setScreenSaver(saver string) {
	oldSaver := d.screenSaver
	d.screenSaver = saver
	d.cfg.ScreenSaver.Type = saver
	d.saveTOML()

	if oldSaver == "XScreensaver" && saver != "XScreensaver" {
		cmd := exec.Command("xscreensaver-command", "-exit")
		_ = cmd.Start()
	} else if oldSaver != "XScreensaver" && saver == "XScreensaver" {
		cmd := exec.Command("xscreensaver", "--no-splash")
		_ = cmd.Start()
	}
}

func (d *deskSettings) setScreenSaverClock(show bool) {
	d.screenSaverClock = show
	d.cfg.ScreenSaver.ShowClock = show
	d.saveTOML()
}

func (d *deskSettings) setScreenSaverLabel(text string) {
	d.screenSaverLabel = text
	d.cfg.ScreenSaver.Label = text
	d.saveTOML()
}

func (d *deskSettings) setBorderButtonPosition(pos string) {
	d.borderButtonPosition = pos
	d.cfg.Display.BorderButtonPosition = pos
	d.saveAndApply()
}

func (d *deskSettings) setClockFormatting(format string) {
	d.clockFormatting = format
	d.cfg.Clock.Format = format
	d.saveAndApply()
}

func (d *deskSettings) setClockShowSeconds(show bool) {
	d.clockShowSeconds = show
	d.cfg.Clock.ShowSeconds = show
	d.saveAndApply()
}

func (d *deskSettings) setNightLightEnabled(on bool) {
	d.nightLightEnabled = on
	d.cfg.NightLight.Enabled = on
	d.saveAndApply()
}

func (d *deskSettings) setDesktopCount(count int) {
	if count < 1 {
		count = 1
	} else if count > 8 {
		count = 8
	}
	d.desktopCount = count
	d.cfg.Desktops.Count = count
	d.saveAndApply()
}

func (d *deskSettings) setDesktopNames(names []string) {
	d.desktopNames = names
	d.cfg.Desktops.Names = names
	d.saveAndApply()
}

func (d *deskSettings) setColorScheme(scheme string) {
	if scheme != "dark" && scheme != "light" {
		scheme = "auto"
	}
	d.colorScheme = scheme
	d.cfg.Display.ColorScheme = scheme
	d.saveAndApply()
}

func (d *deskSettings) setReduceMotion(reduce bool) {
	d.cfg.Display.ReduceMotion = reduce
	d.saveAndApply()
}

func (d *deskSettings) ReduceMotion() bool {
	if d.cfg == nil {
		return false
	}
	return d.cfg.Display.ReduceMotion
}

func (d *deskSettings) HighContrast() bool {
	return d.cfg.Display.HighContrast
}

func (d *deskSettings) setHighContrast(on bool) {
	d.cfg.Display.HighContrast = on
	d.saveAndApply()
}

func (d *deskSettings) Language() string {
	if d.cfg == nil || d.cfg.Display.Language == "" {
		return "en"
	}
	return d.cfg.Display.Language
}

func (d *deskSettings) setLanguage(lang string) {
	d.cfg.Display.Language = lang
	d.saveAndApply()
}

func (d *deskSettings) PowerLockTimeout() int {
	if d.cfg == nil {
		return 5
	}
	return d.cfg.Power.LockTimeoutMin
}

func (d *deskSettings) PowerBlankTimeout() int {
	if d.cfg == nil {
		return 6
	}
	return d.cfg.Power.BlankTimeoutMin
}

func (d *deskSettings) PowerSuspendTimeout() int {
	if d.cfg == nil {
		return 0
	}
	return d.cfg.Power.SuspendTimeoutMin
}

func (d *deskSettings) PowerSuspendAction() string {
	if d.cfg == nil || d.cfg.Power.SuspendAction == "" {
		return "suspend"
	}
	return d.cfg.Power.SuspendAction
}

func (d *deskSettings) setPowerSettings(lockMin, blankMin, suspendMin int, action string) {
	if lockMin < 0 {
		lockMin = 0
	}
	if blankMin < 0 {
		blankMin = 0
	}
	if suspendMin < 0 {
		suspendMin = 0
	}
	if action != "suspend" && action != "hibernate" && action != "hybrid-sleep" {
		action = "nothing"
	}
	d.cfg.Power.LockTimeoutMin = lockMin
	d.cfg.Power.BlankTimeoutMin = blankMin
	d.cfg.Power.SuspendTimeoutMin = suspendMin
	d.cfg.Power.SuspendAction = action
	d.saveAndApply()
}

func (d *deskSettings) setFont(family string, size int) {
	if size < 8 {
		size = 8
	} else if size > 24 {
		size = 24
	}
	d.cfg.Theme.FontFamily = family
	d.cfg.Theme.FontSize = size
	d.saveAndApply()
}

// saveAndApply writes TOML, syncs to Fyne prefs, and applies changes.
func (d *deskSettings) saveAndApply() {
	d.saveTOML()
	d.apply()
}

// saveTOML persists the config to TOML and syncs to Fyne prefs cache.
func (d *deskSettings) saveTOML() {
	if d.cfg == nil {
		return
	}
	if err := saveConfig(d.cfg); err != nil {
		log.Printf("Warning: could not save config.toml: %v\n", err)
	}
	syncToFynePrefs(d.cfg)
}

func (d *deskSettings) load() {
	cfg, migrated := loadConfig()
	d.cfg = cfg

	// Environment variable overrides
	env := os.Getenv("FYNEDESK_BACKGROUND")
	if env != "" {
		d.background = env
	} else {
		d.background = cfg.Display.Background
	}

	env = os.Getenv("FYNEDESK_ICONTHEME")
	if env != "" {
		d.iconTheme = env
	} else {
		d.iconTheme = cfg.Display.IconTheme
	}
	if d.iconTheme == "" {
		d.iconTheme = "hicolor"
	}

	d.launcherIcons = cfg.Launcher.Icons
	if len(d.launcherIcons) == 0 {
		defaultApps := fynedesk.Instance().IconProvider().DefaultApps()
		for _, appData := range defaultApps {
			d.launcherIcons = append(d.launcherIcons, appData.Name())
		}
	}

	d.launcherIconSize = float32(cfg.Launcher.IconSize)
	if d.launcherIconSize == 0 {
		d.launcherIconSize = 48
	}

	d.launcherDisableTaskbar = cfg.Launcher.DisableTaskbar
	d.launcherDisableZoom = cfg.Launcher.DisableZoom

	d.launcherZoomScale = float32(cfg.Launcher.ZoomScale)
	if d.launcherZoomScale == 0.0 {
		d.launcherZoomScale = 2.0
	}

	d.moduleNames = cfg.Modules.Enabled
	if len(d.moduleNames) == 0 {
		defaultModules := "Next Meeting|Today's Agenda|Battery|Brightness|Compositor|Sound|Keyboard Layout|Launcher: Calculate|Launcher: Convert units|Launcher: Open URLs|Network|Notifications|Virtual Desktops|SystemTray|Terminal Overlay|Desktop Files"
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" { // testing
			defaultModules = "Battery|Brightness|Sound|Launcher: Calculate|Launcher: Open URLs|Network|Virtual Desktops"
		}
		d.moduleNames = strings.Split(defaultModules, "|")
	}
	// Auto-migrate: add new modules for existing users
	d.migrateModules("Keyboard Layout", "Notifications", "Power Profile", "Notes",
		"Next Meeting", "Today's Agenda")

	if cfg.Input.KeyboardModifier == "Alt" {
		d.modifier = fyne.KeyModifierAlt
	} else {
		d.modifier = fyne.KeyModifierSuper
	}
	d.narrowLeftLauncher = cfg.Launcher.NarrowLeft
	d.barPosition = cfg.Launcher.BarPosition
	if d.barPosition == "" {
		if d.narrowLeftLauncher {
			d.barPosition = "left"
		}
	}
	d.narrowPanel = cfg.Panel.NarrowWidget
	d.naturalScroll = cfg.Input.NaturalScroll
	d.nightLightEnabled = cfg.NightLight.Enabled
	d.nightLightTemperature = cfg.NightLight.Temperature
	if d.nightLightTemperature == 0 {
		d.nightLightTemperature = 4500
	}

	d.keyboardLayouts = cfg.Input.KeyboardLayouts
	if len(d.keyboardLayouts) == 0 {
		d.keyboardLayouts = []string{"fr:bepo_afnor"}
	}

	d.colorScheme = cfg.Display.ColorScheme
	if d.colorScheme != "dark" && d.colorScheme != "light" {
		d.colorScheme = "auto"
	}

	d.borderButtonPosition = cfg.Display.BorderButtonPosition
	if d.borderButtonPosition == "" {
		d.borderButtonPosition = "Left"
	}
	d.screenSaver = cfg.ScreenSaver.Type
	if d.screenSaver == "" {
		d.screenSaver = "FyshOS"
	}
	d.screenSaverClock = cfg.ScreenSaver.ShowClock
	d.screenSaverLabel = cfg.ScreenSaver.Label
	if d.screenSaverLabel == "" {
		d.screenSaverLabel = "FyneDesk"
	}

	d.desktopCount = cfg.Desktops.Count
	if d.desktopCount < 2 {
		d.desktopCount = 4
	}
	d.desktopNames = cfg.Desktops.Names

	d.clockFormatting = cfg.Clock.Format
	if d.clockFormatting == "" {
		d.clockFormatting = "12h"
	}
	d.clockShowSeconds = cfg.Clock.ShowSeconds
	d.loadRecents()

	// If migrated from Fyne prefs, sync back so both stores are consistent
	if migrated {
		syncToFynePrefs(cfg)
	}
}

func (d *deskSettings) loadRecents() {
	str := fyne.CurrentApp().Preferences().String("recentapps")
	desk := fynedesk.Instance().(*desktop)

	var apps []appie.AppData
	list := strings.Split(str, ",")

	for _, s := range list {
		app := desk.icons.FindAppFromName(s)
		if app == nil {
			continue
		}
		apps = append(apps, app)
	}

	desk.recent = apps
}

func (d *deskSettings) saveRecents() {
	var list []string

	for _, a := range fynedesk.Instance().(*desktop).recent {
		list = append(list, a.Name())
	}

	fyne.CurrentApp().Preferences().SetString("recentapps", strings.Join(list, ","))
}

// loadKeybindings reads keybinding configuration from TOML config, merged with defaults.
func (d *deskSettings) loadKeybindings() wlipc.ActionBindings {
	if d.cfg != nil && len(d.cfg.Keybindings) > 0 {
		user := tomlToActionBindings(d.cfg.Keybindings)
		return wlipc.MergeWithDefaults(user)
	}
	// Fallback: try Fyne preferences (backward compat)
	jsonStr := fyne.CurrentApp().Preferences().String("keybindings")
	user, err := wlipc.ParseBindingsJSON(jsonStr)
	if err != nil {
		log.Printf("Warning: invalid keybindings preference: %v\n", err)
	}
	return wlipc.MergeWithDefaults(user)
}

// saveKeybindings stores keybinding configuration to TOML and notifies compositor.
func (d *deskSettings) saveKeybindings(bindings wlipc.ActionBindings) {
	// Only save non-default bindings to keep config clean
	defaults := wlipc.DefaultBindings()
	custom := make(wlipc.ActionBindings)
	for action, bs := range bindings {
		if !bindingsEqual(bs, defaults[action]) {
			custom[action] = bs
		}
	}

	if len(custom) == 0 {
		d.cfg.Keybindings = nil
	} else {
		d.cfg.Keybindings = actionBindingsToTOML(custom)
	}
	d.saveTOML()
	d.apply()
}

// bindingsEqual compares two binding slices for equality.
func bindingsEqual(a, b []wlipc.KeyBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key {
			return false
		}
		if len(a[i].Mods) != len(b[i].Mods) {
			return false
		}
		for j := range a[i].Mods {
			if a[i].Mods[j] != b[i].Mods[j] {
				return false
			}
		}
	}
	return true
}

// migrateModules adds newly introduced modules to the user's saved module list.
func (d *deskSettings) migrateModules(names ...string) {
	changed := false
	for _, name := range names {
		found := false
		for _, m := range d.moduleNames {
			if m == name {
				found = true
				break
			}
		}
		if !found {
			d.moduleNames = append(d.moduleNames, name)
			changed = true
		}
	}
	if changed {
		d.cfg.Modules.Enabled = d.moduleNames
		d.saveTOML()
	}
}

// prefsSnapshot returns a map of all settings that the compositor needs.
// Reads from the TOML config (source of truth) to avoid Fyne flush races.
func (d *deskSettings) prefsSnapshot() map[string]any {
	var modVal float64
	if d.cfg.Input.KeyboardModifier == "Alt" {
		modVal = float64(fyne.KeyModifierAlt)
	} else {
		modVal = float64(fyne.KeyModifierSuper)
	}

	snapshot := map[string]any{
		"background":            d.cfg.Display.Background,
		"background_type":       d.cfg.Display.BackgroundType,
		"borderbuttonposition":  d.cfg.Display.BorderButtonPosition,
		"keyboardmodifier":      modVal,
		"naturalscroll":         d.cfg.Input.NaturalScroll,
		"narrowpanel":           d.cfg.Panel.NarrowWidget,
		"launchernarrowleft":    d.cfg.Launcher.NarrowLeft,
		"barposition":           d.cfg.Launcher.BarPosition,
		"keyboardlayouts":       strings.Join(d.cfg.Input.KeyboardLayouts, "|"),
		"nightlightenabled":     d.cfg.NightLight.Enabled,
		"nightlighttemperature": float64(d.cfg.NightLight.Temperature),
		"reducemotion":          d.cfg.Display.ReduceMotion,
		"highcontrast":          d.cfg.Display.HighContrast,
		"language":              d.cfg.Display.Language,
	}

	// Power management
	snapshot["power_lock_timeout"] = float64(d.cfg.Power.LockTimeoutMin)
	snapshot["power_blank_timeout"] = float64(d.cfg.Power.BlankTimeoutMin)
	snapshot["power_suspend_timeout"] = float64(d.cfg.Power.SuspendTimeoutMin)
	snapshot["power_suspend_action"] = d.cfg.Power.SuspendAction

	// Keybindings: serialize to JSON for compositor backward compat
	if len(d.cfg.Keybindings) > 0 {
		bindings := tomlToActionBindings(d.cfg.Keybindings)
		jsonStr, err := wlipc.BindingsToJSON(bindings)
		if err == nil {
			snapshot["keybindings"] = jsonStr
		}
	} else {
		snapshot["keybindings"] = ""
	}

	// Font
	snapshot["fontfamily"] = d.cfg.Theme.FontFamily
	snapshot["fontsize"] = float64(d.cfg.Theme.FontSize)

	// Theme name and custom colors
	snapshot["theme_name"] = d.cfg.Theme.Name
	if len(d.cfg.Theme.Colors) > 0 {
		colorParts := make([]string, 0, len(d.cfg.Theme.Colors))
		for k, v := range d.cfg.Theme.Colors {
			colorParts = append(colorParts, k+"="+v)
		}
		snapshot["theme_colors"] = strings.Join(colorParts, "|")
	}

	// Per-monitor wallpapers
	if len(d.cfg.Display.Monitors) > 0 {
		monData, err := json.Marshal(d.cfg.Display.Monitors)
		if err == nil {
			snapshot["monitor_wallpapers"] = string(monData)
		}
	}

	return snapshot
}

// newDeskSettings loads the user's preferences from environment or config
func newDeskSettings() *deskSettings {
	settings := &deskSettings{}
	settings.load()

	return settings
}
