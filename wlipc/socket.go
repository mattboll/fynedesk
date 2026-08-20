// Package wlipc — UNIX socket IPC protocol for FyneDesk.
//
// Protocol: JSON-line over UNIX domain socket at /run/user/$UID/fynedesk.sock.
// Each message is a single JSON object terminated by newline (\n).
//
// Message envelope:
//
//	{"type":"event|request|response","id":123,"name":"...", "data":{...}}
//
// Clients connect, optionally subscribe to events, and send requests.
// The compositor pushes events to subscribed clients.
package wlipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Message is the envelope for all socket IPC messages.
type Message struct {
	Type string          `json:"type"`           // "event", "request", "response"
	ID   int64           `json:"id,omitempty"`   // Request ID for correlation
	Name string          `json:"name"`           // Message name (e.g., "windows-state")
	Data json.RawMessage `json:"data,omitempty"` // Payload
}

// Event names pushed by the compositor.
const (
	EventWindowsState     = "windows-state"
	EventDesktopState     = "desktop-state"
	EventNotification     = "notification"
	EventScreenshot       = "screenshot"
	EventClipboardHist    = "clipboard-history"
	EventKeyboardLayout   = "keyboard-layout"
	EventVolumeChange     = "volume-change"
	EventBrightnessChange = "brightness-change"
	EventLauncherRequest  = "launcher-request"
	EventEmojiPicker      = "emoji-picker"
	EventContextMenu      = "context-menu"
	EventClipboardShow    = "clipboard-show"
	EventCommandPalette   = "command-palette"
	EventSidebar          = "sidebar-toggle"
	EventOverview         = "overview"
	EventPanelHotspot     = "panel-hotspot"
)

// Request names sent by clients.
const (
	ReqSubscribe          = "subscribe"
	ReqWindowAction       = "window-action"
	ReqDesktopSwitch      = "desktop-switch"
	ReqSettingsChanged    = "settings-changed"
	ReqLayoutRequest      = "layout-request"
	ReqKeyboardLayout     = "keyboard-layout"
	ReqEmojiPaste         = "emoji-paste"
	ReqClipboardPaste     = "clipboard-paste"
	ReqClipboardClear     = "clipboard-clear"
	ReqOverlay            = "overlay"
	ReqLock               = "lock"
	ReqLogout             = "logout"
	ReqRestart            = "restart"
	ReqListWindows        = "list-windows"
	ReqGetDesktop         = "get-desktop"
	ReqShutdown           = "shutdown"
	ReqHibernate          = "hibernate"
	ReqSuspend            = "suspend"
	ReqCompositorAction   = "compositor-action"
	ReqWindowPreview      = "window-preview"
	ReqRaiseByTitle       = "raise-by-title"
	ReqRaiseByClass       = "raise-by-class"
	ReqNotificationAction = "notification-action"
)

// SubscribeRequest is sent by clients to register for events.
type SubscribeRequest struct {
	Events []string `json:"events"` // Event names to subscribe to
}

// DesktopSwitchRequest is sent by clients to change virtual desktop.
type DesktopSwitchRequest struct {
	Desktop int `json:"desktop"`
}

// Note: EmojiPasteRequest, ClipboardPasteRequest, and KeyboardLayoutRequest
// are defined in wlipc.go and keyboard_layout.go respectively.
// Socket requests reuse those types (the data payload is JSON-decoded
// by the request handler, so extra fields like Timestamp are harmless).

// SocketPath returns the UNIX socket path for the IPC server.
func SocketPath() string {
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = filepath.Join("/run/user", fmt.Sprintf("%d", os.Getuid()))
	}
	return filepath.Join(runDir, "fynedesk.sock")
}

// --- Server ---

// IPCServer manages the UNIX socket server and connected clients.
type IPCServer struct {
	listener net.Listener
	mu       sync.RWMutex
	clients  map[*ipcClient]struct{}
	handler  RequestHandler
	done     chan struct{}
}

// RequestHandler processes incoming requests from clients.
// Return value is sent as response data (nil = no data).
type RequestHandler func(msg *Message) (json.RawMessage, error)

type ipcClient struct {
	conn       net.Conn
	writer     *bufio.Writer
	mu         sync.Mutex // protects writer (used by request/response paths)
	subs       map[string]bool
	broadcasts chan []byte   // buffered; events drop-oldest when full
	done       chan struct{} // closed when the client is being torn down
}

