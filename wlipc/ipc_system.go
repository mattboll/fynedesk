package wlipc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// RequestLock writes a lock request for the compositor to launch a screen locker
func RequestLock() error {
	if trySendRequest(ReqLock, struct{}{}) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	req := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "lock-request.json"), data)
}

// RequestLogout writes a logout request for the compositor to terminate
func RequestLogout() error {
	if trySendRequest(ReqLogout, struct{}{}) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	req := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "logout-request.json"), data)
}

// RequestRestart writes a restart request for the compositor to exit and relaunch
func RequestRestart() error {
	if trySendRequest(ReqRestart, struct{}{}) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	req := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "restart-request.json"), data)
}

// RequestShutdown asks the compositor to shut down the system
func RequestShutdown() error {
	if trySendRequest(ReqShutdown, struct{}{}) {
		return nil
	}
	// Direct fallback: execute systemctl poweroff
	return execSystemctl("poweroff")
}

// RequestHibernate asks the compositor to hibernate the system
func RequestHibernate() error {
	if trySendRequest(ReqHibernate, struct{}{}) {
		return nil
	}
	return execSystemctl("hibernate")
}

// RequestSuspend asks the compositor to suspend the system
func RequestSuspend() error {
	if trySendRequest(ReqSuspend, struct{}{}) {
		return nil
	}
	return execSystemctl("suspend")
}

func execSystemctl(action string) error {
	return exec.Command("systemctl", action).Run()
}

// LayoutRequest is written by the panel to request output positioning/mirroring/primary changes
type LayoutRequest struct {
	OutputName string `json:"output_name"` // Output to reposition
	Position   string `json:"position"`    // "left","right","above","below","mirror" (empty = primary-only change)
	RelativeTo string `json:"relative_to"` // Reference output name
	Primary    bool   `json:"primary"`     // Set as primary output
}

// RequestOutputLayout writes a layout request for the compositor
func RequestOutputLayout(req LayoutRequest) error {
	if trySendRequest(ReqLayoutRequest, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "layout-request.json"), data)
}

// RequestModeChange writes a mode change request for the compositor.
func RequestModeChange(modeIndex int, outputName string) error {
	req := struct {
		ModeIndex  int    `json:"mode_index"`
		OutputName string `json:"output_name,omitempty"`
	}{modeIndex, outputName}
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "mode-request.json"), data)
}

// RequestScaleChange writes a scale change request for the compositor.
func RequestScaleChange(scale float32, outputName string) error {
	req := struct {
		Scale      float32 `json:"scale"`
		OutputName string  `json:"output_name,omitempty"`
	}{scale, outputName}
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "scale-request.json"), data)
}

// RequestVRRChange writes a VRR (adaptive sync) toggle request for the compositor
func RequestVRRChange(outputName string, enabled bool) error {
	req := struct {
		OutputName string `json:"output_name,omitempty"`
		Enabled    bool   `json:"enabled"`
	}{outputName, enabled}
	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "vrr-request.json"), data)
}

// NotifyBrightnessEvent signals the panel that brightness changed
func NotifyBrightnessEvent() error {
	evt := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	broadcastIfServer(EventBrightnessChange, evt)

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "brightness-event.json"), data)
}

// WatchBrightnessEvent watches for brightness events and calls callback.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchBrightnessEvent(callback func(), done <-chan struct{}) {
	if trySocketWatch(EventBrightnessChange, func(data json.RawMessage) {
		callback()
	}, done) {
		return
	}

	configDir := getConfigDir()
	eventPath := filepath.Join(configDir, "brightness-event.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(eventPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					callback()
				}
			}
		}
	}()
}

// NotifyVolumeEvent signals the panel that volume changed.
func NotifyVolumeEvent() error {
	evt := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	broadcastIfServer(EventVolumeChange, evt)

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "volume-event.json"), data)
}

// WatchVolumeEvent watches for volume events and calls callback.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchVolumeEvent(callback func(), done <-chan struct{}) {
	if trySocketWatch(EventVolumeChange, func(data json.RawMessage) {
		callback()
	}, done) {
		return
	}

	configDir := getConfigDir()
	eventPath := filepath.Join(configDir, "volume-event.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(eventPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					callback()
				}
			}
		}
	}()
}

// SettingsChanged is the IPC payload for settings change notifications.
// It embeds a snapshot of the Fyne preferences so the compositor does not
// need to re-read the prefs JSON file (which may not have been flushed yet).
type SettingsChanged struct {
	Timestamp int64          `json:"timestamp"`
	Prefs     map[string]any `json:"prefs,omitempty"`
}

// NotifySettingsChanged signals the compositor to reload preferences.
// The prefs map should contain all current Fyne preference values so
// the compositor can apply them immediately without reading the prefs file.
func NotifySettingsChanged(prefs map[string]any) error {
	msg := SettingsChanged{
		Timestamp: time.Now().UnixMilli(),
		Prefs:     prefs,
	}
	if trySendRequest(ReqSettingsChanged, msg) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "settings-changed.json"), data)
}

