package compositor

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"

	"deedles.dev/wlr"
	"deedles.dev/wlr/xkb"
	"github.com/BurntSushi/toml"

	"fyshos.com/fynedesk/wlipc"
)

func (s *server) getConfigDir() string {
	home := os.Getenv("HOME")
	configDir := filepath.Join(home, ".config", "fynedesk")
	os.MkdirAll(configDir, 0700)
	return configDir
}

// atomicWriteFile writes data atomically using write-to-temp + rename
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ipc-*")
	if err != nil {
		return os.WriteFile(path, data, 0644) // Fallback to direct write
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// readPrefs reads settings, preferring TOML config over Fyne preferences JSON.
func (s *server) readPrefs() (map[string]interface{}, string, error) {
	// Try TOML config first (source of truth)
	tomlPath := filepath.Join(s.getConfigDir(), "config.toml")
	if prefs, err := readTOMLAsPrefs(tomlPath); err == nil {
		return prefs, tomlPath, nil
	}

	// Fall back to Fyne preferences JSON
	home := os.Getenv("HOME")
	prefsPath := filepath.Join(home, ".config", "fyne", "com.fyshos.fynedesk", "preferences.json")

	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return nil, "", err
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, "", err
	}
	return prefs, prefsPath, nil
}

// compositorConfig mirrors the TOML structure for compositor-side reading.
// monitorWallpaper mirrors the panel-side MonitorWallpaper for TOML decoding.
type monitorWallpaper struct {
	Background     string `toml:"background" json:"Background"`
	BackgroundType string `toml:"background_type" json:"BackgroundType"`
}

type compositorConfig struct {
	Display struct {
		Background           string                          `toml:"background"`
		BackgroundType       string                          `toml:"background_type"`
		BorderButtonPosition string                          `toml:"border_button_position"`
		Monitors             map[string]monitorWallpaper     `toml:"monitors"`
	} `toml:"display"`
	Launcher struct {
		NarrowLeft  bool    `toml:"narrow_left"`
		BarPosition string  `toml:"bar_position"`
		IconSize    int     `toml:"icon_size"`
		ZoomScale   float64 `toml:"zoom_scale"`
	} `toml:"launcher"`
	Panel struct {
		NarrowWidget bool `toml:"narrow_widget"`
	} `toml:"panel"`
	Input struct {
		KeyboardModifier string   `toml:"keyboard_modifier"`
		NaturalScroll    bool     `toml:"natural_scroll"`
		KeyboardLayouts  []string `toml:"keyboard_layouts"`
	} `toml:"input"`
	Screensaver struct {
		Type  string `toml:"type"`
		Label string `toml:"label"`
	} `toml:"screensaver"`
	Keybindings map[string][]struct {
		Key  string   `toml:"key"`
		Mods []string `toml:"mods"`
	} `toml:"keybindings"`
	Theme struct {
		AutoAccentColor bool   `toml:"auto_accent_color"`
		FontFamily      string `toml:"font_family"`
		FontSize        int    `toml:"font_size"`
	} `toml:"theme"`
	WindowRules []wlipc.WindowRule `toml:"window_rules"`
	Windows struct {
		InnerGap int `toml:"inner_gap"`
		OuterGap int `toml:"outer_gap"`
	} `toml:"windows"`
	HotCorners struct {
		TopLeft     string `toml:"top_left"`
		TopRight    string `toml:"top_right"`
		BottomLeft  string `toml:"bottom_left"`
		BottomRight string `toml:"bottom_right"`
	} `toml:"hot_corners"`
	NightLight struct {
		Enabled     bool `toml:"enabled"`
		Temperature int  `toml:"temperature"`
	} `toml:"night_light"`
}

