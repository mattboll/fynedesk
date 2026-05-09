package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"fyshos.com/fynedesk/internal/calendar"
	"fyshos.com/fynedesk/internal/calendar/google"
)

// cmdCalendar dispatches `fynedesk-ctl calendar <sub>` to the right
// handler. None of the subcommands require the compositor IPC socket —
// they read and mutate the calendar store on disk directly. This makes
// the CLI usable for one-shot setup before the panel is even running.
func cmdCalendar(args []string) {
	if len(args) < 1 {
		calendarUsage()
		os.Exit(1)
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "help", "--help", "-h":
		calendarUsage()
	case "add-goa":
		calAddGOA(rest)
	case "add-oauth":
		calAddOAuth(rest)
	case "list", "ls":
		calList()
	case "remove", "rm":
		requireArg(rest, "account ID or email")
		calRemove(rest[0])
	case "refresh":
		calRefresh(rest)
	case "today":
		calToday()
	case "next":
		calNext()
	default:
		fmt.Fprintf(os.Stderr, "fynedesk-ctl calendar: unknown subcommand %q\n", sub)
		calendarUsage()
		os.Exit(1)
	}
}

func calendarUsage() {
	fmt.Fprintln(os.Stderr, `Usage: fynedesk-ctl calendar <subcommand>

Subcommands:
  add-goa                Add a Google account from GNOME Online Accounts.
                         If a single Google account is configured in GOA,
                         it is added directly; otherwise pick from a list.
  add-oauth              Add a Google account via OAuth loopback flow.
                         You must supply your own OAuth client_id.
  list                   List configured accounts and their calendars.
  remove <id|email>      Remove an account and its cached events.
  refresh [<id|email>]   Force a sync now. Default: all accounts.
  today                  List today's events across all accounts.
  next                   Show the next upcoming event.

Storage:
  Accounts:   ~/.config/fynedesk/calendar/accounts.json
  Cache:      ~/.config/fynedesk/calendar/cache_*.json
  OAuth keys: Secret Service (org.freedesktop.secrets) — gnome-keyring,
              kwallet, KeePassXC, … — with encrypted-file fallback.`)
}

func calAddGOA(args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if !google.GOAAvailable() {
		fatal("GNOME Online Accounts is not available on this session.\n" +
			"Install gnome-online-accounts and add a Google account in GNOME Settings,\n" +
			"or use 'fynedesk-ctl calendar add-oauth'.")
	}

	candidates, err := google.ListGOAGoogleAccounts(ctx)
	if err != nil {
		fatal("query GOA: %v", err)
	}
	if len(candidates) == 0 {
		fatal("no Google accounts found in GOA. Open GNOME Settings → Online Accounts and add one.")
	}

	var chosen calendar.Account
	if len(args) > 0 {
		// User specified an email or path; match by email or by path suffix.
		needle := args[0]
		for _, c := range candidates {
			if c.Email == needle || strings.HasSuffix(c.ID, needle) {
				chosen = c
				break
			}
		}
		if chosen.ID == "" {
			fatal("no GOA Google account matches %q", needle)
		}
	} else if len(candidates) == 1 {
		chosen = candidates[0]
	} else {
		fmt.Println("Multiple Google accounts found in GOA. Pick one:")
		for i, c := range candidates {
			fmt.Printf("  [%d] %s\n", i+1, c.Display)
		}
		fmt.Print("Choice: ")
		idx := readChoice(len(candidates))
		chosen = candidates[idx]
	}

	if _, _, err := google.GOAToken(ctx, chosen); err != nil {
		fatal("GOA returned no token for %s: %v\n"+
			"Open GNOME Settings → Online Accounts and re-enable Calendar access.", chosen.Email, err)
	}

	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	if err := store.PutAccount(chosen); err != nil {
		fatal("save account: %v", err)
	}
	fmt.Printf("Connected GOA account: %s\n", chosen.Display)
	fmt.Println("Run 'fynedesk-ctl calendar refresh' to fetch events now.")
}

