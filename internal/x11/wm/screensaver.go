//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

package wm

import (
	"log"
	"os/exec"
	"time"

	"fyshos.com/fynedesk"
	"github.com/BurntSushi/xgb/screensaver"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/FyshOS/saver"

	"fyne.io/fyne/v2"
)

func (x *x11WM) initScreensaver() {
	err := screensaver.Init(x.x.Conn())
	if err != nil {
		log.Println("Failed to init screensaver extension")
		return
	}

	//screensaver.SelectInput(conn.Conn(), xproto.Drawable(conn.Screen().Root),
	//	screensaver.EventNotifyMask)
	go x.watchScreensaver()
}

func (x *x11WM) watchScreensaver() {
	// time.NewTicker drops ticks when the OS sleeps (the runtime can't deliver
	// them while suspended), so we still notice the gap via the wall-clock
	// delta below — same behavior as the previous AfterFunc chain but with a
	// proper exit path.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	previous := time.Now()

	for {
		select {
		case <-x.shutdown:
			return
		case <-ticker.C:
		}

		info, err := screensaver.QueryInfo(x.x.Conn(), xproto.Drawable(x.x.Screen().Root)).Reply()
		if err != nil {
			fyne.LogError("Failed to query screensaver info", err)
			continue
		}

		now := time.Now()
		slept := now.Sub(previous).Seconds() > 2 // skipped a tick
		previous = now
		if slept {
			fynedesk.Instance().TriggerScreenSaver(false) // no delay on lock prompt after sleep
		} else if info.MsSinceUserInput <= 1500 {
			fynedesk.Instance().DelayScreenSaver()
		}
	}
}

var screenSaverActive bool

func (x *x11WM) ShowScreensaver(s *saver.ScreenSaver) {
	if fynedesk.Instance().Settings().ScreenSaverType() == "XScreensaver" {
		task := "-activate"
		if s.Lock {
			task = "-lock"
		}
		cmd := exec.Command("xscreensaver-command", task)
		cmd.Start()
		return
	}

	if screenSaverActive {
		return
	}

	screenSaverActive = true
	s.OnUnlocked = func() {
		screenSaverActive = false
	}

	if path, err := exec.LookPath("fyshsaver"); err == nil {
		var params []string
		if s.Lock {
			params = append(params, "-lock")
			if !s.LockImmediately {
				params = append(params, "-lock-delay")
			}
		}
		if s.Label != "" {
			params = append(params, "-label", s.Label)
		}

		go func() {
			time.Sleep(time.Millisecond * 100)

			err = exec.Command(path, params...).Run()
			if err != nil {
				fyne.LogError("Failed to activate fyne screensaver", err)
			}
			s.OnUnlocked()
		}()
		return
	}

	go func() {
		time.Sleep(time.Millisecond * 100)
		fyne.Do(s.ShowWindows)
	}()
}
