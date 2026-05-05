package compositor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// overlayRequest matches wlipc.OverlayRequest
type overlayRequest struct {
	Title  string  `json:"title"`
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

func (s *server) readOverlayRequest() *overlayRequest {
	reqPath := filepath.Join(s.getConfigDir(), "overlay-request.json")
	data, err := os.ReadFile(reqPath)
	if err != nil || len(data) == 0 {
		return nil
	}

	var req overlayRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil // Partial write, retry next poll
	}
	os.Remove(reqPath)
	return &req
}

func (s *server) writeDesktopState() {
	configDir := s.getConfigDir()
	statePath := filepath.Join(configDir, "desktop-state.json")

	state := DesktopState{
		Version:  wlipc.IPCStateVersion,
		Current:  s.currentDesk,
		NumDesks: s.numDesks,
		Names:    s.desktopNames,
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	writeAtomic(statePath, data)

	// Also broadcast to socket clients
	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventDesktopState, state)
	}
}

// truncateIPCTitle limits title length to prevent IPC bloat from malicious clients.
// Slices on a UTF-8 rune boundary so the result is always valid UTF-8.
func truncateIPCTitle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Walk back from maxLen to find the start of a UTF-8 sequence
	// (continuation bytes have the form 10xxxxxx).
	cut := maxLen
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}

// writeWindowsState marks the windows state as dirty. The actual write
// happens once per frame in renderOutput, batching rapid successive changes.
func (s *server) writeWindowsState() {
	s.windowsStateDirty = true
}

// flushWindowsState snapshots the window list on the main thread (wlroots
// views are not thread-safe), then hands JSON marshaling + disk I/O off to
// the background ipcFlusher goroutine so the render path is not blocked.
func (s *server) flushWindowsState() {
	var windows []wlipc.WindowInfo

	// Build windows list in correct z-order using scene tree traversal.
	// getViewsInZOrder() returns enabled (visible) views in topmost-first order.
	// The pager iterates in reverse, so wins[0] (topmost) is drawn last (on top).
	zOrder := s.getViewsInZOrder()
	seen := make(map[string]bool, len(zOrder))

	for _, entry := range zOrder {
		if entry.xdg != nil {
			v := entry.xdg
			if !v.mapped && !v.minimized {
				continue
			}
			seen[v.id] = true
			windows = append(windows, s.xdgWindowInfo(v))
		} else if entry.xway != nil {
			v := entry.xway
			if (!v.mapped && !v.minimized) || v.isPanel || v.isOverlay || v.overrideRedirect {
				continue
			}
			seen[v.id] = true
			windows = append(windows, s.xwayWindowInfo(v))
		}
	}

	// Append windows NOT in the z-order list: minimized windows and
	// windows on other desktops (their scene nodes are disabled).
	// Their relative order doesn't matter for the pager since they're
	// filtered by desktop and drawn below visible windows.
	for _, v := range s.xdgViews {
		if seen[v.id] || (!v.mapped && !v.minimized) {
			continue
		}
		windows = append(windows, s.xdgWindowInfo(v))
	}
	for _, v := range s.xwayViews {
		if seen[v.id] || (!v.mapped && !v.minimized) || v.isPanel || v.isOverlay || v.overrideRedirect {
			continue
		}
		windows = append(windows, s.xwayWindowInfo(v))
	}

	state := wlipc.WindowsState{
		Version:   wlipc.IPCStateVersion,
		Windows:   windows,
		Timestamp: time.Now().UnixNano(),
	}

	// Send to background flusher (non-blocking: if flusher is busy,
	// drain the stale state and replace with the latest snapshot).
	select {
	case s.ipcFlushChan <- state:
	default:
		select {
		case <-s.ipcFlushChan:
		default:
		}
		s.ipcFlushChan <- state
	}
}

// startIPCFlusher launches a background goroutine that serializes and writes
// the windows state. This keeps JSON marshal + atomicWriteFile off the render
// thread, eliminating 2-15ms stalls per dirty frame.
func (s *server) startIPCFlusher() {
	s.ipcFlushChan = make(chan wlipc.WindowsState, 1)
	go func() {
		configDir := s.getConfigDir()
		statePath := filepath.Join(configDir, "windows-state.json")
		for st := range s.ipcFlushChan {
			data, err := json.Marshal(st)
			if err != nil {
				continue
			}
			writeAtomic(statePath, data)
			if s.ipcServer != nil {
				s.ipcServer.Broadcast(wlipc.EventWindowsState, st)
			}
		}
	}()
}

