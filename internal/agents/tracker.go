package agents

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

// settle is how long a new status must hold before it counts: between two
// steps of its work an agent is briefly idle.
const settle = 2 * time.Second

// Kind is what happened to an agent that the user may want to know.
type Kind int

// Kinds of notice.
const (
	Finished   Kind = iota // it was working and stopped
	NeedsInput             // it waits for an answer or a permission
)

// Notice tells that an agent finished or needs the user.
type Notice struct {
	Kind  Kind
	Agent Agent
}

// Agent is an agent as the desktop shows it.
type Agent struct {
	Pane
	// Unseen is set when it finished or asked something while the user was
	// not looking at its pane, until they do.
	Unseen bool
	since  time.Time // when Status last changed
	shown  Status    // the settled status
}

// NeedsAttention reports whether the agent waits for the user.
func (a Agent) NeedsAttention() bool {
	return a.Unseen || a.shown == StatusBlocked
}

// Shown returns the settled status, the one to display.
func (a Agent) Shown() Status {
	return a.shown
}

// Tracker keeps the state of the agents from herdr events and tells when one
// finishes or needs the user. It is not safe for concurrent use.
type Tracker struct {
	agents map[string]*Agent
	// Visible reports whether the user is looking at herdr, so that what
	// happens in its focused pane needs no notice.
	Visible func() bool
}

// NewTracker returns a tracker for the given agents, as listed by herdr.
func NewTracker(panes []Pane, now time.Time) *Tracker {
	t := &Tracker{agents: map[string]*Agent{}}
	for _, p := range panes {
		if p.Agent != "" {
			// herdr says "done" for a finished agent the user has not looked at.
			t.agents[p.PaneID] = &Agent{Pane: p, since: now, shown: p.Status, Unseen: p.Status == StatusDone}
		}
	}
	return t
}

func (t *Tracker) looking(a *Agent) bool {
	return a.Focused && t.Visible != nil && t.Visible()
}

// Apply updates the tracker with a herdr event.
func (t *Tracker) Apply(ev Event, now time.Time) {
	switch {
	case ev.Closed != "":
		delete(t.agents, ev.Closed)
	case ev.FocusedPane != "":
		for id, a := range t.agents {
			a.Focused = id == ev.FocusedPane
		}
		t.MarkSeen()
	case ev.Pane != nil:
		p := *ev.Pane
		if p.Agent == "" {
			delete(t.agents, p.PaneID) // the agent exited, a shell is left
			return
		}
		a := t.agents[p.PaneID]
		if a == nil {
			t.agents[p.PaneID] = &Agent{Pane: p, since: now, shown: p.Status}
			return
		}
		if a.Status != p.Status {
			a.since = now
		}
		a.Pane = p
		t.MarkSeen()
	}
}

// Sync brings the tracker in line with the agents as herdr lists them now:
// those it does not list any more are gone.
func (t *Tracker) Sync(panes []Pane, now time.Time) {
	listed := map[string]bool{}
	for i := range panes {
		if panes[i].Agent == "" {
			continue
		}
		listed[panes[i].PaneID] = true
		t.Apply(Event{Pane: &panes[i]}, now)
	}
	for id := range t.agents {
		if !listed[id] {
			delete(t.agents, id)
		}
	}
}

// MarkSeen clears Unseen on the agent the user is looking at. Call it too
// when herdr becomes visible.
func (t *Tracker) MarkSeen() {
	for _, a := range t.agents {
		if a.Unseen && t.looking(a) {
			a.Unseen = false
		}
	}
}

// Settle applies the statuses that held long enough and returns the notices
// they give. Call it regularly.
func (t *Tracker) Settle(now time.Time) []Notice {
	var notices []Notice
	for _, a := range t.agents {
		if a.Status == a.shown || now.Sub(a.since) < settle {
			continue
		}
		from := a.shown
		a.shown = a.Status
		looking := t.looking(a)

		var kind Kind
		switch {
		case a.shown == StatusBlocked:
			kind = NeedsInput
		case from == StatusWorking && (a.shown == StatusIdle || a.shown == StatusDone):
			kind = Finished
		default:
			if a.shown == StatusWorking {
				a.Unseen = false // at work again: nothing left to see
			}
			continue
		}
		if looking {
			continue
		}
		a.Unseen = true
		notices = append(notices, Notice{Kind: kind, Agent: *a})
	}
	sort.Slice(notices, func(i, j int) bool { return notices[i].Agent.PaneID < notices[j].Agent.PaneID })
	return notices
}

// Pending reports whether a status change is waiting to settle.
func (t *Tracker) Pending() bool {
	for _, a := range t.agents {
		if a.Status != a.shown {
			return true
		}
	}
	return false
}

// Agents returns the agents: those asking something first, then those
// that finished unseen, those at work, then the idle ones; by title within
// each group.
func (t *Tracker) Agents() []Agent {
	list := make([]Agent, 0, len(t.agents))
	for _, a := range t.agents {
		list = append(list, *a)
	}
	rank := func(a Agent) int {
		switch {
		case a.shown == StatusBlocked:
			return 0
		case a.NeedsAttention():
			return 1
		case a.shown == StatusWorking:
			return 2
		}
		return 3
	}
	sort.Slice(list, func(i, j int) bool {
		if ri, rj := rank(list[i]), rank(list[j]); ri != rj {
			return ri < rj
		}
		if list[i].Title != list[j].Title {
			return list[i].Title < list[j].Title
		}
		return list[i].PaneID < list[j].PaneID
	})
	return list
}

// Get returns the agent of a pane.
func (t *Tracker) Get(paneID string) (Agent, bool) {
	a, ok := t.agents[paneID]
	if !ok {
		return Agent{}, false
	}
	return *a, true
}

// CleanTitle removes what agents put before their title to show activity:
// spinners, stars and blanks.
func CleanTitle(title string) string {
	return strings.TrimLeftFunc(title, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsSymbol(r) || (r >= 0x2800 && r <= 0x28FF) || // braille spinners
			(r >= 0x25A0 && r <= 0x25FF) || (r >= 0x2700 && r <= 0x27BF) // shapes, dingbats
	})
}

// DisplayName is how the desktop names an agent program.
func DisplayName(agent string) string {
	switch agent {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini"
	case "github_copilot":
		return "Copilot"
	case "open_code":
		return "OpenCode"
	case "":
		return ""
	}
	return strings.ToUpper(agent[:1]) + agent[1:]
}
