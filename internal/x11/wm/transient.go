//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

package wm

import (
	"slices"

	"github.com/BurntSushi/xgb/xproto"
)

func (x *x11WM) transientChildAdd(leader xproto.Window, child xproto.Window) {
	for _, win := range x.transientMap[leader] {
		if win == child {
			return
		}
	}
	x.transientMap[leader] = append(x.transientMap[leader], child)
}

// transientForget removes a destroyed window from the transient map, as a
// leader and as a child. Its WM_TRANSIENT_FOR cannot be read any more.
func (x *x11WM) transientForget(win xproto.Window) {
	delete(x.transientMap, win)
	for leader, children := range x.transientMap {
		kept := slices.DeleteFunc(children, func(c xproto.Window) bool { return c == win })
		if len(kept) == 0 {
			delete(x.transientMap, leader)
		} else {
			x.transientMap[leader] = kept
		}
	}
}
