package ui

import (
	"fmt"
	"image/color"
	"os/exec"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/internal/calendar"
	"fyshos.com/fynedesk/locale"
)

// calendarPopupSize is the natural size of the popup. Vertical layout:
// header on top, month grid in the middle, agenda below. Width is the
// month grid's natural size plus padding; height accommodates ~6 weeks
// of grid plus the agenda scroll.
var calendarPopupSize = fyne.NewSize(380, 500)

// calendarPopup builds the navigable month + agenda popup. When a calendar
// service is running, day cells gain coloured event dots and clicking a
// day populates the agenda panel below the grid; otherwise it falls back
// to the plain month grid the popup used before calendar integration
// landed.
func calendarPopup(now time.Time) fyne.CanvasObject {
	state := &calendarPopupState{
		today:       now,
		selected:    now,
		viewYear:    now.Year(),
		viewMonth:   now.Month(),
		agendaBody:  container.NewVBox(),
		gridContent: container.New(layout.NewGridLayoutWithColumns(8)),
	}

	state.headerLabel = widget.NewLabelWithStyle(
		monthHeader(state.viewYear, state.viewMonth),
		fyne.TextAlignCenter, fyne.TextStyle{Bold: true},
	)

	prevBtn := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		state.viewMonth--
		if state.viewMonth < 1 {
			state.viewMonth = 12
			state.viewYear--
		}
		state.refreshGrid()
	})
	prevBtn.Importance = widget.LowImportance

	nextBtn := widget.NewButtonWithIcon("", theme.NavigateNextIcon(), func() {
		state.viewMonth++
		if state.viewMonth > 12 {
			state.viewMonth = 1
			state.viewYear++
		}
		state.refreshGrid()
	})
	nextBtn.Importance = widget.LowImportance

	todayBtn := widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		state.viewYear = state.today.Year()
		state.viewMonth = state.today.Month()
		state.selected = state.today
		state.refreshGrid()
		state.refreshAgenda()
	})
	todayBtn.Importance = widget.LowImportance

	filterBtn := widget.NewButtonWithIcon("", theme.ListIcon(), nil)
	filterBtn.Importance = widget.LowImportance
	filterBtn.OnTapped = func() {
		state.showFilterPopup(filterBtn)
	}

	rightHeader := container.NewHBox(filterBtn, todayBtn, nextBtn)
	header := container.NewBorder(nil, nil, prevBtn, rightHeader, state.headerLabel)

	state.refreshGrid()

	sep := canvas.NewRectangle(color.NRGBA{R: 128, G: 128, B: 128, A: 64})
	sep.SetMinSize(fyne.NewSize(0, 1))

	monthBlock := container.NewVBox(header, sep, state.gridContent)
	agenda := state.buildAgenda()

	// Vertical stack: month grid on top (fixed natural size), agenda
	// below taking the remaining height. A second separator visually
	// splits the two zones.
	sep2 := canvas.NewRectangle(color.NRGBA{R: 128, G: 128, B: 128, A: 64})
	sep2.SetMinSize(fyne.NewSize(0, 1))

	root := container.NewBorder(monthBlock, nil, nil, nil,
		container.NewBorder(sep2, nil, nil, nil, agenda))
	state.refreshAgenda()

	// Subscribe to store changes so the popup repaints when sync brings in
	// new events or the user toggles a calendar via the filter / Settings.
	if svc := calendar.Get(); svc != nil && svc.Store() != nil {
		unsub := svc.Store().Subscribe(func() {
			fyne.Do(func() {
				state.refreshGrid()
				state.refreshAgenda()
			})
		})
		_ = unsub // popup is short-lived; rebuild next time replaces it
	}

	return root
}

type calendarPopupState struct {
	today       time.Time
	selected    time.Time
	viewYear    int
	viewMonth   time.Month
	headerLabel *widget.Label
	gridContent *fyne.Container
	agendaBody  *fyne.Container
	agendaTitle *widget.Label
}

func (s *calendarPopupState) refreshGrid() {
	s.headerLabel.SetText(monthHeader(s.viewYear, s.viewMonth))
	s.gridContent.Objects = s.buildGrid()
	s.gridContent.Refresh()
}

