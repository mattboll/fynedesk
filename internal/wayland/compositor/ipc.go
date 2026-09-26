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

	"fyshos.com/tyde/wlipc"
)

func (s *server) readOverlayRequest() *wlipc.OverlayRequest {
	reqPath := filepath.Join(s.getConfigDir(), "overlay-request.json")
	data, err := os.ReadFile(reqPath)
	if err != nil || len(data) == 0 {
		return nil
	}

	var req wlipc.OverlayRequest
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
	if !s.windowsStateDirty {
		// It is written on the next frame: make sure there is one (a quiet
		// desktop renders none).
		s.scheduleAllOutputFrames()
	}
	s.windowsStateDirty = true
	s.windowsMoved()
}

// flushWindowsState snapshots the window list on the main thread (wlroots
// views are not thread-safe), then hands JSON marshaling + disk I/O off to
// the background ipcFlusher goroutine so the render path is not blocked.
func (s *server) flushWindowsState() {
	state := s.buildWindowsState()

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
	resp := wlipc.WindowPreview{
		WindowID: windowID,
		Title:    title,
		Width:    bounds.Dx(),
		Height:   bounds.Dy(),
		PNG:      base64.StdEncoding.EncodeToString(buf.Bytes()),
	}
	return json.Marshal(resp)
}
