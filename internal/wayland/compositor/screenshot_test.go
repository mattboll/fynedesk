package compositor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyshos.com/tyde/wlipc"
)

func TestScreenshotPathReservesTheName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seen := map[string]bool{}
	for range 3 {
		name, err := screenshotPath()
		if err != nil {
			t.Fatal(err)
		}
		if seen[name] {
			t.Fatalf("%s given twice", filepath.Base(name))
		}
		seen[name] = true
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s not created: %v", filepath.Base(name), err)
		}
	}
}

func TestDragRect(t *testing.T) {
	want := regionRect{x: 10, y: 20, w: 90, h: 60}
	for _, c := range [][4]float64{{10, 20, 100, 80}, {100, 80, 10, 20}, {100, 20, 10, 80}} {
		if got := dragRect(c[0], c[1], c[2], c[3]); got != want {
			t.Errorf("%v: got %+v, want %+v", c, got, want)
		}
	}
}

func TestCountdownImage(t *testing.T) {
	img := countdownImage(3, fontFaceOfSize(countdownFontSize))
	if b := img.Bounds(); b.Dx() != countdownSize || b.Dy() != countdownSize {
		t.Fatalf("size %v", b)
	}
	if img.NRGBAAt(0, 0).A != 0 {
		t.Error("the corners should be clear")
	}
	light := 0 // the digit, white on the dark disc
	for y := 0; y < countdownSize; y++ {
		for x := 0; x < countdownSize; x++ {
			if c := img.NRGBAAt(x, y); c.R > 200 && c.A > 200 {
				light++
			}
		}
	}
	if fontFaceOfSize(countdownFontSize) != nil && light == 0 {
		t.Error("the digit is not drawn")
	}
}

func TestRecordingPathReservesTheName(t *testing.T) {
	dir := t.TempDir()
	a, err := recordingPath(dir, "webm")
	if err != nil {
		t.Fatal(err)
	}
	b, err := recordingPath(dir, "webm")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || filepath.Ext(a) != ".webm" || filepath.Dir(a) != dir {
		t.Fatalf("paths %q, %q", a, b)
	}
}

func TestRecorderArgs(t *testing.T) {
	zone := regionRect{10, 20, 640, 480}
	has := func(args []string, want ...string) bool {
		return strings.Contains(strings.Join(args, " "), strings.Join(want, " "))
	}

	args := recorderArgs(wlipc.RecordingSettings{}, zone, "/v/part.mp4", "/dev/dri/renderD128")
	if !has(args, "-g", "10,20 640x480") || !has(args, "-c", "h264_vaapi", "-d", "/dev/dri/renderD128") || has(args, "--audio") {
		t.Errorf("default, on the GPU: %v", args)
	}
	args = recorderArgs(wlipc.RecordingSettings{Microphone: true}, zone, "/v/part.mp4", "")
	if !has(args, "-c", "libx264") || !has(args, "--audio=@DEFAULT_SOURCE@") || !has(args, "-C", "aac") {
		t.Errorf("microphone, on the processor: %v", args)
	}
	// Both sounds: the computer's with the video, the microphone aside.
	args = recorderArgs(wlipc.RecordingSettings{Microphone: true, SystemAudio: true, Format: "webm"}, zone, "/v/part.webm", "")
	if !has(args, "--audio=@DEFAULT_MONITOR@") || has(args, "@DEFAULT_SOURCE@") || !has(args, "-c", "libvpx-vp9") || !has(args, "-C", "libopus") {
		t.Errorf("both sounds, WebM: %v", args)
	}
	if m := micArgs(wlipc.RecordingSettings{Format: "webm"}, "/v/mic.ogg"); !has(m, "-f", "pulse", "-i", "@DEFAULT_SOURCE@", "-c:a", "libopus") {
		t.Errorf("microphone aside: %v", m)
	}
}

func TestToggleBarImage(t *testing.T) {
	face := fontFaceOfSize(13)
	if face == nil {
		t.Skip("no font")
	}
	img, rects := toggleBarImage(wlipc.RecordingSettings{Webcam: true}, face)
	if len(rects) != len(recordToggles) {
		t.Fatalf("%d toggles", len(rects))
	}
	for i, r := range rects {
		if !r.In(img.Bounds()) || r.Empty() {
			t.Fatalf("toggle %d at %v, outside %v", i, r, img.Bounds())
		}
		if i > 0 && r.Min.X <= rects[i-1].Max.X {
			t.Errorf("toggles %d and %d overlap", i-1, i)
		}
	}
	// The webcam (third) is on: red; the microphone (first) is off.
	if c := img.NRGBAAt(rects[2].Min.X+2, rects[2].Min.Y+2); c != toggleOn {
		t.Errorf("webcam toggle: %v", c)
	}
	if c := img.NRGBAAt(rects[0].Min.X+2, rects[0].Min.Y+2); c != toggleOff {
		t.Errorf("microphone toggle: %v", c)
	}
}
