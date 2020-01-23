package notify

import "fyne.io/desktop"

// ScreenNotify is an interface that can be used by objects interested in when the window manager changes screen or root properties
type ScreenNotify interface {
	RootMovedNotify(screen *desktop.Screen)
}