// readTOMLAsPrefs reads config.toml and returns settings as a flat prefs map.
func readTOMLAsPrefs(path string) (map[string]interface{}, error) {
	var cfg compositorConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}

	prefs := make(map[string]interface{})
	prefs["background"] = cfg.Display.Background
	prefs["background_type"] = cfg.Display.BackgroundType
	prefs["borderbuttonposition"] = cfg.Display.BorderButtonPosition

	// Per-monitor wallpapers
	if len(cfg.Display.Monitors) > 0 {
		monData, err := json.Marshal(cfg.Display.Monitors)
		if err == nil {
			prefs["monitor_wallpapers"] = string(monData)
		}
	}

	prefs["launchernarrowleft"] = cfg.Launcher.NarrowLeft
	prefs["barposition"] = cfg.Launcher.BarPosition
	prefs["launchericonsize"] = float64(cfg.Launcher.IconSize)
	prefs["launcherzoomscale"] = cfg.Launcher.ZoomScale
	prefs["narrowpanel"] = cfg.Panel.NarrowWidget
	prefs["naturalscroll"] = cfg.Input.NaturalScroll
	prefs["keyboardlayouts"] = strings.Join(cfg.Input.KeyboardLayouts, "|")

	// Keyboard modifier
	if cfg.Input.KeyboardModifier == "Alt" {
		prefs["keyboardmodifier"] = float64(4) // fyne.KeyModifierAlt
	} else {
		prefs["keyboardmodifier"] = float64(8) // fyne.KeyModifierSuper
	}

	// Screensaver
	prefs["savertype"] = cfg.Screensaver.Type
	prefs["saverlabel"] = cfg.Screensaver.Label

	// Keybindings: convert to JSON string for existing loadKeybindings()
	if len(cfg.Keybindings) > 0 {
		bindings := make(wlipc.ActionBindings)
		for action, bs := range cfg.Keybindings {
			for _, b := range bs {
				bindings[action] = append(bindings[action], wlipc.KeyBinding{
					Key:  b.Key,
					Mods: b.Mods,
				})
			}
		}
		jsonStr, err := wlipc.BindingsToJSON(bindings)
		if err == nil {
			prefs["keybindings"] = jsonStr
		}
	}

	// Auto accent color from wallpaper
	prefs["autoaccentcolor"] = cfg.Theme.AutoAccentColor
	prefs["fontfamily"] = cfg.Theme.FontFamily
	prefs["fontsize"] = float64(cfg.Theme.FontSize)

	// Window rules: serialize to JSON for loadWindowRules()
	if len(cfg.WindowRules) > 0 {
		data, err := json.Marshal(cfg.WindowRules)
		if err == nil {
			prefs["windowrules"] = string(data)
		}
	}

	// Window gaps
	prefs["windowinnergap"] = float64(cfg.Windows.InnerGap)
	prefs["windowoutergap"] = float64(cfg.Windows.OuterGap)

	// Night light
	prefs["nightlightenabled"] = cfg.NightLight.Enabled
	prefs["nightlighttemperature"] = float64(cfg.NightLight.Temperature)

	// Hot corners
	prefs["hotcorner_topleft"] = cfg.HotCorners.TopLeft
	prefs["hotcorner_topright"] = cfg.HotCorners.TopRight
	prefs["hotcorner_bottomleft"] = cfg.HotCorners.BottomLeft
	prefs["hotcorner_bottomright"] = cfg.HotCorners.BottomRight

	return prefs, nil
}

