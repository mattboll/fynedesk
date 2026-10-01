package ui

import (
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
)

func TestExportArgs(t *testing.T) {
	e := videoExport{start: 1500 * time.Millisecond, end: 11500 * time.Millisecond, format: "mp4", targetMB: 2, audio: true}
	args := strings.Join(exportArgs("in.mp4", "out.mp4", e), " ")
	// 2 MB over 10 s: 1638 kbit/s, less 128 for the sound, less 5 %.
	if !strings.Contains(args, "-ss 1.500 -to 11.500 -i in.mp4") || !strings.Contains(args, "-b:v 1434k") {
		t.Errorf("mp4 to a size: %s", args)
	}
	e = videoExport{end: 5 * time.Second, format: "webm"}
	if args := strings.Join(exportArgs("in.mp4", "out.webm", e), " "); !strings.Contains(args, "-crf 33 -b:v 0") || !strings.Contains(args, "libopus") {
		t.Errorf("webm: %s", args)
	}
	e.format = "gif"
	if args := strings.Join(exportArgs("in.mp4", "out.gif", e), " "); !strings.Contains(args, "palettegen") || strings.Contains(args, "-c:v") {
		t.Errorf("gif: %s", args)
	}
	if got := formatClock(65300 * time.Millisecond); got != "1:05.3" {
		t.Errorf("clock: %s", got)
	}
}

// testVideo makes a 6-second video with sound to edit.
func testVideo(t *testing.T) string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	path := filepath.Join(t.TempDir(), "recording.mp4")
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440", "-t", "6", "-pix_fmt", "yuv420p", "-shortest", path).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return path
}

func TestExportVideo(t *testing.T) {
	in := testVideo(t)
	info, err := probeVideo(in)
	if err != nil || info.duration < 5*time.Second || !info.audio {
		t.Fatalf("probe: %+v, %v", info, err)
	}
	if _, err := frameAt(in, 2*time.Second); err != nil {
		t.Fatalf("frame: %v", err)
	}

	out, err := exportVideo(context.Background(), in, videoExport{start: time.Second, end: 4 * time.Second, format: "mp4", targetMB: 1, audio: true})
	if err != nil {
		t.Fatal(err)
	}
	cut, err := probeVideo(out)
	if err != nil || cut.duration < 2800*time.Millisecond || cut.duration > 3200*time.Millisecond {
		t.Errorf("cut to 3 s: %+v, %v", cut, err)
	}
	if st, _ := os.Stat(out); st.Size() > 1<<20 {
		t.Errorf("%d bytes, over 1 MB", st.Size())
	}
	if filepath.Base(out) != "recording_edit.mp4" {
		t.Errorf("name %q", out)
	}

	gif, err := exportVideo(context.Background(), in, videoExport{end: 2 * time.Second, format: "gif"})
	if err != nil || filepath.Ext(gif) != ".gif" {
		t.Fatalf("gif: %q, %v", gif, err)
	}
}

func TestVideoEditorWindow(t *testing.T) {
	in := testVideo(t)
	test.NewTempApp(t)
	showVideoEditor(in)
	wins := fyne.CurrentApp().Driver().AllWindows()
	if len(wins) == 0 {
		t.Fatal("no editor window")
	}
	if out := os.Getenv("EDITOR_WINDOW"); out != "" { // a look at it
		time.Sleep(time.Second) // the preview
		c := software.NewCanvas()
		c.SetContent(wins[len(wins)-1].Content())
		c.Resize(fyne.NewSize(760, 640))
		if f, err := os.Create(out); err == nil {
			png.Encode(f, c.Capture())
			f.Close()
		}
	}
}
