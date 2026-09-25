package ui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/agents"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

func init() {
	tyde.RegisterModule(agentsModuleMeta)
}

// agentsModuleMeta describes the Coding Agents module: the agents of herdr
// in the widget panel, their notifications, the glow of the herdr window and
// Super+G. It is offered once herdr is found.
var agentsModuleMeta = tyde.ModuleMetadata{
	Name:        wlipc.AgentsModule,
	NewInstance: newAgentsModule,
}

// agentsModule runs the agent hub while it is enabled.
type agentsModule struct {
	hub *agentHub // nil outside a Wayland session
}

func newAgentsModule() tyde.Module {
	m := &agentsModule{}
	if wlipc.IsWaylandSession() {
		m.hub = acquireAgentHub()
	}
	return m
}

func (m *agentsModule) Metadata() tyde.ModuleMetadata {
	return agentsModuleMeta
}

func (m *agentsModule) Destroy() {
	if m.hub != nil {
		m.hub.release()
		m.hub = nil
	}
}

// herdrInstalled reports whether herdr is there to follow.
func herdrInstalled() bool {
	if _, err := exec.LookPath("herdr"); err == nil {
		return true
	}
	_, err := os.Stat(agents.SocketPath())
	return err == nil
}

// agentHub follows the coding agents running in herdr. It is only used from
// the Fyne thread, except for its connection goroutines.
type agentHub struct {
	client    *agents.Client
	tracker   *agents.Tracker
	listeners []func()
	attention bool           // some agent waits for the user
	done      chan struct{}  // closed when the module stops
	refs      int            // module instances using it (hubLock)
	running   sync.WaitGroup // its goroutines
}

var (
	hub     *agentHub
	hubLock sync.Mutex
)

// agentHubInstance returns the hub of the Coding Agents module, nil when it
// is off.
func agentHubInstance() *agentHub {
	hubLock.Lock()
	defer hubLock.Unlock()
	return hub
}

// acquireAgentHub returns the hub, started if needed, for a module instance.
func acquireAgentHub() *agentHub {
	hubLock.Lock()
	defer hubLock.Unlock()
	if hub == nil {
		hub = &agentHub{client: &agents.Client{Path: agents.SocketPath()}, done: make(chan struct{})}
		hub.running.Add(2)
		go hub.run()
		go hub.tick()
	}
	hub.refs++
	return hub
}

// hubGrace is how long the hub outlives its last module instance: modules
// are made again each time settings are applied, and the hub keeps going
// through that, with what the user has seen.
const hubGrace = time.Second

// release lets go of the hub; it stops once no instance took it back.
func (h *agentHub) release() {
	hubLock.Lock()
	h.refs--
	last := h.refs == 0
	hubLock.Unlock()
	if last {
		time.AfterFunc(hubGrace, h.stopUnused)
	}
}

// stopUnused stops the hub if no module instance uses it.
func (h *agentHub) stopUnused() {
	hubLock.Lock()
	if h.refs > 0 || hub != h {
		hubLock.Unlock()
		return
	}
	hub = nil
	hubLock.Unlock()
	h.stop()
}

// stop stops following herdr and forgets the agents: no more widget,
// notifications or glow. It waits for the goroutines of the hub, so it must
// not be called from the Fyne thread.
func (h *agentHub) stop() {
	close(h.done)
	h.running.Wait()
	fyne.Do(func() {
		h.tracker = nil
		h.changed()
		h.listeners = nil
	})
}

// wait sleeps for d, and reports false if the hub stopped meanwhile.
func (h *agentHub) wait(d time.Duration) bool {
	select {
	case <-h.done:
		return false
	case <-time.After(d):
		return true
	}
}

// onChange registers a function called on the Fyne thread when the agents change.
func (h *agentHub) onChange(fn func()) {
	h.listeners = append(h.listeners, fn)
}

func (h *agentHub) changed() {
	attention := false
	if h.tracker != nil {
		for _, a := range h.tracker.Agents() {
			if a.NeedsAttention() {
				attention = true
				break
			}
		}
	}
	if attention != h.attention {
		h.attention = attention
		setHerdrAttention(attention)
	}
	for _, fn := range h.listeners {
		fn()
	}
}

