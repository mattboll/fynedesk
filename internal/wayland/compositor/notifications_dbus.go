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
// notifications here. Received notifications are forwarded to the panel via IPC.
type notificationsDBus struct {
	mu      sync.Mutex
	nextID  uint32
	limiter *notifRateLimiter
}

func newNotificationsDBus() *notificationsDBus {
	return &notificationsDBus{
		nextID:  1,
		limiter: newNotifRateLimiter(30, 10*time.Second),
	}
}

func (n *notificationsDBus) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]interface{}, timeout int32) (uint32, error) {
	n.mu.Lock()
	id := n.nextID
	n.nextID++
	n.mu.Unlock()

	if !n.limiter.allow() {
		log.Printf("[NOTIFY-DBUS] rate limit exceeded, dropping notification from %s: %q\n", appName, summary)
		return id, nil
	}

	log.Printf("[NOTIFY-DBUS] %s: %q %q (timeout=%d)\n", appName, summary, body, timeout)

	if err := wlipc.NotifyDBusNotification(appName, summary, body, timeout); err != nil {
		log.Printf("[NOTIFY-DBUS] IPC write error: %v\n", err)
	}

	return id, nil
}

func (n *notificationsDBus) CloseNotification(id uint32) error {
	return nil
}

func (n *notificationsDBus) GetServerInformation() (string, string, string, string) {
	return "FyneDesk", "Fyne.io", "0", "1.2"
}

func (n *notificationsDBus) GetCapabilities() []string {
	return []string{"body", "icon-static", "persistence"}
}

// startNotificationsDBus registers the Notifications D-Bus service in the compositor.
func (s *server) startNotificationsDBus() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("D-Bus: could not connect to session bus for Notifications: %v\n", err)
		return
	}

	nd := newNotificationsDBus()

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
