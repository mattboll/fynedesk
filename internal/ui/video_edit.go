package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wm"
)

// The editor of a recording cuts its start and end, and exports it as MP4,
// WebM or GIF, to a size if asked: light enough for a chat or a ticket.

// videoInfo is what the editor needs to know of a recording.
type videoInfo struct {
	duration time.Duration
	audio    bool
}

// probeVideo reads the duration of the video at path, and whether it has
// sound, with ffprobe.
func probeVideo(path string) (videoInfo, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type:format=duration",
		"-of", "json", path).Output()
	if err != nil {
		return videoInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return videoInfo{}, err
	}
	secs, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return videoInfo{}, fmt.Errorf("duration %q: %w", probe.Format.Duration, err)
	}
	info := videoInfo{duration: time.Duration(secs * float64(time.Second))}
	for _, s := range probe.Streams {
		info.audio = info.audio || s.CodecType == "audio"
	}
	return info, nil
}

// frameAt is the picture of the video at path at t.
func frameAt(path string, t time.Duration) (image.Image, error) {
	out, err := exec.Command("ffmpeg", "-loglevel", "error", "-ss", seconds(t), "-i", path,
		"-frames:v", "1", "-f", "image2pipe", "-vcodec", "png", "pipe:1").Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return png.Decode(bytes.NewReader(out))
}

// seconds is t for ffmpeg: seconds, to the millisecond.
func seconds(t time.Duration) string {
	return strconv.FormatFloat(t.Seconds(), 'f', 3, 64)
}

// videoExport is what the editor exports.
type videoExport struct {
	start, end time.Duration
	format     string // "mp4", "webm" or "gif"
	targetMB   int    // 0: no size asked
	audio      bool   // the recording has sound
}

// gifFilter makes a GIF light enough to share: 12 frames a second, at
// most 960 pixels wide, with a palette of its own.
const gifFilter = "fps=12,scale='min(960,iw)':-1:flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse"

// exportArgs are the arguments of ffmpeg exporting in to out as e asks.
func exportArgs(in, out string, e videoExport) []string {
	args := []string{"-y", "-loglevel", "error", "-ss", seconds(e.start), "-to", seconds(e.end), "-i", in}
	if e.format == "gif" {
		return append(args, "-vf", gifFilter, out)
	}
	// The size asked, over the duration, less the sound, is the video's rate.
	rate := ""
	if e.targetMB > 0 {
		kbits := float64(e.targetMB) * 8 * 1024 / (e.end - e.start).Seconds()
		if e.audio {
			kbits -= 128
		}
		rate = fmt.Sprintf("%dk", max(int(kbits*0.95), 100)) // a little margin
	}
	if e.format == "webm" {
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-row-mt", "1")
		if rate != "" {
			args = append(args, "-b:v", rate)
		} else {
			args = append(args, "-crf", "33", "-b:v", "0")
		}
		args = append(args, "-c:a", "libopus")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-movflags", "+faststart")
		if rate != "" {
			args = append(args, "-b:v", rate, "-maxrate", rate, "-bufsize", rate)
		} else {
			args = append(args, "-crf", "23")
		}
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}
	return append(args, out)
}

// uniquePath creates a new, empty file named base+suffix+ext (numbered if
// taken) and returns its path.
func uniquePath(base, suffix, ext string) (string, error) {
	name := base + suffix + ext
	for i := 2; ; i++ {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		name = fmt.Sprintf("%s%s_%d%s", base, suffix, i, ext)
	}
}

// exportVideo exports in as e asks, beside it, and returns the new file.
func exportVideo(ctx context.Context, in string, e videoExport) (string, error) {
	out, err := uniquePath(strings.TrimSuffix(in, filepath.Ext(in)), "_edit", "."+e.format)
	if err != nil {
		return "", err
	}
	if b, err := exec.CommandContext(ctx, "ffmpeg", exportArgs(in, out, e)...).CombinedOutput(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(b)))
	}
	return out, nil
}

// formatClock shows a time in a video as 1:05.3.
func formatClock(t time.Duration) string {
	tenths := int(t.Round(100*time.Millisecond) / (100 * time.Millisecond))
	return fmt.Sprintf("%d:%02d.%d", tenths/600, tenths/10%60, tenths%10)
}