// broadcastBufferSize is the per-client broadcast queue depth. A slow
// consumer that can't keep up will see older events dropped rather than
// blocking the broadcast loop for everyone.
const broadcastBufferSize = 16

// NewIPCServer creates and starts the IPC server.
func NewIPCServer(handler RequestHandler) (*IPCServer, error) {
	sockPath := SocketPath()

	// Remove stale socket
	os.Remove(sockPath)

	// Set a tight umask before bind so the socket is created mode 0700
	// instead of being briefly world-accessible between Listen and Chmod.
	prevMask := syscall.Umask(0077)
	listener, err := net.Listen("unix", sockPath)
	syscall.Umask(prevMask)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", sockPath, err)
	}
	// Belt-and-braces: enforce 0700 even if the umask path didn't take.
	os.Chmod(sockPath, 0700)

	srv := &IPCServer{
		listener: listener,
		clients:  make(map[*ipcClient]struct{}),
		handler:  handler,
		done:     make(chan struct{}),
	}

	go srv.acceptLoop()

	log.Printf("[IPC] Socket server listening on %s\n", sockPath)
	return srv, nil
}

// Close shuts down the server and all connections.
func (s *IPCServer) Close() {
	close(s.done)
	s.listener.Close()

	s.mu.Lock()
	for c := range s.clients {
		c.conn.Close()
	}
	s.clients = nil
	s.mu.Unlock()

	os.Remove(SocketPath())
}

// Broadcast sends an event to all clients subscribed to the given event name.
func (s *IPCServer) Broadcast(eventName string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := Message{
		Type: "event",
		Name: eventName,
		Data: raw,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return
	}
	line = append(line, '\n')

	s.mu.RLock()
	defer s.mu.RUnlock()

	for c := range s.clients {
		c.mu.Lock()
		subscribed := c.subs[eventName]
		c.mu.Unlock()
		if !subscribed {
			continue
		}
		// Non-blocking send. If the client's queue is full (slow consumer),
		// drop the oldest event to make room — broadcasts are state snapshots
		// so the latest is what matters.
		select {
		case c.broadcasts <- line:
		default:
			select {
			case <-c.broadcasts:
			default:
			}
			select {
			case c.broadcasts <- line:
			default:
			}
		}
	}
}

// ClientCount returns the number of connected clients.
func (s *IPCServer) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *IPCServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("[IPC] Accept error: %v\n", err)
				continue
			}
		}

		client := &ipcClient{
			conn:       conn,
			writer:     bufio.NewWriter(conn),
			subs:       make(map[string]bool),
			broadcasts: make(chan []byte, broadcastBufferSize),
			done:       make(chan struct{}),
		}

		s.mu.Lock()
		s.clients[client] = struct{}{}
		s.mu.Unlock()

		go s.broadcastWriter(client)
		go s.handleClient(client)
	}
}

// broadcastWriter drains the client's broadcast queue. Running in a per-client
// goroutine means a slow consumer can't block the broadcast loop or any other
// subscriber.
func (s *IPCServer) broadcastWriter(c *ipcClient) {
	for {
		select {
		case <-c.done:
			return
		case line := <-c.broadcasts:
			c.mu.Lock()
			_, werr := c.writer.Write(line)
			if werr == nil {
				werr = c.writer.Flush()
			}
			c.mu.Unlock()
			if werr != nil {
				// Connection is broken; close it so handleClient exits and
				// the cleanup path runs.
				c.conn.Close()
				return
			}
		}
	}
}

// preSubscribeTimeout is the read deadline applied before a client has
// subscribed to any event. A client that does not send a complete message
// within this window is disconnected so it can't tie up a goroutine
// indefinitely. After the first subscription, the deadline is cleared
// because legitimate subscribers stay quiet for hours waiting for events.
const preSubscribeTimeout = 60 * time.Second

