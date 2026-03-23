package wlipc

import "encoding/json"

// KeyBinding represents a single key binding with a key name and modifiers.
// Key names are XKB names (e.g. "Escape", "Tab", "space", "t", "Print").
// Modifier names are "Shift", "Ctrl", "Alt", "WM" (WM = user-configurable modifier).
type KeyBinding struct {
	Key  string   `json:"key"`
	Mods []string `json:"mods"`
}

// ActionBindings maps action names to their key bindings.
type ActionBindings map[string][]KeyBinding

// Action name constants
const (
	ActionQuit              = "quit"
	ActionSwitchAppNext     = "switch_app_next"
	ActionSwitchAppPrev     = "switch_app_prev"
	ActionToggleFullscreen  = "toggle_fullscreen"
	ActionMaximize          = "maximize"
	ActionMinimize          = "minimize"
	ActionCloseWindow       = "close_window"
	ActionOpenTerminal      = "open_terminal"
	ActionEmergencyLogout   = "emergency_logout"
	ActionPrevDesktop       = "prev_desktop"
	ActionNextDesktop       = "next_desktop"
	ActionSwitchDesk1       = "switch_desk_1"
	ActionSwitchDesk2       = "switch_desk_2"
	ActionSwitchDesk3       = "switch_desk_3"
	ActionSwitchDesk4       = "switch_desk_4"
	ActionMoveToDesk1       = "move_to_desk_1"
	ActionMoveToDesk2       = "move_to_desk_2"
	ActionMoveToDesk3       = "move_to_desk_3"
	ActionMoveToDesk4       = "move_to_desk_4"
	ActionMoveToPrevDesktop = "move_to_prev_desktop"
	ActionMoveToNextDesktop = "move_to_next_desktop"
	ActionScreenshotFull    = "screenshot_full"
	ActionScreenshotRegion  = "screenshot_region"
	ActionScreenshotWindow  = "screenshot_window"
	ActionToggleDropdown    = "toggle_dropdown_terminal"
	ActionLockScreen        = "lock_screen"
	ActionShowLauncher      = "show_launcher"
	ActionVolumeUp          = "volume_up"
	ActionVolumeDown        = "volume_down"
	ActionVolumeMute        = "volume_mute"
	ActionBrightnessUp      = "brightness_up"
	ActionBrightnessDown    = "brightness_down"
	ActionCalculator        = "calculator"
	ActionShowEmojiPicker   = "show_emoji_picker"
	ActionShowClipboard     = "show_clipboard"
	ActionSnapLeft          = "snap_left"
	ActionSnapRight         = "snap_right"
	ActionCommandPalette    = "command_palette"
	ActionToggleSidebar     = "toggle_sidebar"
	ActionWindowOverview    = "window_overview"
	ActionToggleTiling      = "toggle_tiling"
	ActionSwapMaster        = "swap_master"
	ActionShrinkMaster      = "shrink_master"
	ActionGrowMaster        = "grow_master"
	ActionToggleFloat       = "toggle_float"
	ActionToggleNightLight  = "toggle_night_light"
	ActionFocusMode         = "focus_mode"
	ActionShowDesktop       = "show_desktop"
	ActionConfirmSwitcher   = "confirm_switcher"
	ActionCancelSwitcher    = "cancel_switcher"
)

