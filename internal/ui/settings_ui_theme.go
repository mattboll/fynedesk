package ui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

//go:embed "themes/*"
var bundledThemes embed.FS

func (d *settingsUI) loadThemeScreen() fyne.CanvasObject {
	var themeList []string

	embedList, _ := bundledThemes.ReadDir("themes")
	for _, dir := range embedList {
		themeList = append(themeList, dir.Name())
	}

	// Also scan user themes in ~/.config/fynedesk/themes/
	userThemesDir := filepath.Join(configDir(), "themes")
	os.MkdirAll(userThemesDir, 0755)
	if entries, err := os.ReadDir(userThemesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				// Avoid duplicates with bundled themes
				dup := false
				for _, t := range themeList {
					if t == e.Name() {
						dup = true
						break
					}
				}
				if !dup {
					themeList = append(themeList, e.Name())
				}
			}
		}
	}

	storageRoot := fyne.CurrentApp().Storage().RootURI()

	// Track the currently active theme name
	activeTheme := d.settings.cfg.Theme.Name
	if activeTheme == "" {
		activeTheme = "default"
	}
	activeLabel := widget.NewLabel(fmt.Sprintf(locale.T("theme.active"), cases.Title(language.Make("en")).String(activeTheme)))
	activeLabel.TextStyle = fyne.TextStyle{Bold: true}

	useTheme := func(name string) {
		dest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")
		out, err := os.Create(dest)
		if err != nil {
			fyne.LogError("Failed to create theme file", err)
			return
		}
		defer out.Close()
		var in io.ReadCloser
		if builtin, err := bundledThemes.Open(filepath.Join("themes/", name, "theme.json")); err == nil {
			in = builtin
		} else {
			source := filepath.Join(userThemesDir, name, "theme.json")
			opened, err := os.Open(source)
			if err != nil {
				fyne.LogError("Failed to open theme source", err)
			} else {
				in = opened
			}
		}
		if in != nil {
			if _, err := io.Copy(out, in); err != nil {
				fyne.LogError("Failed to copy theme data", err)
			}
			in.Close()
		}

		// Extract fynedesk titlebar colors from the theme for compositor propagation
		d.settings.cfg.Theme.Name = name
		d.settings.cfg.Theme.Colors = extractTitlebarColors(dest)
		d.settings.saveTOML()
		d.settings.apply()
		reloadFyneTheme()

		activeLabel.SetText(fmt.Sprintf(locale.T("theme.active"), cases.Title(language.Make("en")).String(name)))
	}

	// Theme gallery list
	themeGallery := widget.NewList(
		func() int {
			return len(themeList)
		},
		func() fyne.CanvasObject {
			install := widget.NewButtonWithIcon(locale.T("theme.apply"), theme.ComputerIcon(), nil)
			preview := &canvas.Image{FillMode: canvas.ImageFillContain}
			preview.SetMinSize(fyne.NewSize(120, 68))
			return container.NewBorder(nil, nil, nil, preview,
				container.NewBorder(nil, install, nil, nil,
					widget.NewRichTextFromMarkdown("## Theme Name")))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			outer := o.(*fyne.Container)
			inner := outer.Objects[0].(*fyne.Container)
			b := inner.Objects[1].(*widget.Button)
			name := themeList[id]
			b.OnTapped = func() {
				useTheme(name)
			}

			p := outer.Objects[1].(*canvas.Image)
			if builtin, err := bundledThemes.Open(filepath.Join("themes/", name, "preview.png")); err == nil {
				data, _ := io.ReadAll(builtin)
				p.Resource = fyne.NewStaticResource(name+"/preview.png", data)
				p.File = ""
				_ = builtin.Close()
			} else {
				source := filepath.Join(userThemesDir, name, "preview.png")
				p.File = source
				p.Resource = nil
			}
			p.Refresh()

			l := inner.Objects[0].(*widget.RichText)
			title := cases.Title(language.Make("en")).String(name)
			l.ParseMarkdown(fmt.Sprintf("## %s", title))
		})

	// --- Color customization section ---
	type colorEntry struct {
		label string
		key   string
	}
	colorDefs := []colorEntry{
		// Standard Fyne colors
		{locale.T("theme.primary"), "primary"},
		{locale.T("theme.background"), "background"},
		{locale.T("theme.foreground"), "foreground"},
		{locale.T("theme.button"), "button"},
		{locale.T("theme.inputBg"), "inputBackground"},
		{locale.T("theme.inputBorder"), "inputBorder"},
		{locale.T("theme.pressed"), "pressed"},
		{locale.T("theme.scrollBar"), "scrollBar"},
		{locale.T("theme.shadow"), "shadow"},
		{locale.T("theme.hyperlink"), "hyperlink"},
		{locale.T("theme.success"), "success"},
		{locale.T("theme.warning"), "warning"},
		{locale.T("theme.error"), "error"},
		{locale.T("theme.disabledBtn"), "disabledButton"},
		// FyneDesk-specific colors
		{locale.T("theme.notifBg"), string(wmtheme.ColorNameToastBackground)},
		{locale.T("theme.notifTitle"), string(wmtheme.ColorNameToastTitle)},
		{locale.T("theme.notifText"), string(wmtheme.ColorNameToastBody)},
		{locale.T("theme.highlight"), string(wmtheme.ColorNameAccentGlow)},
		{locale.T("theme.sidebarBg"), string(wmtheme.ColorNameSidebarBackground)},
		{locale.T("theme.categoryHeader"), string(wmtheme.ColorNameSectionLabel)},
		{locale.T("theme.titlebarActive"), string(wmtheme.ColorNameTitlebarActive)},
		{locale.T("theme.titlebarInactive"), string(wmtheme.ColorNameTitlebarInactive)},
		{locale.T("theme.titlebarText"), string(wmtheme.ColorNameTitlebarText)},
		{locale.T("theme.counterBadge"), string(wmtheme.ColorNameBadge)},
	}

	// Load colors from active theme as defaults, then overlay custom overrides
	themeFile := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")
	themeColors := readThemeColors(themeFile)
	customColors := make(map[string]string)
	for k, v := range d.settings.cfg.Theme.Colors {
		customColors[k] = v
	}

	formItems := make([]*widget.FormItem, 0, len(colorDefs))
	for _, cd := range colorDefs {
		cd := cd
		preview := canvas.NewRectangle(color.Transparent)
		preview.SetMinSize(fyne.NewSize(20, 20))
		preview.CornerRadius = 3

		entry := widget.NewEntry()
		entry.SetPlaceHolder("#rrggbbaa")
		if v, ok := customColors[cd.key]; ok {
			entry.SetText(v)
			if c := parseSettingsHexColor(v); c != nil {
				preview.FillColor = c
				preview.Refresh()
			}
		} else if v, ok := themeColors[cd.key]; ok {
			// Pre-fill from active theme (shown as placeholder, not saved until edited)
			entry.SetPlaceHolder(v)
			if c := parseSettingsHexColor(v); c != nil {
				preview.FillColor = c
				preview.Refresh()
			}
		}
		entry.OnChanged = func(s string) {
			if s == "" {
				delete(customColors, cd.key)
				preview.FillColor = color.Transparent
			} else {
				customColors[cd.key] = s
				if c := parseSettingsHexColor(s); c != nil {
					preview.FillColor = c
				}
			}
			preview.Refresh()
		}

		row := container.NewBorder(nil, nil, nil, preview, entry)
		formItems = append(formItems, widget.NewFormItem(cd.label, row))
	}
	colorForm := widget.NewForm(formItems...)

	applyColors := widget.NewButton(locale.T("theme.applyColors"), func() {
		// Merge custom colors into the active theme.json
		d.mergeCustomColors(customColors)

		// Re-extract titlebar colors (including any custom overrides) for compositor
		dest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")
		merged := extractTitlebarColors(dest)
		// Overlay custom colors onto titlebar colors
		for k, v := range customColors {
			if merged == nil {
				merged = make(map[string]string)
			}
			merged[k] = v
		}
		if len(merged) == 0 {
			d.settings.cfg.Theme.Colors = nil
		} else {
			d.settings.cfg.Theme.Colors = merged
		}

		d.settings.saveTOML()
		d.settings.apply()
		reloadFyneTheme()
	})
	applyColors.Importance = widget.HighImportance

	// Export as TOML theme button
	exportBtn := widget.NewButton(locale.T("theme.exportTheme"), func() {
		d.exportThemeDialog()
	})

	// Auto accent color from wallpaper toggle
	autoAccent := widget.NewCheck(locale.T("theme.autoAccent"), func(checked bool) {
		d.settings.cfg.Theme.AutoAccentColor = checked
		d.settings.saveTOML()
		d.settings.apply()
	})
	autoAccent.Checked = d.settings.cfg.Theme.AutoAccentColor

	colorSection := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle(locale.T("theme.colorOverrides"), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			autoAccent,
		),
		container.NewHBox(layout.NewSpacer(), applyColors, exportBtn),
		nil, nil,
		container.NewVScroll(colorForm),
	)

	// Layout: top = active theme label, left = gallery, right = color customization
	header := container.NewHBox(activeLabel, layout.NewSpacer())
	split := container.NewHSplit(themeGallery, colorSection)
	split.Offset = 0.4
	return container.NewBorder(header, nil, nil, nil, split)
}

