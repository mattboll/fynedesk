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
	"strings"
	"syscall"
	"time"
	"unsafe"

	"fyshos.com/tyde/wlipc"
)

// Screen recordings are made by wf-recorder, of a zone or a window chosen
// as for a screenshot, with the defaults of the [recording] settings. The
// same shortcut stops them; meanwhile a red frame surrounds the zone, just
// outside it so that it is not recorded. A pause ends the current part,
// resuming starts another: on stop, the parts are put together, without
// encoding them again, in ~/Videos.
const recordFrameWidth = 3

// gpuFailureWindow: a recorder ending this soon after it started, with an
// error, failed to encode on the GPU; the recording goes on, encoded on the
// processor.
const gpuFailureWindow = 3 * time.Second

// recording is a screen recording under way.
type recording struct {
	zone    regionRect
	opts    wlipc.RecordingSettings
	dir     string   // where the parts go
	parts   []string // the video parts recorded
	mics    []string // the microphone parts, recorded aside when the computer's sound is too
	cmd     *exec.Cmd
	micCmd  *exec.Cmd
	state   string
	elapsed time.Duration // recorded before the current part
	since   time.Time     // the current part started
	noGPU   bool          // the GPU failed to encode: the processor does
	stop    bool          // stop once the current part ends
}

// recordingsDir is the folder of the recordings.
func recordingsDir(opts wlipc.RecordingSettings) (string, error) {
	if opts.Folder != "" {
		return opts.Folder, os.MkdirAll(opts.Folder, 0o755)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Videos")
	return dir, os.MkdirAll(dir, 0o755)
}

// recordingPath creates a new, empty file in dir for a recording with the
// extension ext, named after the time, and returns its path.
func recordingPath(dir, ext string) (string, error) {
	base := filepath.Join(dir, "recording_"+time.Now().Format("2006-01-02_15-04-05"))
	name := base + "." + ext
	for i := 2; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		name = fmt.Sprintf("%s_%d.%s", base, i, ext)
	}
}

// renderNode is the GPU that encodes: one that VA-API drives (Intel, AMD),
// not an NVIDIA one; "" when there is none.
func renderNode() string {
	nodes, _ := filepath.Glob("/sys/class/drm/renderD*")
	for _, n := range nodes {
		driver, err := os.Readlink(filepath.Join(n, "device", "driver"))
		if err != nil {
			continue
		}
		switch filepath.Base(driver) {
		case "i915", "xe", "amdgpu", "radeon":
			return "/dev/dri/" + filepath.Base(n)
		}
	}
	return ""
}

// recorderArgs are the arguments of wf-recorder recording zone in file,
// encoded on the GPU at node when it is not "".
func recorderArgs(opts wlipc.RecordingSettings, zone regionRect, file, node string) []string {
	args := []string{"-y", "-g", fmt.Sprintf("%d,%d %dx%d", zone.x, zone.y, zone.w, zone.h), "-f", file}
	webm := opts.FileExt() == "webm"
	switch {
	case node != "" && webm:
		args = append(args, "-c", "vp9_vaapi", "-d", node)
	case node != "":
		args = append(args, "-c", "h264_vaapi", "-d", node)
	case webm:
		args = append(args, "-c", "libvpx-vp9", "-x", "yuv420p",
			"-p", "deadline=realtime", "-p", "cpu-used=8", "-p", "row-mt=1")
	default:
		args = append(args, "-c", "libx264", "-x", "yuv420p", "-p", "preset=veryfast", "-p", "crf=23")
	}
	// One sound with the video: the computer's, or else the microphone's
	// (with both, the microphone is recorded aside).
	switch {
	case opts.SystemAudio:
		args = append(args, "--audio=@DEFAULT_MONITOR@")
	case opts.Microphone:
		args = append(args, "--audio=@DEFAULT_SOURCE@")
	}
	if opts.SystemAudio || opts.Microphone {
		if webm {
			args = append(args, "-C", "libopus")
		} else {
			args = append(args, "-C", "aac")
		}
	}
	return args
}

// micArgs are the arguments of ffmpeg recording the microphone in file.
func micArgs(opts wlipc.RecordingSettings, file string) []string {
	codec := "aac"
	if opts.FileExt() == "webm" {
		codec = "libopus"
	}
	return []string{"-y", "-loglevel", "error", "-f", "pulse", "-i", "@DEFAULT_SOURCE@", "-c:a", codec, file}
}

// toggleRecording stops the recording, or lets the user choose what to
// record.
func (s *server) toggleRecording() {
	if s.rec != nil {
		s.stopRecording()
		return
	}
	s.startRegionSelectFor(regionRecord)
}