// Agents returns the agents to show, or nil when herdr is not running.
func (h *agentHub) Agents() []agents.Agent {
	if h.tracker == nil {
		return nil
	}
	return h.tracker.Agents()
}

// run keeps a subscription to herdr, reconnecting when it restarts.
func (h *agentHub) run() {
	defer h.running.Done()
	for {
		panes, err := h.client.Agents()
		if err != nil {
			fyne.Do(func() {
				if h.tracker != nil {
					h.tracker = nil
					h.changed()
				}
			})
			if !h.wait(10 * time.Second) { // herdr not running
				return
			}
			continue
		}
		stopped := false
		fyne.DoAndWait(func() {
			select {
			case <-h.done:
				stopped = true
				return
			default:
			}
			h.tracker = agents.NewTracker(panes, time.Now())
			h.tracker.Visible = herdrFocused
			h.changed()
		})
		if stopped {
			return
		}
		done := make(chan struct{})
		ended := make(chan struct{})
		go func() { // the subscription ends with the module
			select {
			case <-h.done:
			case <-ended:
			}
			close(done)
		}()
		go h.resyncEvery(20*time.Second, done)
		err = h.client.Watch(done, func(ev agents.Event) {
			if ev.Resync {
				go h.resync()
				return
			}
			fyne.Do(func() {
				if h.tracker != nil {
					h.tracker.Apply(ev, time.Now())
					h.changed()
				}
			})
		})
		close(ended)
		log.Println("[agents] herdr subscription ended:", err)
		if !h.wait(3 * time.Second) {
			return
		}
	}
}

// resync lists the agents again, for what the events do not tell.
func (h *agentHub) resync() {
	panes, err := h.client.Agents()
	if err != nil {
		return
	}
	fyne.Do(func() {
		if h.tracker != nil {
			h.tracker.Sync(panes, time.Now())
			h.changed()
		}
	})
}

// resyncEvery resyncs regularly until done is closed: a missed event must
// not leave an agent "working" for ever.
func (h *agentHub) resyncEvery(period time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			h.resync()
		}
	}
}

// tick settles the status changes and tells about agents that finished or
// need the user.
func (h *agentHub) tick() {
	defer h.running.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}
		fyne.Do(func() {
			if h.tracker == nil {
				return
			}
			before := attentionSet(h.tracker)
			h.tracker.MarkSeen() // the user may have switched to herdr
			notices := h.tracker.Settle(time.Now())
			for _, n := range notices {
				h.notify(n)
			}
			after := attentionSet(h.tracker)
			for pane := range before {
				if !after[pane] {
					wm.WithdrawTagged(agentTag(pane)) // seen: its notification is stale
				}
			}
			if len(notices) > 0 || len(before) != len(after) {
				h.changed()
			}
		})
	}
}

func attentionSet(t *agents.Tracker) map[string]bool {
	set := map[string]bool{}
	for _, a := range t.Agents() {
		if a.NeedsAttention() {
			set[a.PaneID] = true
		}
	}
	return set
}

func agentTag(paneID string) string {
	return "agent:" + paneID
}

// notify posts a notification for an agent that finished or waits. When it
// asks a question with numbered options, they become buttons of the
// notification, which answer right away.
func (h *agentHub) notify(notice agents.Notice) {
	if notice.Kind != agents.NeedsInput {
		notifyAgent(notice, nil)
		return
	}
	pane := notice.Agent.PaneID
	go func() {
		var buttons []wm.NotificationButton
		if screen, err := h.client.Screen(pane, 40); err == nil {
			for _, c := range agents.Choices(screen) {
				if len(buttons) == maxAnswerButtons {
					break
				}
				key := c.Key
				buttons = append(buttons, wm.NotificationButton{Label: c.Label, OnTap: func() {
					go func() {
						if err := h.client.SendKeys(pane, key); err != nil {
							log.Println("[agents] answer:", err)
						}
					}()
				}})
			}
		}
		fyne.Do(func() { notifyAgent(notice, buttons) })
	}()
}

// maxAnswerButtons is how many options of a question a notification offers.
const maxAnswerButtons = 3

