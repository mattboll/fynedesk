package wm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
)

func TestSendNotification(t *testing.T) {
	var got *Notification
	SetNotificationListener(func(n *Notification) {
		got = n
	})

	message := &Notification{Title: "Test", Body: "Message"}
	SendNotification(message)

	assert.NotNil(t, got)
	assert.Equal(t, "Test", got.Title)
	assert.Equal(t, "Message", got.Body)
}

func TestNewNotification(t *testing.T) {
	n1 := NewNotification("test", "body")
	n2 := NewNotification("test", "body")

	assert.NotZero(t, n1.ID)
	assert.NotEqual(t, n1.ID, n2.ID)
}

func TestNewNotificationWithTimeout(t *testing.T) {
	n := NewNotificationWithTimeout("title", "body", -1)

	assert.Equal(t, "title", n.Title)
	assert.Equal(t, "body", n.Body)
	assert.Equal(t, int32(-1), n.Timeout)
	assert.False(t, n.Timestamp.IsZero())
}

func TestNewNotificationDefaultTimeout(t *testing.T) {
	n := NewNotification("title", "body")

	assert.Equal(t, int32(0), n.Timeout)
	assert.False(t, n.Timestamp.IsZero())
}

func TestNotificationHistory(t *testing.T) {
	// Clear any prior history
	ClearNotificationHistory()

	SetNotificationListener(func(n *Notification) {})

	n1 := NewNotification("first", "body1")
	n2 := NewNotification("second", "body2")
	SendNotification(n1)
	SendNotification(n2)

	history := NotificationHistory()
	assert.Len(t, history, 2)
	assert.Equal(t, "second", history[0].Title) // most recent first
	assert.Equal(t, "first", history[1].Title)
}

func TestNotificationHistoryCap(t *testing.T) {
	ClearNotificationHistory()
	SetNotificationListener(func(n *Notification) {})

	for i := 0; i < maxHistory+10; i++ {
		SendNotification(NewNotification("n", "b"))
	}

	history := NotificationHistory()
	assert.Len(t, history, maxHistory)
}

func TestClearNotificationHistory(t *testing.T) {
	ClearNotificationHistory()
	SetNotificationListener(func(n *Notification) {})

	SendNotification(NewNotification("test", "body"))
	assert.NotEmpty(t, NotificationHistory())

	ClearNotificationHistory()
	assert.Empty(t, NotificationHistory())
}

func TestRemoveNotification(t *testing.T) {
	ClearNotificationHistory()
	SetNotificationListener(func(n *Notification) {})

	n1 := NewNotification("keep", "body")
	n2 := NewNotification("remove", "body")
	SendNotification(n1)
	SendNotification(n2)

	RemoveNotification(n2.ID)

	history := NotificationHistory()
	assert.Len(t, history, 1)
	assert.Equal(t, "keep", history[0].Title)
}

func TestRemoveNotificationNotFound(t *testing.T) {
	ClearNotificationHistory()
	SetNotificationListener(func(n *Notification) {})

	SendNotification(NewNotification("test", "body"))

	// Should not panic with non-existent ID
	RemoveNotification(999999)
	assert.Len(t, NotificationHistory(), 1)
}

func TestHistoryChangeListener(t *testing.T) {
	ClearNotificationHistory()
	SetNotificationListener(func(n *Notification) {})

	callCount := 0
	AddHistoryChangeListener(func() {
		callCount++
	})

	// Add triggers listener
	n := NewNotification("test", "body")
	SendNotification(n)
	assert.Equal(t, 1, callCount)

	// Remove triggers listener
	RemoveNotification(n.ID)
	assert.Equal(t, 2, callCount)

	// Clear triggers listener
	SendNotification(NewNotification("x", "y"))
	ClearNotificationHistory()
	assert.Equal(t, 4, callCount) // +1 send, +1 clear
}

func TestMultipleListeners(t *testing.T) {
	var got1, got2 *Notification
	AddNotificationListener(func(n *Notification) {
		got1 = n
	})
	AddNotificationListener(func(n *Notification) {
		got2 = n
	})

	msg := NewNotification("multi", "test")
	SendNotification(msg)

	assert.NotNil(t, got1)
	assert.NotNil(t, got2)
	assert.Equal(t, "multi", got1.Title)
	assert.Equal(t, "multi", got2.Title)
}

func TestNotificationReplacement(t *testing.T) {
	ClearNotificationHistory()
	var got []*Notification
	AddNotificationListener(func(n *Notification) { got = append(got, n) })

	first := NewNotificationFull("Slack", "", "Alice", "hello", nil, 0)
	first.DBusID = 42
	SendNotification(first)
	update := NewNotificationFull("Slack", "", "Alice", "hello again", nil, 0)
	update.DBusID = 42
	SendNotification(update)

	assert.True(t, update.Replaced)
	assert.Equal(t, first.ID, update.ID, "the update keeps the ID of what it replaces")
	hist := NotificationHistory()
	assert.Len(t, hist, 1)
	assert.Equal(t, "hello again", hist[0].Body)

	tagged := NewNotification("Agent", "working")
	tagged.Tag = "agent:w1:p1"
	SendNotification(tagged)
	done := NewNotification("Agent", "done")
	done.Tag = "agent:w1:p1"
	SendNotification(done)
	assert.True(t, done.Replaced)
	assert.Len(t, NotificationHistory(), 2)
}