func (s *IPCServer) handleClient(c *ipcClient) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		// Signal the broadcast writer to exit, then close the connection.
		select {
		case <-c.done:
		default:
			close(c.done)
		}
		c.conn.Close()
	}()

	// 1 MiB max message — large enough for window-preview PNG responses
	// while still bounding memory per connection.
	const maxMsg = 1 << 20
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 64*1024), maxMsg)

	for {
		c.mu.Lock()
		hasSubs := len(c.subs) > 0
		c.mu.Unlock()
		if hasSubs {
			_ = c.conn.SetReadDeadline(time.Time{})
		} else {
			_ = c.conn.SetReadDeadline(time.Now().Add(preSubscribeTimeout))
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				log.Printf("[IPC] scanner: %v", err)
			}
			break
		}
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("[IPC] Parse error: %v\n", err)
			continue
		}

		if msg.Type != "request" {
			continue
		}

		// Handle subscribe specially
		if msg.Name == ReqSubscribe {
			var sub SubscribeRequest
			if err := json.Unmarshal(msg.Data, &sub); err == nil {
				c.mu.Lock()
				for _, ev := range sub.Events {
					c.subs[ev] = true
				}
				c.mu.Unlock()
			}
			s.sendResponse(c, msg.ID, nil, nil)
			continue
		}

		// Dispatch to handler
		data, err := s.handler(&msg)
		s.sendResponse(c, msg.ID, data, err)
	}
}

func (s *IPCServer) sendResponse(c *ipcClient, reqID int64, data json.RawMessage, err error) {
	resp := Message{
		Type: "response",
		ID:   reqID,
		Name: "ok",
	}
	if err != nil {
		resp.Name = "error"
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		resp.Data = errData
	} else if data != nil {
		resp.Data = data
	}

	line, _ := json.Marshal(resp)
	line = append(line, '\n')

	c.mu.Lock()
	c.writer.Write(line)
	c.writer.Flush()
	c.mu.Unlock()
}

// --- Default server (used by compositor-side Notify* functions) ---

var (
	defaultServerMu sync.RWMutex
	defaultServer   *IPCServer
)

// SetDefaultServer sets the global socket server used by Notify*/Write* functions
// to broadcast events alongside file-based IPC.
func SetDefaultServer(s *IPCServer) {
	defaultServerMu.Lock()
	defaultServer = s
	defaultServerMu.Unlock()
}

// broadcastIfServer broadcasts an event if a default server is registered.
func broadcastIfServer(eventName string, data any) {
	defaultServerMu.RLock()
	srv := defaultServer
	defaultServerMu.RUnlock()
	if srv != nil {
		srv.Broadcast(eventName, data)
	}
}

// --- Default client (used by Request* functions for transparent socket upgrade) ---

var (
	defaultClientMu sync.RWMutex
	defaultClient   *IPCClient
)

// SetDefaultClient sets the global socket client used by Request* functions.
// When set, outgoing requests go via socket instead of file-based IPC.
func SetDefaultClient(c *IPCClient) {
	defaultClientMu.Lock()
	defaultClient = c
	defaultClientMu.Unlock()
}

// DefaultClient returns the global socket client, or nil if not connected.
func DefaultClient() *IPCClient {
	defaultClientMu.RLock()
	defer defaultClientMu.RUnlock()
	return defaultClient
}

// trySendRequest attempts to send a request via the default socket client.
// Returns true if the request was sent successfully.
func trySendRequest(name string, data any) bool {
	c := DefaultClient()
	if c == nil {
		return false
	}
	return c.SendRequest(name, data) == nil
}

// --- Client ---

// IPCClient connects to the compositor socket server.
type IPCClient struct {
	conn       net.Conn
	scanner    *bufio.Scanner
	writer     *bufio.Writer
	mu         sync.Mutex // protects writer + nextID
	nextID     int64
	listeners  map[string][]EventCallback
	listenerMu sync.RWMutex

	// Response demux: when the read pump is active, responses are routed
	// through pending channels instead of being read directly by Request.
	readPumpOnce sync.Once
	pendingMu    sync.Mutex
	pending      map[int64]chan *Message // request ID → response channel
}

// Connect establishes a connection to the IPC server.
// Returns nil if the socket doesn't exist (compositor not running or file-based mode).
func Connect() (*IPCClient, error) {
	sockPath := SocketPath()
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	return &IPCClient{
		conn:      conn,
		scanner:   scanner,
		writer:    bufio.NewWriter(conn),
		listeners: make(map[string][]EventCallback),
	}, nil
}

// Close disconnects from the server.
func (c *IPCClient) Close() {
	c.conn.Close()
}

// Subscribe registers for the given event types.
func (c *IPCClient) Subscribe(events ...string) error {
	sub := SubscribeRequest{Events: events}
	_, err := c.Request(ReqSubscribe, sub)
	return err
}

