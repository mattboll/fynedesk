package wm

import (
	"testing"

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
