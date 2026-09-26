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
