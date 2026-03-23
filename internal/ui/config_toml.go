package ui

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"fyne.io/fyne/v2"
	"fyshos.com/fynedesk/wlipc"
)

// Config is the top-level TOML configuration for FyneDesk.
// Stored at ~/.config/fynedesk/config.toml.
type Config struct {
	Display     DisplayConfig               `toml:"display"`
	Clock       ClockConfig                 `toml:"clock"`
	Launcher    LauncherConfig              `toml:"launcher"`
	Panel       PanelConfig                 `toml:"panel"`
	Desktops    DesktopsConfig              `toml:"desktops"`
	Input       InputConfig                 `toml:"input"`
	NightLight  NightLightConfig            `toml:"night_light"`
	ScreenSaver ScreenSaverConfig           `toml:"screensaver"`
	Modules     ModulesConfig               `toml:"modules"`
	Theme       ThemeConfig                 `toml:"theme"`
	Windows     WindowConfig                `toml:"windows"`
	HotCorners  HotCornersConfig            `toml:"hot_corners"`
	Power       PowerConfig                 `toml:"power"`
	Keybindings map[string][]KeyBindingTOML `toml:"keybindings"`
	WindowRules []wlipc.WindowRule          `toml:"window_rules"`
}

// PowerConfig holds power management / idle timeout settings.
type PowerConfig struct {
	LockTimeoutMin    int    `toml:"lock_timeout_min"`    // Minutes of idle before locking (0 = never, default 5)
	BlankTimeoutMin   int    `toml:"blank_timeout_min"`   // Minutes of idle before blanking display (0 = never, default 6)
	SuspendTimeoutMin int    `toml:"suspend_timeout_min"` // Minutes of idle before auto-suspend (0 = never, default 0)
	SuspendAction     string `toml:"suspend_action"`      // "suspend", "hibernate", "hybrid-sleep", or "nothing" (default "suspend")
}

// WindowConfig holds window management settings.
type WindowConfig struct {
	InnerGap int `toml:"inner_gap"` // Pixel gap between adjacent windows (default 6)
	OuterGap int `toml:"outer_gap"` // Pixel gap between windows and screen edges (default 6)
}

// HotCornersConfig holds hot corner activation settings.
type HotCornersConfig struct {
	TopLeft     string `toml:"top_left"` // Action: "overview", "launcher", "show_desktop", or ""
	TopRight    string `toml:"top_right"`
	BottomLeft  string `toml:"bottom_left"`
	BottomRight string `toml:"bottom_right"`
}

// MonitorWallpaper holds per-monitor wallpaper settings.
// When set, this overrides the global Background/BackgroundType for the named output.
type MonitorWallpaper struct {
	Background     string `toml:"background"`
	BackgroundType string `toml:"background_type"`
}

// DisplayConfig holds display-related settings.
type DisplayConfig struct {
	Background           string                      `toml:"background"`
	BackgroundType       string                      `toml:"background_type"`
	IconTheme            string                      `toml:"icon_theme"`
	BorderButtonPosition string                      `toml:"border_button_position"`
	ColorScheme          string                      `toml:"color_scheme"`  // "auto" (default), "dark", "light"
	ReduceMotion         bool                        `toml:"reduce_motion"` // Disable all animations for accessibility
	HighContrast         bool                        `toml:"high_contrast"` // WCAG AA high contrast borders and colors
	Language             string                      `toml:"language"`      // Locale code: "en", "fr", etc.
	Monitors             map[string]MonitorWallpaper `toml:"monitors"`      // Per-output wallpaper overrides (key = output name, e.g. "eDP-1")
}

// ClockConfig holds clock display settings.
type ClockConfig struct {
	Format      string `toml:"format"`
	ShowSeconds bool   `toml:"show_seconds"`
}

// LauncherConfig holds launcher/dock settings.
type LauncherConfig struct {
	Icons          []string `toml:"icons"`
	IconSize       int      `toml:"icon_size"`
	DisableTaskbar bool     `toml:"disable_taskbar"`
	DisableZoom    bool     `toml:"disable_zoom"`
	ZoomScale      float64  `toml:"zoom_scale"`
	NarrowLeft     bool     `toml:"narrow_left"`
	BarPosition    string   `toml:"bar_position"` // "left" or "bottom"
}

// PanelConfig holds panel layout settings.
type PanelConfig struct {
	NarrowWidget bool `toml:"narrow_widget"`
}

// DesktopsConfig holds virtual desktop/workspace settings.
type DesktopsConfig struct {
	Count int      `toml:"count"` // Number of desktops (2-8, default 4)
	Names []string `toml:"names"` // Workspace names (empty = use "1", "2", ...)
}