// applyPrefs applies preferences that can be reloaded at runtime
func (s *server) applyPrefs(prefs map[string]interface{}) {
	// Button position
	if pos, ok := prefs["borderbuttonposition"].(string); ok {
		s.buttonsOnLeft = (pos == "Left")
	}

	// Keyboard modifier (Fyne KeyModifierAlt=4, KeyModifierSuper=8)
	if modVal, ok := prefs["keyboardmodifier"].(float64); ok {
		if int(modVal) == 4 {
			s.wmModifier = wlr.KeyboardModifierAlt
		} else {
			s.wmModifier = wlr.KeyboardModifierLogo
		}
	}

	// Natural scroll
	if natural, ok := prefs["naturalscroll"].(bool); ok {
		s.naturalScroll = natural
	} else {
		s.naturalScroll = false
	}

	// Narrow widget panel
	if narrow, ok := prefs["narrowpanel"].(bool); ok {
		s.narrowWidgetPanel = narrow
	} else {
		s.narrowWidgetPanel = false
	}

	// Narrow left launcher (vertical bar on left)
	if narrow, ok := prefs["launchernarrowleft"].(bool); ok {
		s.narrowLeftLauncher = narrow
	} else {
		s.narrowLeftLauncher = true // default: narrow left bar
	}

	// Bar position (left or bottom)
	if pos, ok := prefs["barposition"].(string); ok && (pos == "left" || pos == "bottom") {
		s.barPosition = pos
	} else {
		s.barPosition = "left"
	}

	// Launcher icon size (default 48)
	if sz, ok := prefs["launchericonsize"].(float64); ok && sz > 0 {
		s.launcherIconSize = int(sz)
	} else {
		s.launcherIconSize = 48
	}

	// Launcher zoom scale (default 2.0)
	if zs, ok := prefs["launcherzoomscale"].(float64); ok && zs > 0 {
		s.launcherZoomScale = zs
	} else {
		s.launcherZoomScale = 2.0
	}

	// Theme colors for decorations
	if colorsStr, ok := prefs["theme_colors"].(string); ok && colorsStr != "" {
		s.applyThemeColors(colorsStr)
	} else {
		// Fall back to reading the active theme.json directly
		if colorsStr := s.readThemeFileColors(); colorsStr != "" {
			s.applyThemeColors(colorsStr)
		}
	}

	// Keyboard layouts
	kbStr, _ := prefs["keyboardlayouts"].(string)
	if kbStr == "" {
		kbStr = "fr:bepo_afnor"
	}
	parsed := wlipc.ParseKeyboardLayoutPref(kbStr)
	if len(parsed) > 0 {
		// Build display names from layout:variant
		var layouts []wlipc.KeyboardLayout
		for _, p := range parsed {
			layouts = append(layouts, wlipc.KeyboardLayout{
				Layout:      p.Layout,
				Variant:     p.Variant,
				DisplayName: p.ShortName(),
			})
		}
		s.keyboardLayouts = layouts
		if s.activeLayoutIndex >= len(layouts) {
			s.activeLayoutIndex = 0
		}
	}

	// Night light
	prevNL := s.nightLight
	if enabled, ok := prefs["nightlightenabled"].(bool); ok {
		s.nightLight.enabled = enabled
	}
	if temp, ok := prefs["nightlighttemperature"].(float64); ok && temp >= 2700 && temp <= 6500 {
		s.nightLight.temperature = int(temp)
	} else if s.nightLight.temperature == 0 {
		s.nightLight.temperature = 4500
	}
	if s.nightLight != prevNL {
		s.applyNightLight()
	}

	// Desktop count and names
	if count, ok := prefs["desktopcount"].(float64); ok && int(count) >= 2 && int(count) <= 8 {
		s.numDesks = int(count)
		// Ensure tiling state array matches
		if len(s.tiling) < s.numDesks {
			s.tiling = append(s.tiling, make([]tilingState, s.numDesks-len(s.tiling))...)
		}
	}
	if namesStr, ok := prefs["desktopnames"].(string); ok && namesStr != "" {
		s.desktopNames = strings.Split(namesStr, "|")
	} else {
		s.desktopNames = nil
	}

	// Color scheme for portal (auto/dark/light → 0/1/2)
	prevColorScheme := s.colorScheme
	if cs, ok := prefs["colorscheme"].(string); ok {
		switch cs {
		case "dark":
			s.colorScheme = 1
		case "light":
			s.colorScheme = 2
		default:
			s.colorScheme = 0
		}
	}
	if s.colorScheme != prevColorScheme && s.portal != nil {
		s.portal.emitColorSchemeChanged(s.colorScheme)
	}

	// Window gaps
	if ig, ok := prefs["windowinnergap"].(float64); ok && ig >= 0 {
		s.innerGap = int(ig)
	} else if s.innerGap == 0 {
		s.innerGap = defaultInnerGap
	}
	if og, ok := prefs["windowoutergap"].(float64); ok {
		s.outerGap = int(og)
	}

	// Hot corners
	if v, ok := prefs["hotcorner_topleft"].(string); ok {
		s.hotCornerActions[cornerTopLeft] = v
	}
	if v, ok := prefs["hotcorner_topright"].(string); ok {
		s.hotCornerActions[cornerTopRight] = v
	}
	if v, ok := prefs["hotcorner_bottomleft"].(string); ok {
		s.hotCornerActions[cornerBottomLeft] = v
	}
	if v, ok := prefs["hotcorner_bottomright"].(string); ok {
		s.hotCornerActions[cornerBottomRight] = v
	}

	// Reduce motion (accessibility)
	if rm, ok := prefs["reducemotion"].(bool); ok {
		s.reduceMotion = rm
	}

	// High contrast (accessibility)
	if hc, ok := prefs["highcontrast"].(bool); ok {
		s.highContrast = hc
	}

	// Global background path
	if bgPath, ok := prefs["background"].(string); ok {
		s.backgroundPath = bgPath
	}

	// Per-monitor wallpapers
	if monStr, ok := prefs["monitor_wallpapers"].(string); ok && monStr != "" {
		var monCfg map[string]monitorWallpaper
		if err := json.Unmarshal([]byte(monStr), &monCfg); err == nil {
			monMap := make(map[string]monitorWP, len(monCfg))
			for name, mw := range monCfg {
				monMap[name] = monitorWP{background: mw.Background, backgroundType: mw.BackgroundType}
			}
			s.monitorWallpapers = monMap
		}
	} else {
		s.monitorWallpapers = nil
	}

	// Auto accent color from wallpaper
	if autoAccent, ok := prefs["autoaccentcolor"].(bool); ok {
		s.autoAccentColor = autoAccent
	}

	// Font settings
	if ff, ok := prefs["fontfamily"].(string); ok && ff != "" {
		s.fontFamily = ff
	}
	if fs, ok := prefs["fontsize"].(float64); ok && fs >= 8 && fs <= 24 {
		s.fontSize = int(fs)
	} else if s.fontSize == 0 {
		s.fontSize = 13
	}

	// Screensaver
	if st, ok := prefs["savertype"].(string); ok {
		s.lockScreenType = st
	}
	if label, ok := prefs["saverlabel"].(string); ok {
		s.lockLabel = label
	}

	// Power management
	if v, ok := prefs["power_lock_timeout"].(float64); ok {
		s.powerLockTimeout = int(v)
	}
	if v, ok := prefs["power_blank_timeout"].(float64); ok {
		s.powerBlankTimeout = int(v)
	}
	if v, ok := prefs["power_suspend_timeout"].(float64); ok {
		s.powerSuspendTimeout = int(v)
	}
	if v, ok := prefs["power_suspend_action"].(string); ok && v != "" {
		s.powerSuspendAction = v
	}
}

