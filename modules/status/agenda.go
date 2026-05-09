package status

import (
	"sort"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	cal "fyshos.com/fynedesk/internal/calendar"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
)

var agendaMeta = fynedesk.ModuleMetadata{
	Name:        "Today's Agenda",
	NewInstance: newAgendaSidebar,
}

// agendaSidebar shows the next few events of the day in the wide widget
// panel. It hides itself in narrow-panel mode (where the next-meeting
// status row already covers the urgent case) and when no service is
// running.
type agendaSidebar struct {
	wrapper *fyne.Container
	mu      sync.Mutex
	stopCh  chan struct{}
	unsub   func()
}

func newAgendaSidebar() fynedesk.Module {
	return &agendaSidebar{}
}

func (a *agendaSidebar) Metadata() fynedesk.ModuleMetadata { return agendaMeta }

func (a *agendaSidebar) Destroy() {
	a.mu.Lock()
	if a.stopCh != nil {
		close(a.stopCh)
		a.stopCh = nil
	}
	if a.unsub != nil {
		a.unsub()
		a.unsub = nil
	}
	a.mu.Unlock()
}

// StatusAreaWidget returns the wrapper used by the widget panel. In
// narrow mode we return nil so the panel skips us entirely.
func (a *agendaSidebar) StatusAreaWidget() fyne.CanvasObject {
	if fynedesk.Instance() != nil && fynedesk.Instance().Settings().NarrowWidgetPanel() {
		return nil
	}

	a.wrapper = container.NewStack()
	a.refresh()

	if svc := cal.Get(); svc != nil && svc.Store() != nil {
		a.unsub = svc.Store().Subscribe(func() {
			fyne.Do(a.refresh)
		})
	}
	a.startTicker()

	return a.wrapper
}

func (a *agendaSidebar) startTicker() {
	a.mu.Lock()
	if a.stopCh != nil {
		a.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	a.stopCh = stop
	a.mu.Unlock()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(a.refresh)
			}
		}
	}()
}

// refresh rebuilds the agenda body. We pull "rest of today" from the
// cache; an empty result hides the wrapper so it doesn't take vertical
// space the user could reclaim for other widgets.
func (a *agendaSidebar) refresh() {
	if a.wrapper == nil {
		return
	}
	svc := cal.Get()
	if svc == nil || svc.Store() == nil {
		a.wrapper.Hide()
		a.wrapper.Refresh()
		return
	}
	now := time.Now()
	endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	events := svc.Store().EventsBetween(now.Add(-15*time.Minute), endOfDay)
	if len(events) == 0 {
		a.wrapper.Hide()
		a.wrapper.Refresh()
		return
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].AllDay != events[j].AllDay {
			return events[i].AllDay
		}
		return events[i].Start.Before(events[j].Start)
	})
	const maxRows = 4
	if len(events) > maxRows {
		events = events[:maxRows]
	}

	colors := agendaColorMap(svc.Store())

	header := widget.NewLabelWithStyle(locale.T("cal.today"),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	rows := make([]fyne.CanvasObject, 0, len(events)+1)
	rows = append(rows, header)
	for _, ev := range events {
		rows = append(rows, agendaRow(ev, colors[ev.CalendarID]))
	}

	body := container.NewVBox(rows...)
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 6

	pad := canvas.NewRectangle(fyne.CurrentApp().Settings().Theme().
		Color(theme.ColorNameSeparator, fyne.CurrentApp().Settings().ThemeVariant()))
	pad.SetMinSize(fyne.NewSize(0, 1))

	a.wrapper.Objects = []fyne.CanvasObject{
		container.NewBorder(nil, nil, nil, nil,
			container.NewStack(bg,
				container.New(layout.NewCustomPaddedLayout(6, 6, 8, 8), body))),
	}
	a.wrapper.Show()
	a.wrapper.Refresh()

	// Keep the wrapper width tight to the widget panel, height grows
	// naturally with the number of rows.
	width := wmtheme.WidgetPanelWidth - 16
	a.wrapper.Resize(fyne.NewSize(width, body.MinSize().Height+12))
}

func agendaRow(ev cal.Event, _ string) fyne.CanvasObject {
	timeText := agendaRowTime(ev)
	timeLbl := widget.NewLabelWithStyle(timeText, fyne.TextAlignLeading,
		fyne.TextStyle{Monospace: true})
	timeLbl.Importance = widget.MediumImportance

	titleLbl := widget.NewLabel(ev.Title)
	titleLbl.Truncation = fyne.TextTruncateEllipsis

	row := container.NewBorder(nil, nil, timeLbl, nil, titleLbl)

	target := ev.MeetingURL
	if target == "" {
		target = ev.HTMLLink
	}
	if target == "" {
		return row
	}
	btn := widget.NewButton("", func() { openCalendarURL(target) })
	btn.Importance = widget.LowImportance
	return container.NewStack(btn, row)
}

func agendaRowTime(ev cal.Event) string {
	if ev.AllDay {
		return locale.T("cal.allDay")
	}
	return ev.Start.Format("15:04")
}

// agendaColorMap returns calendarID → ColorHex string. Kept simple here
// because the row background doesn't currently render the color (room
// for a 3-px coloured bar later); the map is wired so we can light up
// the row consistently with the popup once we settle the visual.
func agendaColorMap(store *cal.Store) map[string]string {
	out := map[string]string{}
	for _, acc := range store.Accounts() {
		for _, c := range store.CalendarsFor(acc.ID) {
			out[c.ID] = c.ColorHex
		}
	}
	return out
}
