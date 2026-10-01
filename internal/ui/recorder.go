package ui

import (
	"fmt"
	"image/color"
	"log"
	"os/exec"
	"slices"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

func init() {
	tyde.RegisterModule(recorderModuleMeta)
}

// recorderModuleMeta describes the Screen Recorder module: Super+Shift+R,
// its indicator in the bar and its quick settings. It is offered once
// wf-recorder is found.
var recorderModuleMeta = tyde.ModuleMetadata{
	Name:        wlipc.RecorderModule,
	NewInstance: newRecorderModule,
}

// recorderModule shows the screen recorder while it is enabled.
type recorderModule struct {
	indicator *recorderIndicator
}

func newRecorderModule() tyde.Module {
	return &recorderModule{}
}

func (m *recorderModule) Metadata() tyde.ModuleMetadata {
	return recorderModuleMeta
}

func (m *recorderModule) Destroy() {
	if m.indicator != nil {
		m.indicator.stop()
	}
}

// StatusAreaWidget is the indicator of the recordings in the bar.
func (m *recorderModule) StatusAreaWidget() fyne.CanvasObject {
	if !wlipc.IsWaylandSession() {
		return nil
	}
	m.indicator = newRecorderIndicator()
	return m.indicator.root
}

// recorderInstalled reports whether wf-recorder, which records, is there.
func recorderInstalled() bool {
	_, err := exec.LookPath("wf-recorder")
	return err == nil
}

// recorderEnabled reports whether the Screen Recorder module is enabled.
func recorderEnabled() bool {
	d := tyde.Instance()
	return d != nil && slices.Contains(d.Settings().ModuleNames(), wlipc.RecorderModule)
}

// recorder holds what the compositor last said of the recordings, for the
// indicator and the quick settings.
var recorder struct {
	mu        sync.Mutex
	state     wlipc.RecordingState
	listeners map[int]func(wlipc.RecordingState)
	nextID    int
}

// setRecordingState records what the compositor says of the recordings
// and tells the listeners, on the Fyne thread.
func setRecordingState(st wlipc.RecordingState) {
	recorder.mu.Lock()
	recorder.state = st
	listeners := make([]func(wlipc.RecordingState), 0, len(recorder.listeners))
	for _, l := range recorder.listeners {
		listeners = append(listeners, l)
	}
	recorder.mu.Unlock()
	fyne.Do(func() {
		for _, l := range listeners {
			l(st)
		}
	})
}

// recordingState is what the compositor last said of the recordings.
func recordingState() wlipc.RecordingState {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.state.State == "" {
		return wlipc.RecordingState{State: wlipc.RecordingIdle}
	}
	return recorder.state
}

// onRecordingState calls l with each new state of the recordings, until
// the function it returns is called.
func onRecordingState(l func(wlipc.RecordingState)) (forget func()) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.listeners == nil {
		recorder.listeners = map[int]func(wlipc.RecordingState){}
	}
	id := recorder.nextID
	recorder.nextID++
	recorder.listeners[id] = l
	return func() {
		recorder.mu.Lock()
		delete(recorder.listeners, id)
		recorder.mu.Unlock()
	}
}

// recordedTime is how long st has recorded, at now.
func recordedTime(st wlipc.RecordingState, now time.Time) time.Duration {
	d := time.Duration(st.ElapsedMs) * time.Millisecond
	if st.State == wlipc.RecordingActive && st.SinceMs > 0 {
		d += now.Sub(time.UnixMilli(st.SinceMs))
	}
	return max(d, 0)
}