func (s *calendarPopupState) buildGrid() []fyne.CanvasObject {
	loc := s.today.Location()

	first := time.Date(s.viewYear, s.viewMonth, 1, 0, 0, 0, 0, loc)
	weekday := int(first.Weekday())
	if weekday == 0 {
		weekday = 7 // Monday-start
	}
	weekday-- // 0=Mon, 6=Sun

	daysInMonth := time.Date(s.viewYear, s.viewMonth+1, 0, 0, 0, 0, 0, loc).Day()

	dotsByDay := s.eventDotsForMonth()

	dayNames := []string{
		locale.T("cal.week"),
		locale.T("cal.mon"), locale.T("cal.tue"), locale.T("cal.wed"),
		locale.T("cal.thu"), locale.T("cal.fri"), locale.T("cal.sat"),
		locale.T("cal.sun"),
	}

	var objects []fyne.CanvasObject
	for i, name := range dayNames {
		style := fyne.TextStyle{}
		if i == 0 {
			style.Italic = true
		}
		label := widget.NewLabelWithStyle(name, fyne.TextAlignCenter, style)
		objects = append(objects, label)
	}

	cellIdx := 0
	d := 1
	for d <= daysInMonth {
		dayDate := time.Date(s.viewYear, s.viewMonth, d, 0, 0, 0, 0, loc)
		if cellIdx%8 == 0 {
			_, wk := dayDate.ISOWeek()
			wkLabel := widget.NewLabelWithStyle(fmt.Sprintf("%d", wk),
				fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
			wkLabel.Importance = widget.LowImportance
			objects = append(objects, wkLabel)
			cellIdx++
		}

		if d == 1 && cellIdx%8-1 < weekday {
			for cellIdx%8-1 < weekday {
				objects = append(objects, layout.NewSpacer())
				cellIdx++
			}
		}

		objects = append(objects, s.buildDayCell(d, dayDate, dotsByDay[d]))
		cellIdx++
		d++
	}

	for cellIdx%8 != 0 {
		objects = append(objects, layout.NewSpacer())
		cellIdx++
	}
	return objects
}

func (s *calendarPopupState) buildDayCell(day int, date time.Time, dots []color.Color) fyne.CanvasObject {
	label := widget.NewLabel(fmt.Sprintf("%d", day))
	label.Alignment = fyne.TextAlignCenter

	isToday := sameDay(date, s.today)
	isSelected := sameDay(date, s.selected)

	if isToday {
		label.Importance = widget.HighImportance
	}

	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(40, 32))
	stack := []fyne.CanvasObject{sizer}
	if isToday {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		bg.CornerRadius = 4
		stack = append(stack, bg)
	} else if isSelected {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
		bg.StrokeColor = theme.Color(theme.ColorNamePrimary)
		bg.StrokeWidth = 1.5
		bg.CornerRadius = 4
		stack = append(stack, bg)
	}
	stack = append(stack, label)

	if len(dots) > 0 {
		const maxDots = 3
		shown := dots
		if len(shown) > maxDots {
			shown = shown[:maxDots]
		}
		dotRow := container.New(layout.NewHBoxLayout())
		dotRow.Add(layout.NewSpacer())
		for _, c := range shown {
			circle := canvas.NewCircle(c)
			sizer := canvas.NewRectangle(color.Transparent)
			sizer.SetMinSize(fyne.NewSize(4, 4))
			dotRow.Add(container.NewStack(sizer, circle))
		}
		dotRow.Add(layout.NewSpacer())
		stack = append(stack, container.NewBorder(nil, dotRow, nil, nil))
	}

	cellContent := container.NewStack(stack...)
	clickedDay := day
	return newTappableContainer(cellContent, func() {
		s.selected = time.Date(s.viewYear, s.viewMonth, clickedDay, 0, 0, 0, 0, s.today.Location())
		s.refreshGrid()
		s.refreshAgenda()
	})
}

// eventDotsForMonth returns, for each day of the month being viewed, the
// list of distinct calendar colours that have at least one event on that
// day. Multi-day events contribute a dot to every day they cover, so a
// 3-day trip lights up three cells, not just the start.
func (s *calendarPopupState) eventDotsForMonth() map[int][]color.Color {
	out := map[int][]color.Color{}
	svc := calendar.Get()
	if svc == nil || svc.Store() == nil {
		return out
	}
	loc := s.today.Location()
	monthStart := time.Date(s.viewYear, s.viewMonth, 1, 0, 0, 0, 0, loc)
	monthEnd := monthStart.AddDate(0, 1, 0)
	events := svc.Store().EventsBetween(monthStart, monthEnd)
	if len(events) == 0 {
		return out
	}
	colors := buildCalendarColorMap(svc.Store())
	seen := map[int]map[string]struct{}{}

	for _, ev := range events {
		col := colors[ev.CalendarID]
		// Clamp event range to the month being viewed.
		evStart := ev.Start.In(loc)
		evEnd := ev.End.In(loc)
		// Google all-day events have an exclusive end date (next-day
		// midnight). Treat one-second-before-end so the loop below
		// stops on the right day.
		if ev.AllDay {
			evEnd = evEnd.Add(-time.Second)
		}
		if evStart.Before(monthStart) {
			evStart = monthStart
		}
		if evEnd.After(monthEnd) {
			evEnd = monthEnd
		}

		cur := time.Date(evStart.Year(), evStart.Month(), evStart.Day(), 0, 0, 0, 0, loc)
		endDay := time.Date(evEnd.Year(), evEnd.Month(), evEnd.Day(), 0, 0, 0, 0, loc)
		for !cur.After(endDay) {
			if cur.Year() == s.viewYear && cur.Month() == s.viewMonth {
				day := cur.Day()
				if _, ok := seen[day]; !ok {
					seen[day] = map[string]struct{}{}
				}
				if _, dup := seen[day][ev.CalendarID]; !dup {
					seen[day][ev.CalendarID] = struct{}{}
					out[day] = append(out[day], col)
				}
			}
			cur = cur.AddDate(0, 0, 1)
		}
	}
	return out
}