// xdgWindowInfo builds a wlipc.WindowInfo for an XDG view.
func (s *server) xdgWindowInfo(v *xdgView) wlipc.WindowInfo {
	title := truncateIPCTitle(v.xdgToplevel.Title(), 1024)
	appID := getXdgToplevelAppID(v.xdgToplevel)
	if appID == "" {
		appID = title
	}
	var parentID string
	if v.parent != nil {
		parentID = v.parent.id
	}
	var vw, vh int
	if v.fullscreen {
		// Report the output size when fullscreen, not the pre-fullscreen configured size
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		if out != nil {
			outGeo := s.getOutputGeometry(out)
			vw, vh = outGeo.width, outGeo.height
		}
	} else if v.configuredW > 0 {
		vw, vh = v.configuredW, v.configuredH
	} else {
		state := v.xdgToplevel.Base().Surface().Current()
		vw, vh = state.Width(), state.Height()
	}
	return wlipc.WindowInfo{
		ID:           v.id,
		Title:        title,
		AppID:        appID,
		Desktop:      v.desk,
		Output:       s.outputNameForPosition(v.x, v.y),
		Focused:      s.activeXdg == v,
		Iconic:       v.minimized,
		Maximized:    v.maximized,
		Fullscreened: v.fullscreen,
		Pinned:       v.pinned,
		Urgent:       v.urgent,
		IsPanel:      false,
		ParentID:     parentID,
		X:            float32(v.x),
		Y:            float32(v.y),
		Width:        float32(vw),
		Height:       float32(vh),
	}
}

// xwayWindowInfo builds a wlipc.WindowInfo for an XWayland view.
func (s *server) xwayWindowInfo(v *xwayView) wlipc.WindowInfo {
	title := truncateIPCTitle(v.surface.Title(), 1024)
	class := getXwaylandSurfaceClass(v.surface)
	if class == "" {
		class = title
	}
	var parentID string
	if v.parent != nil {
		parentID = v.parent.id
	}
	var vw, vh int
	if v.fullscreen {
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		if out != nil {
			outGeo := s.getOutputGeometry(out)
			vw, vh = outGeo.width, outGeo.height
		}
	} else {
		vw, vh = v.surface.Width(), v.surface.Height()
	}
	return wlipc.WindowInfo{
		ID:           v.id,
		Title:        title,
		AppID:        class,
		Desktop:      v.desk,
		Output:       s.outputNameForPosition(v.x, v.y),
		Focused:      s.activeXway == v,
		Iconic:       v.minimized,
		Maximized:    v.maximized,
		Fullscreened: v.fullscreen,
		Pinned:       v.pinned,
		Urgent:       v.urgent,
		IsPanel:      false,
		ParentID:     parentID,
		X:            float32(v.x),
		Y:            float32(v.y),
		Width:        float32(vw),
		Height:       float32(vh),
	}
}

// findWindowByID finds an XDG or XWayland view by its IPC ID (e.g. "xdg-0", "xway-3")
func (s *server) findWindowByID(id string) (*xdgView, *xwayView) {
	for _, v := range s.xdgViews {
		if v.id == id {
			return v, nil
		}
	}
	for _, v := range s.xwayViews {
		if v.id == id {
			return nil, v
		}
	}
	return nil, nil
}

// encodePreview encodes an NRGBA thumbnail as base64 PNG in a JSON response.
func encodePreview(thumb *image.NRGBA, windowID, title string) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, thumb); err != nil {
		return nil, fmt.Errorf("png encode: %w", err)
	}

	bounds := thumb.Bounds()
	resp := struct {
		WindowID string `json:"window_id"`
		Title    string `json:"title"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		PNG      string `json:"png"` // base64-encoded
	}{
		WindowID: windowID,
		Title:    title,
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
		PNG:      base64.StdEncoding.EncodeToString(buf.Bytes()),
	}
	return json.Marshal(resp)
}
