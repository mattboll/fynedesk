package compositor

import "C"

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// clipServer is set during Run() so the CGO export can access the server.
var clipServer *server

// loadClipboardHistory restores the clipboard history from the persisted file on disk.
func (s *server) loadClipboardHistory() {
	entries := wlipc.ReadClipboardHistory()
	if len(entries) > 0 {
		s.clipboardHistory = entries
		log.Printf("[clipboard] restored %d entries from disk\n", len(entries))
	}
}

//export goClipboardChanged
func goClipboardChanged(fd C.int) {
	go func() {
		f := os.NewFile(uintptr(fd), "clipboard-pipe")
		defer f.Close()

		// Cap at 10KB + 1 byte so we can detect (and log) truncation rather
		// than silently swallowing the tail of a large paste.
		const maxClip = 10240
		data, err := io.ReadAll(io.LimitReader(f, maxClip+1))
		if err != nil || len(data) == 0 {
			return
		}
		if len(data) > maxClip {
			log.Printf("[clipboard] entry exceeded %d bytes, truncating", maxClip)
			data = data[:maxClip]
		}

		text := string(data)
		if clipServer != nil {
			clipServer.mainThreadActions <- func() {
				clipServer.addClipboardEntry(text)
			}
			clipServer.triggerWakeup()
		}
	}()
}

func (s *server) addClipboardEntry(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}

	// Limit single entry size
	if len(text) > 10240 {
		text = text[:10240]
	}

	// Deduplicate: skip if latest entry has the same text
	if len(s.clipboardHistory) > 0 && s.clipboardHistory[0].Text == text {
		return
	}

	// Remove any existing entry with same text
	filtered := make([]wlipc.ClipboardEntry, 0, len(s.clipboardHistory))
	for _, e := range s.clipboardHistory {
		if e.Text != text {
			filtered = append(filtered, e)
		}
	}

	// Prepend new entry
	entry := wlipc.ClipboardEntry{
		Text:      text,
		Timestamp: time.Now().UnixMilli(),
	}
	s.clipboardHistory = append([]wlipc.ClipboardEntry{entry}, filtered...)

	// Limit to 50 entries
	if len(s.clipboardHistory) > 50 {
		s.clipboardHistory = s.clipboardHistory[:50]
	}

	// Write to IPC
	wlipc.WriteClipboardHistory(s.clipboardHistory)
	log.Printf("[clipboard] added entry: %q (total: %d)\n", clipTruncate(text, 40), len(s.clipboardHistory))
}

func clipTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// clearClipboardHistory removes all entries and updates the IPC file.
func (s *server) clearClipboardHistory() {
	s.clipboardHistory = nil
	wlipc.WriteClipboardHistory(s.clipboardHistory)
	log.Println("[clipboard] history cleared")
}

// handleClipboardPaste reads the clipboard paste request and injects Ctrl+V.
// Called when the clipboard overlay window unmaps (same pattern as handleEmojiPaste).
func (s *server) handleClipboardPaste() {
	configDir := s.getConfigDir()
	pastePath := filepath.Join(configDir, "clipboard-paste.json")

	data, err := os.ReadFile(pastePath)
	if err != nil {
		return
	}
	os.Remove(pastePath)

	var req wlipc.ClipboardPasteRequest
	if err := json.Unmarshal(data, &req); err != nil || req.Text == "" {
		return
	}

	s.setClipboard(req.Text)

	// Inject Ctrl+V to paste into the focused window
	if len(s.keyboards) > 0 {
		s.injectCtrlV(s.keyboards[0])
	}

	log.Printf("[clipboard] pasted: %q\n", clipTruncate(req.Text, 40))
}

// requestShowClipboard sends a clipboard show request to the panel via IPC
func (s *server) requestShowClipboard() {
	// Save the currently active window so we can refocus it after the overlay closes
	s.preOverlayXdg = s.activeXdg
	s.preOverlayXway = s.activeXway

	configDir := s.getConfigDir()
	os.MkdirAll(configDir, 0700)

	ts := map[string]int64{"timestamp": time.Now().UnixMilli()}
	data, _ := json.Marshal(ts)
	writeAtomic(filepath.Join(configDir, "clipboard-show-request.json"), data)

	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventClipboardShow, ts)
	}

	// Focus the panel so it can receive keyboard input
	s.focusPanelKeyboard()
}
