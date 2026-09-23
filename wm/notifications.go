package wm

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"

	"github.com/godbus/dbus/v5"
)

var (
	server             *notifications
	serverOnce         sync.Once
	lastNotificationID atomic.Uint32

	dndMu        sync.RWMutex
	doNotDisturb bool
)

const maxHistory = 50

// Urgency is how much a notification should interrupt the user.
type Urgency int

// Urgency levels, as in the freedesktop notification specification.
const (
	UrgencyLow      Urgency = iota // kept in the history, no popup
	UrgencyNormal                  // popup that goes away on its own
	UrgencyCritical                // popup that stays until dismissed
)

// ParseUrgency converts the urgency names used over IPC ("low", "critical",
// anything else is normal).
func ParseUrgency(name string) Urgency {
	switch name {
	case "low":
		return UrgencyLow
	case "critical":
		return UrgencyCritical
	}
	return UrgencyNormal
}

// Notification is a simple struct representing message that can be displayed in the notification area
type Notification struct {
	ID          uint32
	DBusID      uint32 // originating D-Bus notification id (0 = local); the id the sending app knows, used to invoke actions back to it
	AppName     string // source application name (from D-Bus appName parameter)
	AppID       string // desktop app_id or WM_CLASS for window matching
	Title, Body string
	Icon        fyne.Resource // image loaded from an icon file path, if one was given
	IconName    string        // icon name from D-Bus appIcon parameter (FDO icon theme name or file path)
	Actions     []string      // D-Bus actions as pairs: [id, label, id, label, ...]
	Timeout     int32         // D-Bus timeout in milliseconds: -1 = persistent, 0 = server default, >0 = ms
	Timestamp   time.Time     // when the notification was created

	Urgency   Urgency
	Transient bool // popup only, not kept in the history
	// Replaced is set by SendNotification when this notification took the
	// place of an earlier one (same DBusID, or same Tag): it then keeps the
	// earlier ID, and a popup still on screen is updated instead of stacked.
	Replaced bool
	// Tag identifies notifications that replace one another, for those sent
	// by Tyde itself (the D-Bus ones use DBusID).
	Tag string
	// OnActivate, if set, runs when the notification is clicked instead of
	// the default action of its application.
	OnActivate func()
}

// NewNotification creates a new message that can be passed to SendNotification
func NewNotification(title, body string) *Notification {
	return NewNotificationWithTimeout(title, body, 0)
}

