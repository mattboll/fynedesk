// Package agents follows the coding agents (Claude Code, Codex…) running in
// herdr, the terminal workspace manager, through its socket API: which ones
// are working, which finished and which wait for the user.
package agents

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Status is what an agent is doing, as herdr reports it.
type Status string

// Agent statuses.
const (
	StatusIdle    Status = "idle"
	StatusWorking Status = "working"
	StatusBlocked Status = "blocked" // waiting for the user: a question, a permission
	StatusDone    Status = "done"
	StatusUnknown Status = "unknown"
)

// Pane is a herdr pane, with the agent running in it if any.
type Pane struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Agent       string `json:"agent"` // "claude", "codex"…; empty without an agent
	Status      Status `json:"agent_status"`
	Focused     bool   `json:"focused"` // the pane herdr shows and types into
	Title       string `json:"terminal_title_stripped"`
	Cwd         string `json:"cwd"`
}

// SocketPath returns the path of the herdr API socket.
func SocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "herdr", "herdr.sock")
}

// Client talks to the herdr API: one JSON request per line, one response
// per line.
type Client struct {
	Path string
}

var requestSeq atomic.Uint64

type request struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) dial() (net.Conn, error) {
	if c.Path == "" {
		return nil, errors.New("herdr: no socket path")
	}
	return net.DialTimeout("unix", c.Path, time.Second)
}

func send(conn net.Conn, method string, params any) error {
	if params == nil {
		params = struct{}{}
	}
	req := request{ID: fmt.Sprintf("tyde:%d", requestSeq.Add(1)), Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(data, '\n'))
	return err
}

func decodeResponse(line []byte) (json.RawMessage, error) {
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("herdr: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// call sends one request and returns its result.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := send(conn, method, params); err != nil {
		return nil, err
	}
	r := bufio.NewReader(conn)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return decodeResponse(line)
}

// Agents returns the panes running an agent.
func (c *Client) Agents() ([]Pane, error) {
	res, err := c.call("agent.list", nil)
	if err != nil {
		return nil, err
	}
	var list struct {
		Agents []Pane `json:"agents"`
	}
	if err := json.Unmarshal(res, &list); err != nil {
		return nil, err
	}
	return list.Agents, nil
}

// Focus shows the agent of a pane in herdr: its workspace, tab and pane.
func (c *Client) Focus(paneID string) error {
	_, err := c.call("agent.focus", map[string]string{"target": paneID})
	return err
}

// Event is a change in herdr: Pane is set when a pane changed, Closed when
// one went away, and FocusedPane when the user moved to another pane.
// Resync asks to list the agents again: a tab or a workspace closed with
// its panes, which get no event of their own.
type Event struct {
	Pane        *Pane
	Closed      string
	FocusedPane string
	Resync      bool
}

// Watch subscribes to the changes of the panes and calls onEvent for each,
// until done is closed or the connection drops (then it returns the error).
func (c *Client) Watch(done <-chan struct{}, onEvent func(Event)) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-done
		conn.Close()
	}()

	var subs []map[string]string
	for _, kind := range []string{"pane.updated", "pane.closed", "pane.focused", "pane.agent_detected",
		"pane.exited", "tab.closed", "workspace.closed"} {
		subs = append(subs, map[string]string{"type": kind})
	}
	if err := send(conn, "events.subscribe", map[string]any{"subscriptions": subs}); err != nil {
		return err
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if ev, ok := parseEvent(scanner.Bytes()); ok {
			onEvent(ev)
		}
	}
	select {
	case <-done:
		return nil
	default:
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("herdr: connection closed")
}

// parseEvent decodes one line of a subscription.
func parseEvent(line []byte) (Event, bool) {
	var msg struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(line, &msg) != nil || msg.Event == "" {
		return Event{}, false
	}
	var data struct {
		Pane   *Pane  `json:"pane"`
		PaneID string `json:"pane_id"`
	}
	if json.Unmarshal(msg.Data, &data) != nil {
		return Event{}, false
	}
	switch msg.Event {
	case "pane_updated", "pane_agent_detected":
		if data.Pane != nil {
			return Event{Pane: data.Pane}, true
		}
	case "pane_closed":
		return Event{Closed: data.PaneID}, data.PaneID != ""
	case "pane_focused":
		return Event{FocusedPane: data.PaneID}, data.PaneID != ""
	case "pane_exited", "tab_closed", "workspace_closed":
		return Event{Resync: true}, true
	}
	return Event{}, false
}