// reloadFyneTheme reads the active theme.json and applies it to the running Fyne app.
// It also syncs the Fyne PrimaryColor setting into theme.json so the JSON theme
// reflects the user's "Main Color" selection.
func reloadFyneTheme() {
	storageRoot := fyne.CurrentApp().Storage().RootURI()
	dest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")

	// Sync Fyne's PrimaryColor into theme.json before loading,
	// BUT skip if auto accent color is active (wallpaper accent takes priority).
	cfg, _ := loadConfig()
	if cfg == nil || !cfg.Theme.AutoAccentColor {
		primary := fyne.CurrentApp().Settings().PrimaryColor()
		if hex, ok := fynePrimaryColorHex[primary]; ok {
			syncPrimaryColors(dest, hex)
		}
	}

	applyThemeFromJSON(dest)
}

// applyThemeFromJSON loads theme.json and applies it without syncing PrimaryColor first.
func applyThemeFromJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	th, err := theme.FromJSONReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	fyne.CurrentApp().Settings().SetTheme(th)
}

// watchFynePrimaryColor listens for Fyne settings changes and syncs the
// "Main Color" selection (from Fyne Settings -> Appearance) into theme.json.
// Without this, the jsonTheme always returns the hardcoded "primary" value
// from theme.json, ignoring the user's PrimaryColor() preference.
// fynePrimaryColorHex maps Fyne named primary colors to their hex values.
// These match the values from fyne.io/fyne/v2/internal/theme.PrimaryColorNamed().
var fynePrimaryColorHex = map[string]string{
	"blue":   "#2196f3",
	"red":    "#f44336",
	"orange": "#ff9800",
	"yellow": "#ffeb3b",
	"green":  "#4caf50",
	"purple": "#9c27b0",
	"brown":  "#795548",
	"gray":   "#9e9e9e",
}

