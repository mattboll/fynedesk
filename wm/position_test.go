package wm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"fyshos.com/tyde"
	"fyshos.com/tyde/test"
	"fyshos.com/tyde/theme"
)

func TestPositionForNewWindow_Default(t *testing.T) {
	w := test.NewWindow("Hi")
	screen := &tyde.Screen{X: 50, Y: 0, Width: 500, Height: 500, Scale: 1}
	x, y, _, _ := PositionForNewWindow(w, 0, 0, 100, 100, true, test.NewScreensProvider(screen))

	assert.Equal(t, 250-int(theme.BorderWidth), x)
	assert.Equal(t, 200-int(theme.TitleHeight()), y)
}

func TestPositionForNewWindow_DefaultBorderless(t *testing.T) {
	w := test.NewWindow("Hi")
	screen := &tyde.Screen{X: 50, Y: 0, Width: 500, Height: 500, Scale: 1}
	x, y, _, _ := PositionForNewWindow(w, 0, 0, 100, 100, false, test.NewScreensProvider(screen))

	assert.Equal(t, 250, x)
	assert.Equal(t, 200, y)
}

func TestPositionForNewWindow_WithParent(t *testing.T) {
	parent := test.NewWindow("Parent")
	parent.SetGeometry(200, 100, 400, 200)

	w := test.NewWindow("Child")
	w.SetParent(parent)

	screen := &tyde.Screen{X: 50, Y: 0, Width: 500, Height: 500, Scale: 1}
	x, y, _, _ := PositionForNewWindow(w, 0, 0, 100, 100, false, test.NewScreensProvider(screen))

	assert.Equal(t, 350, x)
	assert.Equal(t, 150, y)

	// move up/left by 100,100
	parent.SetGeometry(100, 0, 400, 200)
	x, y, _, _ = PositionForNewWindow(w, 0, 0, 100, 100, false, test.NewScreensProvider(screen))

	assert.Equal(t, 250, x)
	assert.Equal(t, 50, y)
}

// parentOnly is a window that cannot tell its geometry.
type parentOnly struct{ tyde.Window }

// TestPositionWithAParentWithoutGeometry checks that a parent that cannot
// tell its geometry does not panic: the window goes to the screen.
func TestPositionWithAParentWithoutGeometry(t *testing.T) {
	win := test.NewWindow("child")
	win.SetParent(parentOnly{test.NewWindow("parent")})
	x, y, _, _ := PositionForNewWindow(win, 0, 0, 100, 100, false, test.NewScreensProvider())
	if x != 950 || y != 450 {
		t.Errorf("placed at %d,%d", x, y)
	}
}
