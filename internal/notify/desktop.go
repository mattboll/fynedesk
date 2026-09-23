// Package notify provides interfaces for desktop change notifications to modules.
package notify

// DesktopNotify allows modules to be informed when user changes virtual desktop
type DesktopNotify interface {
	DesktopChangeNotify(int)
}