// Request sends a request and waits for the response.
func (c *IPCClient) Request(name string, data any) (*Message, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID

	raw, err := json.Marshal(data)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}

	msg := Message{
		Type: "request",
		ID:   id,
		Name: name,
		Data: raw,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	line = append(line, '\n')

	// If the read pump is active, register a pending channel BEFORE sending
	// so the pump can route the response to us.
	var ch chan *Message
	c.pendingMu.Lock()
	if c.pending != nil {
		ch = make(chan *Message, 1)
		c.pending[id] = ch
	}
	c.pendingMu.Unlock()

	_, err = c.writer.Write(line)
	if err != nil {
		c.mu.Unlock()
		c.removePending(id)
		return nil, err
	}
	err = c.writer.Flush()
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, err
	}

	if ch != nil {
		// Read pump is active — wait for the response via channel
		resp, ok := <-ch
		if !ok || resp == nil {
			return nil, fmt.Errorf("connection closed")
		}
		return resp, nil
	}

	// No read pump — read directly (used before ListenEvents is called)
	for c.scanner.Scan() {
		var resp Message
		if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
			continue
		}
		if resp.Type == "response" && resp.ID == id {
			return &resp, nil
		}
	}
	return nil, fmt.Errorf("connection closed")
}

func (c *IPCClient) removePending(id int64) {
	c.pendingMu.Lock()
	if c.pending != nil {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

// ReadEvent reads the next event from the server (blocking).
// Returns nil when the connection is closed.
func (c *IPCClient) ReadEvent() *Message {
	for c.scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(c.scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type == "event" {
			return &msg
		}
	}
	return nil
}

// EventCallback is called when an event is received via socket IPC.
type EventCallback func(data json.RawMessage)

// OnEvent registers a callback for a specific event name.
// Must be called before ListenEvents. Thread-safe for registration.
func (c *IPCClient) OnEvent(eventName string, cb EventCallback) {
	c.listenerMu.Lock()
	defer c.listenerMu.Unlock()
	c.listeners[eventName] = append(c.listeners[eventName], cb)
}

// ListenEvents starts a background goroutine that reads events from the
// server and dispatches them to registered callbacks. It blocks until the
// done channel is closed or the connection is lost. Call Subscribe() first
// to register for the desired event types.
func (c *IPCClient) ListenEvents(done <-chan struct{}) {
	// Start the read pump exactly once. It reads all messages from the socket
	// and dispatches events to listeners and responses to pending Request callers.
	c.readPumpOnce.Do(func() {
		c.pendingMu.Lock()
		c.pending = make(map[int64]chan *Message)
		c.pendingMu.Unlock()

		go func() {
			go func() {
				<-done
				c.conn.Close()
			}()

			for c.scanner.Scan() {
				var msg Message
				if err := json.Unmarshal(c.scanner.Bytes(), &msg); err != nil {
					continue
				}
				if msg.Type == "response" {
					c.pendingMu.Lock()
					ch := c.pending[msg.ID]
					delete(c.pending, msg.ID)
					c.pendingMu.Unlock()
					if ch != nil {
						ch <- &msg
					}
					continue
				}
				if msg.Type == "event" {
					c.listenerMu.RLock()
					cbs := c.listeners[msg.Name]
					c.listenerMu.RUnlock()
					for _, cb := range cbs {
						cb(msg.Data)
					}
				}
			}

			// Connection closed — unblock any pending requests
			c.pendingMu.Lock()
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.pendingMu.Unlock()
		}()
	})
}

// SendRequest sends a fire-and-forget request (no response wait).
func (c *IPCClient) SendRequest(name string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextID++
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}

	msg := Message{
		Type: "request",
		ID:   c.nextID,
		Name: name,
		Data: raw,
	}
	line, _ := json.Marshal(msg)
	line = append(line, '\n')

	_, err = c.writer.Write(line)
	if err != nil {
		return err
	}
	return c.writer.Flush()
}

// trySocketWatch attempts to connect to the socket server, subscribe to the
// given event, and start listening. On success it registers the callback and
// returns true. On failure (socket not available) it returns false so the
// caller can fall back to file-based polling.
func trySocketWatch(eventName string, cb EventCallback, done <-chan struct{}) bool {
	client, err := Connect()
	if err != nil {
		return false
	}

	if err := client.Subscribe(eventName); err != nil {
		client.Close()
		return false
	}

	client.OnEvent(eventName, cb)
	client.ListenEvents(done)
	return true
}
