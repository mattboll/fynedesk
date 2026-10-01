package ui

import (
	"image/png"
	"os"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"

	"fyshos.com/tyde/wlipc"
)

func TestRecordedTime(t *testing.T) {
	now := time.UnixMilli(100_000)
	st := wlipc.RecordingState{State: wlipc.RecordingActive, ElapsedMs: 60_000, SinceMs: 95_000}
	if got := formatRecorded(recordedTime(st, now)); got != "1:05" {
		t.Errorf("recording: %s", got)
	}
	st = wlipc.RecordingState{State: wlipc.RecordingPaused, ElapsedMs: 3_725_000, SinceMs: 1}
	if got := formatRecorded(recordedTime(st, now)); got != "1:02:05" {
		t.Errorf("paused: %s (the time since the part started does not count)", got)
	}
}

func TestOnRecordingStateForget(t *testing.T) {
	test.NewTempApp(t)
	calls := 0
	forget := onRecordingState(func(wlipc.RecordingState) { calls++ })
	setRecordingState(wlipc.RecordingState{State: wlipc.RecordingActive})
	forget()
	setRecordingState(wlipc.RecordingState{State: wlipc.RecordingIdle})
	if calls != 1 {
		t.Fatalf("%d calls, want 1 before forget", calls)
	}
}

func TestCapturesScreen(t *testing.T) {
	ui := &settingsUI{settings: &deskSettings{cfg: defaultConfig()}}
	page := ui.loadCapturesScreen()
	w := test.NewTempWindow(t, page)
	w.Resize(fyne.NewSize(640, 560))
	if out := os.Getenv("CAPTURES_PAGE"); out != "" { // a look at the page
		c := software.NewCanvas()
		c.SetContent(page)
		c.Resize(fyne.NewSize(640, 560))
		if f, err := os.Create(out); err == nil {
			png.Encode(f, c.Capture())
			f.Close()
		}
	}
}
