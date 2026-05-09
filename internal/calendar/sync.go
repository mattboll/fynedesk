package calendar

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// Syncer keeps the Store fresh by periodically pulling events from the
// upstream provider for each configured account. It runs one goroutine
// per account; the per-account loop honours context cancellation, the
// configured interval, and an explicit Refresh() request.
type Syncer struct {
	Store    *Store
	Provider Provider
	// Interval between auto-refreshes. Defaults to 5 min if zero.
	Interval time.Duration
	// Window controls how far ahead and behind we keep events cached.
	// Defaults to from=now-1h, to=now+30d if zero.
	Lookback time.Duration
	Lookahead time.Duration

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	pokes   map[string]chan struct{}
}

// Start launches per-account sync loops for every account currently in
// the store, and subscribes to store changes so new accounts start syncing
// automatically. Returns a stop function that tears everything down.
func (s *Syncer) Start(ctx context.Context) (stop func()) {
	if s.Interval == 0 {
		s.Interval = 5 * time.Minute
	}
	if s.Lookback == 0 {
		s.Lookback = time.Hour
	}
	if s.Lookahead == 0 {
		s.Lookahead = 30 * 24 * time.Hour
	}
	s.mu.Lock()
	s.cancels = make(map[string]context.CancelFunc)
	s.pokes = make(map[string]chan struct{})
	s.mu.Unlock()

	rootCtx, rootCancel := context.WithCancel(ctx)

	s.reconcile(rootCtx)
	unsub := s.Store.Subscribe(func() { s.reconcile(rootCtx) })

	return func() {
		unsub()
		rootCancel()
		s.mu.Lock()
		for _, c := range s.cancels {
			c()
		}
		s.cancels = nil
		s.pokes = nil
		s.mu.Unlock()
	}
}

// Refresh asks the per-account loop for an account to do a sync now.
// It is non-blocking; if a sync is already in progress the request is
// coalesced.
func (s *Syncer) Refresh(accountID string) {
	s.mu.Lock()
	ch, ok := s.pokes[accountID]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// RefreshAll fans out a refresh request to every running loop.
func (s *Syncer) RefreshAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.pokes))
	for id := range s.pokes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Refresh(id)
	}
}

// reconcile starts loops for new accounts and stops loops for removed ones.
func (s *Syncer) reconcile(rootCtx context.Context) {
	current := s.Store.Accounts()
	currentIDs := make(map[string]struct{}, len(current))
	for _, a := range current {
		currentIDs[a.ID] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancels == nil {
		// Stop() was called; no-op.
		return
	}

	// Cancel loops for removed accounts.
	for id, cancel := range s.cancels {
		if _, ok := currentIDs[id]; !ok {
			cancel()
			delete(s.cancels, id)
			delete(s.pokes, id)
		}
	}

	// Start loops for new accounts.
	for _, acc := range current {
		if _, running := s.cancels[acc.ID]; running {
			continue
		}
		ctx, cancel := context.WithCancel(rootCtx)
		poke := make(chan struct{}, 1)
		s.cancels[acc.ID] = cancel
		s.pokes[acc.ID] = poke
		go s.runAccount(ctx, acc.ID, poke)
	}
}

// runAccount runs the sync loop for a single account. It re-reads the
// account from the store on every tick so calendar-prefs changes (which
// don't restart the loop) take effect immediately.
func (s *Syncer) runAccount(ctx context.Context, accountID string, poke <-chan struct{}) {
	// Initial sync: once on start, then on tick or poke.
	s.syncOnce(ctx, accountID)

	timer := time.NewTimer(s.Interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poke:
			s.syncOnce(ctx, accountID)
		case <-timer.C:
			s.syncOnce(ctx, accountID)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(s.Interval)
	}
}

// syncOnce performs a single pull for one account. Failures are logged
// but do not crash the loop — transient network errors are common.
func (s *Syncer) syncOnce(ctx context.Context, accountID string) {
	acc, ok := s.Store.AccountByID(accountID)
	if !ok {
		return
	}

	cals, err := s.Provider.ListCalendars(ctx, acc)
	if err != nil {
		log.Printf("[calendar-sync] %s: list calendars: %v", acc.Email, err)
		return
	}
	s.Store.PutCalendars(accountID, cals)

	// Sync default-on for newly discovered calendars: respect existing
	// CalendarPrefs (so users can disable a calendar and the choice
	// sticks), but default Enabled=true for unseen IDs.
	if acc.Calendars == nil {
		acc.Calendars = make(map[string]CalendarPrefs)
	}
	added := false
	for _, c := range cals {
		if _, present := acc.Calendars[c.ID]; !present {
			acc.Calendars[c.ID] = CalendarPrefs{Enabled: true}
			added = true
		}
	}
	if added {
		_ = s.Store.PutAccount(acc)
	}

	from := time.Now().Add(-s.Lookback)
	to := time.Now().Add(s.Lookahead)

	var allEvents []Event
	for _, c := range cals {
		if prefs, ok := acc.Calendars[c.ID]; ok && !prefs.Enabled {
			continue
		}
		evs, err := s.Provider.ListEvents(ctx, acc, c.ID, from, to)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("[calendar-sync] %s/%s: %v", acc.Email, c.Name, err)
			continue
		}
		allEvents = append(allEvents, evs...)
	}
	if err := s.Store.PutEvents(accountID, allEvents); err != nil {
		log.Printf("[calendar-sync] %s: persist events: %v", acc.Email, err)
	}
}
