//go:build linux || openbsd || freebsd || netbsd

package win

import (
	"testing"

	"fyne.io/fyne/v2/theme"

	wmTheme "fyshos.com/tyde/theme"

	"github.com/stretchr/testify/assert"
)

// The bar we draw must fill the height the frame reserves for it and its
// buttons must grow with it, at either of the pointer and touch screen sizes.
func TestTransparentTheme_TitleBarSizes(t *testing.T) {
	defer wmTheme.SetTouchScreen(false)
	th := &transparentTheme{Theme: theme.DefaultTheme()}

	for _, touch := range []bool{false, true} {
		wmTheme.SetTouchScreen(touch)

		assert.Equal(t, wmTheme.TitleHeight(), th.Size(theme.SizeNameWindowTitleBarHeight))
		assert.Equal(t, wmTheme.TitleButtonHeight(), th.Size(theme.SizeNameWindowButtonHeight))
		assert.Equal(t, wmTheme.TitleButtonIconSize(), th.Size(theme.SizeNameWindowButtonIcon))
	}

	assert.Equal(t, theme.DefaultTheme().Size(theme.SizeNamePadding), th.Size(theme.SizeNamePadding))
}