// loadSettings reads user preferences from Fyne preferences file
func (s *server) loadSettings() {
	prefs, _, err := s.readPrefs()
	if err != nil {
		// Still load default keybindings even without preferences file
		s.loadKeybindings(nil)
		return
	}

	s.applyPrefs(prefs)
	s.loadKeybindings(prefs)
	s.loadWindowRules(prefs)

	// Resolve font on initial load
	if s.fontFamily != "" {
		customFontPath = resolveFontPath(s.fontFamily)
	}
	if s.fontSize > 0 {
		customFontSize = float64(s.fontSize)
	}

	log.Printf("Settings: buttons=%v, naturalScroll=%v, narrowWidgetPanel=%v, narrowLeftLauncher=%v, barPosition=%v, iconSize=%d, zoomScale=%.1f\n",
		map[bool]string{true: "left", false: "right"}[s.buttonsOnLeft], s.naturalScroll, s.narrowWidgetPanel, s.narrowLeftLauncher, s.barPosition, s.launcherIconSize, s.launcherZoomScale)

	// Read background type
	bgType, _ := prefs["background_type"].(string)
	if bgType == "" {
		bgType = "image"
	}
	s.backgroundType = bgType

	// Start dynamic wallpaper timer if needed
	if bgType == "dynamic" {
		s.startDynamicWallpaperTimer()
	}

	// Handle animated wallpapers
	if bgType == "matrix" || bgType == "starfield" {
		for _, out := range s.outputs {
			s.initAnimWallpaper(out, bgType)
		}
		log.Printf("[WALLPAPER] loadSettings: %s animation for %d outputs\n", bgType, len(s.outputs))
		return
	}

	// Clear any animation state when switching to static
	for _, out := range s.outputs {
		s.clearAnimWallpaper(out)
	}

	// Load wallpaper image (static or dynamic)
	// For dynamic type, loadWallpaperForNewOutput resolves the time-based path
	log.Printf("[WALLPAPER] loadSettings: loading wallpaper (%s) for %d outputs\n", bgType, len(s.outputs))
	for _, out := range s.outputs {
		s.loadWallpaperForNewOutput(out)
	}
}

