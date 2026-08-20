package wm

import (
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"github.com/godbus/dbus/v5"
)

var (
	server             *notifications
	serverOnce         sync.Once
	lastNotificationID uint32

	dndMu        sync.RWMutex
	doNotDisturb bool
)

const maxHistory = 50

// Notification is a simple struct representing message that can be displayed in the notification area
type Notification struct {
	ID          uint32
	DBusID      uint32 // originating D-Bus notification id (0 = local); the id the sending app knows, used to invoke actions back to it
	AppName     string // source application name (from D-Bus appName parameter)
	AppID       string // desktop app_id or WM_CLASS for window matching
	Title, Body string
	IconName    string    // icon name from D-Bus appIcon parameter (FDO icon theme name or file path)
	Actions     []string  // D-Bus actions as pairs: [id, label, id, label, ...]
	Timeout     int32     // D-Bus timeout in milliseconds: -1 = persistent, 0 = server default, >0 = ms
	Timestamp   time.Time // when the notification was created
}

// NewNotification creates a new message that can be passed to SendNotification
func NewNotification(title, body string) *Notification {
	return NewNotificationWithTimeout(title, body, 0)
}

// NewNotificationWithTimeout creates a new notification with an explicit D-Bus timeout
func NewNotificationWithTimeout(title, body string, timeout int32) *Notification {
	lastNotificationID++

	return &Notification{
		ID:        lastNotificationID,
		Title:     title,
		Body:      body,
		Timeout:   timeout,
		Timestamp: time.Now(),
	}
}

// NewNotificationFull creates a notification with all fields including app name, icon, and actions.
// The appName is also stored as AppID for window matching, since D-Bus app names
// typically correspond to desktop entry IDs or WM_CLASS values.
func NewNotificationFull(appName, iconName, title, body string, actions []string, timeout int32) *Notification {
	lastNotificationID++

	return &Notification{
		ID:        lastNotificationID,
		AppName:   appName,
		AppID:     appName,
		IconName:  iconName,
		Actions:   actions,
		Title:     title,
		Body:      body,
		Timeout:   timeout,
		Timestamp: time.Now(),
	}
}

// AddNotificationListener registers a listener that will be called for each new notification.
// Multiple listeners can be registered and all will be called.
func AddNotificationListener(listen func(*Notification)) {
	s := ensureServer()
	s.mu.Lock()
	s.listeners = append(s.listeners, listen)
	s.mu.Unlock()
}

// SetNotificationListener connects the user interface to display notifications.
// Deprecated: use AddNotificationListener instead.
func SetNotificationListener(listen func(*Notification)) {
	AddNotificationListener(listen)
}