// showVideoEditor opens the recording at path in the editor. Fyne thread.
func showVideoEditor(path string) {
	info, err := probeVideo(path)
	if err != nil {
		log.Printf("[EDIT] %v", err)
		return
	}
	w := fyne.CurrentApp().NewWindow(locale.T("edit.title") + " — " + filepath.Base(path))

	preview := canvas.NewImageFromImage(nil)
	preview.FillMode = canvas.ImageFillContain
	preview.SetMinSize(fyne.NewSize(640, 360))
	var shown time.Duration = -1
	var debounce *time.Timer
	showFrame := func(t time.Duration) {
		if t == shown {
			return
		}
		shown = t
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(150*time.Millisecond, func() {
			img, err := frameAt(path, t)
			if err != nil {
				log.Printf("[EDIT] %v", err)
				return
			}
			fyne.Do(func() {
				preview.Image = img
				preview.Refresh()
			})
		})
	}

	total := info.duration.Seconds()
	startLabel, endLabel := widget.NewLabel(""), widget.NewLabel("")
	start := widget.NewSlider(0, total)
	end := widget.NewSlider(0, total)
	start.Step, end.Step = 0.1, 0.1
	end.Value = total
	update := func() {
		startLabel.SetText(locale.T("edit.start") + " " + formatClock(time.Duration(start.Value*float64(time.Second))))
		endLabel.SetText(locale.T("edit.end") + " " + formatClock(time.Duration(end.Value*float64(time.Second))))
	}
	start.OnChanged = func(v float64) {
		if v > end.Value-0.5 {
			start.SetValue(max(end.Value-0.5, 0))
			return
		}
		update()
		showFrame(time.Duration(v * float64(time.Second)))
	}
	end.OnChanged = func(v float64) {
		if v < start.Value+0.5 {
			end.SetValue(min(start.Value+0.5, total))
			return
		}
		update()
		showFrame(time.Duration(v * float64(time.Second)))
	}
	update()
	showFrame(0)

	format := widget.NewRadioGroup([]string{"MP4", "WebM", "GIF"}, nil)
	format.Horizontal = true
	format.Required = true
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webm":
		format.SetSelected("WebM")
	default:
		format.SetSelected("MP4")
	}
	target := widget.NewEntry()
	target.SetPlaceHolder("0")
	if r := recordingSettings(); r.TargetSizeMB > 0 {
		target.SetText(strconv.Itoa(r.TargetSizeMB))
	}

	progress := widget.NewProgressBarInfinite()
	progress.Hide()
	var export *widget.Button
	export = &widget.Button{Text: locale.T("edit.export"), Importance: widget.HighImportance, OnTapped: func() {
		size, _ := strconv.Atoi(strings.TrimSpace(target.Text))
		e := videoExport{
			start:    time.Duration(start.Value * float64(time.Second)),
			end:      time.Duration(end.Value * float64(time.Second)),
			format:   strings.ToLower(format.Selected),
			targetMB: max(size, 0),
			audio:    info.audio,
		}
		export.Disable()
		progress.Show()
		progress.Start()
		go func() {
			out, err := exportVideo(context.Background(), path, e)
			fyne.Do(func() {
				progress.Stop()
				progress.Hide()
				export.Enable()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				w.Close()
				wm.SendNotification(recordingNotification(out))
			})
		}()
	}}

	form := widget.NewForm(
		widget.NewFormItem(locale.T("captures.format"), format),
		widget.NewFormItem(locale.T("edit.targetSize"), target),
	)
	cuts := container.NewGridWithColumns(2,
		container.NewBorder(nil, nil, nil, nil, container.NewVBox(startLabel, start)),
		container.NewBorder(nil, nil, nil, nil, container.NewVBox(endLabel, end)),
	)
	bottom := container.NewVBox(cuts, form, progress, container.NewHBox(layout.NewSpacer(), export))
	w.SetContent(container.NewBorder(nil, container.NewPadded(bottom), nil, nil, preview))
	w.Resize(fyne.NewSize(760, 640))
	w.CenterOnScreen()
	w.Show()
}