// beginRecording records zone (layout coordinates) with opts, after the
// countdown.
func (s *server) beginRecording(zone regionRect, opts wlipc.RecordingSettings) {
	if s.rec != nil {
		return
	}
	base, err := recordingsDir(opts)
	if err != nil {
		log.Printf("[RECORD] no place for it: %v", err)
		return
	}
	dir, err := os.MkdirTemp(base, ".recording-")
	if err != nil {
		log.Printf("[RECORD] no place for its parts: %v", err)
		return
	}
	log.Printf("[RECORD] options: microphone %v, sound %v, webcam %v, clicks and keys %v, %s",
		opts.Microphone, opts.SystemAudio, opts.Webcam, opts.ShowInput, opts.FileExt())
	s.rec = &recording{zone: zone, opts: opts, dir: dir, state: wlipc.RecordingCountdown}
	s.showRecordFrame(zone)
	start := func() { s.startPart() }
	if opts.NoCountdown {
		start()
		return
	}
	s.broadcastRecording()
	s.startCountdown(zone.x+(zone.w-countdownSize)/2, zone.y+(zone.h-countdownSize)/2, false, func() {
		// The countdown must be gone from the screen first.
		time.AfterFunc(150*time.Millisecond, func() { _ = s.enqueueAction(start) })
	})
}

// startPart starts recording a new part.
func (s *server) startPart() {
	r := s.rec
	if r == nil {
		return
	}
	n := len(r.parts) + 1
	part := filepath.Join(r.dir, fmt.Sprintf("part-%03d.%s", n, r.opts.FileExt()))
	node := ""
	if !r.opts.NoGPU && !r.noGPU {
		node = renderNode()
	}
	cmd := exec.Command(findBinary("wf-recorder"), recorderArgs(r.opts, r.zone, part, node)...)
	cmd.Env = safeEnv()
	if err := cmd.Start(); err != nil {
		log.Printf("[RECORD] wf-recorder could not start (apt install wf-recorder): %v", err)
		s.endRecording()
		return
	}
	r.cmd, r.state, r.since = cmd, wlipc.RecordingActive, time.Now()
	r.micCmd = nil
	if r.opts.Microphone && r.opts.SystemAudio {
		mic := filepath.Join(r.dir, fmt.Sprintf("mic-%03d.%s", n, micExt(r.opts)))
		r.micCmd = exec.Command(findBinary("ffmpeg"), micArgs(r.opts, mic)...)
		r.micCmd.Env = safeEnv()
		if err := r.micCmd.Start(); err != nil {
			log.Printf("[RECORD] the microphone could not be recorded: %v", err)
			r.micCmd = nil
		} else {
			r.mics = append(r.mics, mic)
		}
	}
	log.Printf("[RECORD] part %d of %d,%d %dx%d (gpu %q)", n, r.zone.x, r.zone.y, r.zone.w, r.zone.h, node)
	s.broadcastRecording()
	started, gpu := r.since, node != ""
	go func() {
		err := cmd.Wait()
		_ = s.enqueueAction(func() { s.partEnded(cmd, part, err, gpu, started) })
	}()
}

// micExt is the extension of the microphone parts.
func micExt(opts wlipc.RecordingSettings) string {
	if opts.FileExt() == "webm" {
		return "ogg"
	}
	return "m4a"
}

// partEnded records the part cmd made, then resumes, stays paused or
// finishes, as asked.
func (s *server) partEnded(cmd *exec.Cmd, part string, err error, gpu bool, started time.Time) {
	r := s.rec
	if r == nil || r.cmd != cmd {
		return
	}
	r.cmd = nil
	s.stopMic()
	if info, statErr := os.Stat(part); statErr == nil && info.Size() > 0 {
		r.parts = append(r.parts, part)
	} else if gpu && err != nil && time.Since(started) < gpuFailureWindow && !r.stop && r.state == wlipc.RecordingActive {
		log.Printf("[RECORD] the GPU could not encode (%v): the processor does", err)
		r.noGPU = true
		os.Remove(part)
		if n := len(r.mics); n > 0 { // the microphone part goes with it
			os.Remove(r.mics[n-1])
			r.mics = r.mics[:n-1]
		}
		s.startPart()
		return
	} else {
		log.Printf("[RECORD] nothing recorded in %s: %v", part, err)
	}
	switch {
	case r.stop:
		s.finishRecording()
	case r.state == wlipc.RecordingActive:
		// wf-recorder ended by itself (a screen went away): keep what
		// there is.
		log.Printf("[RECORD] the recorder ended: %v", err)
		s.finishRecording()
	}
}

// stopMic ends the microphone part.
func (s *server) stopMic() {
	if r := s.rec; r != nil && r.micCmd != nil {
		_ = r.micCmd.Process.Signal(syscall.SIGINT)
		mic := r.micCmd
		r.micCmd = nil
		go func() { _ = mic.Wait() }()
	}
}

// pauseRecording ends the current part, until resumed.
func (s *server) pauseRecording() {
	r := s.rec
	if r == nil || r.state != wlipc.RecordingActive || r.cmd == nil {
		return
	}
	r.elapsed += time.Since(r.since)
	r.state = wlipc.RecordingPaused
	_ = r.cmd.Process.Signal(syscall.SIGINT) // wf-recorder finishes the file
	s.broadcastRecording()
}

// resumeRecording starts a new part.
func (s *server) resumeRecording() {
	if r := s.rec; r != nil && r.state == wlipc.RecordingPaused && r.cmd == nil {
		s.startPart()
	}
}

