package compositor

/*
#include <wlr/types/wlr_scene.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"fyshos.com/tyde/wlipc"
)

// Screenshots are taken by grim, saved in ~/Pictures, announced to the panel
// and put in the clipboard.

// screenshotPath creates a new, empty file in ~/Pictures for a screenshot,
// named after the time (with a number when there is already one that
// second), and returns its path. Creating it reserves the name: two captures
// in the same second do not get the same file. Remove it if the capture
// fails.
func screenshotPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Pictures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(dir, "screenshot_"+time.Now().Format("2006-01-02_15-04-05"))
	name := base + ".png"
	for i := 2; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		name = fmt.Sprintf("%s_%d.png", base, i)
	}
}

// captureScreen saves a screenshot of region (a grim geometry, "" for every
// screen), then notifies it. It does not wait for grim.
func (s *server) captureScreen(region string) {
	filename, err := screenshotPath()
	if err != nil {
		log.Printf("[SCREENSHOT] no place for it: %v", err)
		return
	}
	s.grim(region, filename, func() {
		log.Printf("[SCREENSHOT] saved to %s", filename)
		s.notifyScreenshot(filename)
	})
}

// captureForText captures region in a temporary file, for the panel to
// read its text and delete it.
func (s *server) captureForText(region string) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "tyde-ocr-*.png")
	if err != nil {
		log.Printf("[SCREENSHOT] no place for the text capture: %v", err)
		return
	}
	filename := f.Name()
	f.Close()
	s.grim(region, filename, func() {
		evt := wlipc.ScreenshotEvent{FilePath: filename, Timestamp: time.Now().UnixMilli(), ForText: true}
		if s.ipcServer == nil || s.ipcServer.Broadcast(wlipc.EventScreenshot, evt) == 0 {
			log.Printf("[SCREENSHOT] no panel to read the text")
			os.Remove(filename)
		}
	})
}

// grim saves region (a grim geometry, "" for every screen) in filename,
// then runs done on the main thread. It does not wait for grim, and
// removes the file if it fails.
func (s *server) grim(region, filename string, done func()) {
	args := []string{filename}
	if region != "" {
		args = []string{"-g", region, filename}
	}
	cmd := exec.Command(findBinary("grim"), args...)
	cmd.Env = safeEnv()
	if err := cmd.Start(); err != nil {
		log.Printf("[SCREENSHOT] grim could not start (apt install grim slurp): %v", err)
		os.Remove(filename)
		return
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("[SCREENSHOT] grim failed: %v", err)
			os.Remove(filename)
			return
		}
		_ = s.enqueueAction(done)
	}()
}

// takeScreenshot captures the screen under the pointer, or lets the user
// pick a region or a window first.
func (s *server) takeScreenshot(regionSelect, windowCapture bool) {
	switch {
	case regionSelect:
		// The overlay lets the user drag a rectangle; finishRegionSelect
		// captures it.
		_ = s.enqueueAction(s.startRegionSelect)
	case windowCapture:
		// The next click captures the window under it.
		_ = s.enqueueAction(s.startWindowPick)
	default:
		_ = s.enqueueAction(func() {
			s.captureScreen(grimGeometry(s.getActiveOutputGeo()))
		})
	}
}

// grimGeometry is the grim -g argument for an area in layout coordinates.
func grimGeometry(g outputGeometry) string {
	return fmt.Sprintf("%d,%d %dx%d", g.x, g.y, g.width, g.height)
}

// startWindowPick enters a mode where the next click captures the clicked window.
func (s *server) startWindowPick() {
	s.windowPickMode = true
	log.Println("[SCREENSHOT] Window pick mode: click a window to capture")
}

// captureClickedWindow captures the window at the given coordinates, with
// its titlebar. Called from the pointer click handler when windowPickMode is
// active.
func (s *server) captureClickedWindow(x, y float64) {
	s.windowPickMode = false
	zone, ok := s.windowRectAt(x, y)
	if !ok {
		log.Println("[SCREENSHOT] No window at click position")
		return
	}
	s.captureScreen(fmt.Sprintf("%d,%d %dx%d", zone.x, zone.y, zone.w, zone.h))
}

// windowRectAt is the window at (x, y), its title bar included; the zone
// selection overlay, when there is one, is looked through.
func (s *server) windowRectAt(x, y float64) (regionRect, bool) {
	if s.regionTree != nil {
		node := &(*C.struct_wlr_scene_tree)(s.regionTree).node
		C.wlr_scene_node_set_enabled(node, false)
		defer C.wlr_scene_node_set_enabled(node, true)
	}
	var (
		vx, vy    float64
		w, h      int
		decorated bool
	)
	switch xdgV, xwayV, _, _, _ := s.viewAt(x, y); {
	case xdgV != nil:
		state := xdgV.xdgToplevel.Base().Surface().Current()
		vx, vy, w, h, decorated = xdgV.x, xdgV.y, state.Width(), state.Height(), xdgV.decorated
	case xwayV != nil && !xwayV.isPanel && !xwayV.isOverlay:
		state := xwayV.surface.Surface().Current()
		vx, vy, w, h, decorated = xwayV.x, xwayV.y, state.Width(), state.Height(), xwayV.decorated
	default:
		return regionRect{}, false
	}
	zone := regionRect{int(vx), int(vy), w, h}
	if decorated {
		zone.y -= titlebarHeight
		zone.h += titlebarHeight
	}
	return zone, w > 0 && h > 0
}

// notifyScreenshot tells the panel, which shows a notification, and copies
// the picture to the clipboard. The event goes through the socket, or a file
// for a panel that polls.
func (s *server) notifyScreenshot(filePath string) {
	evt := wlipc.ScreenshotEvent{FilePath: filePath, Timestamp: time.Now().UnixMilli()}
	if s.ipcServer == nil || s.ipcServer.Broadcast(wlipc.EventScreenshot, evt) == 0 {
		data, _ := json.Marshal(evt)
		writeAtomic(filepath.Join(s.getConfigDir(), "screenshot-event.json"), data)
	}

	// Copy to Wayland clipboard (best-effort)
	go func() {
		cmd := exec.Command(findBinary("wl-copy"), "--type", "image/png")
		f, err := os.Open(filePath)
		if err != nil {
			return
		}
		cmd.Stdin = f
		cmd.Env = safeEnv()
		cmd.Run()
		f.Close()
	}()
}
