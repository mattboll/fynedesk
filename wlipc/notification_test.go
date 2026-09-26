package wlipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotificationFileOnlyWithoutSocket(t *testing.T) {
	srv, _ := testServer(t, func(*Message) (json.RawMessage, error) { return nil, nil })
	SetDefaultServer(srv)
	t.Cleanup(func() { SetDefaultServer(nil) })
	file := filepath.Join(getConfigDir(), "dbus-notification.json")
	os.Remove(file)

	done := make(chan struct{})
	defer close(done)
	received := make(chan DBusNotification, 1)
	WatchDBusNotification(func(n *DBusNotification) { received <- *n }, done)
	deadline := time.Now().Add(2 * time.Second)
	for srv.Broadcast(EventNotification, struct{}{}) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	<-received // the probe

	if err := NotifyDBusNotification(DBusNotification{Title: "code 123456"}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-received:
		if n.Title != "code 123456" {
			t.Fatalf("got %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("not delivered over the socket")
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("delivered over the socket, and still written to disk")
	}
}

func TestStaleNotificationFileIgnored(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // no socket: the file is used
	file := filepath.Join(getConfigDir(), "dbus-notification.json")
	os.MkdirAll(filepath.Dir(file), 0o700)
	old, _ := json.Marshal(DBusNotification{Title: "yesterday", Timestamp: time.Now().Add(-time.Hour).UnixMilli()})
	if err := os.WriteFile(file, old, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	defer close(done)
	received := make(chan DBusNotification, 1)
	WatchDBusNotification(func(n *DBusNotification) { received <- *n }, done)

	select {
	case n := <-received:
		t.Fatalf("stale notification shown: %+v", n)
	case <-time.After(300 * time.Millisecond):
	}
	if err := NotifyDBusNotification(DBusNotification{Title: "now"}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-received:
		if n.Title != "now" {
			t.Fatalf("got %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fresh notification not read from the file")
	}
}