// watchFynePrimaryColor polls ~/.config/fyne/settings.json for Main Color changes.
// We cannot use app.Settings().AddListener() because Fyne disables its file watcher
// when a custom theme is set via SetTheme() (themeSpecified=true).
func watchFynePrimaryColor(app fyne.App) {
	// Resolve settings.json path: ~/.config/fyne/settings.json
	// App storage root is ~/.config/fyne/<appID>/, parent is ~/.config/fyne/
	storageRoot := app.Storage().RootURI()
	settingsPath := filepath.Join(filepath.Dir(storageRoot.Path()), "settings.json")
	themeDest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")

	lastPrimary := app.Settings().PrimaryColor()

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			data, err := os.ReadFile(settingsPath)
			if err != nil {
				continue
			}
			var schema struct {
				PrimaryColor string `json:"primary_color"`
			}
			if err := json.Unmarshal(data, &schema); err != nil {
				continue
			}
			if schema.PrimaryColor == "" || schema.PrimaryColor == lastPrimary {
				continue
			}
			lastPrimary = schema.PrimaryColor

			fyne.Do(func() {
				// Disable auto accent color when user explicitly picks a color
				if cfg, _ := loadConfig(); cfg != nil && cfg.Theme.AutoAccentColor {
					cfg.Theme.AutoAccentColor = false
					_ = saveConfig(cfg)
					fyne.CurrentApp().Preferences().SetBool("autoaccentcolor", false)
				}

				hex, ok := fynePrimaryColorHex[lastPrimary]
				if !ok {
					hex = fynePrimaryColorHex["blue"]
				}

				syncPrimaryColors(themeDest, hex)
				applyThemeFromJSON(themeDest)

				// Notify compositor to re-read theme colors for decorations
				if wlipc.IsWaylandSession() {
					_ = wlipc.NotifySettingsChanged(nil)
				}
			})
		}
	}()
}

// syncPrimaryColors updates the primary color and all accent-derived colors
// in theme.json so the entire UI reflects the selected Main Color.
func syncPrimaryColors(path, hex string) {
	updateThemeJSONColor(path, "primary", hex)
	updateThemeJSONColor(path, "pressed", hex)
	updateThemeJSONColor(path, "scrollBar", hex)
	updateThemeJSONColor(path, "fynedeskToastTitle", hex)
	updateThemeJSONColor(path, "fynedeskAccentGlow", hex+"dc")
	updateThemeJSONColor(path, "fynedeskSidebarSeparator", hex+"3c")
	updateThemeJSONColor(path, "fynedeskSectionLabel", hex)
	updateThemeJSONColor(path, "fynedeskBadge", hex)
}