// NewNotificationWithTimeout creates a new notification with an explicit D-Bus timeout
func NewNotificationWithTimeout(title, body string, timeout int32) *Notification {
	return &Notification{
		ID:        lastNotificationID.Add(1),
		Urgency:   UrgencyNormal,
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
	return &Notification{
		ID:        lastNotificationID.Add(1),
		Urgency:   UrgencyNormal,
		AppName:   appName,
		AppID:     appName,
		Icon:      loadNotificationIcon(iconName),
		IconName:  iconName,
		Actions:   actions,
		Title:     title,
		Body:      body,
		Timeout:   timeout,
		Timestamp: time.Now(),
	}
}

// loadNotificationIcon reads an icon given as an absolute file path. Icon theme
// names are resolved later by the user interface, so return nil for them.
func loadNotificationIcon(icon string) fyne.Resource {
	if !filepath.IsAbs(icon) {
		return nil
	}
	res, err := fyne.LoadResourceFromPath(icon)
	if err != nil {
		fyne.LogError("Failed to read notification icon: "+icon, err)
		return nil
	}
	return res
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

// SendNotification posts a given notification into the user interface's
// notification area. A notification with the DBusID or Tag of an earlier one
// replaces it (see Notification.Replaced); a transient one is not kept in
// the history.
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

	s.histMu.Lock()
	if i := s.indexOfReplaced(n); i >= 0 {
		n.ID, n.Replaced = s.history[i].ID, true
		s.history = append(s.history[:i], s.history[i+1:]...)
	} else if n.DBusID != 0 || n.Tag != "" {
		n.Replaced = s.onScreen[replaceKey(n)] != 0
		if n.Replaced {
			n.ID = s.onScreen[replaceKey(n)]
		}
	}
	if n.DBusID != 0 || n.Tag != "" {
		if len(s.onScreen) >= 4*maxHistory {
			clear(s.onScreen)
		}
		s.onScreen[replaceKey(n)] = n.ID
	}
	if !n.Transient {
		s.history = append([]*Notification{n}, s.history...)
		if len(s.history) > maxHistory {
			s.history = s.history[:maxHistory]
		}
	}
	s.histMu.Unlock()
	if !n.Transient {
		s.notifyHistoryChange()
	}

	for _, listen := range listeners {
		listen(n)
	}
}

// replaceKey identifies the notifications that replace one another.
func replaceKey(n *Notification) string {
	if n.DBusID != 0 {
		return fmt.Sprintf("dbus:%d", n.DBusID)
	}
	return "tag:" + n.Tag
}

// indexOfReplaced returns the position in the history of the notification
// that next replaces, or -1. histMu must be held.
func (n *notifications) indexOfReplaced(next *Notification) int {
	if next.DBusID == 0 && next.Tag == "" {
		return -1
	}
	for i, old := range n.history {
		if (next.DBusID != 0 && old.DBusID == next.DBusID) || (next.Tag != "" && old.Tag == next.Tag) {
			return i
		}
	}
	return -1
}

// WithdrawNotification removes the notification that an application closed,
// from the history and from the screen.
func WithdrawNotification(dbusID uint32) {
	s := ensureServer()
	s.histMu.Lock()
	id := s.onScreen[fmt.Sprintf("dbus:%d", dbusID)]
	for i, n := range s.history {
		if n.DBusID == dbusID {
			id = n.ID
			s.history = append(s.history[:i], s.history[i+1:]...)
			break
		}
	}
	delete(s.onScreen, fmt.Sprintf("dbus:%d", dbusID))
	closeListeners := append([]func(uint32){}, s.closeListeners...)
	s.histMu.Unlock()

	if id == 0 {
		return
	}
	s.notifyHistoryChange()
	for _, fn := range closeListeners {
		fn(id)
	}
}

// WithdrawTagged removes the notification with the given tag, from the
// history and from the screen: what it said no longer matters.
func WithdrawTagged(tag string) {
	s := ensureServer()
	s.histMu.Lock()
	id := s.onScreen["tag:"+tag]
	delete(s.onScreen, "tag:"+tag)
	for i, n := range s.history {
		if n.Tag == tag {
			id = n.ID
			s.history = append(s.history[:i], s.history[i+1:]...)
			break
		}
	}
	closeListeners := append([]func(uint32){}, s.closeListeners...)
	s.histMu.Unlock()

	if id == 0 {
		return
	}
	s.notifyHistoryChange()
	for _, fn := range closeListeners {
		fn(id)
	}
}

// AddWithdrawListener registers a callback invoked with the local ID of a
// notification that its application withdrew.
func AddWithdrawListener(fn func(id uint32)) {
	s := ensureServer()
	s.histMu.Lock()
	s.closeListeners = append(s.closeListeners, fn)
	s.histMu.Unlock()
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

	histMu         sync.RWMutex
	history        []*Notification
	histListeners  []func()
	closeListeners []func(id uint32)
	// onScreen maps the replace key of recent notifications to their local
	// ID, so that a transient one still on screen can be replaced too.
	onScreen map[string]uint32
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
	actions []string, hints map[string]interface{}, timeout int32,
) (uint32, error) {
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
	return "Tyde", "Fyne.io", "0", "1.2"
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
		server = &notifications{onScreen: map[string]uint32{}}
		go server.register()
	})
	return server
}