// InputConfig holds input device settings.
type InputConfig struct {
	KeyboardModifier string   `toml:"keyboard_modifier"`
	NaturalScroll    bool     `toml:"natural_scroll"`
	KeyboardLayouts  []string `toml:"keyboard_layouts"`
}

// NightLightConfig holds night light (blue light filter) settings.
type NightLightConfig struct {
	Enabled     bool `toml:"enabled"`
	Temperature int  `toml:"temperature"` // Color temperature in Kelvin (2700-6500, default 4500)
}

// ScreenSaverConfig holds screen saver settings.
type ScreenSaverConfig struct {
	Type      string `toml:"type"`
	ShowClock bool   `toml:"show_clock"`
	Label     string `toml:"label"`
}

// ModulesConfig holds enabled module names.
type ModulesConfig struct {
	Enabled []string `toml:"enabled"`
}

// ThemeConfig holds the active desktop theme.
type ThemeConfig struct {
	Name            string            `toml:"name"`              // Active theme name ("default", "neon", "matrix", or custom)
	Colors          map[string]string `toml:"colors"`            // Custom color overrides (hex strings, e.g. "#ff00ff")
	AutoAccentColor bool              `toml:"auto_accent_color"` // Extract accent color from wallpaper
	FontFamily      string            `toml:"font_family"`       // System font family (resolved via fc-match, default "sans-serif")
	FontSize        int               `toml:"font_size"`         // Title bar font size in points (default 13)
}

// KeyBindingTOML is a single keybinding entry in TOML format.
type KeyBindingTOML struct {
	Key  string   `toml:"key"`
	Mods []string `toml:"mods"`
}

// configPath returns the path to config.toml.
func configPath() string {
	home := os.Getenv("HOME")
	return filepath.Join(home, ".config", "fynedesk", "config.toml")
}

// configDir returns the config directory, creating it if needed.
func configDir() string {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".config", "fynedesk")
	os.MkdirAll(dir, 0755)
	return dir
}

// loadConfig reads config.toml if it exists, otherwise migrates from Fyne prefs.
// Returns the loaded config and whether migration occurred.
func loadConfig() (*Config, bool) {
	path := configPath()
	cfg := defaultConfig()

	if _, err := os.Stat(path); err == nil {
		// TOML file exists — load it
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			log.Printf("Warning: error reading %s: %v (using defaults)\n", path, err)
		}
		return cfg, false
	}

	// No TOML file — migrate from Fyne preferences
	migrated := migrateFromFynePrefs(cfg)
	if migrated {
		if err := saveConfig(cfg); err != nil {
			log.Printf("Warning: could not write migrated config: %v\n", err)
		} else {
			log.Printf("Migrated Fyne preferences to %s\n", path)
		}
	}
	return cfg, migrated
}

