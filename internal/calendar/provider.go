// Package calendar provides multi-account calendar integration for fynedesk.
//
// The package is provider-agnostic: a Provider abstracts the upstream
// service (Google, CalDAV, Microsoft, …). Phase 1 ships a Google
// implementation that prefers GNOME Online Accounts (GOA) when available,
// and falls back to a direct OAuth loopback flow.
//
// Tokens are stored via the freedesktop Secret Service (any compliant
// keyring: gnome-keyring, kwallet, KeePassXC, …) with an encrypted-file
// fallback. Event caches are plain JSON in $XDG_CONFIG_HOME/fynedesk/calendar.
package calendar

import (
	"context"
	"time"
)

// AccountSource identifies how an account's credentials are obtained.
type AccountSource string

const (
	// SourceGOA: credentials are brokered by GNOME Online Accounts via
	// D-Bus. We never see a refresh_token; we ask GOA for a fresh
	// access_token each time we need one.
	SourceGOA AccountSource = "goa"
	// SourceOAuth: we ran the OAuth loopback flow ourselves and hold a
	// refresh_token in the secret store.
	SourceOAuth AccountSource = "oauth"
)

// Provider is the upstream calendar service. Phase 1 ships only Google.
type Provider interface {
	// Name is a stable identifier ("google", "caldav", …).
	Name() string

	// ListAccounts enumerates the accounts known to this provider on the
	// local system (e.g. via GOA D-Bus, or by reading the local store).
	ListAccounts(ctx context.Context) ([]Account, error)

	// ListCalendars returns the calendars accessible on a given account.
	ListCalendars(ctx context.Context, account Account) ([]Calendar, error)

	// ListEvents returns events for a single calendar within [from, to).
	ListEvents(ctx context.Context, account Account, calendarID string, from, to time.Time) ([]Event, error)
}

// Account is a credential bundle for one upstream identity.
type Account struct {
	// ID is a stable per-account identifier:
	//   - GOA: D-Bus object path ("/org/gnome/OnlineAccounts/Accounts/account_NN")
	//   - OAuth: "google:<email>"
	// Used as filename for the cache.
	ID string `json:"id"`

	Provider string        `json:"provider"` // "google"
	Source   AccountSource `json:"source"`   // "goa" | "oauth"
	Email    string        `json:"email"`
	Display  string        `json:"display"` // user-facing label

	// Per-calendar visibility/colour overrides, keyed by Calendar.ID.
	Calendars map[string]CalendarPrefs `json:"calendars,omitempty"`

	// Provider-specific extras (refresh path, GOA object path, …).
	// Kept opaque so we don't leak token material into JSON: the
	// secrets layer handles real credentials, this map carries only
	// non-sensitive lookups.
	Extra map[string]string `json:"extra,omitempty"`
}

// CalendarPrefs holds per-calendar user preferences.
type CalendarPrefs struct {
	Enabled  bool   `json:"enabled"`
	ColorHex string `json:"color,omitempty"` // override; empty = use upstream
}

// Calendar represents a calendar inside an account.
type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ColorHex string `json:"color,omitempty"` // upstream colour, e.g. "#7986CB"
	Primary  bool   `json:"primary,omitempty"`
}

// Event is one occurrence of a meeting/event.
type Event struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	Title      string    `json:"title"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	AllDay     bool      `json:"allDay,omitempty"`
	Location   string    `json:"location,omitempty"`
	// MeetingURL is the best-effort video conference URL (Meet, Zoom, …)
	// extracted from the event. Empty if none.
	MeetingURL string `json:"meetingUrl,omitempty"`
	// HTMLLink points to the event on the upstream service (for "open").
	HTMLLink string `json:"htmlLink,omitempty"`
	// ColorHex overrides the calendar colour for this event (rare).
	ColorHex string `json:"color,omitempty"`
}
