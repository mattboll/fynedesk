// Package wlipc — UNIX socket IPC protocol for Tyde.
//
// Protocol: JSON-line over UNIX domain socket at /run/user/$UID/tyde-compositor.sock.
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
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
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
	EventNotifClosed      = "notification-closed"
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
	EventNextAgent        = "next-agent"
	EventOverview         = "overview"
	EventPanelHotspot     = "panel-hotspot"
	EventNightLight       = "night-light" // NightLightEvent: toggled by its shortcut
)

// AllEvents are all the events the compositor pushes.
var AllEvents = []string{
	EventWindowsState, EventDesktopState, EventNotification, EventNotifClosed,
	EventScreenshot, EventClipboardHist, EventKeyboardLayout, EventVolumeChange,
	EventBrightnessChange, EventLauncherRequest, EventEmojiPicker,
	EventContextMenu, EventClipboardShow, EventCommandPalette, EventSidebar,
	EventNextAgent, EventOverview, EventPanelHotspot, EventNightLight,
}

// NightLightEvent tells the panel the night light was toggled by its
// shortcut, for it to save the setting.
type NightLightEvent struct {
	Enabled bool `json:"enabled"`
}

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
	ReqWindowAttention    = "window-attention"
	ReqDockIcons          = "dock-icons"
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
	if p := os.Getenv(SocketEnv); p != "" {
		return p
	}
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	if runDir == "" {
		runDir = filepath.Join("/run/user", fmt.Sprintf("%d", os.Getuid()))
	}
	return filepath.Join(runDir, "tyde-compositor.sock")
}

// SocketEnv names the socket to use instead of the default one. A nested
// compositor sets it for itself and the programs it starts, so that it
// never touches the socket of the session it runs in.
const SocketEnv = "TYDE_IPC_SOCKET"

// --- Server ---

// IPCServer manages the UNIX socket server and connected clients.
type IPCServer struct {
	listener net.Listener
	path     string      // where it listens
	file     os.FileInfo // the socket file it made, to remove only that one

	closeOnce sync.Once

	mu      sync.RWMutex
	clients map[*ipcClient]struct{}
	handler RequestHandler
	done    chan struct{}
}

// RequestHandler processes incoming requests from clients.
// Return value is sent as response data (nil = no data).
type RequestHandler func(msg *Message) (json.RawMessage, error)

type ipcClient struct {
	conn   net.Conn
	writer *bufio.Writer
	mu     sync.Mutex // protects writer (used by request/response paths)
	// subsMu protects subs. It is not mu: Broadcast reads subs from the
	// compositor's main thread, and must not wait for a write in progress.
	subsMu     sync.Mutex
	subs       map[string]bool
	broadcasts chan []byte   // buffered; events drop-oldest when full
	done       chan struct{} // closed when the client is being torn down
}

// broadcastBufferSize is the per-client broadcast queue depth. A slow
// consumer that can't keep up will see older events dropped rather than
// blocking the broadcast loop for everyone.
const broadcastBufferSize = 16