// saveConfig writes the config to config.toml atomically.
func saveConfig(cfg *Config) error {
	configDir()
	path := configPath()
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(cfg); err != nil {
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

// defaultConfig returns a Config with sensible defaults.
func defaultConfig() *Config {
	return &Config{
		Display: DisplayConfig{
			BackgroundType:       "image",
			IconTheme:            "hicolor",
			BorderButtonPosition: "Left",
		},
		Clock: ClockConfig{
			Format:      "12h",
			ShowSeconds: false,
		},
		Launcher: LauncherConfig{
			IconSize:   48,
			ZoomScale:  2.0,
			NarrowLeft: true,
		},
		Panel: PanelConfig{
			NarrowWidget: false,
		},
		Desktops: DesktopsConfig{
			Count: 4,
		},
		Input: InputConfig{
			KeyboardModifier: "Super",
			NaturalScroll:    false,
			KeyboardLayouts:  nil,
		},
		NightLight: NightLightConfig{
			Enabled:     false,
			Temperature: 4500,
		},
		ScreenSaver: ScreenSaverConfig{
			Type:      "FyshOS",
			ShowClock: true,
			Label:     "FyneDesk",
		},
		Windows: WindowConfig{
			InnerGap: 6,
			OuterGap: 6,
		},
		HotCorners: HotCornersConfig{
			TopLeft: "overview",
		},
		Power: PowerConfig{
			LockTimeoutMin:    5,
			BlankTimeoutMin:   6,
			SuspendTimeoutMin: 0,
			SuspendAction:     "suspend",
		},
	}
}

// migrateFromFynePrefs reads Fyne preferences and populates the Config.
// Returns true if any preferences were found.
func migrateFromFynePrefs(cfg *Config) bool {
	p := fyne.CurrentApp().Preferences()

	found := false

	// Display
	if bg := p.String("background"); bg != "" {
		cfg.Display.Background = bg
		found = true
	}
	if bt := p.String("background_type"); bt != "" {
		cfg.Display.BackgroundType = bt
		found = true
	}
	if it := p.String("icontheme"); it != "" {
		cfg.Display.IconTheme = it
		found = true
	}
	if bp := p.String("borderbuttonposition"); bp != "" {
		cfg.Display.BorderButtonPosition = bp
		found = true
	}

	// Clock
	if cf := p.String("clockformatting"); cf != "" {
		cfg.Clock.Format = cf
		found = true
	}
	if p.Bool("clockshowseconds") {
		cfg.Clock.ShowSeconds = true
		found = true
	}

	// Launcher
	if icons := p.String("launchericons"); icons != "" {
		cfg.Launcher.Icons = strings.Split(icons, "|")
		found = true
	}
	if size := p.Int("launchericonsize"); size > 0 {
		cfg.Launcher.IconSize = size
		found = true
	}
	if p.Bool("launcherdisabletaskbar") {
		cfg.Launcher.DisableTaskbar = true
		found = true
	}
	if p.Bool("launcherdisablezoom") {
		cfg.Launcher.DisableZoom = true
		found = true
	}
	if zs := p.Float("launcherzoomscale"); zs > 0 {
		cfg.Launcher.ZoomScale = zs
		found = true
	}
	// launchernarrowleft defaults to true, only override if explicitly set to false
	cfg.Launcher.NarrowLeft = true
	if v := p.String("launchernarrowleft"); v == "false" {
		cfg.Launcher.NarrowLeft = false
		found = true
	} else if v != "" {
		found = true
	}

	if v := p.String("barposition"); v != "" {
		cfg.Launcher.BarPosition = v
		found = true
	}

	// Panel
	if p.Bool("narrowpanel") {
		cfg.Panel.NarrowWidget = true
		found = true
	}

	// Input
	modVal := p.IntWithFallback("keyboardmodifier", int(fyne.KeyModifierSuper))
	if modVal == int(fyne.KeyModifierAlt) {
		cfg.Input.KeyboardModifier = "Alt"
	} else {
		cfg.Input.KeyboardModifier = "Super"
	}
	if p.Bool("naturalscroll") {
		cfg.Input.NaturalScroll = true
		found = true
	}
	if kbl := p.String("keyboardlayouts"); kbl != "" {
		cfg.Input.KeyboardLayouts = strings.Split(kbl, "|")
		found = true
	}

	// ScreenSaver
	if st := p.String("savertype"); st != "" {
		cfg.ScreenSaver.Type = st
		found = true
	}
	if sc := p.String("saverclock"); sc != "" {
		cfg.ScreenSaver.ShowClock = p.BoolWithFallback("saverclock", true)
		found = true
	}
	if sl := p.String("saverlabel"); sl != "" {
		cfg.ScreenSaver.Label = sl
		found = true
	}

	// Modules
	if mods := p.String("modulenames"); mods != "" {
		cfg.Modules.Enabled = strings.Split(mods, "|")
		found = true
	}

	// Keybindings (stored as JSON string in Fyne prefs)
	if kb := p.String("keybindings"); kb != "" {
		bindings, err := wlipc.ParseBindingsJSON(kb)
		if err == nil && len(bindings) > 0 {
			cfg.Keybindings = actionBindingsToTOML(bindings)
			found = true
		}
	}

	return found
}

// syncToFynePrefs writes key settings back to Fyne preferences (cache layer).
// This keeps backward compatibility with the compositor reading Fyne prefs.
func syncToFynePrefs(cfg *Config) {
	p := fyne.CurrentApp().Preferences()

	p.SetString("background", cfg.Display.Background)
	p.SetString("background_type", cfg.Display.BackgroundType)

	// Per-monitor wallpapers: serialize as JSON for compositor
	if len(cfg.Display.Monitors) > 0 {
		monData, err := json.Marshal(cfg.Display.Monitors)
		if err == nil {
			p.SetString("monitor_wallpapers", string(monData))
		}
	} else {
		p.SetString("monitor_wallpapers", "")
	}
	p.SetString("icontheme", cfg.Display.IconTheme)
	p.SetString("borderbuttonposition", cfg.Display.BorderButtonPosition)
	p.SetString("colorscheme", cfg.Display.ColorScheme)
	p.SetBool("reducemotion", cfg.Display.ReduceMotion)

	p.SetString("clockformatting", cfg.Clock.Format)
	p.SetBool("clockshowseconds", cfg.Clock.ShowSeconds)

	p.SetString("launchericons", strings.Join(cfg.Launcher.Icons, "|"))
	p.SetInt("launchericonsize", cfg.Launcher.IconSize)
	p.SetBool("launcherdisabletaskbar", cfg.Launcher.DisableTaskbar)
	p.SetBool("launcherdisablezoom", cfg.Launcher.DisableZoom)
	p.SetFloat("launcherzoomscale", cfg.Launcher.ZoomScale)
	p.SetBool("launchernarrowleft", cfg.Launcher.NarrowLeft)
	if cfg.Launcher.BarPosition != "" {
		p.SetString("barposition", cfg.Launcher.BarPosition)
	}

	p.SetBool("narrowpanel", cfg.Panel.NarrowWidget)

	if cfg.Desktops.Count >= 2 {
		p.SetInt("desktopcount", cfg.Desktops.Count)
	}
	p.SetString("desktopnames", strings.Join(cfg.Desktops.Names, "|"))

	var modInt int
	if cfg.Input.KeyboardModifier == "Alt" {
		modInt = int(fyne.KeyModifierAlt)
	} else {
		modInt = int(fyne.KeyModifierSuper)
	}
	p.SetInt("keyboardmodifier", modInt)
	p.SetBool("naturalscroll", cfg.Input.NaturalScroll)
	p.SetString("keyboardlayouts", strings.Join(cfg.Input.KeyboardLayouts, "|"))

	p.SetBool("nightlightenabled", cfg.NightLight.Enabled)
	p.SetInt("nightlighttemperature", cfg.NightLight.Temperature)

	p.SetString("savertype", cfg.ScreenSaver.Type)
	p.SetBool("saverclock", cfg.ScreenSaver.ShowClock)
	p.SetString("saverlabel", cfg.ScreenSaver.Label)

	p.SetString("modulenames", strings.Join(cfg.Modules.Enabled, "|"))
	p.SetBool("autoaccentcolor", cfg.Theme.AutoAccentColor)
	if cfg.Theme.FontFamily != "" {
		p.SetString("fontfamily", cfg.Theme.FontFamily)
	}
	if cfg.Theme.FontSize > 0 {
		p.SetInt("fontsize", cfg.Theme.FontSize)
	}

	// Keybindings: convert TOML format back to JSON for backward compat
	if len(cfg.Keybindings) > 0 {
		bindings := tomlToActionBindings(cfg.Keybindings)
		jsonStr, err := wlipc.BindingsToJSON(bindings)
		if err == nil {
			p.SetString("keybindings", jsonStr)
		}
	} else {
		p.SetString("keybindings", "")
	}

	// Window gaps
	p.SetInt("windowinnergap", cfg.Windows.InnerGap)
	p.SetInt("windowoutergap", cfg.Windows.OuterGap)

	// Hot corners
	p.SetString("hotcorner_topleft", cfg.HotCorners.TopLeft)
	p.SetString("hotcorner_topright", cfg.HotCorners.TopRight)
	p.SetString("hotcorner_bottomleft", cfg.HotCorners.BottomLeft)
	p.SetString("hotcorner_bottomright", cfg.HotCorners.BottomRight)

	// Power management
	p.SetInt("power_lock_timeout", cfg.Power.LockTimeoutMin)
	p.SetInt("power_blank_timeout", cfg.Power.BlankTimeoutMin)
	p.SetInt("power_suspend_timeout", cfg.Power.SuspendTimeoutMin)
	p.SetString("power_suspend_action", cfg.Power.SuspendAction)

	// Window rules: store as JSON for compositor
	if len(cfg.WindowRules) > 0 {
		data, err := json.Marshal(cfg.WindowRules)
		if err == nil {
			p.SetString("windowrules", string(data))
		}
	} else {
		p.SetString("windowrules", "")
	}
}

// actionBindingsToTOML converts wlipc.ActionBindings to TOML-friendly map.
func actionBindingsToTOML(bindings wlipc.ActionBindings) map[string][]KeyBindingTOML {
	result := make(map[string][]KeyBindingTOML)
	for action, bs := range bindings {
		var entries []KeyBindingTOML
		for _, b := range bs {
			entries = append(entries, KeyBindingTOML{
				Key:  b.Key,
				Mods: b.Mods,
			})
		}
		result[action] = entries
	}
	return result
}

// tomlToActionBindings converts TOML keybindings back to wlipc.ActionBindings.
func tomlToActionBindings(bindings map[string][]KeyBindingTOML) wlipc.ActionBindings {
	result := make(wlipc.ActionBindings)
	for action, bs := range bindings {
		var entries []wlipc.KeyBinding
		for _, b := range bs {
			entries = append(entries, wlipc.KeyBinding{
				Key:  b.Key,
				Mods: b.Mods,
			})
		}
		result[action] = entries
	}
	return result
}

// LoadConfigForCompositor reads config.toml and returns settings as a flat map
// compatible with the compositor's applyPrefs(). Falls back to Fyne prefs JSON
// if config.toml doesn't exist.
func LoadConfigForCompositor() (map[string]any, error) {
	path := configPath()
	if _, err := os.Stat(path); err != nil {
		return nil, err // caller should fall back to Fyne prefs
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}

	// Build the flat prefs map the compositor expects
	prefs := make(map[string]any)
	prefs["background"] = cfg.Display.Background
	prefs["background_type"] = cfg.Display.BackgroundType
	prefs["borderbuttonposition"] = cfg.Display.BorderButtonPosition
	prefs["colorscheme"] = cfg.Display.ColorScheme

	// Per-monitor wallpapers
	if len(cfg.Display.Monitors) > 0 {
		monData, err := json.Marshal(cfg.Display.Monitors)
		if err == nil {
			prefs["monitor_wallpapers"] = string(monData)
		}
	}

	var modVal float64
	if cfg.Input.KeyboardModifier == "Alt" {
		modVal = float64(fyne.KeyModifierAlt)
	} else {
		modVal = float64(fyne.KeyModifierSuper)
	}
	prefs["keyboardmodifier"] = modVal
	prefs["naturalscroll"] = cfg.Input.NaturalScroll
	prefs["narrowpanel"] = cfg.Panel.NarrowWidget
	prefs["launchernarrowleft"] = cfg.Launcher.NarrowLeft
	if cfg.Desktops.Count >= 2 {
		prefs["desktopcount"] = float64(cfg.Desktops.Count)
	}
	prefs["desktopnames"] = strings.Join(cfg.Desktops.Names, "|")
	prefs["barposition"] = cfg.Launcher.BarPosition
	prefs["keyboardlayouts"] = strings.Join(cfg.Input.KeyboardLayouts, "|")

	// Keybindings: convert TOML back to JSON string for compositor
	if len(cfg.Keybindings) > 0 {
		bindings := tomlToActionBindings(cfg.Keybindings)
		jsonStr, err := wlipc.BindingsToJSON(bindings)
		if err == nil {
			prefs["keybindings"] = jsonStr
		}
	}

	// Night light
	prefs["nightlightenabled"] = cfg.NightLight.Enabled
	prefs["nightlighttemperature"] = float64(cfg.NightLight.Temperature)

	// Theme
	prefs["theme_name"] = cfg.Theme.Name
	prefs["autoaccentcolor"] = cfg.Theme.AutoAccentColor
	prefs["fontfamily"] = cfg.Theme.FontFamily
	prefs["fontsize"] = float64(cfg.Theme.FontSize)
	if len(cfg.Theme.Colors) > 0 {
		colorParts := make([]string, 0, len(cfg.Theme.Colors))
		for k, v := range cfg.Theme.Colors {
			colorParts = append(colorParts, k+"="+v)
		}
		prefs["theme_colors"] = strings.Join(colorParts, "|")
	}

	// Window gaps
	prefs["windowinnergap"] = float64(cfg.Windows.InnerGap)
	prefs["windowoutergap"] = float64(cfg.Windows.OuterGap)

	// Hot corners
	prefs["hotcorner_topleft"] = cfg.HotCorners.TopLeft
	prefs["hotcorner_topright"] = cfg.HotCorners.TopRight
	prefs["hotcorner_bottomleft"] = cfg.HotCorners.BottomLeft
	prefs["hotcorner_bottomright"] = cfg.HotCorners.BottomRight

	// Power management
	prefs["power_lock_timeout"] = float64(cfg.Power.LockTimeoutMin)
	prefs["power_blank_timeout"] = float64(cfg.Power.BlankTimeoutMin)
	prefs["power_suspend_timeout"] = float64(cfg.Power.SuspendTimeoutMin)
	prefs["power_suspend_action"] = cfg.Power.SuspendAction

	// Window rules: pass as JSON string for compositor
	if len(cfg.WindowRules) > 0 {
		data, err := json.Marshal(cfg.WindowRules)
		if err == nil {
			prefs["windowrules"] = string(data)
		}
	}

	return prefs, nil
}
