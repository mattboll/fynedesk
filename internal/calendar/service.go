package calendar

import (
	"context"
	"log"
	"sync"
	"time"
)

// Service is the long-lived calendar runtime that the UI consumes. One
// instance lives for the lifetime of the desktop process; UI surfaces
// reach it via the package-level Get() accessor.
type Service struct {
	store    *Store
	syncer   *Syncer
	stopSync func()

	// Reminders is wired by the reminder scheduler when it starts.
	// nil-safe: callers should check before invoking.
	reminders Reminder

	mu sync.RWMutex
}

// Reminder is implemented by the reminder scheduler. Defined here as an
// interface to avoid a hard dependency from Service onto the scheduler
// package; reminders.go satisfies it.
type Reminder interface {
	Stop()
}

// Store returns the underlying store. Always non-nil after Start.
func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}

// Refresh asks the syncer to pull events for one account immediately.
// Pass an empty string to refresh every account.
func (s *Service) Refresh(accountID string) {
	if s == nil || s.syncer == nil {
		return
	}
	if accountID == "" {
		s.syncer.RefreshAll()
		return
	}
	s.syncer.Refresh(accountID)
}

// SetReminder wires a Reminder implementation into the service so it can
// be torn down at Stop. Idempotent: replacing an existing reminder stops
// the previous one.
func (s *Service) SetReminder(r Reminder) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prev := s.reminders
	s.reminders = r
	s.mu.Unlock()
	if prev != nil {
		prev.Stop()
	}
}

// --- package-level singleton ---

var (
	svcMu     sync.RWMutex
	singleton *Service
)

// Get returns the active service, or nil if Start has not run (or
// failed). Callers should treat the result as nil-safe — every method on
// *Service tolerates a nil receiver so UI code can do:
//
//	calendar.Get().Refresh("")
//
// without guarding.
func Get() *Service {
	svcMu.RLock()
	defer svcMu.RUnlock()
	return singleton
}

// Start opens the store, launches the syncer, and installs the result as
// the package singleton. The provider is required: pass a non-nil Provider
// implementation (typically google.Provider with a TokenFunc that handles
// both GOA and OAuth accounts).
//
// Returns the started service and a cleanup func that stops sync, the
// reminder scheduler, and clears the singleton. If a service is already
// running, the existing one is returned and the new provider is ignored —
// callers should stop the old service first if they want to swap.
func Start(ctx context.Context, provider Provider, interval time.Duration) (*Service, func(), error) {
	svcMu.Lock()
	if singleton != nil {
		s := singleton
		svcMu.Unlock()
		return s, func() {}, nil
	}

	store, err := OpenStore()
	if err != nil {
		svcMu.Unlock()
		return nil, nil, err
	}
	syncer := &Syncer{
		Store:    store,
		Provider: provider,
		Interval: interval,
	}
	stopSync := syncer.Start(ctx)

	svc := &Service{
		store:    store,
		syncer:   syncer,
		stopSync: stopSync,
	}
	singleton = svc
	svcMu.Unlock()

	cleanup := func() {
		svcMu.Lock()
		s := singleton
		singleton = nil
		svcMu.Unlock()
		if s == nil {
			return
		}
		s.mu.Lock()
		r := s.reminders
		s.reminders = nil
		s.mu.Unlock()
		if r != nil {
			r.Stop()
		}
		if s.stopSync != nil {
			s.stopSync()
		}
	}
	log.Printf("[calendar] service started (interval=%s, accounts=%d)",
		syncer.Interval, len(store.Accounts()))
	return svc, cleanup, nil
}
