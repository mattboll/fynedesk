package status

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	cal "fyshos.com/fynedesk/internal/calendar"
	"fyshos.com/fynedesk/locale"
)

var calendarMeta = fynedesk.ModuleMetadata{
	Name:        "Next Meeting",
	NewInstance: newCalendarStatus,
}

// calendarStatus shows the next upcoming meeting in the status panel.
// It hides itself when no service is running, no upcoming event exists,
// or the next event is more than 8 hours away (we don't want a permanent
// "morning standup at 9am" stuck on a 6pm panel).
type calendarStatus struct {
	icon    *widget.Button
	label   *widget.Label
	box     *fyne.Container
	mu      sync.Mutex
	stopCh  chan struct{}
	unsub   func()
	current cal.Event
	hasEv   bool
}

func newCalendarStatus() fynedesk.Module {
	return &calendarStatus{}
}

func (c *calendarStatus) Metadata() fynedesk.ModuleMetadata { return calendarMeta }

func (c *calendarStatus) Destroy() {
	c.mu.Lock()
	if c.stopCh != nil {
		close(c.stopCh)
		c.stopCh = nil
	}
	if c.unsub != nil {
		c.unsub()
		c.unsub = nil
	}
	c.mu.Unlock()
}

// StatusAreaWidget builds the widget. It always returns a container so
// the widget panel can keep slot positions stable; the container shows
// itself only when an event is upcoming.
func (c *calendarStatus) StatusAreaWidget() fyne.CanvasObject {
	c.label = widget.NewLabel("")
	c.label.Truncation = fyne.TextTruncateEllipsis

	c.icon = &widget.Button{
		Icon:       theme.HistoryIcon(),
		Importance: widget.LowImportance,
		OnTapped:   c.openMeeting,
	}
	c.box = container.New(&handleNarrow{}, c.icon, c.label)
	c.box.Hide() // start hidden until we have data

	c.refresh()

	if svc := cal.Get(); svc != nil && svc.Store() != nil {
		c.unsub = svc.Store().Subscribe(func() {
			fyne.Do(c.refresh)
		})
	}

	c.startTicker()

	return c.box
}

// startTicker drives the "in N min" relative time so the status updates
// without waiting on the next sync. 30 s feels right: short enough that
// the countdown stays plausible, long enough to be cheap.
func (c *calendarStatus) startTicker() {
	c.mu.Lock()
	if c.stopCh != nil {
		c.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	c.stopCh = stop
	c.mu.Unlock()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(c.refresh)
			}
		}
	}()
}

// refresh recomputes "next event" from the cache and updates the
// widget. Runs on the Fyne goroutine.
func (c *calendarStatus) refresh() {
	if c.box == nil {
		return
	}
	svc := cal.Get()
	if svc == nil || svc.Store() == nil {
		c.box.Hide()
		c.box.Refresh()
		return
	}
	now := time.Now()
	ev, ok := svc.Store().NextEvent(now)
	if !ok {
		c.mu.Lock()
		c.hasEv = false
		c.mu.Unlock()
		c.box.Hide()
		c.box.Refresh()
		return
	}
	until := time.Until(ev.Start)
	// Hide events that are too far away — the widget is meant for
	// "what's next today", not the agenda.
	if until > 8*time.Hour {
		c.mu.Lock()
		c.hasEv = false
		c.mu.Unlock()
		c.box.Hide()
		c.box.Refresh()
		return
	}

	c.mu.Lock()
	c.current = ev
	c.hasEv = true
	c.mu.Unlock()

	c.label.SetText(formatNextMeetingLabel(ev, until))
	c.icon.SetIcon(meetingIcon(ev, until))
	c.box.Show()
	c.box.Refresh()
}

// openMeeting joins the meeting URL when present, otherwise opens the
// upstream HTML link.
func (c *calendarStatus) openMeeting() {
	c.mu.Lock()
	ev := c.current
	has := c.hasEv
	c.mu.Unlock()
	if !has {
		return
	}
	target := ev.MeetingURL
	if target == "" {
		target = ev.HTMLLink
	}
	openCalendarURL(target)
}

// formatNextMeetingLabel builds the inline label, e.g. "Standup · 5m"
// or "Standup · now". The title is capped to keep the panel narrow.
func formatNextMeetingLabel(ev cal.Event, until time.Duration) string {
	title := ev.Title
	if title == "" {
		title = locale.T("cal.next")
	}
	const maxTitleLen = 24
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-1] + "…"
	}
	switch {
	case until <= 0:
		return fmt.Sprintf("%s · %s", title, locale.T("cal.now"))
	case until < time.Minute:
		return fmt.Sprintf("%s · <1 %s", title, locale.T("cal.minutes"))
	default:
		minutes := int(until.Round(time.Minute).Minutes())
		if minutes >= 60 {
			h := minutes / 60
			m := minutes % 60
			if m == 0 {
				return fmt.Sprintf("%s · %dh", title, h)
			}
			return fmt.Sprintf("%s · %dh%02d", title, h, m)
		}
		return fmt.Sprintf("%s · %dm", title, minutes)
	}
}

// meetingIcon picks an icon hinting at urgency: clock for "soon",
// play for "joining now". Falls back to a calendar icon for distant.
func meetingIcon(_ cal.Event, until time.Duration) fyne.Resource {
	if until <= 2*time.Minute {
		return theme.MediaPlayIcon()
	}
	return theme.HistoryIcon()
}

// openCalendarURL launches a URL in the user's default browser via
// xdg-open. Detached: we don't wait for the browser process.
func openCalendarURL(target string) {
	if target == "" {
		return
	}
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}
