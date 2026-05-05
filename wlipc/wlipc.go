// Package wlipc provides IPC between the Wayland compositor and panel.
package wlipc

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// DesktopRequest is written by panel to request desktop change
type DesktopRequest struct {
	Desktop int `json:"desktop"`
}

// DesktopState is written by compositor with current desktop info
type DesktopState struct {
	Current  int      `json:"current"`
	NumDesks int      `json:"num_desks"`
	Names    []string `json:"names,omitempty"` // Workspace names (empty = use numbers)
}

// WindowInfo represents a window for IPC
type WindowInfo struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	AppID        string  `json:"app_id"`
	Desktop      int     `json:"desktop"`
	Output       string  `json:"output,omitempty"` // Output name where window is positioned
	Focused      bool    `json:"focused"`
	Iconic       bool    `json:"iconic"`
	Maximized    bool    `json:"maximized"`
	Fullscreened bool    `json:"fullscreened"`
	Pinned       bool    `json:"pinned"`
	Urgent       bool    `json:"urgent,omitempty"`
	IsPanel      bool    `json:"is_panel"`
	ParentID     string  `json:"parent_id,omitempty"` // Parent window ID for transient/dialog windows
	X            float32 `json:"x"`
	Y            float32 `json:"y"`
	Width        float32 `json:"w"`
	Height       float32 `json:"h"`
}

// WindowsState is written by compositor with current window list
type WindowsState struct {
	Windows   []WindowInfo `json:"windows"`
	Timestamp int64        `json:"timestamp"`
}

// SessionWindow describes a window's state for session save/restore
type SessionWindow struct {
	AppID     string  `json:"app_id"`
	Title     string  `json:"title,omitempty"`
	Desktop   int     `json:"desktop"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Maximized bool    `json:"maximized,omitempty"`
	Minimized bool    `json:"minimized,omitempty"`
	Pinned    bool    `json:"pinned,omitempty"`
	Floating  bool    `json:"floating,omitempty"`
	FocusSeq  uint64  `json:"focus_seq"` // Restore focus order
}

// SessionState is saved at logout and restored at login
type SessionState struct {
	Version        int             `json:"version"`
	Timestamp      int64           `json:"timestamp"`
	CurrentDesktop int             `json:"current_desktop"`
	Windows        []SessionWindow `json:"windows"`
}

// VolumeEvent is written by compositor when volume changes
type VolumeEvent struct {
	Timestamp int64 `json:"timestamp"`
}

// WindowRule defines per-app behavior rules for the compositor.
// Rules are matched by AppID (exact) or Pattern (glob) against
// the window's app_id (Wayland) or WM_CLASS (XWayland).
type WindowRule struct {
	AppID     string  `json:"app_id,omitempty" toml:"app_id,omitempty"`
	Pattern   string  `json:"pattern,omitempty" toml:"pattern,omitempty"`
	Float     *bool   `json:"float,omitempty" toml:"float,omitempty"`
	Workspace int     `json:"workspace,omitempty" toml:"workspace,omitempty"` // 1-based (0 = unset)
	Width     int     `json:"width,omitempty" toml:"width,omitempty"`
	Height    int     `json:"height,omitempty" toml:"height,omitempty"`
	Maximize  *bool   `json:"maximize,omitempty" toml:"maximize,omitempty"`
	Opacity   float32 `json:"opacity,omitempty" toml:"opacity,omitempty"` // 0.1-1.0 (0 = default)
	Pinned    *bool   `json:"pinned,omitempty" toml:"pinned,omitempty"`
}

// IsWaylandSession returns true if running under Wayland compositor
func IsWaylandSession() bool {
	return os.Getenv("WAYLAND_DISPLAY") != ""
}

func getConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = os.Getenv("HOME")
		if configDir == "" {
			configDir = "/tmp"
			log.Println("Warning: HOME not set, using /tmp for fynedesk config")
		}
	}
	return filepath.Join(configDir, "fynedesk")
}

// atomicWriteFile writes data to a file atomically using write-to-temp + rename
// to avoid race conditions with concurrent readers. If the target path is a
// symlink, refuse to write rather than silently replacing it (defense in depth
// — the parent directory should be 0700 anyway).
func atomicWriteFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink: %s", path)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ipc-*")
	if err != nil {
		return err
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

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