// buildCalendarColorMap maps calendarID → display colour. Falls back to
// the theme primary when a calendar carries no colour hint.
func buildCalendarColorMap(store *calendar.Store) map[string]color.Color {
	out := map[string]color.Color{}
	primary := theme.Color(theme.ColorNamePrimary)
	for _, acc := range store.Accounts() {
		for _, c := range store.CalendarsFor(acc.ID) {
			col := parseHexColor(c.ColorHex)
			if col == nil {
				col = primary
			}
			out[c.ID] = col
		}
	}
	return out
}

// parseHexColor accepts "#RRGGBB" / "RRGGBB"; returns nil if unparseable.
func parseHexColor(s string) color.Color {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) != 6 {
		return nil
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return nil
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}
}

// --- agenda panel ---

func (s *calendarPopupState) buildAgenda() fyne.CanvasObject {
	s.agendaTitle = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	scroll := container.NewVScroll(s.agendaBody)
	return container.NewBorder(s.agendaTitle, nil, nil, nil, scroll)
}

func (s *calendarPopupState) refreshAgenda() {
	if s.agendaTitle == nil || s.agendaBody == nil {
		return
	}
	s.agendaTitle.SetText(formatAgendaTitle(s.selected, s.today))

	svc := calendar.Get()
	if svc == nil || svc.Store() == nil {
		s.agendaBody.Objects = []fyne.CanvasObject{centeredHint(locale.T("cal.noAccount"))}
		s.agendaBody.Refresh()
		return
	}

	loc := s.today.Location()
	dayStart := time.Date(s.selected.Year(), s.selected.Month(), s.selected.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	events := svc.Store().EventsBetween(dayStart, dayEnd)
	if len(events) == 0 {
		s.agendaBody.Objects = []fyne.CanvasObject{centeredHint(locale.T("cal.noEvents"))}
		s.agendaBody.Refresh()
		return
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].AllDay != events[j].AllDay {
			return events[i].AllDay
		}
		return events[i].Start.Before(events[j].Start)
	})

	colors := buildCalendarColorMap(svc.Store())
	objects := make([]fyne.CanvasObject, 0, len(events))
	for _, ev := range events {
		objects = append(objects, buildEventRow(ev, colors[ev.CalendarID], dayStart))
	}
	s.agendaBody.Objects = objects
	s.agendaBody.Refresh()
}

