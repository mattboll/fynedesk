package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"fyshos.com/tyde/internal/calendar"
)

// Provider implements calendar.Provider for Google Calendar.
//
// It owns no credentials directly: each call goes through TokenFunc, which
// must produce a fresh access token for the given account. This lets the
// same Provider serve both GOA-sourced accounts (where TokenFunc calls
// GOAToken) and OAuth-flow accounts (where it pulls from the secret store
// and refreshes via the upstream token endpoint).
type Provider struct {
	// TokenFunc returns a fresh access_token for the given account.
	// It is called per request — implementations should cache to avoid
	// hitting GOA / the refresh endpoint on every API call.
	TokenFunc func(ctx context.Context, account calendar.Account) (string, time.Time, error)

	mu       sync.Mutex
	services map[string]cachedService // by account ID
}

// cachedService is a calendar client and the access token it uses.
type cachedService struct {
	svc    *gcal.Service
	expiry time.Time
}

// tokenMargin is how long before its expiry a token is renewed.
const tokenMargin = time.Minute

// Name returns the provider identifier.
func (p *Provider) Name() string { return "google" }

// ListAccounts returns no accounts: the calendar.Store is the source of
// truth for configured accounts. The Provider only acts on accounts handed
// to it — this method satisfies the interface but is not the discovery path.
// Use ListGOAGoogleAccounts (GOA) or the OAuth flow to populate the store.
func (p *Provider) ListAccounts(ctx context.Context) ([]calendar.Account, error) {
	return nil, nil
}

// ListCalendars returns the calendars accessible to the account.
func (p *Provider) ListCalendars(ctx context.Context, account calendar.Account) ([]calendar.Calendar, error) {
	svc, err := p.service(ctx, account)
	if err != nil {
		return nil, err
	}
	res, err := svc.CalendarList.List().ShowHidden(false).Context(ctx).Do()
	if err != nil {
		p.forgetOnAuthError(account.ID, err)
		return nil, fmt.Errorf("calendar list: %w", err)
	}
	out := make([]calendar.Calendar, 0, len(res.Items))
	for _, item := range res.Items {
		out = append(out, calendar.Calendar{
			ID:       item.Id,
			Name:     coalesce(item.SummaryOverride, item.Summary),
			ColorHex: item.BackgroundColor,
			Primary:  item.Primary,
		})
	}
	return out, nil
}