// notifyAgent posts a notification for an agent that finished or waits.
func notifyAgent(notice agents.Notice, buttons []wm.NotificationButton) {
	a := notice.Agent
	name := agents.DisplayName(a.Agent)
	title := fmt.Sprintf(locale.T("agents.finished"), name)
	icon := agentDoneIcon
	timeout := int32(0)
	if notice.Kind == agents.NeedsInput {
		title = fmt.Sprintf(locale.T("agents.needsInput"), name)
		icon = agentAskIcon
		timeout = 8000
	}

	n := wm.NewNotificationFull(name, "", title, agentLabel(a), nil, timeout)
	n.Icon = icon
	n.Tag = agentTag(a.PaneID)
	pane := a.PaneID
	n.OnActivate = func() { focusAgent(pane) }
	n.Buttons = buttons
	if len(buttons) > 0 {
		n.Urgency = wm.UrgencyCritical // it stays until answered or dismissed
	}
	wm.SendNotification(n)
}

// agentLabel describes what an agent works on: its title, or its folder.
func agentLabel(a agents.Agent) string {
	if title := agents.CleanTitle(a.Title); title != "" && !strings.EqualFold(title, a.Agent) {
		return title
	}
	return shortPath(a.Cwd)
}

func shortPath(p string) string {
	if i := strings.LastIndex(strings.TrimRight(p, "/"), "/"); i >= 0 {
		return strings.TrimRight(p, "/")[i+1:]
	}
	return p
}

// focusAgent shows an agent: herdr switches to its pane and its window comes
// to the front.
func focusAgent(paneID string) {
	if h := agentHubInstance(); h != nil {
		go func() {
			if err := h.client.Focus(paneID); err != nil {
				log.Println("[agents] focus:", err)
			}
		}()
	}
	if w := herdrWindow(); w != nil {
		desk := tyde.Instance()
		if w.Desktop() != desk.Desktop() && !w.Pinned() {
			desk.SetDesktop(w.Desktop())
		}
		if w.Iconic() {
			w.Uniconify()
		}
		w.RaiseToTop()
		w.Focus()
	}
}

// herdrWindow returns the terminal window running herdr, if any.
func herdrWindow() tyde.Window {
	desk := tyde.Instance()
	if desk == nil || desk.WindowManager() == nil {
		return nil
	}
	var found tyde.Window
	for _, w := range desk.WindowManager().Windows() {
		if isHerdrTitle(w.Properties().Title()) {
			if w.Focused() {
				return w
			}
			if found == nil {
				found = w
			}
		}
	}
	return found
}

// herdrTitle is the window title that herdr gives its terminal.
const herdrTitle = "herdr"

// isHerdrTitle recognises the window of herdr.
func isHerdrTitle(title string) bool {
	return wlipc.TitleMatches(herdrTitle, title)
}

// herdrFocused reports whether the user is looking at herdr.
func herdrFocused() bool {
	w := herdrWindow()
	return w != nil && w.Focused()
}

// setHerdrAttention asks the compositor to make the herdr window call for
// attention, or to stop.
func setHerdrAttention(on bool) {
	if !wlipc.IsWaylandSession() {
		return
	}
	go func() {
		if err := wlipc.RequestWindowAttention(herdrTitle, on); err != nil {
			log.Println("[agents] attention:", err)
		}
	}()
}

// focusWaiting goes to an agent that waits for the user: the next one after
// the agent in view, so that pressing again goes round them.
func (h *agentHub) focusWaiting() {
	if h.tracker == nil {
		return
	}
	if pane := nextWaiting(h.tracker.Agents()); pane != "" {
		focusAgent(pane)
	}
}

// nextWaiting picks, among agents listed those waiting first, the waiting
// agent after the focused one.
func nextWaiting(list []agents.Agent) string {
	var waiting []agents.Agent
	current := -1
	for _, a := range list {
		if !a.NeedsAttention() {
			continue
		}
		if a.Focused {
			current = len(waiting)
		}
		waiting = append(waiting, a)
	}
	if len(waiting) == 0 {
		return ""
	}
	return waiting[(current+1)%len(waiting)].PaneID
}
