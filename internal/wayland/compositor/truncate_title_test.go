package compositor

import (
	"testing"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

func TestTruncateTitle(t *testing.T) {
	d := &font.Drawer{Face: basicfont.Face7x13} // 7 px per character
	for _, tc := range []struct {
		title string
		width int
		want  string
	}{
		{"short", 100, "short"},
		{"abcdefghij", 70, "abcdefghij"},
		{"abcdefghij", 69, "abcdef..."},
		{"éèàùçéèàùç", 49, "éèàù..."},
		{"日本語のタイトル", 42, "日本語..."},
		{"abc", 14, ""},
	} {
		got := truncateTitle(d, tc.title, tc.width)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tc.title, tc.width, got, tc.want)
		}
		if w := d.MeasureString(got).Ceil(); w > tc.width {
			t.Errorf("truncateTitle(%q, %d) is %d px wide", tc.title, tc.width, w)
		}
	}
}