// reloadSettings re-reads preferences from disk and applies runtime-changeable settings.
// Used as fallback when no prefs snapshot is available in the IPC notification.
func (s *server) reloadSettings() {
	prefs, _, err := s.readPrefs()
	if err != nil {
		return
	}
	s.reloadSettingsFrom(prefs)
}

// reloadSettingsFrom applies runtime-changeable settings from the given prefs map.
func (s *server) reloadSettingsFrom(prefs map[string]interface{}) {
	oldNarrowWidget := s.narrowWidgetPanel
	oldNarrowLeft := s.narrowLeftLauncher
	oldButtonsOnLeft := s.buttonsOnLeft
	oldLayouts := s.keyboardLayouts
	oldBgType := s.backgroundType
	oldBgPath := s.backgroundPath
	oldInnerGap := s.innerGap
	oldOuterGap := s.outerGap
	oldFontFamily := s.fontFamily
	oldFontSize := s.fontSize
	oldMonitorWPs := s.monitorWallpapers

	s.applyPrefs(prefs)
	s.loadKeybindings(prefs)
	s.loadWindowRules(prefs)
	log.Printf("Settings reloaded: buttons=%v, naturalScroll=%v, narrowWidgetPanel=%v, narrowLeftLauncher=%v, barPosition=%v, gaps=%d/%d\n",
		map[bool]string{true: "left", false: "right"}[s.buttonsOnLeft], s.naturalScroll, s.narrowWidgetPanel, s.narrowLeftLauncher, s.barPosition, s.innerGap, s.outerGap)

	// If panel layout or button position changed, refresh maximized windows and decorations
	if s.narrowWidgetPanel != oldNarrowWidget || s.narrowLeftLauncher != oldNarrowLeft {
		s.refreshMaximizedWindows()
	}
	if s.buttonsOnLeft != oldButtonsOnLeft {
		s.refreshAllDecorations()
	}

	// If window gaps changed, retile and re-snap all affected windows
	if s.innerGap != oldInnerGap || s.outerGap != oldOuterGap {
		s.retile()
		s.reSnapAllWindows()
		s.refreshMaximizedWindows()
	}

	// If font settings changed, resolve the new font path and invalidate caches
	if s.fontFamily != oldFontFamily || s.fontSize != oldFontSize {
		customFontPath = resolveFontPath(s.fontFamily)
		customFontSize = float64(s.fontSize)
		invalidateFontCaches()
		s.refreshAllDecorations()
		log.Printf("Font changed: family=%q size=%d path=%q\n", s.fontFamily, s.fontSize, customFontPath)
	}

	// If keyboard layouts changed, re-apply and notify panel
	if !keyboardLayoutsEqual(oldLayouts, s.keyboardLayouts) {
		s.activeLayoutIndex = 0
		s.applyKeyboardLayout()
		log.Printf("Keyboard layouts reloaded: %d layouts configured\n", len(s.keyboardLayouts))
	}

	// Handle background type changes
	newBgType, _ := prefs["background_type"].(string)
	if newBgType == "" {
		newBgType = "image"
	}
	s.backgroundType = newBgType

	bgTypeChanged := newBgType != oldBgType
	monitorWPsChanged := !monitorWPsEqual(oldMonitorWPs, s.monitorWallpapers)

	if bgTypeChanged {
		log.Printf("[WALLPAPER] Background type changed: %s → %s\n", oldBgType, newBgType)
		if newBgType == "matrix" || newBgType == "starfield" {
			// Switch to animated wallpaper
			for _, out := range s.outputs {
				s.initAnimWallpaper(out, newBgType)
			}
		} else {
			// Switch to static/dynamic wallpaper
			for _, out := range s.outputs {
				s.clearAnimWallpaper(out)
			}
			s.loadSettings() // Re-load wallpaper
		}
		// Start/stop dynamic wallpaper timer
		if newBgType == "dynamic" {
			s.startDynamicWallpaperTimer()
		}
	} else if monitorWPsChanged {
		// Per-monitor wallpapers changed without global type change — reload affected outputs
		log.Printf("[WALLPAPER] Per-monitor wallpapers changed, reloading outputs\n")
		for _, out := range s.outputs {
			s.loadWallpaperForNewOutput(out)
		}
	} else if s.backgroundPath != oldBgPath {
		// Global background path changed without type change — reload wallpaper
		log.Printf("[WALLPAPER] Global background path changed: %s → %s\n", oldBgPath, s.backgroundPath)
		for _, out := range s.outputs {
			s.loadWallpaperForNewOutput(out)
		}
	}
}

