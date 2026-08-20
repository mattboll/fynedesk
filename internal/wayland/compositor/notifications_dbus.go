package compositor

import (
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"fyshos.com/fynedesk/wlipc"
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
	mu      sync.Mutex
	nextID  uint32
	limiter *notifRateLimiter
	conn    *dbus.Conn // owns the Notifications name; used to emit signals back to apps
}

func newNotificationsDBus() *notificationsDBus {
	return &notificationsDBus{
		nextID:  1,
		limiter: newNotifRateLimiter(30, 10*time.Second),
	}
}

func (n *notificationsDBus) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]interface{}, timeout int32) (uint32, error) {
	// Reuse the client-supplied id when it is replacing an existing notification,
	// so the id we forward matches the one the app already tracks.
	id := replacesID
	if id == 0 {
		n.mu.Lock()
		id = n.nextID
		n.nextID++
		n.mu.Unlock()
	}

	if !n.limiter.allow() {
		log.Printf("[NOTIFY-DBUS] rate limit exceeded, dropping notification from %s: %q\n", appName, summary)
		return id, nil
	}

	log.Printf("[NOTIFY-DBUS] #%d %s: %q %q (timeout=%d, actions=%d)\n", id, appName, summary, body, timeout, len(actions)/2)

	if err := wlipc.NotifyDBusNotification(wlipc.DBusNotification{
		ID:      id,
		AppName: appName,
		AppIcon: appIcon,
		Title:   summary,
		Body:    body,
		Actions: actions,
		Timeout: timeout,
	}); err != nil {
		log.Printf("[NOTIFY-DBUS] IPC write error: %v\n", err)
	}

	return id, nil
}

func (n *notificationsDBus) CloseNotification(id uint32) error {
	n.emitClosed(id, 3) // reason 3 = closed by CloseNotification call
	return nil
}

func (n *notificationsDBus) GetServerInformation() (string, string, string, string) {
	return "FyneDesk", "Fyne.io", "0", "1.2"
}

func (n *notificationsDBus) GetCapabilities() []string {
	return []string{"actions", "body", "icon-static", "persistence"}
}

// emitAction emits ActionInvoked for a notification (so the app acts on it, e.g.
// Slack opens the right channel) followed by NotificationClosed, matching what
// GNOME does when a notification is activated. Safe to call from any goroutine.
func (n *notificationsDBus) emitAction(id uint32, actionKey string) {
	if n == nil || n.conn == nil {
		return
	}
	log.Printf("[NOTIFY-DBUS] emit ActionInvoked #%d %q\n", id, actionKey)
	_ = n.conn.Emit("/org/freedesktop/Notifications",
		"org.freedesktop.Notifications.ActionInvoked", id, actionKey)
	n.emitClosed(id, 2) // reason 2 = dismissed by user
}

// emitClosed emits the NotificationClosed signal for a notification id.
func (n *notificationsDBus) emitClosed(id uint32, reason uint32) {
	if n == nil || n.conn == nil {
		return
	}
	_ = n.conn.Emit("/org/freedesktop/Notifications",
		"org.freedesktop.Notifications.NotificationClosed", id, reason)
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

	err = conn.ExportAll(nd, "/org/freedesktop/Notifications", "org.freedesktop.Notifications")
	if err != nil {
		log.Printf("D-Bus: could not export Notifications: %v\n", err)
		return
	}

	reply, err := conn.RequestName("org.freedesktop.Notifications",
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