// DefaultBindings returns the default key bindings matching the current compositor behavior.
func DefaultBindings() ActionBindings {
	return ActionBindings{
		ActionQuit:              {{Key: "Escape", Mods: []string{"Alt"}}},
		ActionSwitchAppNext:     {{Key: "Tab", Mods: []string{"WM"}}},
		ActionSwitchAppPrev:     {{Key: "Tab", Mods: []string{"Shift", "WM"}}},
		ActionToggleFullscreen:  {{Key: "F11", Mods: nil}},
		ActionMaximize:          {{Key: "Up", Mods: []string{"WM"}}, {Key: "F10", Mods: []string{"WM"}}},
		ActionMinimize:          {{Key: "F9", Mods: []string{"WM"}}, {Key: "Down", Mods: []string{"WM"}}},
		ActionCloseWindow:       {{Key: "F4", Mods: []string{"Alt"}}},
		ActionOpenTerminal:      {{Key: "t", Mods: []string{"WM"}}, {Key: "Return", Mods: []string{"WM"}}},
		ActionEmergencyLogout:   {{Key: "BackSpace", Mods: []string{"Ctrl", "Alt"}}},
		ActionPrevDesktop:       {{Key: "Left", Mods: []string{"Ctrl", "Alt"}}},
		ActionNextDesktop:       {{Key: "Right", Mods: []string{"Ctrl", "Alt"}}},
		ActionSwitchDesk1:       {{Key: "1", Mods: []string{"WM"}}},
		ActionSwitchDesk2:       {{Key: "2", Mods: []string{"WM"}}},
		ActionSwitchDesk3:       {{Key: "3", Mods: []string{"WM"}}},
		ActionSwitchDesk4:       {{Key: "4", Mods: []string{"WM"}}},
		ActionMoveToDesk1:       {{Key: "1", Mods: []string{"WM", "Shift"}}},
		ActionMoveToDesk2:       {{Key: "2", Mods: []string{"WM", "Shift"}}},
		ActionMoveToDesk3:       {{Key: "3", Mods: []string{"WM", "Shift"}}},
		ActionMoveToDesk4:       {{Key: "4", Mods: []string{"WM", "Shift"}}},
		ActionMoveToPrevDesktop: {{Key: "Left", Mods: []string{"Ctrl", "Alt", "Shift"}}},
		ActionMoveToNextDesktop: {{Key: "Right", Mods: []string{"Ctrl", "Alt", "Shift"}}},
		ActionScreenshotFull:    {{Key: "Print", Mods: nil}},
		ActionScreenshotRegion:  {{Key: "Print", Mods: []string{"Shift"}}},
		ActionScreenshotWindow:  {{Key: "Print", Mods: []string{"Ctrl"}}},
		ActionToggleDropdown:    {{Key: "grave", Mods: []string{"WM"}}},
		ActionLockScreen:        {{Key: "l", Mods: []string{"WM"}}},
		ActionShowLauncher:      {{Key: "space", Mods: []string{"WM"}}},
		ActionVolumeUp:          {{Key: "XF86AudioRaiseVolume", Mods: nil}},
		ActionVolumeDown:        {{Key: "XF86AudioLowerVolume", Mods: nil}},
		ActionVolumeMute:        {{Key: "XF86AudioMute", Mods: nil}},
		ActionBrightnessUp:      {{Key: "XF86MonBrightnessUp", Mods: nil}},
		ActionBrightnessDown:    {{Key: "XF86MonBrightnessDown", Mods: nil}},
		ActionCalculator:        {{Key: "XF86Calculator", Mods: nil}},
		ActionShowEmojiPicker:   {{Key: "period", Mods: []string{"WM"}}},
		ActionShowClipboard:     {{Key: "v", Mods: []string{"WM"}}},
		ActionSnapLeft:          {{Key: "Left", Mods: []string{"WM"}}},
		ActionSnapRight:         {{Key: "Right", Mods: []string{"WM"}}},
		ActionCommandPalette:    {{Key: "p", Mods: []string{"WM"}}},
		ActionToggleSidebar:     {{Key: "a", Mods: []string{"WM"}}},
		ActionWindowOverview:    {{Key: "w", Mods: []string{"WM"}}},
		ActionToggleTiling:      {{Key: "t", Mods: []string{"WM", "Shift"}}},
		ActionSwapMaster:        {{Key: "j", Mods: []string{"WM", "Shift"}}},
		ActionShrinkMaster:      {{Key: "h", Mods: []string{"WM", "Shift"}}},
		ActionGrowMaster:        {{Key: "l", Mods: []string{"WM", "Shift"}}},
		ActionToggleFloat:       {{Key: "f", Mods: []string{"WM", "Shift"}}},
		ActionToggleNightLight:  {{Key: "n", Mods: []string{"WM", "Shift"}}},
		ActionFocusMode:         {{Key: "f", Mods: []string{"WM"}}},
	}
}

// ActionDisplayName returns a human-readable name for an action.
func ActionDisplayName(action string) string {
	names := map[string]string{
		ActionQuit:              "Log out",
		ActionSwitchAppNext:     "Switch app (next)",
		ActionSwitchAppPrev:     "Switch app (previous)",
		ActionToggleFullscreen:  "Toggle fullscreen",
		ActionMaximize:          "Maximize / restore",
		ActionMinimize:          "Minimize",
		ActionCloseWindow:       "Close window",
		ActionOpenTerminal:      "Open terminal",
		ActionEmergencyLogout:   "Emergency logout",
		ActionPrevDesktop:       "Previous desktop",
		ActionNextDesktop:       "Next desktop",
		ActionSwitchDesk1:       "Switch to desktop 1",
		ActionSwitchDesk2:       "Switch to desktop 2",
		ActionSwitchDesk3:       "Switch to desktop 3",
		ActionSwitchDesk4:       "Switch to desktop 4",
		ActionMoveToDesk1:       "Move window to desktop 1",
		ActionMoveToDesk2:       "Move window to desktop 2",
		ActionMoveToDesk3:       "Move window to desktop 3",
		ActionMoveToDesk4:       "Move window to desktop 4",
		ActionMoveToPrevDesktop: "Move window to previous desktop",
		ActionMoveToNextDesktop: "Move window to next desktop",
		ActionScreenshotFull:    "Screenshot (full screen)",
		ActionScreenshotRegion:  "Screenshot (region)",
		ActionScreenshotWindow:  "Screenshot (window)",
		ActionToggleDropdown:    "Toggle dropdown terminal",
		ActionLockScreen:        "Lock screen",
		ActionShowLauncher:      "Show app launcher",
		ActionVolumeUp:          "Volume up",
		ActionVolumeDown:        "Volume down",
		ActionVolumeMute:        "Volume mute/unmute",
		ActionBrightnessUp:      "Brightness up",
		ActionBrightnessDown:    "Brightness down",
		ActionCalculator:        "Calculator",
		ActionShowEmojiPicker:   "Emoji picker",
		ActionShowClipboard:     "Clipboard manager",
		ActionSnapLeft:          "Snap window left",
		ActionSnapRight:         "Snap window right",
		ActionCommandPalette:    "Command palette",
		ActionToggleSidebar:     "Toggle sidebar",
		ActionWindowOverview:    "Window overview",
		ActionToggleTiling:      "Toggle tiling mode",
		ActionSwapMaster:        "Swap master (tiling)",
		ActionShrinkMaster:      "Shrink master (tiling)",
		ActionGrowMaster:        "Grow master (tiling)",
		ActionToggleFloat:       "Toggle float (tiling)",
		ActionToggleNightLight:  "Toggle night light",
		ActionFocusMode:         "Focus mode",
		ActionShowDesktop:       "Show desktop",
	}
	if n, ok := names[action]; ok {
		return n
	}
	return action
}