func buildEventRow(ev calendar.Event, col color.Color, dayStart time.Time) fyne.CanvasObject {
	if col == nil {
		col = theme.Color(theme.ColorNamePrimary)
	}
	bar := canvas.NewRectangle(col)
	bar.SetMinSize(fyne.NewSize(3, 0))

	timeText := formatEventTimeForDay(ev, dayStart)
	timeLbl := widget.NewLabelWithStyle(timeText, fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	timeLbl.Importance = widget.MediumImportance

	titleLbl := widget.NewLabel(ev.Title)
	titleLbl.Truncation = fyne.TextTruncateEllipsis
	titleLbl.Wrapping = fyne.TextWrapOff

	rows := []fyne.CanvasObject{titleLbl}
	if ev.MeetingURL != "" {
		joinBtn := widget.NewButtonWithIcon(locale.T("cal.join"), theme.MediaPlayIcon(), func() {
			openURL(ev.MeetingURL)
		})
		joinBtn.Importance = widget.LowImportance
		rows = append(rows, joinBtn)
	} else if ev.Location != "" {
		loc := widget.NewLabel(ev.Location)
		loc.Truncation = fyne.TextTruncateEllipsis
		loc.Importance = widget.LowImportance
		rows = append(rows, loc)
	}

	body := container.NewVBox(timeLbl, container.NewVBox(rows...))
	row := container.NewBorder(nil, nil, bar, nil, body)

	target := ev.HTMLLink
	if target == "" {
		target = ev.MeetingURL
	}
	if target == "" {
		return row
	}
	return newTappableContainer(row, func() { openURL(target) })
}

// --- calendar filter popup ---

// showFilterPopup opens a small popup listing every calendar across all
// accounts with a checkbox for its Enabled flag. Toggling immediately
// persists the change and triggers a refresh; the popup itself observes
// no state — the store's listener pushes the new colours/dots back into
// the popup on the next sync (and the cached events the user already
// has remain visible until then).
func (s *calendarPopupState) showFilterPopup(anchor fyne.CanvasObject) {
	svc := calendar.Get()
	if svc == nil || svc.Store() == nil {
		return
	}
	store := svc.Store()
	accounts := store.Accounts()
	if len(accounts) == 0 {
		return
	}

	canv := fyne.CurrentApp().Driver().CanvasForObject(anchor)
	if canv == nil {
		return
	}

	var rows []fyne.CanvasObject
	for _, acc := range accounts {
		acc := acc
		header := widget.NewLabelWithStyle(acc.Display,
			fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		rows = append(rows, header)

		cals := store.CalendarsFor(acc.ID)
		sort.Slice(cals, func(i, j int) bool {
			if cals[i].Primary != cals[j].Primary {
				return cals[i].Primary
			}
			return cals[i].Name < cals[j].Name
		})
		for _, c := range cals {
			c := c
			prefs := acc.Calendars[c.ID]
			row := buildFilterRow(c, prefs.Enabled, func(enabled bool) {
				updated, ok := store.AccountByID(acc.ID)
				if !ok {
					return
				}
				if updated.Calendars == nil {
					updated.Calendars = make(map[string]calendar.CalendarPrefs)
				}
				p := updated.Calendars[c.ID]
				p.Enabled = enabled
				updated.Calendars[c.ID] = p
				_ = store.PutAccount(updated)
				svc.Refresh(acc.ID)
			})
			rows = append(rows, row)
		}
	}

	body := container.NewVBox(rows...)
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(280, 300))

	popup := widget.NewPopUp(scroll, canv)
	// Anchor the popup just below the filter button, right-aligned with it.
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	popupSize := popup.MinSize()
	x := pos.X + anchor.Size().Width - popupSize.Width
	if x < 0 {
		x = pos.X
	}
	y := pos.Y + anchor.Size().Height + 4
	popup.ShowAtPosition(fyne.NewPos(x, y))
}

func buildFilterRow(c calendar.Calendar, enabled bool, onToggle func(bool)) fyne.CanvasObject {
	col := parseHexColor(c.ColorHex)
	if col == nil {
		col = theme.Color(theme.ColorNamePrimary)
	}
	swatch := canvas.NewCircle(col)
	swatchSizer := canvas.NewRectangle(color.Transparent)
	swatchSizer.SetMinSize(fyne.NewSize(10, 10))
	swatchBox := container.NewStack(swatchSizer, swatch)

	chk := widget.NewCheck(c.Name, onToggle)
	chk.Checked = enabled

	return container.NewBorder(nil, nil, swatchBox, nil, chk)
}

// --- helpers ---

func monthHeader(year int, month time.Month) string {
	return fmt.Sprintf("%s %d", locale.MonthName(int(month)), year)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func formatAgendaTitle(selected, today time.Time) string {
	if sameDay(selected, today) {
		return locale.T("cal.today")
	}
	return selected.Format("Mon 2 Jan")
}

// formatEventTimeForDay describes an event's time relative to the day
// being shown. Single-day timed events render as "09:00 – 10:00".
// Events that started yesterday render as "→ 10:00" (only the end is
// today). Events that continue past today render as "23:00 →". All-day
// events render as the localized "all-day" label regardless of span.
func formatEventTimeForDay(ev calendar.Event, dayStart time.Time) string {
	if ev.AllDay {
		return locale.T("cal.allDay")
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	startsToday := !ev.Start.Before(dayStart)
	endsToday := !ev.End.After(dayEnd)
	switch {
	case startsToday && endsToday:
		return fmt.Sprintf("%s – %s", ev.Start.Format("15:04"), ev.End.Format("15:04"))
	case !startsToday && endsToday:
		return "→ " + ev.End.Format("15:04")
	case startsToday && !endsToday:
		return ev.Start.Format("15:04") + " →"
	default:
		return locale.T("cal.allDay")
	}
}

func centeredHint(text string) fyne.CanvasObject {
	lbl := widget.NewLabel(text)
	lbl.Importance = widget.LowImportance
	lbl.Alignment = fyne.TextAlignCenter
	return container.NewCenter(lbl)
}

// openURL launches a URL in the user's default browser via xdg-open.
// Detached on purpose: we don't want to wait on the browser process.
func openURL(target string) {
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}