func TestTransientNotification(t *testing.T) {
	ClearNotificationHistory()
	AddNotificationListener(func(*Notification) {})

	n := NewNotificationFull("volume", "", "Volume", "50%", nil, 0)
	n.DBusID = 7
	n.Transient = true
	SendNotification(n)
	assert.Empty(t, NotificationHistory())

	// Still replaceable while on screen.
	next := NewNotificationFull("volume", "", "Volume", "60%", nil, 0)
	next.DBusID = 7
	next.Transient = true
	SendNotification(next)
	assert.True(t, next.Replaced)
	assert.Equal(t, n.ID, next.ID)
}

func TestWithdrawNotification(t *testing.T) {
	ClearNotificationHistory()
	AddNotificationListener(func(*Notification) {})
	var withdrawn []uint32
	AddWithdrawListener(func(id uint32) { withdrawn = append(withdrawn, id) })

	n := NewNotificationFull("Slack", "", "Bob", "hi", nil, 0)
	n.DBusID = 99
	SendNotification(n)
	WithdrawNotification(99)

	assert.Empty(t, NotificationHistory())
	assert.Equal(t, []uint32{n.ID}, withdrawn)

	WithdrawNotification(12345) // unknown: nothing happens
	assert.Len(t, withdrawn, 1)
}

func TestParseUrgency(t *testing.T) {
	assert.Equal(t, UrgencyLow, ParseUrgency("low"))
	assert.Equal(t, UrgencyCritical, ParseUrgency("critical"))
	assert.Equal(t, UrgencyNormal, ParseUrgency(""))
}

func TestWithdrawTagged(t *testing.T) {
	ClearNotificationHistory()
	AddNotificationListener(func(*Notification) {})
	n := NewNotification("Claude Code", "done")
	n.Tag = "agent:w1:p1"
	SendNotification(n)
	SendNotification(NewNotification("Other", "stays"))

	WithdrawTagged("agent:w1:p1")
	hist := NotificationHistory()
	if assert.Len(t, hist, 1) {
		assert.Equal(t, "Other", hist[0].Title)
	}
}

func TestBusNotifications(t *testing.T) {
	ClearNotificationHistory()
	AddNotificationListener(func(*Notification) {})
	s := ensureServer()

	first, _ := s.Notify("mail", 0, "", "one", "", nil, nil, 0)
	again, _ := s.Notify("mail", first, "", "two", "", nil, nil, 0)
	assert.Equal(t, first, again, "replaces_id keeps the id")
	if hist := NotificationHistory(); assert.Len(t, hist, 1) {
		assert.Equal(t, "two", hist[0].Title)
	}

	tag := map[string]dbus.Variant{"x-dunst-stack-tag": dbus.MakeVariant("vol")}
	vol, _ := s.Notify("volume", 0, "", "50%", "", nil, tag, 0)
	vol2, _ := s.Notify("volume", 0, "", "60%", "", nil, tag, 0)
	assert.Equal(t, vol, vol2, "the stack tag replaces")
	assert.Len(t, NotificationHistory(), 2)

	low := map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(0)), "transient": dbus.MakeVariant(true)}
	var got *Notification
	AddNotificationListener(func(n *Notification) { got = n })
	_, _ = s.Notify("", 0, "", "quiet", "", nil, low, 0)
	assert.Equal(t, UrgencyLow, got.Urgency)
	assert.True(t, got.Transient)

	_ = s.CloseNotification(first)
	assert.Len(t, NotificationHistory(), 1, "CloseNotification withdraws it")
}

func TestNotificationIconBounded(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	big := filepath.Join(dir, "big.png")
	assert.NoError(t, os.WriteFile(small, []byte("png"), 0o600))
	assert.NoError(t, os.WriteFile(big, make([]byte, maxNotificationIcon+1), 0o600))

	assert.NotNil(t, loadNotificationIcon(small))
	assert.Nil(t, loadNotificationIcon(big), "too big")
	assert.Nil(t, loadNotificationIcon(dir), "a directory")
	assert.Nil(t, loadNotificationIcon("/dev/zero"), "a device")
	assert.Nil(t, loadNotificationIcon("dialog-information"), "a theme name")
}

func TestHoldPopupDuringShare(t *testing.T) {
	SetDoNotDisturb(false)
	if HoldPopup() {
		t.Fatal("popups show when the screen is not shared")
	}
	SetScreenShared(true)
	if !HoldPopup() || !HoldPopup() {
		t.Fatal("popups are held while the screen is shared")
	}
	if held := SetScreenShared(false); held != 2 {
		t.Fatalf("held = %d, want 2", held)
	}
	if HoldPopup() {
		t.Fatal("popups show again once the share ended")
	}
	SetDoNotDisturb(true)
	defer SetDoNotDisturb(false)
	if !HoldPopup() {
		t.Fatal("Do Not Disturb holds the popups")
	}
	if held := SetScreenShared(false); held != 0 {
		t.Fatal("Do Not Disturb popups are not counted as held by a share")
	}
}
