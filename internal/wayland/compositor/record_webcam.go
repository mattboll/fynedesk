package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The webcam bubble lets the clicks through to what is below it.
static bool webcam_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
	return false;
}

static struct wlr_scene_buffer *webcam_create(struct wlr_scene_tree *parent, struct pixel_buffer *buf) {
	struct wlr_scene_buffer *node = wlr_scene_buffer_create(parent, &buf->base);
	if (node) {
		node->point_accepts_input = webcam_no_input;
	}
	return node;
}
*/
import "C"

import (
	"fmt"
	"io"
	"log"
	"math"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// While recording with the webcam, the compositor shows it in a round
// bubble at the bottom right of the zone, so that it is in the video:
// ffmpeg reads the camera and sends small square frames, which go
// straight into the bubble; no window to manage.
const (
	webcamFPS    = 15
	webcamMargin = 16 // from the corner of the zone
)

// webcam is the camera shown while recording.
type webcam struct {
	cmd     *exec.Cmd
	node    unsafe.Pointer // *C.struct_wlr_scene_buffer
	buf     unsafe.Pointer // *C.struct_pixel_buffer
	size    int
	pending atomic.Bool // a frame waits for the main thread: the next ones are dropped
	done    atomic.Bool
}

// webcamSize is the diameter of the bubble for a zone h pixels high.
func webcamSize(h int) int {
	return min(max(h/4, 120), 240) &^ 1 // even, for the encoder
}

// webcamArgs are the arguments of ffmpeg sending square, mirrored frames of
// size×size from device: a camera (/dev/video…), or else any input ffmpeg
// reads, played at its pace and looped.
func webcamArgs(device string, size int) []string {
	args := []string{"-loglevel", "error"}
	if strings.HasPrefix(device, "/dev/video") {
		args = append(args, "-f", "v4l2", "-framerate", fmt.Sprint(webcamFPS), "-i", device)
	} else {
		args = append(args, "-re", "-stream_loop", "-1", "-i", device)
	}
	filter := fmt.Sprintf("crop='min(iw,ih)':'min(iw,ih)',scale=%d:%d,hflip,fps=%d", size, size, webcamFPS)
	return append(args, "-an", "-vf", filter, "-pix_fmt", "rgba", "-f", "rawvideo", "pipe:1")
}

// roundMask makes a size×size RGBA frame round, its edge smoothed.
func roundMask(frame []byte, size int) {
	r := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			a := min(max(r-d, 0), 1)
			if a < 1 {
				i := (y*size + x) * 4
				frame[i+3] = uint8(float64(frame[i+3]) * a)
			}
		}
	}
}

// startWebcam shows the camera of the recording at the bottom right of its
// zone.
func (s *server) startWebcam() {
	r := s.rec
	if r == nil || !r.opts.Webcam || s.cam != nil {
		return
	}
	size := webcamSize(r.zone.h)
	cmd := exec.Command(findBinary("ffmpeg"), webcamArgs(r.opts.Camera(), size)...)
	cmd.Env = safeEnv()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[RECORD] the webcam could not be shown: %v", err)
		return
	}
	buf := C.pixel_buffer_create(C.int(size), C.int(size))
	node := C.webcam_create((*C.struct_wlr_scene_tree)(s.cursorAlertTree), buf)
	if buf == nil || node == nil {
		_ = cmd.Process.Kill()
		return
	}
	C.wlr_scene_node_set_position(&node.node,
		C.int(r.zone.x+r.zone.w-size-webcamMargin), C.int(r.zone.y+r.zone.h-size-webcamMargin))
	cam := &webcam{cmd: cmd, node: unsafe.Pointer(node), buf: unsafe.Pointer(buf), size: size}
	s.cam = cam
	go s.readWebcam(cam, out)
}

// readWebcam shows the frames ffmpeg sends, until it ends.
func (s *server) readWebcam(cam *webcam, out io.Reader) {
	frameLen := cam.size * cam.size * 4
	for {
		frame := make([]byte, frameLen)
		if _, err := io.ReadFull(out, frame); err != nil {
			if !cam.done.Load() {
				log.Printf("[RECORD] the webcam stopped: %v", err)
			}
			_ = cam.cmd.Wait()
			return
		}
		if cam.pending.Load() {
			continue // the main thread is behind: this frame goes
		}
		roundMask(frame, cam.size)
		cam.pending.Store(true)
		_ = s.enqueueAction(func() {
			cam.pending.Store(false)
			if s.cam != cam {
				return
			}
			buf := (*C.struct_pixel_buffer)(cam.buf)
			C.pixel_buffer_update(buf, unsafe.Pointer(&frame[0]), C.int(cam.size), C.int(cam.size))
			C.wlr_scene_buffer_set_buffer((*C.struct_wlr_scene_buffer)(cam.node), &buf.base)
		})
	}
}

// stopWebcam takes the camera away.
func (s *server) stopWebcam() {
	cam := s.cam
	if cam == nil {
		return
	}
	s.cam = nil
	cam.done.Store(true)
	_ = cam.cmd.Process.Signal(syscall.SIGTERM)
	C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(cam.node).node)
	C.pixel_buffer_release((*C.struct_pixel_buffer)(cam.buf))
}