// ListEvents returns events from one calendar within [from, to).
// Recurring events are expanded into single occurrences.
func (p *Provider) ListEvents(ctx context.Context, account calendar.Account, calendarID string, from, to time.Time) ([]calendar.Event, error) {
	svc, err := p.service(ctx, account)
	if err != nil {
		return nil, err
	}
	call := svc.Events.List(calendarID).
		SingleEvents(true).
		OrderBy("startTime").
		TimeMin(from.Format(time.RFC3339)).
		TimeMax(to.Format(time.RFC3339)).
		MaxResults(2500).
		Context(ctx)

	var out []calendar.Event
	pageToken := ""
	for {
		c := call
		if pageToken != "" {
			c = c.PageToken(pageToken)
		}
		res, err := c.Do()
		if err != nil {
			p.forgetOnAuthError(account.ID, err)
			return nil, fmt.Errorf("events list %s: %w", calendarID, err)
		}
		for _, ev := range res.Items {
			parsed, ok := convertEvent(ev, calendarID)
			if !ok {
				continue
			}
			out = append(out, parsed)
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}
	return out, nil
}

// service returns the calendar client of an account. The client and its
// access token are kept until shortly before the token expires: a sync
// makes one call per calendar, and each used to get a token (a refresh
// with Google, or a GOA call) and build a client.
func (p *Provider) service(ctx context.Context, account calendar.Account) (*gcal.Service, error) {
	if p.TokenFunc == nil {
		return nil, errors.New("google.Provider.TokenFunc is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.services[account.ID]; ok && time.Until(c.expiry) > tokenMargin {
		return c.svc, nil
	}
	tok, expiry, err := p.TokenFunc(ctx, account)
	if err != nil {
		return nil, err
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: tok,
		TokenType:   "Bearer",
		Expiry:      expiry,
	})
	// The client outlives this call's context: it is kept.
	svc, err := gcal.NewService(context.Background(), option.WithTokenSource(src))
	if err != nil {
		return nil, err
	}
	if p.services == nil {
		p.services = map[string]cachedService{}
	}
	p.services[account.ID] = cachedService{svc: svc, expiry: expiry}
	return svc, nil
}

// forgetOnAuthError drops the kept client when Google refused its token, so
// that the next call gets a new one instead of waiting for it to expire.
func (p *Provider) forgetOnAuthError(accountID string, err error) {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusUnauthorized {
		p.Forget(accountID)
	}
}

// Forget drops what is kept for an account (its access was revoked, or it
// was removed).
func (p *Provider) Forget(accountID string) {
	p.mu.Lock()
	delete(p.services, accountID)
	p.mu.Unlock()
}

// convertEvent maps a Google calendar event into our type. Returns ok=false
// for unparseable events (cancelled placeholders, missing start times, …).
func convertEvent(ev *gcal.Event, calendarID string) (calendar.Event, bool) {
	if ev.Status == "cancelled" {
		return calendar.Event{}, false
	}
	start, allDayStart, ok := parseEventTime(ev.Start)
	if !ok {
		return calendar.Event{}, false
	}
	end, allDayEnd, ok := parseEventTime(ev.End)
	if !ok {
		return calendar.Event{}, false
	}
	allDay := allDayStart && allDayEnd

	out := calendar.Event{
		ID:         ev.Id,
		CalendarID: calendarID,
		Title:      ev.Summary,
		Start:      start,
		End:        end,
		AllDay:     allDay,
		Location:   ev.Location,
		HTMLLink:   ev.HtmlLink,
	}
	out.MeetingURL = extractMeetingURL(ev)
	return out, true
}

// parseEventTime accepts a Google EventDateTime (which can hold a
// timestamped DateTime or an all-day Date) and returns the time + whether
// it is all-day.
func parseEventTime(t *gcal.EventDateTime) (time.Time, bool, bool) {
	if t == nil {
		return time.Time{}, false, false
	}
	if t.DateTime != "" {
		parsed, err := time.Parse(time.RFC3339, t.DateTime)
		if err != nil {
			return time.Time{}, false, false
		}
		return parsed, false, true
	}
	if t.Date != "" {
		// All-day: date string in the timezone of the calendar.
		loc := time.Local
		if t.TimeZone != "" {
			if z, err := time.LoadLocation(t.TimeZone); err == nil {
				loc = z
			}
		}
		parsed, err := time.ParseInLocation("2006-01-02", t.Date, loc)
		if err != nil {
			return time.Time{}, false, false
		}
		return parsed, true, true
	}
	return time.Time{}, false, false
}

// meetURLPattern catches the most common video conference URLs in event
// description/location/conferenceData.
var meetURLPattern = regexp.MustCompile(`https://(?:` +
	`meet\.google\.com/[a-z]{3}-[a-z]{4}-[a-z]{3}|` +
	`[a-z0-9-]+\.zoom\.us/j/\d+(?:\?[^\s]+)?|` +
	`teams\.microsoft\.com/l/meetup-join/[^\s)]+|` +
	`[a-z0-9-]+\.webex\.com/[^\s)]+|` +
	`whereby\.com/[^\s)]+|` +
	`meet\.jit\.si/[^\s)]+` +
	`)`)

// extractMeetingURL pulls the best-effort video conference URL out of an
// event. It prefers structured ConferenceData (most reliable), then falls
// back to regex over location + description.
func extractMeetingURL(ev *gcal.Event) string {
	if ev.ConferenceData != nil {
		for _, ep := range ev.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && calendar.IsWebLink(ep.Uri) {
				return ep.Uri
			}
		}
	}
	for _, hay := range []string{ev.Location, ev.Description} {
		if m := meetURLPattern.FindString(hay); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
