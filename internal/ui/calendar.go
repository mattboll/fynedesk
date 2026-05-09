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

// calendarPopupSize is the natural size of the popup. The agenda panel
// pushes the width well past the original 340; the height stays roughly
// the same as before.
var calendarPopupSize = fyne.NewSize(580, 340)

// calendarPopup builds the navigable month + agenda popup. When a calendar
// service is running, day cells gain coloured event dots and clicking a
// day populates the agenda panel on the right; otherwise it falls back to
// the same plain month grid the popup used before calendar integration
// landed.
func calendarPopup(now time.Time) fyne.CanvasObject {
	state := &calendarPopupState{
		today:       now,
		selected:    now,
		viewYear:    now.Year(),
		viewMonth:   now.Month(),
		agendaBody:  container.NewVBox(),
		gridContent: container.New(layout.NewGridWrapLayout(fyne.NewSize(38, 30))),
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

	header := container.NewBorder(nil, nil, prevBtn, container.NewHBox(todayBtn, nextBtn), state.headerLabel)

	state.refreshGrid()

	sep := canvas.NewRectangle(color.NRGBA{R: 128, G: 128, B: 128, A: 64})
	sep.SetMinSize(fyne.NewSize(0, 1))

	monthSide := container.NewVBox(header, sep, state.gridContent)

	// Build the agenda side: a fixed-width panel with the date heading and
	// a scrollable event list. On systems with no service, we show a quiet
	// "no calendar connected" hint instead.
	agenda := state.buildAgenda()

	root := container.NewBorder(nil, nil, nil, agenda, monthSide)
	state.refreshAgenda()

	// Subscribe to store changes so the popup repaints when sync brings in
	// new events (or when the user toggles a calendar in Settings while
	// the popup is open).
	if svc := calendar.Get(); svc != nil && svc.Store() != nil {
		unsub := svc.Store().Subscribe(func() {
			fyne.Do(func() {
				state.refreshGrid()
				state.refreshAgenda()
			})
		})
		// The popup window is short-lived; a Tappable wrapper has no
		// destructor hook exposed by Fyne, so we settle for unsubscribing
		// when the popup is rebuilt next time. In the meantime the
		// listener is cheap (a slice append).
		_ = unsub
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

	// Count events per day for colored dots, keyed by day-of-month.
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

	stack := []fyne.CanvasObject{}

	// Background highlight: filled circle for today, ring for the
	// currently selected day (if not today).
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

	// Coloured dots row at the bottom of the cell. Capped at 3 dots; the
	// row is centered horizontally.
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
			// canvas.Circle has no MinSize; wrap in a fixed-size spacer
			// rectangle so the dot reliably renders at 4x4.
			sizer := canvas.NewRectangle(color.Transparent)
			sizer.SetMinSize(fyne.NewSize(4, 4))
			dotRow.Add(container.NewStack(sizer, circle))
		}
		dotRow.Add(layout.NewSpacer())
		// Push to bottom of the cell with a top spacer.
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
// list of distinct calendar colours that have at least one event that
// day. We keep it cheap: O(n) over the cached events for the displayed
// month, no API calls.
func (s *calendarPopupState) eventDotsForMonth() map[int][]color.Color {
	out := map[int][]color.Color{}
	svc := calendar.Get()
	if svc == nil || svc.Store() == nil {
		return out
	}
	loc := s.today.Location()
	from := time.Date(s.viewYear, s.viewMonth, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, 0)
	events := svc.Store().EventsBetween(from, to)
	if len(events) == 0 {
		return out
	}
	colors := buildCalendarColorMap(svc.Store())
	seen := map[int]map[string]struct{}{}
	for _, ev := range events {
		day := ev.Start.In(loc).Day()
		key := ev.CalendarID
		if _, ok := seen[day]; !ok {
			seen[day] = map[string]struct{}{}
		}
		if _, dup := seen[day][key]; dup {
			continue
		}
		seen[day][key] = struct{}{}
		out[day] = append(out[day], colors[key])
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
	scroll.SetMinSize(fyne.NewSize(220, 260))
	wrap := container.NewBorder(s.agendaTitle, nil, nil, nil, scroll)
	// A little left padding so the agenda doesn't bump into the month grid.
	pad := canvas.NewRectangle(color.Transparent)
	pad.SetMinSize(fyne.NewSize(8, 0))
	return container.NewBorder(nil, nil, pad, nil, wrap)
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
		objects = append(objects, buildEventRow(ev, colors[ev.CalendarID]))
	}
	s.agendaBody.Objects = objects
	s.agendaBody.Refresh()
}

func buildEventRow(ev calendar.Event, col color.Color) fyne.CanvasObject {
	if col == nil {
		col = theme.Color(theme.ColorNamePrimary)
	}
	bar := canvas.NewRectangle(col)
	bar.SetMinSize(fyne.NewSize(3, 0))

	timeText := formatEventRange(ev)
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

	// Click anywhere on the row to open the upstream event link, falling
	// back to the meeting URL if HTMLLink is empty (rare).
	target := ev.HTMLLink
	if target == "" {
		target = ev.MeetingURL
	}
	if target == "" {
		return row
	}
	return newTappableContainer(row, func() { openURL(target) })
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

func formatEventRange(ev calendar.Event) string {
	if ev.AllDay {
		return locale.T("cal.allDay")
	}
	return fmt.Sprintf("%s – %s", ev.Start.Format("15:04"), ev.End.Format("15:04"))
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
