//go:build linux || openbsd || freebsd || netbsd

package wm

import (
	"testing"

	"github.com/BurntSushi/xgb/xproto"
)

func TestTransientForget(t *testing.T) {
	x := &x11WM{transientMap: map[xproto.Window][]xproto.Window{}}
	x.transientChildAdd(1, 2)
	x.transientChildAdd(1, 3)
	x.transientChildAdd(4, 5)

	x.transientForget(2) // a dialog closes
	if got := x.transientMap[1]; len(got) != 1 || got[0] != 3 {
		t.Errorf("children of 1 = %v, want [3]", got)
	}
	x.transientForget(5) // the last dialog of 4
	if _, ok := x.transientMap[4]; ok {
		t.Error("a leader without dialogs is kept")
	}
	x.transientForget(1) // a leader closes
	if len(x.transientMap) != 0 {
		t.Errorf("map = %v, want empty", x.transientMap)
	}
}

func TestRootAccessors(t *testing.T) {
	x := &x11WM{rootIDs: map[string]xproto.Window{}}
	x.setRoot("eDP-1", 10)
	x.setRoot("HDMI-1", 11)
	if got := x.rootFor("HDMI-1"); got != 11 {
		t.Errorf("rootFor = %d, want 11", got)
	}
	if !x.isRoot(10) || x.isRoot(12) {
		t.Error("isRoot does not match the roots")
	}
	if !x.forgetRoot(10) || x.forgetRoot(10) {
		t.Error("forgetRoot reports a root it did not have, or missed one")
	}
	if got := len(x.roots()); got != 1 {
		t.Errorf("%d roots left, want 1", got)
	}
}