// ActionOrder returns the display order for actions in the settings UI.
func ActionOrder() []string {
	return []string{
		ActionQuit,
		ActionCloseWindow,
		ActionSwitchAppNext,
		ActionSwitchAppPrev,
		ActionToggleFullscreen,
		ActionMaximize,
		ActionMinimize,
		ActionOpenTerminal,
		ActionToggleDropdown,
		ActionShowLauncher,
		ActionShowEmojiPicker,
		ActionShowClipboard,
		ActionLockScreen,
		ActionScreenshotFull,
		ActionScreenshotRegion,
		ActionScreenshotWindow,
		ActionPrevDesktop,
		ActionNextDesktop,
		ActionSwitchDesk1,
		ActionSwitchDesk2,
		ActionSwitchDesk3,
		ActionSwitchDesk4,
		ActionMoveToDesk1,
		ActionMoveToDesk2,
		ActionMoveToDesk3,
		ActionMoveToDesk4,
		ActionSnapLeft,
		ActionSnapRight,
		ActionCommandPalette,
		ActionMoveToPrevDesktop,
		ActionMoveToNextDesktop,
		ActionToggleSidebar,
		ActionWindowOverview,
		ActionToggleTiling,
		ActionSwapMaster,
		ActionShrinkMaster,
		ActionGrowMaster,
		ActionToggleFloat,
		ActionVolumeUp,
		ActionVolumeDown,
		ActionVolumeMute,
		ActionBrightnessUp,
		ActionBrightnessDown,
		ActionToggleNightLight,
		ActionFocusMode,
		ActionShowDesktop,
		ActionEmergencyLogout,
		ActionCalculator,
	}
}

// MergeWithDefaults merges user-customized bindings with the defaults.
// Actions not present in user bindings use defaults.
// Pass nil to get all defaults.
func MergeWithDefaults(user ActionBindings) ActionBindings {
	defaults := DefaultBindings()
	if user == nil {
		return defaults
	}

	result := make(ActionBindings)
	for action, bindings := range defaults {
		result[action] = bindings
	}
	for action, bindings := range user {
		result[action] = bindings
	}

	// Ensure protected actions always have at least one binding
	for _, action := range ProtectedActions() {
		if bindings, ok := result[action]; !ok || len(bindings) == 0 {
			result[action] = defaults[action]
		}
	}

	return result
}

// ProtectedActions returns the list of actions that cannot have their
// keybindings removed entirely. These are essential for session recovery.
func ProtectedActions() []string {
	return []string{
		ActionQuit,            // Alt+Escape — must always be able to log out
		ActionEmergencyLogout, // Ctrl+Alt+Backspace — emergency exit
	}
}

// BindingToString formats a KeyBinding for display (e.g. "Alt+Tab", "WM+T").
func BindingToString(b KeyBinding) string {
	s := ""
	for i, mod := range b.Mods {
		if i > 0 {
			s += "+"
		}
		s += mod
	}
	if len(b.Mods) > 0 {
		s += "+"
	}
	s += keyDisplayName(b.Key)
	return s
}

// keyDisplayName returns a human-readable name for an XKB key name.
func keyDisplayName(key string) string {
	display := map[string]string{
		"grave":                 "`",
		"space":                 "Space",
		"Return":                "Enter",
		"BackSpace":             "Backspace",
		"Print":                 "PrtSc",
		"XF86AudioRaiseVolume":  "Vol+",
		"XF86AudioLowerVolume":  "Vol-",
		"XF86AudioMute":         "Mute",
		"XF86MonBrightnessUp":   "Bright+",
		"XF86MonBrightnessDown": "Bright-",
		"XF86Calculator":        "Calc",
	}
	if d, ok := display[key]; ok {
		return d
	}
	return key
}

// ParseBindingsJSON parses a JSON string into ActionBindings.
func ParseBindingsJSON(data string) (ActionBindings, error) {
	if data == "" {
		return nil, nil
	}
	var bindings ActionBindings
	err := json.Unmarshal([]byte(data), &bindings)
	return bindings, err
}

// BindingsToJSON serializes ActionBindings to a JSON string.
func BindingsToJSON(bindings ActionBindings) (string, error) {
	data, err := json.Marshal(bindings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