// formatRecorded shows a recorded time as 1:05 or 1:02:05.
func formatRecorded(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// sendRecording sends a command to the recorder, off the Fyne thread.
func sendRecording(cmd string) {
	go func() {
		if err := wlipc.RequestRecording(cmd); err != nil {
			log.Printf("[RECORD] %v", err)
		}
	}()
}

// recorderIndicator shows in the bar that the screen is being recorded,
// for how long, and pauses or stops it; when not recording, it shows the
// record button if the settings ask for it.
type recorderIndicator struct {
	root    *fyne.Container
	button  *widget.Button // the dot: stops, or starts when idle
	details *fyne.Container
	clock   *canvas.Text
	pause   *widget.Button
	ticker  *time.Ticker
	done    chan struct{}
	blink   bool
}

func newRecorderIndicator() *recorderIndicator {
	r := &recorderIndicator{done: make(chan struct{})}
	r.button = widget.NewButtonWithIcon("", theme.NewErrorThemedResource(theme.MediaRecordIcon()), func() {
		if recordingState().State == wlipc.RecordingIdle {
			sendRecording(wlipc.RecordToggle)
		} else {
			sendRecording(wlipc.RecordStop)
		}
	})
	r.button.Importance = widget.LowImportance
	r.clock = canvas.NewText("", color.NRGBA{R: 0xE5, G: 0x3B, B: 0x3B, A: 0xFF})
	r.clock.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	r.pause = widget.NewButtonWithIcon("", theme.MediaPauseIcon(), func() {
		if recordingState().State == wlipc.RecordingPaused {
			sendRecording(wlipc.RecordResume)
		} else {
			sendRecording(wlipc.RecordPause)
		}
	})
	r.pause.Importance = widget.LowImportance
	stop := widget.NewButtonWithIcon("", theme.MediaStopIcon(), func() { sendRecording(wlipc.RecordStop) })
	stop.Importance = widget.LowImportance
	r.details = container.NewHBox(container.NewCenter(r.clock), layout.NewSpacer(), r.pause, stop)
	r.root = container.New(&recorderNarrow{}, r.button, r.details)

	onRecordingState(r.show)
	if d := tyde.Instance(); d != nil {
		d.Settings().AddChangeListener(func(tyde.DeskSettings) { r.show(recordingState()) })
	}
	r.ticker = time.NewTicker(500 * time.Millisecond)
	go func() {
		for {
			select {
			case <-r.done:
				return
			case <-r.ticker.C:
				fyne.Do(r.tick)
			}
		}
	}()
	r.show(recordingState())
	return r
}

// show shows st.
func (r *recorderIndicator) show(st wlipc.RecordingState) {
	idle := st.State == wlipc.RecordingIdle
	r.details.Hidden = idle
	switch {
	case idle && !idleButtonWanted():
		r.root.Hide()
		return
	case idle:
		r.button.SetIcon(theme.MediaRecordIcon())
	default:
		r.button.SetIcon(theme.NewErrorThemedResource(theme.MediaRecordIcon()))
	}
	if st.State == wlipc.RecordingPaused {
		r.pause.SetIcon(theme.MediaPlayIcon())
	} else {
		r.pause.SetIcon(theme.MediaPauseIcon())
	}
	r.pause.Hidden = st.State != wlipc.RecordingActive && st.State != wlipc.RecordingPaused
	r.root.Show()
	r.tick()
	r.root.Refresh()
}

// tick updates the time recorded, and blinks the dot while recording.
func (r *recorderIndicator) tick() {
	st := recordingState()
	switch st.State {
	case wlipc.RecordingIdle:
		return
	case wlipc.RecordingCountdown:
		r.clock.Text = locale.T("record.ready")
	case wlipc.RecordingFinishing:
		r.clock.Text = locale.T("record.finishing")
	default:
		r.clock.Text = formatRecorded(recordedTime(st, time.Now()))
	}
	r.clock.Refresh()
	r.blink = !r.blink && st.State == wlipc.RecordingActive
	if r.blink {
		r.button.SetIcon(theme.MediaRecordIcon())
	} else {
		r.button.SetIcon(theme.NewErrorThemedResource(theme.MediaRecordIcon()))
	}
}

func (r *recorderIndicator) stop() {
	r.ticker.Stop()
	close(r.done)
}

// idleButtonWanted reports whether the settings ask for the record button
// in the bar when not recording.
func idleButtonWanted() bool {
	if ds, ok := tyde.Instance().Settings().(*deskSettings); ok {
		return ds.cfg.Recording.IdleButton
	}
	return false
}

// recorderNarrow lays out the dot and, when the bar is wide, the details.
type recorderNarrow struct{}

func (recorderNarrow) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Resize(fyne.NewSize(size.Height, size.Height))
	objects[0].Move(fyne.Position{})
	objects[1].Resize(fyne.NewSize(size.Width-size.Height-theme.Padding(), size.Height))
	objects[1].Move(fyne.NewPos(size.Height+theme.Padding(), 0))
	if tyde.Instance() != nil && tyde.Instance().Settings().NarrowWidgetPanel() {
		objects[1].Hide()
	}
}

func (recorderNarrow) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(36, 36)
}
