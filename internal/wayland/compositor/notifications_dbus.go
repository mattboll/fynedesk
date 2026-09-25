package compositor

import (
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

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
	nextID   uint32
	limiters map[string]*notifRateLimiter // per application, so one noisy app cannot mute the others
	tags     map[string]uint32            // app + stack tag → id of the notification it replaces
	conn     *dbus.Conn                   // owns the Notifications name; used to emit signals back to apps
}

func newNotificationsDBus() *notificationsDBus {
	return &notificationsDBus{
		nextID:   1,
		limiters: map[string]*notifRateLimiter{},
		tags:     map[string]uint32{},
	}
}

// maxStackTags bounds the stack tags remembered; older ones are forgotten.
const maxStackTags = 256

// stackTagHints name the hints with which an application asks for a
// notification to replace its previous one with the same value (volume or
// progress popups, a chat thread…).
var stackTagHints = []string{"x-canonical-private-synchronous", "x-dunst-stack-tag", "synchronous"}

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

// notificationID returns the id of a new notification, and whether it
// replaces an earlier one.
func (n *notificationsDBus) notificationID(appName string, replacesID uint32, tag string) (uint32, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	key := appName + "\x00" + tag
	if replacesID == 0 && tag != "" {
		replacesID = n.tags[key]
	}
	id, replaces := replacesID, replacesID != 0
	if !replaces {
		id = n.nextID
		n.nextID++
	}
	if tag != "" {
		if len(n.tags) >= maxStackTags {
			clear(n.tags)
		}
		n.tags[key] = id
	}
	return id, replaces
}

// hintString returns a string hint, or "".
func hintString(hints map[string]dbus.Variant, key string) string {
	if v, ok := hints[key]; ok {
		if str, ok := v.Value().(string); ok {
			return str
		}
	}
	return ""
}

// hintUrgency returns the urgency hint as the panel names it: "low",
// "critical", or "" for normal.
func hintUrgency(hints map[string]dbus.Variant) string {
	v, ok := hints["urgency"]
	if !ok {
		return ""
	}
	var level int
	switch u := v.Value().(type) {
	case byte:
		level = int(u)
	case int32:
		level = int(u)
	case uint32:
		level = int(u)
	}
	switch level {
	case 0:
		return "low"
	case 2:
		return "critical"
	}
	return ""
}

func (n *notificationsDBus) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]dbus.Variant, timeout int32,
) (uint32, *dbus.Error) {
	var tag string
	for _, key := range stackTagHints {
		if tag = hintString(hints, key); tag != "" {
			break
		}
	}
	// Reuse the client-supplied id when it is replacing an existing notification,
	// so the id we forward matches the one the app already tracks.
	id, replaces := n.notificationID(appName, replacesID, tag)

	urgency := hintUrgency(hints)
	// Updates of a notification already on screen are not throttled: they
	// replace it instead of adding one.
	if !replaces && urgency != "critical" && !n.allow(appName) {
		log.Printf("[NOTIFY-DBUS] rate limit exceeded, dropping notification from %s: %q\n", appName, summary)
		return id, nil
	}

	transient, _ := hints["transient"].Value().(bool)
	if desktopEntry := hintString(hints, "desktop-entry"); appName == "" {
		appName = desktopEntry
	}
	log.Printf("[NOTIFY-DBUS] #%d %s: %q (urgency=%q, replaces=%v, timeout=%d, actions=%d)\n",
		id, appName, summary, urgency, replaces, timeout, len(actions)/2)

	if err := wlipc.NotifyDBusNotification(wlipc.DBusNotification{
		ID:        id,
		AppName:   appName,
		AppIcon:   appIcon,
		Title:     summary,
		Body:      body,
		Actions:   actions,
		Timeout:   timeout,
		Urgency:   urgency,
		Transient: transient,
		Category:  hintString(hints, "category"),
		Replaces:  replaces,
	}); err != nil {
		log.Printf("[NOTIFY-DBUS] IPC write error: %v\n", err)
	}

	return id, nil
}

func (n *notificationsDBus) CloseNotification(id uint32) *dbus.Error {
	wlipc.NotifyNotificationClosed(id)
	n.emitClosed(id, 3) // reason 3 = closed by CloseNotification call
	return nil
}

func (n *notificationsDBus) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "Tyde", "Fyne.io", "0", "1.2", nil
}

func (n *notificationsDBus) GetCapabilities() ([]string, *dbus.Error) {
	return []string{
		"actions", "body", "icon-static", "persistence",
		"x-canonical-private-synchronous", "x-dunst-stack-tag",
	}, nil
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
