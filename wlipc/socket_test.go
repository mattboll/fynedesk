package wlipc

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testServer creates an IPCServer on a temporary socket for testing.
func testServer(t *testing.T, handler RequestHandler) (*IPCServer, string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	t.Setenv("XDG_RUNTIME_DIR", dir)

	srv, err := NewIPCServer(handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv, sock
}

// testConnect creates a client connected to the test server.
func testConnect(t *testing.T) *IPCClient {
	t.Helper()
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestIPCServer_AcceptsConnection(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	c := testConnect(t)
	_ = c

	// Wait briefly for the server to register the client
	time.Sleep(50 * time.Millisecond)
	if srv.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", srv.ClientCount())
	}
}

func TestIPCServer_RequestResponse(t *testing.T) {
	testServer(t, func(msg *Message) (json.RawMessage, error) {
		if msg.Name == ReqListWindows {
			data, _ := json.Marshal(map[string]any{
				"windows":   []any{},
				"timestamp": 12345,
			})
			return data, nil
		}
		return nil, nil
	})

	c := testConnect(t)

	resp, err := c.Request(ReqListWindows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "ok" {
		t.Errorf("expected response name 'ok', got %q", resp.Name)
	}
	if resp.Data == nil {
		t.Fatal("expected response data, got nil")
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatal(err)
	}
	if result["timestamp"] != float64(12345) {
		t.Errorf("expected timestamp 12345, got %v", result["timestamp"])
	}
}

func TestIPCServer_GetDesktop(t *testing.T) {
	testServer(t, func(msg *Message) (json.RawMessage, error) {
		if msg.Name == ReqGetDesktop {
			data, _ := json.Marshal(DesktopState{Current: 2, NumDesks: 4})
			return data, nil
		}
		return nil, nil
	})

	c := testConnect(t)

	resp, err := c.Request(ReqGetDesktop, nil)
	if err != nil {
		t.Fatal(err)
	}

	var state DesktopState
	if err := json.Unmarshal(resp.Data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Current != 2 || state.NumDesks != 4 {
		t.Errorf("expected desktop 2/4, got %d/%d", state.Current, state.NumDesks)
	}
}

func TestIPCServer_Subscribe_Broadcast(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	c := testConnect(t)

	// Subscribe to desktop-state events
	err := c.Subscribe(EventDesktopState)
	if err != nil {
		t.Fatal(err)
	}

	// Broadcast a desktop-state event
	time.Sleep(50 * time.Millisecond) // let subscribe register
	srv.Broadcast(EventDesktopState, DesktopState{Current: 1, NumDesks: 4})

	// Read the event
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	event := c.ReadEvent()
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if event.Name != EventDesktopState {
		t.Errorf("expected event name %q, got %q", EventDesktopState, event.Name)
	}

	var state DesktopState
	if err := json.Unmarshal(event.Data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Current != 1 {
		t.Errorf("expected current desktop 1, got %d", state.Current)
	}
}

func TestIPCServer_UnsubscribedClientDoesNotReceive(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	c := testConnect(t)

	// Subscribe to windows-state only
	err := c.Subscribe(EventWindowsState)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)

	// Broadcast a desktop-state event (client not subscribed)
	srv.Broadcast(EventDesktopState, DesktopState{Current: 0, NumDesks: 4})

	// Try to read — should timeout since client isn't subscribed to this event
	c.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	event := c.ReadEvent()
	if event != nil {
		t.Errorf("expected no event, got %+v", event)
	}
}

func TestIPCServer_MultipleClients(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		if msg.Name == ReqGetDesktop {
			data, _ := json.Marshal(DesktopState{Current: 0, NumDesks: 4})
			return data, nil
		}
		return nil, nil
	})

	c1 := testConnect(t)
	c2 := testConnect(t)

	time.Sleep(50 * time.Millisecond)
	if srv.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", srv.ClientCount())
	}

	// Both clients can make requests independently
	resp1, err := c1.Request(ReqGetDesktop, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := c2.Request(ReqGetDesktop, nil)
	if err != nil {
		t.Fatal(err)
	}

	if resp1.Name != "ok" || resp2.Name != "ok" {
		t.Errorf("expected both responses ok, got %q and %q", resp1.Name, resp2.Name)
	}
}

func TestIPCClient_SendRequest_FireAndForget(t *testing.T) {
	received := make(chan string, 1)
	testServer(t, func(msg *Message) (json.RawMessage, error) {
		received <- msg.Name
		return nil, nil
	})

	c := testConnect(t)

	err := c.SendRequest(ReqDesktopSwitch, DesktopSwitchRequest{Desktop: 2})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case name := <-received:
		if name != ReqDesktopSwitch {
			t.Errorf("expected request %q, got %q", ReqDesktopSwitch, name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request")
	}
}

func TestIPCServer_ClientDisconnect(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	conn, err := net.DialTimeout("unix", SocketPath(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if srv.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", srv.ClientCount())
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)
	if srv.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", srv.ClientCount())
	}
}

func TestTrySocketWatch_DesktopState(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	done := make(chan struct{})
	defer close(done)

	received := make(chan DesktopState, 1)
	WatchDesktopState(func(state *DesktopState) {
		received <- *state
	}, done)

	// Let the client connect and subscribe
	time.Sleep(100 * time.Millisecond)

	// Broadcast a desktop state event
	srv.Broadcast(EventDesktopState, DesktopState{Current: 3, NumDesks: 6})

	select {
	case state := <-received:
		if state.Current != 3 || state.NumDesks != 6 {
			t.Errorf("expected desktop 3/6, got %d/%d", state.Current, state.NumDesks)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for desktop state via socket")
	}
}

func TestTrySocketWatch_WindowsState(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	done := make(chan struct{})
	defer close(done)

	received := make(chan WindowsState, 1)
	WatchWindowsState(func(state *WindowsState) {
		received <- *state
	}, done)

	time.Sleep(100 * time.Millisecond)

	srv.Broadcast(EventWindowsState, WindowsState{
		Windows:   []WindowInfo{{ID: "xdg-1", Title: "Test"}},
		Timestamp: 999,
	})

	select {
	case state := <-received:
		if len(state.Windows) != 1 || state.Windows[0].ID != "xdg-1" {
			t.Errorf("unexpected windows state: %+v", state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for windows state via socket")
	}
}

func TestTrySocketWatch_VolumeEvent(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	done := make(chan struct{})
	defer close(done)

	received := make(chan struct{}, 1)
	WatchVolumeEvent(func() {
		received <- struct{}{}
	}, done)

	time.Sleep(100 * time.Millisecond)

	srv.Broadcast(EventVolumeChange, struct{ Timestamp int64 }{Timestamp: 123})

	select {
	case <-received:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for volume event via socket")
	}
}

func TestTrySocketWatch_FallbackToFile(t *testing.T) {
	// No server running — should fall back to file polling without error.
	// Use a custom XDG_RUNTIME_DIR that has no socket.
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	done := make(chan struct{})

	// WatchDesktopState should not panic and should start file polling
	WatchDesktopState(func(state *DesktopState) {}, done)

	// Give the goroutine time to start, then stop it cleanly
	time.Sleep(50 * time.Millisecond)
	close(done)
}

func TestTrySocketWatch_LauncherRequest(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	done := make(chan struct{})
	defer close(done)

	received := make(chan struct{}, 1)
	WatchLauncherRequest(func() {
		received <- struct{}{}
	}, done)

	time.Sleep(100 * time.Millisecond)

	srv.Broadcast(EventLauncherRequest, struct{ Timestamp int64 }{Timestamp: 123})

	select {
	case <-received:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for launcher request via socket")
	}
}

func TestTrySocketWatch_Notification(t *testing.T) {
	srv, _ := testServer(t, func(msg *Message) (json.RawMessage, error) {
		return nil, nil
	})

	done := make(chan struct{})
	defer close(done)

	received := make(chan DBusNotification, 1)
	WatchDBusNotification(func(n *DBusNotification) {
		received <- *n
	}, done)

	time.Sleep(100 * time.Millisecond)

	srv.Broadcast(EventNotification, DBusNotification{
		Title: "Test", Body: "Hello", Timestamp: 123,
	})

	select {
	case n := <-received:
		if n.Title != "Test" || n.Body != "Hello" {
			t.Errorf("unexpected notification: %+v", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notification via socket")
	}
}

func TestSocketPath(t *testing.T) {
	// With XDG_RUNTIME_DIR set
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/test-runtime")
	path := SocketPath()
	if path != "/tmp/test-runtime/fynedesk.sock" {
		t.Errorf("expected /tmp/test-runtime/fynedesk.sock, got %s", path)
	}

	// Without XDG_RUNTIME_DIR — falls back to /run/user/$UID
	os.Unsetenv("XDG_RUNTIME_DIR")
	path = SocketPath()
	expected := filepath.Join("/run/user", fmt.Sprintf("%d", os.Getuid()), "fynedesk.sock")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}