// monitorWPsEqual compares two per-monitor wallpaper maps for equality.
func monitorWPsEqual(a, b map[string]monitorWP) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va.background != vb.background || va.backgroundType != vb.backgroundType {
			return false
		}
	}
	return true
}

// keyboardLayoutsEqual compares two keyboard layout slices.
func keyboardLayoutsEqual(a, b []wlipc.KeyboardLayout) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Layout != b[i].Layout || a[i].Variant != b[i].Variant {
			return false
		}
	}
	return true
}

// refreshMaximizedWindows re-maximizes all maximized windows with current contentBounds.
func (s *server) refreshMaximizedWindows() {
	for _, v := range s.xdgViews {
		if !v.maximized || !v.mapped {
			continue
		}
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)
		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		v.x = float64(cx)
		v.y = float64(cy + topMargin)
		v.configuredW = cw
		v.configuredH = ch - topMargin
		v.xdgToplevel.SetSize(int32(v.configuredW), int32(v.configuredH))
		setXdgScenePos(v)
		s.updateXdgViewDecorations(v)
	}
	for _, v := range s.xwayViews {
		if !v.maximized || !v.mapped {
			continue
		}
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)
		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		v.x = float64(cx)
		v.y = float64(cy + topMargin)
		v.surface.Configure(int16(v.x), int16(v.y), uint16(cw), uint16(ch-topMargin))
		setXwayScenePos(v)
		s.updateXwayViewDecorations(v)
	}
	log.Println("Refreshed maximized windows with new content bounds")
}

// refreshAllDecorations redraws decorations for all mapped windows (e.g. after button position change).
func (s *server) refreshAllDecorations() {
	for _, v := range s.xdgViews {
		if v.mapped && v.decorated {
			s.updateXdgViewDecorations(v)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && v.decorated {
			s.updateXwayViewDecorations(v)
		}
	}
}

// applyThemeColors parses theme color overrides from a pipe-separated string
// (format: "key1=#rrggbb|key2=#rrggbbaa") and updates decoration colors.
func (s *server) applyThemeColors(colorsStr string) {
	pairs := strings.Split(colorsStr, "|")
	changed := false
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, hex := parts[0], parts[1]
		col := parseHexColor(hex)
		if col == nil {
			continue
		}
		rgba := color.RGBA{R: col.R, G: col.G, B: col.B, A: col.A}
		switch key {
		case "fynedeskTitlebarActive":
			titlebarActiveColor = rgba
			borderActiveColor = rgba
			changed = true
		case "fynedeskTitlebarInactive":
			titlebarColor = rgba
			borderColor = rgba
			buttonBgColor = rgba
			changed = true
		case "fynedeskTitlebarText":
			titlebarTextColor = *col
			changed = true
		}
	}
	if changed {
		s.refreshAllDecorations()
	}
}