// ScreenshotEvent is written by compositor after taking a screenshot
type ScreenshotEvent struct {
	FilePath  string `json:"file_path"`
	Timestamp int64  `json:"timestamp"`
}

// NotifyScreenshot writes a screenshot event for the panel
func NotifyScreenshot(filePath string) error {
	evt := ScreenshotEvent{FilePath: filePath, Timestamp: time.Now().UnixMilli()}
	broadcastIfServer(EventScreenshot, evt)

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "screenshot-event.json"), data)
}

// WatchScreenshotEvent watches for screenshot events from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchScreenshotEvent(callback func(evt *ScreenshotEvent), done <-chan struct{}) {
	if trySocketWatch(EventScreenshot, func(data json.RawMessage) {
		var evt ScreenshotEvent
		if err := json.Unmarshal(data, &evt); err == nil {
			callback(&evt)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	evtPath := filepath.Join(configDir, "screenshot-event.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(evtPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(evtPath)
					if err != nil {
						continue
					}
					os.Remove(evtPath)

					var evt ScreenshotEvent
					if err := json.Unmarshal(data, &evt); err == nil {
						callback(&evt)
					}
				}
			}
		}
	}()
}

// DBusNotification is written by the compositor when it receives a D-Bus
// notification (org.freedesktop.Notifications.Notify). The panel watches
// this file to display toast popups.
type DBusNotification struct {
	AppName   string `json:"app_name,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Timeout   int32  `json:"timeout"`
	Timestamp int64  `json:"timestamp"`
}

// NotifyDBusNotification writes a D-Bus notification for the panel to display.
func NotifyDBusNotification(appName, title, body string, timeout int32) error {
	n := DBusNotification{AppName: appName, Title: title, Body: body, Timeout: timeout, Timestamp: time.Now().UnixMilli()}

	// Broadcast via socket if server is available (compositor-side)
	broadcastIfServer(EventNotification, n)

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "dbus-notification.json"), data)
}

// WatchDBusNotification watches for D-Bus notifications forwarded by the compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchDBusNotification(callback func(n *DBusNotification), done <-chan struct{}) {
	if trySocketWatch(EventNotification, func(data json.RawMessage) {
		var n DBusNotification
		if err := json.Unmarshal(data, &n); err == nil {
			callback(&n)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	evtPath := filepath.Join(configDir, "dbus-notification.json")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(evtPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(evtPath)
					if err != nil {
						continue
					}
					os.Remove(evtPath)

					var n DBusNotification
					if err := json.Unmarshal(data, &n); err == nil {
						callback(&n)
					}
				}
			}
		}
	}()
}

// AccentColor is written by the compositor when a wallpaper's dominant color is extracted.
type AccentColor struct {
	Hex       string `json:"hex"`       // e.g. "#4a9eff"
	Timestamp int64  `json:"timestamp"` // UnixMilli
}

// WriteAccentColor writes the extracted accent color for the panel to read.
func WriteAccentColor(hex string) error {
	ac := AccentColor{Hex: hex, Timestamp: time.Now().UnixMilli()}
	data, err := json.Marshal(ac)
	if err != nil {
		return err
	}
	dir := getConfigDir()
	os.MkdirAll(dir, 0700)
	return atomicWriteFile(filepath.Join(dir, "accent-color.json"), data)
}

// ReadAccentColor reads the current accent color from the IPC file.
func ReadAccentColor() (*AccentColor, error) {
	data, err := os.ReadFile(filepath.Join(getConfigDir(), "accent-color.json"))
	if err != nil {
		return nil, err
	}
	var ac AccentColor
	if err := json.Unmarshal(data, &ac); err != nil {
		return nil, err
	}
	return &ac, nil
}

// WatchAccentColor watches for accent color changes from the compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchAccentColor(callback func(hex string), done <-chan struct{}) {
	// AccentColor doesn't have a dedicated socket event yet, so we check
	// if there's a generic event name. For now there's no EventAccentColor
	// constant, so we skip socket and use file polling only.
	// TODO: add EventAccentColor to socket protocol when needed.

	configDir := getConfigDir()
	acPath := filepath.Join(configDir, "accent-color.json")

	var lastMod time.Time
	ticker := time.NewTicker(500 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(acPath)
				if err != nil {
					continue
				}
				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(acPath)
					if err != nil {
						continue
					}
					var ac AccentColor
					if err := json.Unmarshal(data, &ac); err == nil && ac.Hex != "" {
						callback(ac.Hex)
					}
				}
			}
		}
	}()
}

// ReadSessionState loads session state from disk
func ReadSessionState() (*SessionState, error) {
	path := filepath.Join(getConfigDir(), "session-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// WriteSessionState saves session state to disk
func WriteSessionState(state *SessionState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(getConfigDir(), "session-state.json")
	return atomicWriteFile(path, data)
}

// ClearSessionState removes the session state file (after successful restore)
func ClearSessionState() {
	path := filepath.Join(getConfigDir(), "session-state.json")
	os.Remove(path)
}
