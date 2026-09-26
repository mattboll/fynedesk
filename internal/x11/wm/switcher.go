//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

package wm

import (
	"time"

	"github.com/BurntSushi/xgb/xproto"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/ui"

	"fyne.io/fyne/v2"
)

var (
	// switcherInstance and the methods below manage the X to ui.Switcher bindings.
	// This is needed due to the way that releasing Super is only reported if we grab the whole keyboard.
	// Therefore the UI cannot handle keyboard input - so we add it here instead.
	// Both are only used on the Fyne thread: the X event loop hands its
	// keys over with fyne.Do.
	switcherInstance *ui.Switcher

	// ignoreSwitcher helps us track where key pres+lift is faster than window show
	ignoreSwitcher bool
)

func (x *x11WM) applyAppSwitcher() {
	fyne.Do(func() { endAppSwitcher(true) })
	xproto.UngrabKeyboard(x.x.Conn(), xproto.TimeCurrentTime)
	windowClientListStackingUpdate(x)
}

func (x *x11WM) cancelAppSwitcher() {
	fyne.Do(func() { endAppSwitcher(false) })
	xproto.UngrabKeyboard(x.x.Conn(), xproto.TimeCurrentTime)
}

// endAppSwitcher closes the switcher, switching to the window it shows when
// apply is set. A switcher still to be shown is not. Fyne thread.
func endAppSwitcher(apply bool) {
	if switcherInstance == nil {
		ignoreSwitcher = true
		time.AfterFunc(time.Second/4, func() {
			fyne.Do(func() { ignoreSwitcher = false })
		})
		return
	}
	if apply {
		switcherInstance.HideApply()
	} else {
		switcherInstance.HideCancel()
	}
	switcherInstance = nil
}

func (x *x11WM) nextAppSwitcher() {
	fyne.Do(func() {
		if switcherInstance != nil {
			switcherInstance.Next()
		}
	})
}

func (x *x11WM) previousAppSwitcher() {
	fyne.Do(func() {
		if switcherInstance != nil {
			switcherInstance.Previous()
		}
	})
}

func (x *x11WM) showOrSelectAppSwitcher(reverse bool) {
	var visible []tyde.Window
	for _, win := range x.list() {
		if win.Desktop() == tyde.Instance().Desktop() || win.Pinned() {
			visible = append(visible, win)
		}
	}
	if len(visible) <= 1 {
		return
	}
	xproto.GrabKeyboard(x.x.Conn(), true, x.x.RootWin(), xproto.TimeCurrentTime, xproto.GrabModeAsync, xproto.GrabModeAsync)

	wins := x.Windows()
	fyne.Do(func() {
		if switcherInstance != nil {
			if reverse {
				switcherInstance.Previous()
			} else {
				switcherInstance.Next()
			}
			return
		}
		if ignoreSwitcher { // the keys were released already
			ignoreSwitcher = false
			return
		}

		var win *ui.Switcher
		if reverse {
			win = ui.NewAppSwitcherReverse(wins, tyde.Instance().IconProvider())
		} else {
			win = ui.NewAppSwitcher(wins, tyde.Instance().IconProvider())
		}
		if win == nil {
			return
		}
		switcherInstance = win
		win.Show()
	})
}