// readThemeFileColors reads the active theme.json from Fyne's storage and extracts
// fynedesk titlebar colors as a pipe-delimited string for applyThemeColors.
func (s *server) readThemeFileColors() string {
	home := os.Getenv("HOME")
	themePath := filepath.Join(home, ".config", "fyne", "com.fyshos.fynedesk", "theme.json")
	data, err := os.ReadFile(themePath)
	if err != nil {
		return ""
	}
	var themeData struct {
		Colors map[string]string `json:"Colors"`
	}
	if err := json.Unmarshal(data, &themeData); err != nil || themeData.Colors == nil {
		return ""
	}

	titlebarKeys := []string{
		"fynedeskTitlebarActive",
		"fynedeskTitlebarInactive",
		"fynedeskTitlebarText",
	}
	var parts []string
	for _, key := range titlebarKeys {
		if v, ok := themeData.Colors[key]; ok {
			parts = append(parts, key+"="+v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}

// parseHexColor parses a hex color string (#RGB, #RRGGBB, or #RRGGBBAA).
func parseHexColor(hex string) *color.NRGBA {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b, a uint8
	switch len(hex) {
	case 3:
		n, _ := fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
		if n != 3 {
			return nil
		}
		r, g, b, a = r*17, g*17, b*17, 255
	case 6:
		n, _ := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		if n != 3 {
			return nil
		}
		a = 255
	case 8:
		n, _ := fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a)
		if n != 4 {
			return nil
		}
	default:
		return nil
	}
	return &color.NRGBA{R: r, G: g, B: b, A: a}
}

// modNameToWlr maps modifier string names to wlr.KeyboardModifier values.
var modNameToWlr = map[string]wlr.KeyboardModifier{
	"Shift": wlr.KeyboardModifierShift,
	"Ctrl":  wlr.KeyboardModifierCtrl,
	"Alt":   wlr.KeyboardModifierAlt,
}

// loadKeybindings reads the "keybindings" preference, merges with defaults,
// resolves XKB key names to KeySym values, and builds s.keybindingMap.
func (s *server) loadKeybindings(prefs map[string]interface{}) {
	var userBindings wlipc.ActionBindings
	if kb, ok := prefs["keybindings"].(string); ok && kb != "" {
		var err error
		userBindings, err = wlipc.ParseBindingsJSON(kb)
		if err != nil {
			log.Printf("Warning: invalid keybindings preference: %v\n", err)
		}
	}

	merged := wlipc.MergeWithDefaults(userBindings)
	kbMap := make(map[resolvedBinding]string)

	for action, bindings := range merged {
		for _, b := range bindings {
			sym := xkb.SymFromName(b.Key, xkb.KeySymNoFlags)
			if sym == 0 {
				log.Printf("Warning: unknown XKB key name %q in action %q, skipping\n", b.Key, action)
				continue
			}

			var mods wlr.KeyboardModifier
			hasShift := false
			for _, m := range b.Mods {
				if m == "WM" {
					mods |= s.wmModifier
				} else if wlrMod, ok := modNameToWlr[m]; ok {
					mods |= wlrMod
					if wlrMod == wlr.KeyboardModifierShift {
						hasShift = true
					}
				} else {
					log.Printf("Warning: unknown modifier %q in action %q, skipping\n", m, action)
				}
			}

			kbMap[resolvedBinding{sym: sym, mods: mods}] = action

			// When Shift is held, XKB resolves keysyms with case transformation
			// (e.g. Shift+t → "T" keysym, Shift+Tab → ISO_Left_Tab,
			// Shift+Print → Sys_Req). Register aliases for the shifted
			// variants so both match.
			if hasShift {
				if b.Key == "Tab" {
					isoSym := xkb.SymFromName("ISO_Left_Tab", xkb.KeySymNoFlags)
					if isoSym != 0 {
						kbMap[resolvedBinding{sym: isoSym, mods: mods}] = action
					}
				} else if b.Key == "Print" {
					// Shift+Print produces Sys_Req on most keyboards
					sysReqSym := xkb.SymFromName("Sys_Req", xkb.KeySymNoFlags)
					if sysReqSym != 0 {
						kbMap[resolvedBinding{sym: sysReqSym, mods: mods}] = action
					}
				} else if len(b.Key) == 1 && b.Key[0] >= 'a' && b.Key[0] <= 'z' {
					// Lowercase letter: also register the uppercase keysym
					upper := string(b.Key[0] - 32) // 'a' → 'A'
					upperSym := xkb.SymFromName(upper, xkb.KeySymNoFlags)
					if upperSym != 0 {
						kbMap[resolvedBinding{sym: upperSym, mods: mods}] = action
					}
				}
			}
		}
	}

	s.keybindingMap = kbMap
	log.Printf("Keybindings loaded: %d bindings for %d actions (wmModifier=0x%x)\n", len(kbMap), len(merged), s.wmModifier)
	// Log launcher binding for debugging
	for rb, act := range kbMap {
		if act == wlipc.ActionShowLauncher {
			log.Printf("  Launcher binding: sym=0x%x mods=0x%x\n", rb.sym, rb.mods)
		}
	}
}