// stopRecording finishes the recording, or cancels it before it started.
func (s *server) stopRecording() {
	r := s.rec
	if r == nil {
		return
	}
	switch {
	case r.state == wlipc.RecordingCountdown:
		s.dropCountdown()
		s.endRecording()
	case r.cmd != nil: // recording, or paused but the part still ending
		if r.state == wlipc.RecordingActive {
			r.elapsed += time.Since(r.since)
		}
		r.stop, r.state = true, wlipc.RecordingFinishing
		_ = r.cmd.Process.Signal(syscall.SIGINT)
		s.broadcastRecording()
	default: // paused
		s.finishRecording()
	}
}

// finishRecording puts the parts together in the recording's file, then
// tells the panel about it.
func (s *server) finishRecording() {
	r := s.rec
	r.state = wlipc.RecordingFinishing
	s.hideRecordFrame()
	s.broadcastRecording()
	go func() {
		out, err := assembleRecording(r)
		_ = s.enqueueAction(func() {
			s.endRecording()
			if err != nil {
				log.Printf("[RECORD] %v (the parts are kept in %s)", err, r.dir)
				return
			}
			os.RemoveAll(r.dir)
			log.Printf("[RECORD] saved to %s", out)
			// Told once the recording no longer counts as a screen share
			// (see screencopyGap): during a share, the panel holds its
			// notifications.
			time.AfterFunc(screencopyGap+300*time.Millisecond, func() {
				_ = s.enqueueAction(func() {
					evt := wlipc.ScreenshotEvent{FilePath: out, Timestamp: time.Now().UnixMilli(), Video: true}
					if s.ipcServer != nil {
						s.ipcServer.Broadcast(wlipc.EventScreenshot, evt)
					}
				})
			})
		})
	}()
}

// assembleRecording puts the parts of r together in a new file of the
// recordings folder: copied when there is one, joined without encoding
// them again when there are more, the microphone mixed in when it was
// recorded aside.
func assembleRecording(r *recording) (string, error) {
	if len(r.parts) == 0 {
		return "", errors.New("nothing was recorded")
	}
	base := filepath.Dir(r.dir)
	out, err := recordingPath(base, r.opts.FileExt())
	if err != nil {
		return "", err
	}
	video, err := joinParts(r.dir, r.parts, "video."+r.opts.FileExt())
	if err != nil {
		os.Remove(out)
		return "", err
	}
	if len(r.mics) == 0 {
		return out, os.Rename(video, out)
	}
	mic, err := joinParts(r.dir, r.mics, "mic."+micExt(r.opts))
	if err != nil {
		os.Remove(out)
		return "", err
	}
	codec := "aac"
	if r.opts.FileExt() == "webm" {
		codec = "libopus"
	}
	// The computer's sound and the microphone, mixed; the video as it is.
	cmd := exec.Command(findBinary("ffmpeg"), "-y", "-loglevel", "error", "-i", video, "-i", mic,
		"-filter_complex", "[0:a][1:a]amix=inputs=2:duration=first:normalize=0[a]",
		"-map", "0:v", "-map", "[a]", "-c:v", "copy", "-c:a", codec, out)
	if b, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("mixing the microphone: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// joinParts joins parts, in dir, into name: the part itself when there is
// one, else without encoding them again.
func joinParts(dir string, parts []string, name string) (string, error) {
	if len(parts) == 1 {
		return parts[0], nil
	}
	var list strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&list, "file '%s'\n", strings.ReplaceAll(p, "'", `'\''`))
	}
	listFile := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(listFile, []byte(list.String()), 0o600); err != nil {
		return "", err
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command(findBinary("ffmpeg"), "-y", "-loglevel", "error", "-f", "concat", "-safe", "0",
		"-i", listFile, "-c", "copy", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("joining the parts: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// endRecording forgets the recording.
func (s *server) endRecording() {
	if s.rec == nil {
		return
	}
	if s.rec.state == wlipc.RecordingCountdown || len(s.rec.parts) == 0 && s.rec.cmd == nil && !s.rec.stop {
		os.RemoveAll(s.rec.dir) // nothing was recorded
	}
	s.rec = nil
	s.hideRecordFrame()
	s.dropKeyBubble()
	s.broadcastRecording()
}

// broadcastRecording tells the panel what the recorder does.
func (s *server) broadcastRecording() {
	if s.ipcServer == nil {
		return
	}
	s.ipcServer.Broadcast(wlipc.EventRecording, s.recordingState())
}

// recordingState is what the recorder does.
func (s *server) recordingState() wlipc.RecordingState {
	r := s.rec
	if r == nil {
		return wlipc.RecordingState{State: wlipc.RecordingIdle}
	}
	st := wlipc.RecordingState{State: r.state, ElapsedMs: r.elapsed.Milliseconds()}
	if r.state == wlipc.RecordingActive {
		st.SinceMs = r.since.UnixMilli()
	}
	return st
}

// showRecordFrame surrounds zone with a red frame, outside it.
func (s *server) showRecordFrame(zone regionRect) {
	s.hideRecordFrame()
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
