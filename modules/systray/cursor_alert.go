package systray

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/godbus/dbus/v5"

	"fyshos.com/tyde/wlipc"
)

// slackRedDot matches the tooltip of Slack's tray icon while it shows its
// red dot: someone mentioned the user or wrote to them. With the blue dot
// (unread messages) it says "You have unread messages", and "No unread
// messages" without any.
var slackRedDot = regexp.MustCompile(`^(You have|Vous avez) \d+ notifications?$`)

// trayToolTip is the ToolTip property of a StatusNotifierItem.
type trayToolTip struct {
	IconName   string
	IconPixmap []struct {
		W, H int32
		Data []byte
	}
	Title, Text string
}

// updateCursorAlert turns the red cursor on while Slack's tray icon shows
// its red dot, and off when it no longer does.
func (t *tray) updateCursorAlert(sender dbus.Sender, item *node) {
	if !wlipc.IsWaylandSession() || !isSlack(item.pid) || item.obj == nil {
		return
	}
	ctx, cancel := callCtx()
	defer cancel()
	var v dbus.Variant
	err := item.obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0,
		"org.kde.StatusNotifierItem", "ToolTip").Store(&v)
	var tip trayToolTip
	if err == nil {
		err = v.Store(&tip)
	}
	if err != nil {
		log.Printf("[SYSTRAY] Slack tooltip: %v", err)
		return
	}
	text := strings.TrimSpace(tip.Title)
	if text == "" {
		text = strings.TrimSpace(tip.Text)
	}
	t.setCursorAlert(sender, slackRedDot.MatchString(text))
}

// setCursorAlert records whether the icon of sender asks for the red
// cursor, and tells the compositor when the cursor must change.
func (t *tray) setCursorAlert(sender dbus.Sender, on bool) {
	t.lock.Lock()
	if on {
		t.alerts[sender] = true
	} else {
		delete(t.alerts, sender)
	}
	alert := len(t.alerts) > 0
	changed := alert != t.alertOn
	t.alertOn = alert
	t.lock.Unlock()
	if !changed {
		return
	}
	log.Printf("[SYSTRAY] red cursor: %v", alert)
	if err := wlipc.RequestCursorAlert(alert); err != nil {
		log.Printf("[SYSTRAY] %v", err)
	}
}

// isSlack reports whether the process pid is Slack.
func isSlack(pid uint32) bool {
	if pid == 0 {
		return false
	}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return err == nil && strings.TrimSpace(string(comm)) == "slack"
}
