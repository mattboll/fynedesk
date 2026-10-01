package wlipc

import "errors"

// RecorderModule names the panel module of the screen recordings: without
// it, the screen_record action is left unbound.
const RecorderModule = "Screen Recorder"

// RecordingSettings are the defaults of the screen recordings: the
// [recording] section of config.toml. The zero value is the default: no
// sound, no webcam, nothing of the input shown, a countdown, MP4 encoded
// by the GPU when it can, in ~/Videos.
type RecordingSettings struct {
	Microphone  bool `toml:"microphone" json:"microphone"`
	SystemAudio bool `toml:"system_audio" json:"system_audio"` // what the computer plays
	Webcam      bool `toml:"webcam" json:"webcam"`
	// ShowInput draws the clicks and the key combinations on the screen,
	// so that they are in the video.
	ShowInput   bool   `toml:"show_input" json:"show_input"`
	NoCountdown bool   `toml:"no_countdown,omitempty" json:"no_countdown,omitempty"`
	Format      string `toml:"format,omitempty" json:"format,omitempty"` // "mp4" (default) or "webm"
	NoGPU       bool   `toml:"no_gpu,omitempty" json:"no_gpu,omitempty"` // encode on the processor only
	Folder      string `toml:"folder,omitempty" json:"folder,omitempty"` // default ~/Videos
	// TargetSizeMB is the size the editor exports to by default (0: no
	// target), for a chat or a ticket that limits attachments.
	TargetSizeMB int  `toml:"target_size_mb,omitempty" json:"target_size_mb,omitempty"`
	IdleButton   bool `toml:"idle_button" json:"idle_button"` // the record button in the bar when not recording
	// WebcamDevice is the camera shown; default /dev/video0.
	WebcamDevice string `toml:"webcam_device,omitempty" json:"webcam_device,omitempty"`
}

// FileExt is the extension of the recordings: "mp4" or "webm".
func (r RecordingSettings) FileExt() string {
	if r.Format == "webm" {
		return "webm"
	}
	return "mp4"
}

// Camera is the webcam device shown.
func (r RecordingSettings) Camera() string {
	if r.WebcamDevice == "" {
		return "/dev/video0"
	}
	return r.WebcamDevice
}

// The states of the screen recorder.
const (
	RecordingIdle      = "idle"
	RecordingCountdown = "countdown"
	RecordingActive    = "recording"
	RecordingPaused    = "paused"
	RecordingFinishing = "finishing" // the parts are being put together
)

// RecordingState tells the panel what the screen recorder does.
type RecordingState struct {
	State string `json:"state"`
	// ElapsedMs is the time recorded before the current part; SinceMs, when
	// recording, the Unix time (ms) the current part started: the time
	// recorded is ElapsedMs plus the time since SinceMs.
	ElapsedMs int64 `json:"elapsed_ms"`
	SinceMs   int64 `json:"since_ms,omitempty"`
}

// The commands of the screen recorder.
const (
	RecordToggle = "toggle" // choose a zone and record it, or stop
	RecordStop   = "stop"
	RecordPause  = "pause"
	RecordResume = "resume"
)

// RecordingRequest drives the screen recorder.
type RecordingRequest struct {
	Cmd string `json:"cmd"`
}

// RequestRecording sends a command to the screen recorder. Socket only.
func RequestRecording(cmd string) error {
	if trySendRequest(ReqRecording, RecordingRequest{Cmd: cmd}) {
		return nil
	}
	return errors.New("recording: compositor socket unavailable")
}