// SendNotification posts a given notification into the user interface's notification area
func SendNotification(n *Notification) {
	s := ensureServer()

	s.mu.RLock()
	listeners := make([]func(*Notification), len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.RUnlock()

	if len(listeners) == 0 {
		fyne.LogError("No notifications listener attached", nil)
		return
	}

	// Add to history
	s.histMu.Lock()
	s.history = append([]*Notification{n}, s.history...)
	if len(s.history) > maxHistory {
		s.history = s.history[:maxHistory]
	}
	s.histMu.Unlock()
	s.notifyHistoryChange()

	for _, listen := range listeners {
		listen(n)
	}
}

// NotificationHistory returns a copy of the notification history, most recent first.
func NotificationHistory() []*Notification {
	s := ensureServer()

	s.histMu.RLock()
	defer s.histMu.RUnlock()

	out := make([]*Notification, len(s.history))
	copy(out, s.history)
	return out
}

// RemoveNotification removes a single notification from the history by ID.
func RemoveNotification(id uint32) {
	s := ensureServer()
	s.histMu.Lock()
	for i, n := range s.history {
		if n.ID == id {
			s.history = append(s.history[:i], s.history[i+1:]...)
			break
		}
	}
	s.histMu.Unlock()
	s.notifyHistoryChange()
}

// ClearNotificationHistory removes all stored notifications.
func ClearNotificationHistory() {
	s := ensureServer()

	s.histMu.Lock()
	s.history = nil
	s.histMu.Unlock()
	s.notifyHistoryChange()
}

// AddHistoryChangeListener registers a callback invoked on every history mutation
// (add, remove, clear).
func AddHistoryChangeListener(fn func()) {
	s := ensureServer()
	s.histMu.Lock()
	s.histListeners = append(s.histListeners, fn)
	s.histMu.Unlock()
}

type notifications struct {
	mu        sync.RWMutex
	listeners []func(*Notification)

	histMu        sync.RWMutex
	history       []*Notification
	histListeners []func()
}

func (n *notifications) notifyHistoryChange() {
	n.histMu.RLock()
	listeners := make([]func(), len(n.histListeners))
	copy(listeners, n.histListeners)
	n.histMu.RUnlock()

	for _, fn := range listeners {
		fn()
	}
}

// NotificationGroup represents a set of notifications from the same application.
type NotificationGroup struct {
	AppName       string
	Notifications []*Notification // most recent first
}

// GroupedNotificationHistory returns notifications grouped by AppName, most recent group first.
// Notifications without an AppName each form their own group.
func GroupedNotificationHistory() []*NotificationGroup {
	s := ensureServer()

	s.histMu.RLock()
	defer s.histMu.RUnlock()

	groups := make(map[string]*NotificationGroup)
	var order []string

	for _, n := range s.history {
		key := n.AppName
		if key == "" {
			key = "__ungrouped_" + fmt.Sprint(n.ID)
		}
		g, exists := groups[key]
		if !exists {
			g = &NotificationGroup{AppName: n.AppName}
			groups[key] = g
			order = append(order, key)
		}
		g.Notifications = append(g.Notifications, n)
	}

	result := make([]*NotificationGroup, 0, len(order))
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}

func (n *notifications) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]interface{}, timeout int32) (uint32, error) {
	item := NewNotificationFull(appName, appIcon, summary, body, actions, timeout)

	SendNotification(item)
	return item.ID, nil
}

func (n *notifications) CloseNotification(id uint32) error {
	// Emit NotificationClosed signal (reason 3 = closed by CloseNotification call)
	conn, err := dbus.SessionBus()
	if err == nil {
		_ = conn.Emit("/org/freedesktop/Notifications",
			"org.freedesktop.Notifications.NotificationClosed", id, uint32(3))
	}
	return nil
}

func (n *notifications) GetServerInformation() (string, string, string, string) {
	return "FyneDesk", "Fyne.io", "0", "1.2"
}

func (n *notifications) GetCapabilities() []string {
	return []string{"actions", "body", "icon-static", "persistence"}
}

func (n *notifications) register() {
	err := RegisterService(n, "/org/freedesktop/Notifications", "org.freedesktop.Notifications")
	if err != nil {
		fyne.LogError("Could not start DBus notifications server, using local only", err)
	}
}

// DoNotDisturb returns true if notification toasts should be suppressed.
func DoNotDisturb() bool {
	dndMu.RLock()
	defer dndMu.RUnlock()
	return doNotDisturb
}

// SetDoNotDisturb enables or disables Do Not Disturb mode.
// When enabled, notifications are still added to the history but toasts are suppressed.
func SetDoNotDisturb(on bool) {
	dndMu.Lock()
	doNotDisturb = on
	dndMu.Unlock()
}

var (
	actionCallbackMu sync.RWMutex
	actionCallback   func(notifID uint32, actionKey string)
)

// SetActionCallback registers a callback invoked when a notification action is triggered.
func SetActionCallback(fn func(notifID uint32, actionKey string)) {
	actionCallbackMu.Lock()
	actionCallback = fn
	actionCallbackMu.Unlock()
}

// InvokeAction triggers a notification action and emits the D-Bus ActionInvoked signal.
func InvokeAction(notifID uint32, actionKey string) {
	// Emit D-Bus signal
	conn, err := dbus.SessionBus()
	if err == nil {
		_ = conn.Emit("/org/freedesktop/Notifications",
			"org.freedesktop.Notifications.ActionInvoked", notifID, actionKey)
	}

	// Call registered callback
	actionCallbackMu.RLock()
	cb := actionCallback
	actionCallbackMu.RUnlock()
	if cb != nil {
		cb(notifID, actionKey)
	}
}

func ensureServer() *notifications {
	serverOnce.Do(func() {
		server = &notifications{}
		go server.register()
	})
	return server
}
