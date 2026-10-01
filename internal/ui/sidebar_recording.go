package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
)

// recordingSection is the Recording part of the quick settings: a button
// that records the screen, or stops the recording, and the defaults of the
// recordings, one click away.
func (sb *sidebarPanel) recordingSection() fyne.CanvasObject {
	ds, ok := tyde.Instance().Settings().(*deskSettings)
	if !ok {
		return container.NewVBox()
	}
	label := sectionLabel(locale.T("sidebar.recording"))

	record := widget.NewButtonWithIcon("", theme.MediaRecordIcon(), nil)
	showState := func(st wlipc.RecordingState) {
		if st.State == wlipc.RecordingIdle {
			record.SetText(locale.T("sidebar.recordScreen"))
			record.SetIcon(theme.MediaRecordIcon())
		} else {
			record.SetText(locale.T("sidebar.stopRecording"))
			record.SetIcon(theme.MediaStopIcon())
		}
	}
	record.OnTapped = func() {
		sb.close() // out of the way of the zone to choose
		if recordingState().State == wlipc.RecordingIdle {
			sendRecording(wlipc.RecordToggle)
		} else {
			sendRecording(wlipc.RecordStop)
		}
	}
	showState(recordingState())
	forget := onRecordingState(showState)
	sb.onClosed = append(sb.onClosed, forget)

	r := ds.cfg.Recording
	check := func(text string, on bool, set func(*wlipc.RecordingSettings, bool)) *widget.Check {
		c := widget.NewCheck(text, func(on bool) {
			ds.setRecording(func(r *wlipc.RecordingSettings) { set(r, on) })
		})
		c.Checked = on
		return c
	}
	mic := check(locale.T("record.microphone"), r.Microphone, func(r *wlipc.RecordingSettings, on bool) { r.Microphone = on })
	sound := check(locale.T("record.systemAudio"), r.SystemAudio, func(r *wlipc.RecordingSettings, on bool) { r.SystemAudio = on })
	webcam := check(locale.T("record.webcam"), r.Webcam, func(r *wlipc.RecordingSettings, on bool) { r.Webcam = on })
	input := check(locale.T("record.showInput"), r.ShowInput, func(r *wlipc.RecordingSettings, on bool) { r.ShowInput = on })

	return container.NewVBox(label, record, container.NewGridWithColumns(2, mic, sound, webcam, input))
}
