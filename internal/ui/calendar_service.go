package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde/internal/calendar"
	"fyshos.com/tyde/internal/calendar/google"
	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wm"
)

// startCalendarService boots the calendar runtime (store + syncer +
// reminder scheduler) so UI surfaces can subscribe and reflect events.
// Failures are non-fatal — the desktop still starts; UI surfaces will
// simply show empty state until calendar.Get() returns a working service.
func startCalendarService() {
	provider := &google.Provider{
		TokenFunc: func(ctx context.Context, account calendar.Account) (string, time.Time, error) {
			switch account.Source {
			case calendar.SourceGOA:
				return google.GOAToken(ctx, account)
			case calendar.SourceOAuth:
				return google.OAuthToken(ctx, account)
			default:
				return "", time.Time{}, nil
			}
		},
	}
	svc, _, err := calendar.Start(context.Background(), provider, 5*time.Minute)
	if err != nil {
		fyne.LogError("calendar service start", err)
		return
	}
	if svc == nil || svc.Store() == nil {
		return
	}

	scheduler := calendar.NewScheduler(svc.Store(), buildCalendarNotify())
	scheduler.Start(context.Background())
	svc.SetReminder(scheduler)
}

// buildCalendarNotify produces a NotifyFunc that turns reminder events
// into desktop notifications. Notifications carry a "Join" action when
// the event has a meeting URL; clicking the action launches the URL via
// xdg-open. We chain a single shared action callback so calendar
// notifications and any other consumer can coexist.
func buildCalendarNotify() calendar.NotifyFunc {
	return func(ev calendar.Event, until time.Duration) {
		title := ev.Title
		if title == "" {
			title = locale.T("cal.next")
		}

		var body string
		switch {
		case until <= 0:
			body = locale.T("cal.now")
		case until < time.Minute:
			body = locale.T("cal.in") + " <1 " + locale.T("cal.minutes")
		default:
			minutes := int(until.Round(time.Minute).Minutes())
			body = fmt.Sprintf("%s %d %s", locale.T("cal.in"), minutes, locale.T("cal.minutes"))
		}
		if ev.Location != "" {
			body += " — " + ev.Location
		}

		var actions []string
		if ev.MeetingURL != "" {
			actions = []string{"calendar:join", locale.T("cal.join")}
		}

		n := wm.NewNotificationFull("Tyde", "x-office-calendar", title, body, actions, 0)
		n.AppID = "tyde-calendar"
		if ev.MeetingURL != "" {
			rememberCalendarJoinURL(n.ID, ev.MeetingURL)
		}
		wm.SendNotification(n)
	}
}

// pendingCalendarJoins maps a notification ID to the meeting URL that
// should open when the user invokes the "calendar:join" action. Bounded
// by the maxHistory of the wm notification server (50), so size stays
// trivial; we still prune on use to avoid unbounded growth in absurd
// cases.
var (
	calendarJoinMu          sync.Mutex
	calendarJoinURL         = make(map[uint32]string)
	calendarActionInstalled bool
)

func rememberCalendarJoinURL(id uint32, url string) {
	calendarJoinMu.Lock()
	calendarJoinURL[id] = url
	if len(calendarJoinURL) > 256 {
		// Drop oldest by ID — IDs are monotonic.
		var minID uint32
		for k := range calendarJoinURL {
			if minID == 0 || k < minID {
				minID = k
			}
		}
		delete(calendarJoinURL, minID)
	}
	installed := calendarActionInstalled
	calendarActionInstalled = true
	calendarJoinMu.Unlock()

	if !installed {
		wm.SetActionCallback(func(notifID uint32, actionKey string) {
			if actionKey != "calendar:join" {
				return
			}
			calendarJoinMu.Lock()
			url, ok := calendarJoinURL[notifID]
			delete(calendarJoinURL, notifID)
			calendarJoinMu.Unlock()
			if !ok || url == "" {
				return
			}
			openURL(url)
		})
	}
}
