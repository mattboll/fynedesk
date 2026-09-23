package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// WindowActionRequest is written by the panel to request a window action
type WindowActionRequest struct {
	WindowID string `json:"window_id"`
	Action   string `json:"action"`            // "focus", "close", "iconify", "uniconify", "maximize", "unmaximize", "raise", "set_desktop"
	Desktop  int    `json:"desktop,omitempty"` // Only used for "set_desktop"
}

// RequestWindowAction writes a window action request for the compositor
func RequestWindowAction(windowID, action string) error {
	return RequestWindowActionWithDesktop(windowID, action, 0)
}

// RequestWindowActionWithDesktop writes a window action request with a desktop parameter
func RequestWindowActionWithDesktop(windowID, action string, desktop int) error {
	req := WindowActionRequest{WindowID: windowID, Action: action, Desktop: desktop}
	if trySendRequest(ReqWindowAction, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "window-action-request.json"), data)
}

// RaiseByTitleRequest asks the compositor to raise and focus a window by its title.
type RaiseByTitleRequest struct {
	Title string `json:"title"`
}

// RequestRaiseByTitle asks the compositor to raise a window matching the given title.
func RequestRaiseByTitle(title string) error {
	req := RaiseByTitleRequest{Title: title}
	if trySendRequest(ReqRaiseByTitle, req) {
		return nil
	}
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "raise-by-title.json"), data)
}

// RaiseByClassRequest asks the compositor to raise and focus a window by its WM_CLASS.
type RaiseByClassRequest struct {
	Class string `json:"class"`
}

// RequestRaiseByClass asks the compositor to raise a window matching the given WM_CLASS.
func RequestRaiseByClass(class string) error {
	req := RaiseByClassRequest{Class: class}
	if trySendRequest(ReqRaiseByClass, req) {
		return nil
	}
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "raise-by-class.json"), data)
}

// GetWindowsState reads the current windows state from compositor
func GetWindowsState() (*WindowsState, error) {
	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "windows-state.json")

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var state WindowsState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// WatchWindowsState watches for window state changes and calls callback.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchWindowsState(callback func(state *WindowsState), done <-chan struct{}) {
	if trySocketWatch(EventWindowsState, func(data json.RawMessage) {
		var state WindowsState
		if err := json.Unmarshal(data, &state); err == nil {
			callback(&state)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "windows-state.json")

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
					if state, err := GetWindowsState(); err == nil {
						callback(state)
					}
				}
			}
		}
	}()
}

// ContextMenuRequest is written by compositor to ask panel to show a window context menu
type ContextMenuRequest struct {
	WindowID string  `json:"window_id"`
	Title    string  `json:"title"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
}

// WatchContextMenu watches for context menu requests from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchContextMenu(callback func(req *ContextMenuRequest), done <-chan struct{}) {
	if trySocketWatch(EventContextMenu, func(data json.RawMessage) {
		var req ContextMenuRequest
		if err := json.Unmarshal(data, &req); err == nil {
			callback(&req)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	reqPath := filepath.Join(configDir, "context-menu-request.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(reqPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(reqPath)
					if err != nil {
						continue
					}
					var req ContextMenuRequest
					if err := json.Unmarshal(data, &req); err == nil {
						os.Remove(reqPath)
						callback(&req)
					}
				}
			}
		}
	}()
}

// OverlayRequest is written by the panel to request an overlay window position
type OverlayRequest struct {
	Title  string  `json:"title"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// primaryScreenOffset stores the primary output's layout position.
// When the primary screen is not at (0,0) (multi-monitor), all overlay
// positions must be shifted by this offset so they land on the correct output.
var primaryScreenOffX, primaryScreenOffY float32

// SetPrimaryScreenOffset sets the primary output's position in layout coordinates.
// Called by the panel at startup when the compositor passes the output offset.
func SetPrimaryScreenOffset(x, y float32) {
	primaryScreenOffX = x
	primaryScreenOffY = y
}

// RequestOverlayPosition writes an overlay position request for the compositor.
// Positions are expected in screen-relative coordinates (relative to the primary
// output's top-left). The primary screen offset is added automatically.
func RequestOverlayPosition(title string, x, y, w, h float32) error {
	return RequestOverlayPositionAbsolute(title, x+primaryScreenOffX, y+primaryScreenOffY, w, h)
}

// RequestOverlayPositionAbsolute writes an overlay position request using absolute
// layout coordinates. Unlike RequestOverlayPosition, no offset is added.
// Use this when the caller already computed absolute positions (e.g. multi-monitor
// launcher positioning based on cursor coordinates from the compositor).
func RequestOverlayPositionAbsolute(title string, x, y, w, h float32) error {
	req := OverlayRequest{Title: title, X: x, Y: y, Width: w, Height: h}
	if trySendRequest(ReqOverlay, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "overlay-request.json"), data)
}

// PanelHotspotEvent is sent by the compositor when the panel is raised/lowered
// via the edge hotspot. The panel uses this to hide desktop icons (screen area
// modules) when the panel is above windows.
type PanelHotspotEvent struct {
	Raised bool `json:"raised"`
}

// LauncherRequest is written by compositor to ask panel to open the app launcher.
// CursorX/CursorY are the cursor position in layout-space pixels so the panel
// can open the launcher on the correct output.
type LauncherRequest struct {
	Timestamp int64   `json:"timestamp"`
	CursorX   float32 `json:"cursor_x,omitempty"`
	CursorY   float32 `json:"cursor_y,omitempty"`
}

// RequestLauncher writes a launcher request for the panel
func RequestLauncher() error {
	req := LauncherRequest{Timestamp: time.Now().UnixMilli()}
	if trySendRequest(ReqOverlay, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "launcher-request.json"), data)
}

// WatchLauncherRequest watches for launcher requests from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchLauncherRequest(callback func(), done <-chan struct{}) {
	if trySocketWatch(EventLauncherRequest, func(data json.RawMessage) {
		callback()
	}, done) {
		return
	}

	configDir := getConfigDir()
	reqPath := filepath.Join(configDir, "launcher-request.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(reqPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					os.Remove(reqPath)
					callback()
				}
			}
		}
	}()
}

// EmojiPickerRequest is written by compositor to ask panel to open the emoji picker
type EmojiPickerRequest struct {
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	Timestamp int64   `json:"timestamp"`
}

// EmojiPasteRequest is written by panel to ask compositor to paste an emoji
type EmojiPasteRequest struct {
	Emoji     string `json:"emoji"`
	Timestamp int64  `json:"timestamp"`
}

// RequestEmojiPaste writes an emoji paste request for the compositor
func RequestEmojiPaste(emoji string) error {
	req := EmojiPasteRequest{Emoji: emoji, Timestamp: time.Now().UnixMilli()}
	if trySendRequest(ReqEmojiPaste, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "emoji-paste.json"), data)
}

// WatchEmojiPickerRequest watches for emoji picker requests from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchEmojiPickerRequest(callback func(req *EmojiPickerRequest), done <-chan struct{}) {
	if trySocketWatch(EventEmojiPicker, func(data json.RawMessage) {
		var req EmojiPickerRequest
		if err := json.Unmarshal(data, &req); err == nil {
			callback(&req)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	reqPath := filepath.Join(configDir, "emoji-picker-request.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(reqPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(reqPath)
					if err != nil {
						continue
					}
					var req EmojiPickerRequest
					if err := json.Unmarshal(data, &req); err == nil {
						os.Remove(reqPath)
						callback(&req)
					}
				}
			}
		}
	}()
}
