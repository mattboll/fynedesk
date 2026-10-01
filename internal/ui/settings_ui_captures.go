package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

// loadCapturesScreen sets the defaults of the screen recordings: what they
// record, how they are encoded and where they go.
func (d *settingsUI) loadCapturesScreen() fyne.CanvasObject {
	r := d.settings.cfg.Recording

	check := func(key string, on bool) *widget.Check {
		c := widget.NewCheck(locale.T(key), nil)
		c.Checked = on
		return c
	}
	mic := check("record.microphone", r.Microphone)
	sound := check("record.systemAudio", r.SystemAudio)
	webcam := check("record.webcam", r.Webcam)
	input := check("record.showInput", r.ShowInput)

	format := widget.NewRadioGroup([]string{"MP4", "WebM"}, nil)
	format.Horizontal = true
	format.Required = true
	if r.FileExt() == "mp4" {
		format.SetSelected("MP4")
	} else {
		format.SetSelected("WebM")
	}
	gpu := check("captures.gpu", !r.NoGPU)
	countdown := check("captures.countdown", !r.NoCountdown)
	idle := check("captures.idleButton", r.IdleButton)

	folder := widget.NewEntry()
	folder.SetPlaceHolder("~/Videos")
	folder.SetText(r.Folder)
	camera := widget.NewEntry()
	camera.SetPlaceHolder("/dev/video0")
	camera.SetText(r.WebcamDevice)
	target := widget.NewEntry()
	target.SetPlaceHolder("0")
	if r.TargetSizeMB > 0 {
		target.SetText(strconv.Itoa(r.TargetSizeMB))
	}
	target.Validator = func(s string) error {
		if s == "" {
			return nil
		}
		_, err := strconv.ParseUint(s, 10, 16)
		return err
	}

	recordCard := widget.NewCard(locale.T("captures.recording"), locale.T("captures.recordingHint"), container.NewVBox(
		container.NewGridWithColumns(2, mic, sound, webcam, input),
		widget.NewForm(
			widget.NewFormItem(locale.T("captures.format"), format),
			widget.NewFormItem(locale.T("captures.folder"), folder),
			widget.NewFormItem(locale.T("captures.camera"), camera),
			widget.NewFormItem(locale.T("captures.targetSize"), target),
		),
		gpu, countdown, idle,
	))

	apply := &widget.Button{Text: locale.T("settings.apply"), Importance: widget.HighImportance, OnTapped: func() {
		size, _ := strconv.Atoi(target.Text)
		d.settings.setRecording(func(r *wlipc.RecordingSettings) {
			r.Microphone, r.SystemAudio, r.Webcam, r.ShowInput = mic.Checked, sound.Checked, webcam.Checked, input.Checked
			r.Format = strings.ToLower(format.Selected)
			r.NoGPU, r.NoCountdown, r.IdleButton = !gpu.Checked, !countdown.Checked, idle.Checked
			r.Folder = expandHome(strings.TrimSpace(folder.Text))
			r.WebcamDevice = strings.TrimSpace(camera.Text)
			r.TargetSizeMB = max(size, 0)
		})
		wm.SendNotification(wm.NewNotification(locale.T("settings.settings"), locale.T("notif.applied")))
	}}

	return container.NewBorder(nil, container.NewHBox(layout.NewSpacer(), apply), nil, nil,
		container.NewVScroll(recordCard))
}

// expandHome turns a leading ~ into the home folder.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
