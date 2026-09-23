package agents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func pane(id string, status Status, focused bool) *Pane {
	return &Pane{PaneID: id, Agent: "claude", Status: status, Focused: focused, Title: "task " + id}
}

func TestTrackerFinished(t *testing.T) {
	now := time.Now()
	tr := NewTracker([]Pane{*pane("p1", StatusWorking, false)}, now)

	tr.Apply(Event{Pane: pane("p1", StatusIdle, false)}, now)
	assert.Empty(t, tr.Settle(now.Add(time.Second)), "a short pause between two steps is not the end")

	tr.Apply(Event{Pane: pane("p1", StatusWorking, false)}, now.Add(time.Second))
	tr.Apply(Event{Pane: pane("p1", StatusIdle, false)}, now.Add(3*time.Second))
	assert.Empty(t, tr.Settle(now.Add(4*time.Second)))

	notices := tr.Settle(now.Add(6 * time.Second))
	if assert.Len(t, notices, 1) {
		assert.Equal(t, Finished, notices[0].Kind)
		assert.Equal(t, "p1", notices[0].Agent.PaneID)
	}
	a, _ := tr.Get("p1")
	assert.True(t, a.NeedsAttention())
	assert.Empty(t, tr.Settle(now.Add(10*time.Second)), "said once")

	// Looking at it clears it.
	tr.Visible = func() bool { return true }
	tr.Apply(Event{FocusedPane: "p1"}, now.Add(11*time.Second))
	a, _ = tr.Get("p1")
	assert.False(t, a.NeedsAttention())
}

func TestTrackerNeedsInput(t *testing.T) {
	now := time.Now()
	tr := NewTracker([]Pane{*pane("p1", StatusWorking, false), *pane("p2", StatusWorking, true)}, now)
	tr.Visible = func() bool { return true }

	tr.Apply(Event{Pane: pane("p1", StatusBlocked, false)}, now)
	tr.Apply(Event{Pane: pane("p2", StatusBlocked, true)}, now)
	notices := tr.Settle(now.Add(3 * time.Second))
	if assert.Len(t, notices, 1, "no notice for the pane the user is looking at") {
		assert.Equal(t, NeedsInput, notices[0].Kind)
		assert.Equal(t, "p1", notices[0].Agent.PaneID)
	}
	p2, _ := tr.Get("p2")
	assert.True(t, p2.NeedsAttention(), "a blocked agent waits for the user even when seen")
}

func TestTrackerNotVisible(t *testing.T) {
	now := time.Now()
	tr := NewTracker([]Pane{*pane("p1", StatusWorking, true)}, now)
	tr.Visible = func() bool { return false } // herdr is not on screen

	tr.Apply(Event{Pane: pane("p1", StatusIdle, true)}, now)
	assert.Len(t, tr.Settle(now.Add(3*time.Second)), 1)
}

func TestTrackerAgentsOrder(t *testing.T) {
	now := time.Now()
	tr := NewTracker([]Pane{
		*pane("idle", StatusIdle, false),
		*pane("work", StatusWorking, false),
		*pane("done", StatusDone, false),
	}, now)
	var ids []string
	for _, a := range tr.Agents() {
		ids = append(ids, a.PaneID)
	}
	assert.Equal(t, []string{"done", "work", "idle"}, ids)

	tr.Apply(Event{Closed: "work"}, now)
	tr.Apply(Event{Pane: &Pane{PaneID: "idle"}}, now) // the agent exited
	assert.Len(t, tr.Agents(), 1)
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"✳ Windows managers": "Windows managers",
		"◐ Build":            "Build",
		"⠋ Thinking":         "Thinking",
		"Plain":              "Plain",
		"  ✻ Spaced ":        "Spaced ",
	} {
		assert.Equal(t, want, CleanTitle(in))
	}
}

func TestParseEvent(t *testing.T) {
	ev, ok := parseEvent([]byte(`{"data":{"pane":{"agent":"claude","agent_status":"idle","focused":true,"pane_id":"w5:p5","tab_id":"w5:t5","terminal_title_stripped":"Rust","workspace_id":"w5"},"type":"pane_updated"},"event":"pane_updated"}`))
	assert.True(t, ok)
	assert.Equal(t, StatusIdle, ev.Pane.Status)
	assert.Equal(t, "w5:p5", ev.Pane.PaneID)

	ev, ok = parseEvent([]byte(`{"data":{"pane_id":"w3:p2","type":"pane_focused","workspace_id":"w3"},"event":"pane_focused"}`))
	assert.True(t, ok)
	assert.Equal(t, "w3:p2", ev.FocusedPane)

	_, ok = parseEvent([]byte(`{"id":"t1","result":{"type":"subscription_started"}}`))
	assert.False(t, ok)
}

func TestTrackerSync(t *testing.T) {
	now := time.Now()
	tr := NewTracker([]Pane{*pane("p1", StatusWorking, false), *pane("gone", StatusWorking, false)}, now)

	// "gone" was in a tab that closed: herdr does not list it any more.
	tr.Sync([]Pane{*pane("p1", StatusIdle, false), *pane("new", StatusIdle, false)}, now)
	_, ok := tr.Get("gone")
	assert.False(t, ok)
	_, ok = tr.Get("new")
	assert.True(t, ok)

	// p1 stopped: it settles like after an event.
	assert.Len(t, tr.Settle(now.Add(3*time.Second)), 1)

	ev, ok := parseEvent([]byte(`{"data":{"tab_id":"w1:t2","type":"tab_closed","workspace_id":"w1"},"event":"tab_closed"}`))
	assert.True(t, ok)
	assert.True(t, ev.Resync)
}
