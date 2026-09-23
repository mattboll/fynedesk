package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RequestDesktopSwitch writes a desktop switch request for the compositor
func RequestDesktopSwitch(desktop int) error {
	req := DesktopSwitchRequest{Desktop: desktop}
	if trySendRequest(ReqDesktopSwitch, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	fileReq := DesktopRequest{Desktop: desktop}
	data, err := json.Marshal(fileReq)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "desktop-request.json"), data)
}

// GetDesktopState reads the current desktop state from compositor
func GetDesktopState() (*DesktopState, error) {
	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "desktop-state.json")

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var state DesktopState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// WatchDesktopState watches for desktop state changes and calls callback.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchDesktopState(callback func(state *DesktopState), done <-chan struct{}) {
	if trySocketWatch(EventDesktopState, func(data json.RawMessage) {
		var state DesktopState
		if err := json.Unmarshal(data, &state); err == nil {
			callback(&state)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	statePath := filepath.Join(configDir, "desktop-state.json")

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
					if state, err := GetDesktopState(); err == nil {
						callback(state)
					}
				}
			}
		}
	}()
}
