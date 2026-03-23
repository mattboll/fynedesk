package ui

import (
	"time"

	"fyne.io/fyne/v2"
)

const (
	focusRetryInterval = 20 * time.Millisecond
	focusRetryTimeout  = 500 * time.Millisecond
)

// ensureFocused retries focusing the given entry on the window's canvas until
// it actually holds focus or the timeout expires. This works around Wayland
// window-mapping races where the compositor may re-focus the previous surface
// after a new overlay is shown.
//
// Must be called from a goroutine (it blocks). All canvas operations are
// dispatched via fyne.Do.
func ensureFocused(win fyne.Window, target fyne.Focusable) {
	deadline := time.After(focusRetryTimeout)
	for {
		select {
		case <-deadline:
			return
		case <-time.After(focusRetryInterval):
			done := make(chan bool, 1)
			fyne.Do(func() {
				if win.Canvas().Focused() == target {
					done <- true
					return
				}
				win.Canvas().Focus(target)
				done <- false
			})
			if <-done {
				return
			}
		}
	}
}