// SocketServed reports whether a compositor answers on the socket.
func SocketServed() bool {
	c, err := net.DialTimeout("unix", SocketPath(), 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// NewIPCServer creates and starts the IPC server.
func NewIPCServer(handler RequestHandler) (*IPCServer, error) {
	sockPath := SocketPath()

	// A socket another compositor answers on is not ours to replace.
	if SocketServed() {
		return nil, fmt.Errorf("%s: another compositor is serving it", sockPath)
	}
	os.Remove(sockPath) // stale

	// The runtime directory is private (0700), so the socket is never
	// reachable by others between Listen and Chmod.
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", sockPath, err)
	}
	// Close removes the file itself, only if it is still ours.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	os.Chmod(sockPath, 0o700)
	file, _ := os.Stat(sockPath)

	srv := &IPCServer{
		listener: listener,
		path:     sockPath,
		file:     file,
		clients:  make(map[*ipcClient]struct{}),
		handler:  handler,
		done:     make(chan struct{}),
	}

	go srv.acceptLoop()

	log.Printf("[IPC] Socket server listening on %s\n", sockPath)
	return srv, nil
}

// Close shuts down the server and all connections; later calls do nothing.
func (s *IPCServer) Close() {
	s.closeOnce.Do(s.close)
}

func (s *IPCServer) close() {
	close(s.done)
	s.listener.Close()

	s.mu.Lock()
	for c := range s.clients {
		c.conn.Close()
	}
	s.clients = nil
	s.mu.Unlock()

	if cur, err := os.Stat(s.path); err == nil && s.file != nil && os.SameFile(cur, s.file) {
		os.Remove(s.path)
	}
}

// Broadcast sends an event to all clients subscribed to the given event name,
// and returns how many there are.
func (s *IPCServer) Broadcast(eventName string, data any) int {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0
	}
	msg := Message{
		Type: "event",
		Name: eventName,
		Data: raw,
	}
	line, err := json.Marshal(msg)
	if err != nil {
		return 0
	}
	line = append(line, '\n')

	s.mu.RLock()
	defer s.mu.RUnlock()

	reached := 0
	for c := range s.clients {
		c.subsMu.Lock()
		subscribed := c.subs[eventName]
		c.subsMu.Unlock()
		if !subscribed {
			continue
		}
		reached++
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
	return reached
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
			werr := c.write(line)
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
		c.subsMu.Lock()
		hasSubs := len(c.subs) > 0
		c.subsMu.Unlock()
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
				c.subsMu.Lock()
				for _, ev := range sub.Events {
					c.subs[ev] = true
				}
				c.subsMu.Unlock()
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

	if c.write(line) != nil {
		c.conn.Close() // handleClient exits and cleans up
	}
}

// clientWriteTimeout bounds a write to a client: one that stopped reading
// is dropped instead of holding its goroutines forever.
const clientWriteTimeout = 5 * time.Second

// write sends one line to the client.
func (c *ipcClient) write(line []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout))
	_, err := c.writer.Write(line)
	if err == nil {
		err = c.writer.Flush()
	}
	return err
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

// broadcastIfServer broadcasts an event if a default server is registered,
// and returns how many clients it reached.
func broadcastIfServer(eventName string, data any) int {
	defaultServerMu.RLock()
	srv := defaultServer
	defaultServerMu.RUnlock()
	if srv == nil {
		return 0
	}
	return srv.Broadcast(eventName, data)
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

// IPCClient connects to the compositor socket server. A single goroutine
// reads the connection from Connect on: responses go to the Request waiting
// for them, events to the listeners once ListenEvents is called (those that
// come before are kept for then, or for ReadEvent).
type IPCClient struct {
	conn   net.Conn
	writer *bufio.Writer
	mu     sync.Mutex // protects writer + nextID
	nextID int64

	listenerMu sync.RWMutex
	listeners  map[string][]EventCallback
	listening  bool          // ListenEvents was called
	events     chan *Message // events before that, and for ReadEvent

	pendingMu sync.Mutex
	pending   map[int64]chan *Message // request ID → response channel
	closed    chan struct{}           // closed when the connection ends

	listenOnce sync.Once
}

// clientMaxMessage bounds a message from the server: larger than the
// server's own bound on requests, as responses and events carry more
// (window lists, previews).
const clientMaxMessage = 4 << 20

// requestTimeout bounds the wait for a response.
const requestTimeout = 10 * time.Second

// Connect establishes a connection to the IPC server.
// Returns an error if the socket doesn't exist (compositor not running or file-based mode).
func Connect() (*IPCClient, error) {
	sockPath := SocketPath()
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return nil, err
	}
	c := &IPCClient{
		conn:      conn,
		writer:    bufio.NewWriter(conn),
		listeners: make(map[string][]EventCallback),
		events:    make(chan *Message, 256),
		pending:   make(map[int64]chan *Message),
		closed:    make(chan struct{}),
	}
	go c.readPump()
	return c, nil
}

