package notify

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// Two servers own org.freedesktop.Notifications, one per session: the X11
// desktop shows what it receives itself (wm/notifications.go), the Wayland
// compositor forwards it to the panel. What they share is here.

// The D-Bus names of the notification service.
const (
	Path      dbus.ObjectPath = "/org/freedesktop/Notifications"
	Interface string          = "org.freedesktop.Notifications"
)

// Reasons of a NotificationClosed signal.
const (
	ClosedDismissed uint32 = 2 // by the user
	ClosedByCall    uint32 = 3 // by a CloseNotification call
)

// ServerInformation answers GetServerInformation: name, vendor, version and
// the specification version.
func ServerInformation() (string, string, string, string) {
	return "Tyde", "Fyne.io", "0", "1.2"
}

// Capabilities answers GetCapabilities.
func Capabilities() []string {
	return append([]string{"actions", "body", "icon-static", "persistence"}, StackTagHints()...)
}

// EmitActionInvoked tells the application that the user chose an action of
// its notification.
func EmitActionInvoked(conn *dbus.Conn, id uint32, actionKey string) {
	_ = conn.Emit(Path, Interface+".ActionInvoked", id, actionKey)
}

// EmitClosed tells the application that its notification closed.
func EmitClosed(conn *dbus.Conn, id, reason uint32) {
	_ = conn.Emit(Path, Interface+".NotificationClosed", id, reason)
}

// maxStackTags bounds the stack tags remembered; older ones are forgotten.
const maxStackTags = 256

// IDs gives notifications the ids their applications know them by, and
// finds the one a new notification replaces: by replaces_id, or by stack
// tag within the application. The zero value is ready to use.
type IDs struct {
	mu   sync.Mutex
	tags map[string]uint32 // app + stack tag → id of the notification it replaces
}

// ID returns the id of a notification, and whether it replaces an earlier
// one. fresh gives the id of a notification that replaces nothing; it is
// called with the IDs locked.
func (s *IDs) ID(appName string, replacesID uint32, tag string, fresh func() uint32) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := appName + "\x00" + tag
	if replacesID == 0 && tag != "" {
		replacesID = s.tags[key]
	}
	id, replaces := replacesID, replacesID != 0
	if !replaces {
		id = fresh()
	}
	if tag != "" {
		if s.tags == nil || len(s.tags) >= maxStackTags {
			s.tags = map[string]uint32{}
		}
		s.tags[key] = id
	}
	return id, replaces
}
