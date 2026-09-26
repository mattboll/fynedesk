package compositor

import "testing"

func TestScreenSaverInhibitsGoWithTheirApp(t *testing.T) {
	ss := newScreenSaverDBus(nil)
	video, _ := ss.Inhibit(":1.42", "mpv", "Playing video")
	_, _ = ss.Inhibit(":1.7", "firefox", "Playing video")

	ss.dropSender(":1.42") // mpv crashed without taking its inhibit back
	if !ss.IsInhibited() {
		t.Fatal("firefox's inhibit went with mpv")
	}
	if _, ok := ss.inhibits[video]; ok {
		t.Error("mpv's inhibit outlived mpv")
	}
	ss.dropSender(":1.7")
	if ss.IsInhibited() {
		t.Error("inhibited with no app left")
	}
}