// readPump reads every message of the connection until it ends, then closes
// it and unblocks whoever waits.
func (c *IPCClient) readPump() {
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 64*1024), clientMaxMessage)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "response":
			c.pendingMu.Lock()
			ch := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- &msg
			}
		case "event":
			c.dispatch(&msg)
		}
	}
	// A connection closed by Close ends the pump too, which is no error.
	if err := scanner.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Printf("[IPC] client: %v", err)
	}
	c.conn.Close() // a pump that stopped (message too long) must not leave it open
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	c.listenerMu.Lock()
	close(c.events)
	c.listenerMu.Unlock()
	close(c.closed)
}

// dispatch hands an event to the listeners, or keeps it until
// ListenEvents (dropping it if too many wait).
func (c *IPCClient) dispatch(msg *Message) {
	c.listenerMu.Lock()
	if !c.listening {
		select {
		case c.events <- msg:
		default:
		}
		c.listenerMu.Unlock()
		return
	}
	cbs := c.listeners[msg.Name]
	c.listenerMu.Unlock()
	for _, cb := range cbs {
		cb(msg.Data)
	}
}

// Close disconnects from the server.
func (c *IPCClient) Close() {
	c.conn.Close()
}

// Closed is closed when the connection ended.
func (c *IPCClient) Closed() <-chan struct{} { return c.closed }

// Subscribe registers for the given event types.
func (c *IPCClient) Subscribe(events ...string) error {
	sub := SubscribeRequest{Events: events}
	_, err := c.Request(ReqSubscribe, sub)
	return err
}

// Request sends a request and waits for the response, at most
// requestTimeout.
func (c *IPCClient) Request(name string, data any) (*Message, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	line, err := json.Marshal(Message{Type: "request", ID: id, Name: name, Data: raw})
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	line = append(line, '\n')

	// Register before sending, so the pump can route the response.
	ch := make(chan *Message, 1)
	c.pendingMu.Lock()
	select {
	case <-c.closed:
		c.pendingMu.Unlock()
		c.mu.Unlock()
		return nil, fmt.Errorf("connection closed")
	default:
	}
	c.pending[id] = ch
	c.pendingMu.Unlock()

	_ = c.conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	_, err = c.writer.Write(line)
	if err == nil {
		err = c.writer.Flush()
	}
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return nil, fmt.Errorf("connection closed")
		}
		return resp, nil
	case <-time.After(requestTimeout):
		c.removePending(id)
		return nil, fmt.Errorf("no response to %s within %s", name, requestTimeout)
	}
}

func (c *IPCClient) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

// ReadEvent returns the next event from the server (blocking), for a client
// that does not use ListenEvents. Returns nil when the connection is closed.
func (c *IPCClient) ReadEvent() *Message {
	return <-c.events
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

// ListenEvents hands the events to the registered callbacks from now on,
// those that came since Connect first. The connection is closed when done
// is. Call Subscribe() first to register for the desired event types.
func (c *IPCClient) ListenEvents(done <-chan struct{}) {
	c.listenOnce.Do(func() {
		c.listenerMu.Lock()
		c.listening = true
		var early []*Message
	drain:
		for {
			select {
			case msg, ok := <-c.events:
				if !ok {
					break drain
				}
				early = append(early, msg)
			default:
				break drain
			}
		}
		c.listenerMu.Unlock()
		for _, msg := range early {
			c.listenerMu.RLock()
			cbs := c.listeners[msg.Name]
			c.listenerMu.RUnlock()
			for _, cb := range cbs {
				cb(msg.Data)
			}
		}
		go func() {
			select {
			case <-done:
				c.conn.Close()
			case <-c.closed:
			}
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

	_ = c.conn.SetWriteDeadline(time.Now().Add(requestTimeout))
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
