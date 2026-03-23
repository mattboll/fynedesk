package wlipc

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeyboardLayout represents a single XKB layout+variant pair
type KeyboardLayout struct {
	Layout      string `json:"layout"`       // e.g. "fr", "us"
	Variant     string `json:"variant"`      // e.g. "bepo", "dvorak", ""
	DisplayName string `json:"display_name"` // e.g. "French (BEPO)"
}

// ShortName returns a short display label like "FR" or "FR-BEPO"
func (k KeyboardLayout) ShortName() string {
	short := strings.ToUpper(k.Layout)
	if k.Variant != "" {
		short += "-" + strings.ToUpper(k.Variant)
	}
	return short
}

// KeyboardLayoutState is written by compositor with current active layout
type KeyboardLayoutState struct {
	ActiveIndex int              `json:"active_index"`
	Layouts     []KeyboardLayout `json:"layouts"`
	Timestamp   int64            `json:"timestamp"`
}

// KeyboardLayoutRequest is written by panel to switch layout
type KeyboardLayoutRequest struct {
	Index     int   `json:"index"`
	Timestamp int64 `json:"timestamp"`
}

// RequestKeyboardLayout writes a keyboard layout switch request for the compositor
func RequestKeyboardLayout(index int) error {
	req := KeyboardLayoutRequest{Index: index, Timestamp: time.Now().UnixMilli()}
	if trySendRequest(ReqKeyboardLayout, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "keyboard-layout-request.json"), data)
}

// GetKeyboardLayoutState reads the current keyboard layout state from compositor
func GetKeyboardLayoutState() (*KeyboardLayoutState, error) {
	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "keyboard-layout-state.json")

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var state KeyboardLayoutState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// WatchKeyboardLayoutState watches for keyboard layout state changes and calls callback.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchKeyboardLayoutState(callback func(state *KeyboardLayoutState), done <-chan struct{}) {
	if trySocketWatch(EventKeyboardLayout, func(data json.RawMessage) {
		var state KeyboardLayoutState
		if err := json.Unmarshal(data, &state); err == nil {
			callback(&state)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "keyboard-layout-state.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(statePath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					if state, err := GetKeyboardLayoutState(); err == nil {
						callback(state)
					}
				}
			}
		}
	}()
}

// NotifyKeyboardLayoutState writes the current keyboard layout state for the panel
func NotifyKeyboardLayoutState(state KeyboardLayoutState) error {
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)

	state.Timestamp = time.Now().UnixMilli()
	broadcastIfServer(EventKeyboardLayout, state)

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	return atomicWriteFile(filepath.Join(configDir, "keyboard-layout-state.json"), data)
}

// XKBLayoutInfo represents a layout parsed from evdev.xml
type XKBLayoutInfo struct {
	Name        string           // e.g. "fr"
	Description string           // e.g. "French"
	Variants    []XKBVariantInfo // available variants
}

// XKBVariantInfo represents a variant within a layout
type XKBVariantInfo struct {
	Name        string // e.g. "bepo"
	Description string // e.g. "French (BEPO)"
}

// evdev.xml structures for parsing
type xkbConfigRegistry struct {
	XMLName    xml.Name      `xml:"xkbConfigRegistry"`
	LayoutList xkbLayoutList `xml:"layoutList"`
}

type xkbLayoutList struct {
	Layouts []xkbLayout `xml:"layout"`
}

type xkbLayout struct {
	ConfigItem  xkbConfigItem  `xml:"configItem"`
	VariantList xkbVariantList `xml:"variantList"`
}

type xkbConfigItem struct {
	Name        string `xml:"name"`
	Description string `xml:"description"`
}

type xkbVariantList struct {
	Variants []xkbVariant `xml:"variant"`
}

type xkbVariant struct {
	ConfigItem xkbConfigItem `xml:"configItem"`
}

// ParseXKBLayouts parses /usr/share/X11/xkb/rules/evdev.xml to list available layouts+variants.
func ParseXKBLayouts() ([]XKBLayoutInfo, error) {
	path := "/usr/share/X11/xkb/rules/evdev.xml"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var registry xkbConfigRegistry
	if err := xml.Unmarshal(data, &registry); err != nil {
		return nil, err
	}

	var layouts []XKBLayoutInfo
	for _, l := range registry.LayoutList.Layouts {
		info := XKBLayoutInfo{
			Name:        l.ConfigItem.Name,
			Description: l.ConfigItem.Description,
		}
		for _, v := range l.VariantList.Variants {
			info.Variants = append(info.Variants, XKBVariantInfo{
				Name:        v.ConfigItem.Name,
				Description: v.ConfigItem.Description,
			})
		}
		layouts = append(layouts, info)
	}

	return layouts, nil
}

// ParseKeyboardLayoutPref parses the pipe-separated keyboard layout preference string
// into a slice of KeyboardLayout. Each entry is "layout:variant" (variant may be empty).
func ParseKeyboardLayoutPref(pref string) []KeyboardLayout {
	if pref == "" {
		return nil
	}
	parts := strings.Split(pref, "|")
	var layouts []KeyboardLayout
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		layout := kv[0]
		variant := ""
		if len(kv) > 1 {
			variant = kv[1]
		}
		layouts = append(layouts, KeyboardLayout{
			Layout:  layout,
			Variant: variant,
		})
	}
	return layouts
}

// FormatKeyboardLayoutPref formats a slice of KeyboardLayout as a pipe-separated preference string.
func FormatKeyboardLayoutPref(layouts []KeyboardLayout) string {
	var parts []string
	for _, l := range layouts {
		parts = append(parts, l.Layout+":"+l.Variant)
	}
	return strings.Join(parts, "|")
}
