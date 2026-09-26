package compositor

import (
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"fyshos.com/tyde/internal/notify"
	"fyshos.com/tyde/wlipc"
)

// notifRateLimiter implements a fixed-window rate limiter for notifications.
type notifRateLimiter struct {
	mu      sync.Mutex
	count   int
	resetAt time.Time
	limit   int
	window  time.Duration
}

func newNotifRateLimiter(limit int, window time.Duration) *notifRateLimiter {
	return &notifRateLimiter{
		limit:   limit,
		window:  window,
		resetAt: time.Now().Add(window),
	}
}

func (r *notifRateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if now.After(r.resetAt) {
		r.count = 0
		r.resetAt = now.Add(r.window)
	}
	if r.count >= r.limit {
		return false
	}
	r.count++
	return true
}

// notificationsDBus implements the org.freedesktop.Notifications D-Bus interface.
// The compositor owns this name so that notify-send and other apps send
// notifications here. Received notifications are forwarded to the panel via IPC,
// and action invocations from the panel are emitted back as ActionInvoked so the
// originating app (e.g. Slack) can navigate to the right context.
type notificationsDBus struct {
	mu       sync.Mutex
	limiters map[string]*notifRateLimiter // per application, so one noisy app cannot mute the others
	ids      notify.IDs
	nextID   uint32     // guarded by ids
	conn     *dbus.Conn // owns the Notifications name; used to emit signals back to apps
}

func newNotificationsDBus() *notificationsDBus {
	return &notificationsDBus{
		limiters: map[string]*notifRateLimiter{},
	}
}

// allow reports whether appName may show another notification now.
func (n *notificationsDBus) allow(appName string) bool {
	n.mu.Lock()
	l := n.limiters[appName]
	if l == nil {
		l = newNotifRateLimiter(10, 10*time.Second)
		n.limiters[appName] = l
	}
	n.mu.Unlock()
	return l.allow()
}

// notificationID returns the id of a notification, and whether it replaces
// an earlier one.
func (n *notificationsDBus) notificationID(appName string, replacesID uint32, tag string) (uint32, bool) {
	return n.ids.ID(appName, replacesID, tag, func() uint32 {
		n.nextID++
		return n.nextID
	})
}

func (n *notificationsDBus) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]dbus.Variant, timeout int32,
) (uint32, *dbus.Error) {
	h := notify.ParseHints(hints)
	// Reuse the client-supplied id when it is replacing an existing notification,
	// so the id we forward matches the one the app already tracks.
	id, replaces := n.notificationID(appName, replacesID, h.Tag)

	// Updates of a notification already on screen are not throttled: they
	// replace it instead of adding one.
	if !replaces && h.Urgency != "critical" && !n.allow(appName) {
		log.Printf("[NOTIFY-DBUS] rate limit exceeded, dropping notification from %s\n", appName)
		return id, nil
	}

	if appName == "" {
		appName = h.DesktopEntry
	}
	// What it says stays out of the log: messages, codes, mail.
	log.Printf("[NOTIFY-DBUS] #%d %s (urgency=%q, replaces=%v, timeout=%d, actions=%d)\n",
		id, appName, h.Urgency, replaces, timeout, len(actions)/2)

	if err := wlipc.NotifyDBusNotification(wlipc.DBusNotification{
		ID:        id,
		AppName:   appName,
		AppIcon:   appIcon,
		Title:     summary,
		Body:      body,
		Actions:   actions,
		Timeout:   timeout,
		Urgency:   h.Urgency,
		Transient: h.Transient,
		Category:  h.Category,
		Replaces:  replaces,
	}); err != nil {
		log.Printf("[NOTIFY-DBUS] IPC write error: %v\n", err)
	}

	return id, nil
}

func (n *notificationsDBus) CloseNotification(id uint32) *dbus.Error {
	wlipc.NotifyNotificationClosed(id)
	n.emitClosed(id, notify.ClosedByCall)
	return nil
}

func (n *notificationsDBus) GetServerInformation() (string, string, string, string, *dbus.Error) {
	name, vendor, version, spec := notify.ServerInformation()
	return name, vendor, version, spec, nil
}

func (n *notificationsDBus) GetCapabilities() ([]string, *dbus.Error) {
	return notify.Capabilities(), nil
}

// emitAction emits ActionInvoked for a notification (so the app acts on it, e.g.
// Slack opens the right channel) followed by NotificationClosed, matching what
// GNOME does when a notification is activated. Safe to call from any goroutine.
func (n *notificationsDBus) emitAction(id uint32, actionKey string) {
	if n == nil || n.conn == nil {
		return
	}
	log.Printf("[NOTIFY-DBUS] emit ActionInvoked #%d %q\n", id, actionKey)
	notify.EmitActionInvoked(n.conn, id, actionKey)
	n.emitClosed(id, notify.ClosedDismissed)
}

// emitClosed emits the NotificationClosed signal for a notification id.
func (n *notificationsDBus) emitClosed(id uint32, reason uint32) {
	if n == nil || n.conn == nil {
		return
	}
	notify.EmitClosed(n.conn, id, reason)
}

// startNotificationsDBus registers the Notifications D-Bus service in the compositor.
func (s *server) startNotificationsDBus() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("D-Bus: could not connect to session bus for Notifications: %v\n", err)
		return
	}

	nd := newNotificationsDBus()
	nd.conn = conn
	s.notifDBus = nd

	err = conn.ExportAll(nd, notify.Path, notify.Interface)
	if err != nil {
		log.Printf("D-Bus: could not export Notifications: %v\n", err)
		return
	}

	reply, err := conn.RequestName(notify.Interface,
		dbus.NameFlagReplaceExisting|dbus.NameFlagAllowReplacement)
	if err != nil {
		log.Printf("D-Bus: could not request Notifications name: %v\n", err)
		return
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("D-Bus: Notifications name already taken (reply=%d), notifications will go to host\n", reply)
		return
	}

	log.Println("D-Bus: org.freedesktop.Notifications registered")
}