func calAddOAuth(args []string) {
	scanner := bufio.NewScanner(os.Stdin)

	clientID := ""
	clientSecret := ""
	if len(args) >= 1 {
		clientID = args[0]
	}
	if len(args) >= 2 {
		clientSecret = args[1]
	}

	if clientID == "" {
		fmt.Println("OAuth client_id required.")
		fmt.Println()
		fmt.Println("To create a Google OAuth client:")
		fmt.Println("  1. https://console.cloud.google.com/apis/credentials")
		fmt.Println("  2. Create credentials → OAuth client ID → Application type: Desktop app")
		fmt.Println("  3. Enable the 'Google Calendar API' for the project")
		fmt.Println("  4. Paste the Client ID below.")
		fmt.Println()
		fmt.Print("Client ID: ")
		if !scanner.Scan() {
			fatal("reading client_id: %v", scanner.Err())
		}
		clientID = strings.TrimSpace(scanner.Text())
	}
	if clientID == "" {
		fatal("client_id is required")
	}
	if clientSecret == "" {
		fmt.Print("Client Secret (paste; press Enter if your client has none): ")
		if !scanner.Scan() {
			fatal("reading client_secret: %v", scanner.Err())
		}
		clientSecret = strings.TrimSpace(scanner.Text())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	cfg := google.OAuthConfig{ClientID: clientID, ClientSecret: clientSecret}
	tok, email, err := google.AuthorizeOAuth(ctx, cfg, nil)
	if err != nil {
		fatal("authorization failed: %v", err)
	}
	if tok.RefreshToken == "" {
		fatal("Google did not return a refresh_token. Revoke the consent at\n" +
			"https://myaccount.google.com/permissions and retry — Google only sends a\n" +
			"refresh_token on the first consent.")
	}

	id := "google:" + email
	if email == "" {
		id = "google:oauth-" + time.Now().Format("20060102-150405")
	}
	display := email
	if display == "" {
		display = "Google account"
	}
	acc := calendar.Account{
		ID:       id,
		Provider: "google",
		Source:   calendar.SourceOAuth,
		Email:    email,
		Display:  display,
	}
	if err := calendar.SaveSecret(id, calendar.SecretBundle{
		RefreshToken: tok.RefreshToken,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}); err != nil {
		fatal("save secret: %v", err)
	}
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	if err := store.PutAccount(acc); err != nil {
		fatal("save account: %v", err)
	}
	fmt.Printf("Connected: %s\n", display)
	fmt.Println("Run 'fynedesk-ctl calendar refresh' to fetch events now.")
}

func calList() {
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	accounts := store.Accounts()
	if len(accounts) == 0 {
		fmt.Println("No calendar accounts configured.")
		fmt.Println("Add one with: fynedesk-ctl calendar add-goa  (or add-oauth)")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tSOURCE\tCALENDARS")
	for _, acc := range accounts {
		cals := store.CalendarsFor(acc.ID)
		summary := fmt.Sprintf("%d", len(cals))
		if len(cals) == 0 {
			summary = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", acc.Email, acc.Source, summary)
	}
	_ = tw.Flush()
}

func calRemove(needle string) {
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	target, ok := findAccount(store, needle)
	if !ok {
		fatal("no account matches %q", needle)
	}
	_ = calendar.DeleteSecret(target.ID)
	if err := store.RemoveAccount(target.ID); err != nil {
		fatal("remove account: %v", err)
	}
	fmt.Printf("Removed: %s\n", target.Display)
}

func calRefresh(args []string) {
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	provider := newProvider()

	var targets []calendar.Account
	if len(args) >= 1 {
		acc, ok := findAccount(store, args[0])
		if !ok {
			fatal("no account matches %q", args[0])
		}
		targets = []calendar.Account{acc}
	} else {
		targets = store.Accounts()
	}
	if len(targets) == 0 {
		fmt.Println("No accounts to refresh.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, acc := range targets {
		if err := refreshOne(ctx, store, provider, acc); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", acc.Email, err)
			continue
		}
		evs := store.EventsBetween(time.Now().Add(-time.Hour), time.Now().Add(7*24*time.Hour))
		count := 0
		for _, e := range evs {
			if strings.HasPrefix(e.CalendarID, acc.Email) || acc.Calendars != nil {
				_ = e
				count++
			}
		}
		fmt.Printf("  %s: %d event(s) cached\n", acc.Email, count)
	}
}

func refreshOne(ctx context.Context, store *calendar.Store, provider *google.Provider, acc calendar.Account) error {
	cals, err := provider.ListCalendars(ctx, acc)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}
	store.PutCalendars(acc.ID, cals)

	if acc.Calendars == nil {
		acc.Calendars = make(map[string]calendar.CalendarPrefs)
	}
	added := false
	for _, c := range cals {
		if _, present := acc.Calendars[c.ID]; !present {
			acc.Calendars[c.ID] = calendar.CalendarPrefs{Enabled: true}
			added = true
		}
	}
	if added {
		_ = store.PutAccount(acc)
	}

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(30 * 24 * time.Hour)
	var all []calendar.Event
	for _, c := range cals {
		if prefs, ok := acc.Calendars[c.ID]; ok && !prefs.Enabled {
			continue
		}
		evs, err := provider.ListEvents(ctx, acc, c.ID, from, to)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		all = append(all, evs...)
	}
	return store.PutEvents(acc.ID, all)
}

func calToday() {
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	events := store.EventsBetween(startOfDay, endOfDay)
	if len(events) == 0 {
		fmt.Println("No events today.")
		return
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tTITLE\tLINK")
	for _, e := range events {
		when := formatEventTime(e)
		link := e.MeetingURL
		if link == "" {
			link = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", when, e.Title, link)
	}
	_ = tw.Flush()
}

func calNext() {
	store, err := calendar.OpenStore()
	if err != nil {
		fatal("open store: %v", err)
	}
	now := time.Now()
	ev, ok := store.NextEvent(now)
	if !ok {
		fmt.Println("No upcoming events in the next 7 days.")
		return
	}
	until := time.Until(ev.Start).Round(time.Minute)
	fmt.Printf("Next event: %s\n", ev.Title)
	fmt.Printf("  Starts:    %s (in %s)\n", ev.Start.Format("Mon 15:04"), until)
	if ev.MeetingURL != "" {
		fmt.Printf("  Meeting:   %s\n", ev.MeetingURL)
	}
	if ev.Location != "" {
		fmt.Printf("  Location:  %s\n", ev.Location)
	}
}

// --- helpers ---

func newProvider() *google.Provider {
	return &google.Provider{
		TokenFunc: func(ctx context.Context, account calendar.Account) (string, time.Time, error) {
			switch account.Source {
			case calendar.SourceGOA:
				return google.GOAToken(ctx, account)
			case calendar.SourceOAuth:
				return google.OAuthToken(ctx, account)
			default:
				return "", time.Time{}, fmt.Errorf("unknown account source %q", account.Source)
			}
		},
	}
}

func findAccount(store *calendar.Store, needle string) (calendar.Account, bool) {
	if acc, ok := store.AccountByID(needle); ok {
		return acc, true
	}
	for _, acc := range store.Accounts() {
		if acc.Email == needle || strings.EqualFold(acc.Email, needle) {
			return acc, true
		}
	}
	return calendar.Account{}, false
}

func formatEventTime(e calendar.Event) string {
	if e.AllDay {
		return "all-day"
	}
	end := e.End.Format("15:04")
	return fmt.Sprintf("%s–%s", e.Start.Format("15:04"), end)
}

func readChoice(max int) int {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		if !scanner.Scan() {
			fatal("input: %v", scanner.Err())
		}
		s := strings.TrimSpace(scanner.Text())
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n >= 1 && n <= max {
			return n - 1
		}
		fmt.Print("Invalid; try again: ")
	}
}
