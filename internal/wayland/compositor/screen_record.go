package compositor

/*
#include <wlr/types/wlr_scene.h>

static const float record_frame_color[4] = {0.9f, 0.1f, 0.1f, 0.9f};

static struct wlr_scene_rect *record_frame_rect(struct wlr_scene_tree *parent) {
	return wlr_scene_rect_create(parent, 0, 0, record_frame_color);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"fyshos.com/tyde/wlipc"
)

// Screen recordings are made by wf-recorder, of a zone or a window chosen
// as for a screenshot, and saved in ~/Videos. The same shortcut stops
// them; meanwhile a red frame surrounds the zone, just outside it so that
// it is not recorded.
const recordFrameWidth = 3

// recordingPath creates a new, empty file in ~/Videos for a recording,
// named after the time, and returns its path.
func recordingPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Videos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(dir, "recording_"+time.Now().Format("2006-01-02_15-04-05"))
	name := base + ".mp4"
	for i := 2; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		name = fmt.Sprintf("%s_%d.mp4", base, i)
	}
}

// toggleRecording stops the recording, or lets the user choose what to
// record.
func (s *server) toggleRecording() {
	if s.recordCmd != nil {
		s.stopRecording()
		return
	}
	s.startRegionSelectFor(regionRecord)
}

// startRecording records zone (layout coordinates) until stopped.
func (s *server) startRecording(zone regionRect) {
	filename, err := recordingPath()
	if err != nil {
		log.Printf("[RECORD] no place for it: %v", err)
		return
	}
	geometry := fmt.Sprintf("%d,%d %dx%d", zone.x, zone.y, zone.w, zone.h)
	// -y: the file is the one created above.
	cmd := exec.Command(findBinary("wf-recorder"), "-y", "-g", geometry, "-f", filename)
	cmd.Env = safeEnv()
	if err := cmd.Start(); err != nil {
		log.Printf("[RECORD] wf-recorder could not start (apt install wf-recorder): %v", err)
		os.Remove(filename)
		return
	}
	log.Printf("[RECORD] recording %s to %s", geometry, filename)
	s.recordCmd, s.recordPath = cmd, filename
	s.showRecordFrame(zone)
	go func() {
		err := cmd.Wait()
		_ = s.enqueueAction(func() { s.recordingEnded(cmd, filename, err) })
	}()
}

// stopRecording asks wf-recorder to finish the file (it does on SIGINT).
func (s *server) stopRecording() {
	if s.recordCmd == nil || s.recordCmd.Process == nil {
		return
	}
	if err := s.recordCmd.Process.Signal(syscall.SIGINT); err != nil {
		log.Printf("[RECORD] stop: %v", err)
	}
}

// recordingEnded forgets the recording cmd made, and tells the panel about
// its file. The file of a recording that failed to start is removed.
func (s *server) recordingEnded(cmd *exec.Cmd, filename string, err error) {
	if s.recordCmd == cmd {
		s.recordCmd, s.recordPath = nil, ""
		s.hideRecordFrame()
	}
	info, statErr := os.Stat(filename)
	if statErr != nil || info.Size() == 0 {
		log.Printf("[RECORD] nothing recorded: %v", err)
		os.Remove(filename)
		return
	}
	log.Printf("[RECORD] saved to %s", filename)
	// Told once the recording no longer counts as a screen share (see
	// screencopyGap): during a share, the panel holds its notifications.
	time.AfterFunc(screencopyGap+300*time.Millisecond, func() {
		_ = s.enqueueAction(func() {
			evt := wlipc.ScreenshotEvent{FilePath: filename, Timestamp: time.Now().UnixMilli(), Video: true}
			if s.ipcServer != nil {
				s.ipcServer.Broadcast(wlipc.EventScreenshot, evt)
			}
		})
	})
}

// showRecordFrame surrounds zone with a red frame, outside it.
func (s *server) showRecordFrame(zone regionRect) {
	tree := C.wlr_scene_tree_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree))
	if tree == nil {
		return
	}
	s.recordFrame = unsafe.Pointer(tree)
	w := recordFrameWidth
	for _, r := range [4][4]int{
		{zone.x - w, zone.y - w, zone.w + 2*w, w},      // top
		{zone.x - w, zone.y + zone.h, zone.w + 2*w, w}, // bottom
		{zone.x - w, zone.y, w, zone.h},                // left
		{zone.x + zone.w, zone.y, w, zone.h},           // right
	} {
		rect := C.record_frame_rect(tree)
		if rect == nil {
			continue
		}
		C.wlr_scene_rect_set_size(rect, C.int(r[2]), C.int(r[3]))
		C.wlr_scene_node_set_position(&rect.node, C.int(r[0]), C.int(r[1]))
	}
}

// hideRecordFrame takes the red frame away.
func (s *server) hideRecordFrame() {
	if s.recordFrame != nil {
		C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_tree)(s.recordFrame).node)
		s.recordFrame = nil
	}
}
