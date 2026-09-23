package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ClipboardEntry represents one clipboard history item
type ClipboardEntry struct {
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

// ClipboardHistory is broadcast from compositor to panel
type ClipboardHistory struct {
	Entries   []ClipboardEntry `json:"entries"`
	Timestamp int64            `json:"timestamp"`
}

// ClipboardPasteRequest is sent from panel to compositor to paste a specific entry
type ClipboardPasteRequest struct {
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

// ReadClipboardHistory loads the persisted clipboard history from disk.
// Returns nil entries if file doesn't exist or is unreadable.
// It reads from the encrypted clipboard-history.enc file. For backward
// compatibility, if only the old plaintext clipboard-history.json exists,
// it migrates it to the encrypted format and removes the old file.
func ReadClipboardHistory() []ClipboardEntry {
	configDir := getConfigDir()
	encPath := filepath.Join(configDir, "clipboard-history.enc")
	jsonPath := filepath.Join(configDir, "clipboard-history.json")

	encData, encErr := os.ReadFile(encPath)
	if encErr == nil {
		// Decrypt the encrypted file
		plaintext, err := DecryptData(encData)
		if err != nil {
			return nil
		}
		var hist ClipboardHistory
		if err := json.Unmarshal(plaintext, &hist); err != nil {
			return nil
		}
		return hist.Entries
	}

	// Backward compatibility: migrate plaintext .json to encrypted .enc
	jsonData, jsonErr := os.ReadFile(jsonPath)
	if jsonErr != nil {
		return nil
	}
	var hist ClipboardHistory
	if err := json.Unmarshal(jsonData, &hist); err != nil {
		return nil
	}

	// Re-save as encrypted and remove the old plaintext file
	_ = WriteClipboardHistory(hist.Entries)
	os.Remove(jsonPath)

	return hist.Entries
}

// WriteClipboardHistory writes the clipboard history as an encrypted file
// (clipboard-history.enc) for the panel to read.
func WriteClipboardHistory(entries []ClipboardEntry) error {
	hist := ClipboardHistory{Entries: entries, Timestamp: time.Now().UnixMilli()}
	broadcastIfServer(EventClipboardHist, hist)

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(hist)
	if err != nil {
		return err
	}
	encrypted, err := EncryptData(data)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "clipboard-history.enc"), encrypted)
}

// WatchClipboardHistory watches for clipboard history changes from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchClipboardHistory(callback func(hist *ClipboardHistory), done <-chan struct{}) {
	if trySocketWatch(EventClipboardHist, func(data json.RawMessage) {
		var hist ClipboardHistory
		if err := json.Unmarshal(data, &hist); err == nil {
			callback(&hist)
		}
	}, done) {
		return
	}

	configDir := getConfigDir()
	histPath := filepath.Join(configDir, "clipboard-history.enc")

	var lastMod time.Time
	ticker := time.NewTicker(100 * time.Millisecond)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info, err := os.Stat(histPath)
				if err != nil {
					continue
				}

				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					data, err := os.ReadFile(histPath)
					if err != nil {
						continue
					}

					plaintext, err := DecryptData(data)
					if err != nil {
						continue
					}

					var hist ClipboardHistory
					if err := json.Unmarshal(plaintext, &hist); err == nil {
						callback(&hist)
					}
				}
			}
		}
	}()
}

// RequestClipboardPaste writes a clipboard paste request for the compositor
func RequestClipboardPaste(text string) error {
	req := ClipboardPasteRequest{Text: text, Timestamp: time.Now().UnixMilli()}
	if trySendRequest(ReqClipboardPaste, req) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "clipboard-paste.json"), data)
}

// RequestClipboardClear asks the compositor to clear the clipboard history.
func RequestClipboardClear() error {
	ts := struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()}
	if trySendRequest(ReqClipboardClear, ts) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(ts)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "clipboard-clear.json"), data)
}

// RequestShowClipboard writes a show clipboard request for the panel
func RequestShowClipboard() error {
	if trySendRequest(ReqOverlay, struct {
		Action string `json:"action"`
	}{Action: "clipboard-show"}) {
		return nil
	}

	configDir := getConfigDir()
	os.MkdirAll(configDir, 0700)
	data, err := json.Marshal(struct {
		Timestamp int64 `json:"timestamp"`
	}{Timestamp: time.Now().UnixMilli()})
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(configDir, "clipboard-show-request.json"), data)
}

// WatchClipboardShowRequest watches for clipboard show requests from compositor.
// Close the done channel to stop the watcher goroutine.
// Prefers socket IPC when available, falls back to file polling.
func WatchClipboardShowRequest(callback func(), done <-chan struct{}) {
	if trySocketWatch(EventClipboardShow, func(data json.RawMessage) {
		callback()
	}, done) {
		return
	}

	configDir := getConfigDir()
	reqPath := filepath.Join(configDir, "clipboard-show-request.json")

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