// updateThemeJSONColor updates a single color entry in theme.json.
func updateThemeJSONColor(path, colorName, hexValue string) {
	var themeData map[string]any
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &themeData)
	}
	if themeData == nil {
		themeData = map[string]any{}
	}
	colorsMap, _ := themeData["Colors"].(map[string]any)
	if colorsMap == nil {
		colorsMap = map[string]any{}
	}
	colorsMap[colorName] = hexValue
	themeData["Colors"] = colorsMap
	data, _ := json.MarshalIndent(themeData, "", "\t")
	_ = os.WriteFile(path, data, 0644)
}

// watchAccentColor watches for accent color IPC from the compositor and
// applies it to the theme when auto_accent_color is enabled.
func watchAccentColor(done <-chan struct{}) {
	wlipc.WatchAccentColor(func(hex string) {
		// Check if auto accent color is enabled in TOML config
		cfg, _ := loadConfig()
		if cfg == nil || !cfg.Theme.AutoAccentColor {
			return
		}

		storageRoot := fyne.CurrentApp().Storage().RootURI()
		dest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")
		syncPrimaryColors(dest, hex)
		reloadFyneTheme()
		log.Printf("[ACCENT] Applied accent color from wallpaper: %s\n", hex)
	}, done)
}

// extractTitlebarColors reads a theme.json file and returns fynedesk titlebar
// color entries so they can be propagated to the compositor via settings.
func extractTitlebarColors(path string) map[string]string {
	all := readThemeColors(path)
	if all == nil {
		return nil
	}

	titlebarKeys := []string{
		"fynedeskTitlebarActive",
		"fynedeskTitlebarInactive",
		"fynedeskTitlebarText",
	}
	result := make(map[string]string)
	for _, key := range titlebarKeys {
		if v, ok := all[key]; ok {
			result[key] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// readThemeColors reads all colors from a theme.json file.
func readThemeColors(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var themeData struct {
		Colors map[string]string `json:"Colors"`
	}
	if err := json.Unmarshal(data, &themeData); err != nil || themeData.Colors == nil {
		return nil
	}
	return themeData.Colors
}

// parseSettingsHexColor parses a hex color string (#RGB, #RRGGBB, or #RRGGBBAA).
func parseSettingsHexColor(hex string) color.Color {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b, a uint8
	switch len(hex) {
	case 3:
		fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
		r, g, b, a = r*17, g*17, b*17, 255
	case 6:
		fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		a = 255
	case 8:
		fmt.Sscanf(hex, "%02x%02x%02x%02x", &r, &g, &b, &a)
	default:
		return nil
	}
	return &color.NRGBA{R: r, G: g, B: b, A: a}
}

// mergeCustomColors reads the active theme.json and overlays custom color values.
func (d *settingsUI) mergeCustomColors(colors map[string]string) {
	storageRoot := fyne.CurrentApp().Storage().RootURI()
	dest := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")

	// Read existing theme
	var themeData map[string]any
	if data, err := os.ReadFile(dest); err == nil {
		_ = json.Unmarshal(data, &themeData)
	}
	if themeData == nil {
		themeData = map[string]any{}
	}

	colorsMap, _ := themeData["Colors"].(map[string]any)
	if colorsMap == nil {
		colorsMap = map[string]any{}
	}

	for k, v := range colors {
		colorsMap[k] = v
	}
	themeData["Colors"] = colorsMap

	data, _ := json.MarshalIndent(themeData, "", "\t")
	_ = os.WriteFile(dest, data, 0644)
}

// exportThemeDialog shows a dialog to save the current theme as a shareable TOML file.
func (d *settingsUI) exportThemeDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("my-theme")

	dialog.ShowForm(locale.T("theme.exportTheme"), locale.T("theme.export"), locale.T("theme.cancel"),
		[]*widget.FormItem{
			widget.NewFormItem(locale.T("theme.themeName"), nameEntry),
		},
		func(ok bool) {
			if !ok || nameEntry.Text == "" {
				return
			}
			d.exportTheme(nameEntry.Text)
		}, d.win)
}

// exportTheme saves the current theme to ~/.config/fynedesk/themes/<name>/
func (d *settingsUI) exportTheme(name string) {
	dir := filepath.Join(configDir(), "themes", name)
	os.MkdirAll(dir, 0755)

	// Read current active theme.json
	storageRoot := fyne.CurrentApp().Storage().RootURI()
	src := filepath.Join(filepath.Dir(storageRoot.Path()), "theme.json")
	data, err := os.ReadFile(src)
	if err != nil {
		fyne.LogError("Could not read active theme", err)
		return
	}

	// Write to the theme directory
	dest := filepath.Join(dir, "theme.json")
	if err := os.WriteFile(dest, data, 0644); err != nil {
		fyne.LogError("Could not export theme", err)
		return
	}

	// Update config to point to this theme
	d.settings.cfg.Theme.Name = name
	d.settings.saveTOML()
}
