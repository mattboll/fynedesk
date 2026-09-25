package ui

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"fyshos.com/tyde/internal/agents"
	"fyshos.com/tyde/wm"
)

// fakeHerdr answers the herdr API calls the hub makes, and records the keys
// sent to agents.
type fakeHerdr struct {
	mu     sync.Mutex
	screen string
	keys   [][]string
}

func startFakeHerdr(t *testing.T, screen string) (*fakeHerdr, string) {
	f := &fakeHerdr{screen: screen}
	path := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f, path
}

func (f *fakeHerdr) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewScanner(conn)
	for r.Scan() {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(r.Bytes(), &req)
		var result any = map[string]string{"type": "ok"}
		switch req.Method {
		case "agent.read":
			f.mu.Lock()
			result = map[string]any{"read": map[string]string{"text": f.screen}}
			f.mu.Unlock()
		case "agent.send_keys":
			var p struct{ Keys []string }
			_ = json.Unmarshal(req.Params, &p)
			f.mu.Lock()
			f.keys = append(f.keys, p.Keys)
			f.mu.Unlock()
		}
		data, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
		_, _ = conn.Write(append(data, '\n'))
	}
}

func (f *fakeHerdr) sentKeys() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.keys...)
}

func TestAgentQuestionAnsweredFromNotification(t *testing.T) {
	screen := " Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, and don't ask again\n   3. No (esc)\n"
	fake, path := startFakeHerdr(t, screen)
	h := &agentHub{client: &agents.Client{Path: path}}

	got := make(chan *wm.Notification, 1)
	wm.AddNotificationListener(func(n *wm.Notification) {
		if n.Tag == agentTag("w1:p2") {
			select { // listeners stay: one of an earlier run (-count) must not block
			case got <- n:
			default:
			}
		}
	})
	h.notify(agents.Notice{Kind: agents.NeedsInput, Agent: agents.Agent{Pane: agents.Pane{PaneID: "w1:p2", Agent: "claude"}}})

	var n *wm.Notification
	select {
	case n = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("no notification")
	}
	if assert.Len(t, n.Buttons, 3) {
		assert.Equal(t, "Yes", n.Buttons[0].Label)
		assert.Equal(t, wm.UrgencyCritical, n.Urgency, "it waits for the answer")
		n.Buttons[2].OnTap()
		assert.Eventually(t, func() bool { return len(fake.sentKeys()) == 1 }, 2*time.Second, 10*time.Millisecond)
		assert.Equal(t, [][]string{{"3"}}, fake.sentKeys())
	}
}

func TestAgentFinishedHasNoButtons(t *testing.T) {
	got := make(chan *wm.Notification, 1)
	wm.AddNotificationListener(func(n *wm.Notification) {
		if n.Tag == agentTag("w1:p9") {
			select { // listeners stay: one of an earlier run (-count) must not block
			case got <- n:
			default:
			}
		}
	})
	h := &agentHub{client: &agents.Client{Path: "/nonexistent"}}
	h.notify(agents.Notice{Kind: agents.Finished, Agent: agents.Agent{Pane: agents.Pane{PaneID: "w1:p9", Agent: "claude"}}})
	n := <-got
	assert.Empty(t, n.Buttons)
	assert.Equal(t, wm.UrgencyNormal, n.Urgency)
}

func TestNextWaiting(t *testing.T) {
	now := time.Now()
	p := func(id string, st agents.Status, focused bool) agents.Pane {
		return agents.Pane{PaneID: id, Agent: "claude", Status: st, Focused: focused}
	}
	tr := agents.NewTracker([]agents.Pane{
		p("a", agents.StatusBlocked, false),
		p("b", agents.StatusWorking, true),
		p("c", agents.StatusDone, false),
	}, now)
	assert.Equal(t, "a", nextWaiting(tr.Agents()), "from an agent that does not wait: the first waiting")

	tr = agents.NewTracker([]agents.Pane{p("a", agents.StatusBlocked, true), p("c", agents.StatusDone, false)}, now)
	assert.Equal(t, "c", nextWaiting(tr.Agents()), "then the next one")
	tr = agents.NewTracker([]agents.Pane{p("a", agents.StatusBlocked, false), p("c", agents.StatusDone, true)}, now)
	assert.Equal(t, "a", nextWaiting(tr.Agents()), "and round")

	tr = agents.NewTracker([]agents.Pane{p("b", agents.StatusWorking, false)}, now)
	assert.Equal(t, "", nextWaiting(tr.Agents()), "nobody waits")
}

func TestAgentHubOutlivesModuleReload(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "none.sock"))
	first := acquireAgentHub()
	first.release() // settings applied: the modules are made again...
	second := acquireAgentHub()
	assert.Same(t, first, second, "a new instance takes the running hub back")

	time.Sleep(hubGrace + 200*time.Millisecond)
	assert.Same(t, second, agentHubInstance(), "still in use: kept")

	second.release()
	assert.Eventually(t, func() bool { return agentHubInstance() == nil },
		hubGrace+2*time.Second, 50*time.Millisecond, "the module is off: the hub stops")
	select {
	case <-second.done:
	default:
		t.Error("the stopped hub still runs")
	}
}

func TestNotificationButtonsIncludeApplicationActions(t *testing.T) {
	n := wm.NewNotificationFull("KDE Connect", "", "Alice", "on se voit ?",
		[]string{"default", "Open", "reply", "Reply", "mute", "Mute", "block", "Block", "extra", "Extra"}, 0)
	n.Buttons = []wm.NotificationButton{{Label: "Mine", OnTap: func() {}}}

	var labels []string
	for _, b := range notificationButtons(n) {
		labels = append(labels, b.Label)
	}
	assert.Equal(t, []string{"Mine", "Reply", "Mute"}, labels, "Tyde's first, no default action, three at most")

	plain := wm.NewNotificationFull("app", "", "title", "", []string{"default", ""}, 0)
	assert.Empty(t, notificationButtons(plain))
}
