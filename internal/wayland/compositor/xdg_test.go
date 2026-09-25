package compositor

import "testing"

func TestIsFixedSize(t *testing.T) {
	for _, tc := range []struct {
		v    xdgView
		want bool
	}{
		{xdgView{minWidth: 460, minHeight: 520, maxWidth: 460, maxHeight: 520}, true},
		{xdgView{minWidth: 200, minHeight: 100}, false}, // resizable
		{xdgView{}, false}, // no constraints
		{xdgView{minWidth: 460, minHeight: 520, maxWidth: 460, maxHeight: 0}, false}, // taller allowed
		{xdgView{minWidth: 460, minHeight: 520, maxWidth: 460, maxHeight: 520, parent: &xdgView{}}, false},
	} {
		if got := isFixedSize(&tc.v); got != tc.want {
			t.Errorf("isFixedSize(%+v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
