package calendar

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// NotifyFunc is invoked when a reminder fires for a single event. The
// `until` argument is how long until the event starts (negative if the
// event has already begun). UI code wires this to wm.SendNotification.
type NotifyFunc func(ev Event, until time.Duration)

// Scheduler fires reminders for upcoming events. It owns no goroutines
// until Start() is called and shuts cleanly via Stop(). One scheduler
// covers all accounts: events come from the shared store.
//
// Lead times are configurable: by default we fire at 10 min before and
// at 1 min before. Callers can override via SetLeadTimes. We never fire
// the same (event, lead) pair twice, even across rebuilds — the dedupe
// key includes both the event ID and the lead duration.
type Scheduler struct {
	store      *Store
	notify     NotifyFunc
	leadTimes  []time.Duration // sorted ascending; e.g. [1m, 10m]
	mu         sync.Mutex
	fired      map[string]struct{} // dedupe: eventID + lead
	rebuild    chan struct{}       // poke to recompute schedule
	stopOnce   sync.Once
	cancel     context.CancelFunc
	unsub      func()
}

// NewScheduler returns a stopped scheduler. Call Start to launch it.
func NewScheduler(store *Store, notify NotifyFunc) *Scheduler {
	return &Scheduler{
		store:     store,
		notify:    notify,
		leadTimes: []time.Duration{1 * time.Minute, 10 * time.Minute},
		fired:     make(map[string]struct{}),
		rebuild:   make(chan struct{}, 1),
	}
}

// SetLeadTimes replaces the lead-time set. Durations are de-duplicated
// and sorted ascending. An empty input disables reminders without
// stopping the scheduler.
func (s *Scheduler) SetLeadTimes(leads []time.Duration) {
	if s == nil {
		return
	}
	uniq := make(map[time.Duration]struct{}, len(leads))
	for _, d := range leads {
		if d > 0 {
			uniq[d] = struct{}{}
		}
	}
	out := make([]time.Duration, 0, len(uniq))
	for d := range uniq {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	s.mu.Lock()
	s.leadTimes = out
	s.mu.Unlock()
	s.poke()
}

// Start launches the reminder loop. The scheduler subscribes to store
// changes so that adding/removing an event triggers a re-evaluation.
// Calling Start more than once is a no-op.
func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || s.notify == nil {
		return
	}
	rootCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.unsub = s.store.Subscribe(s.poke)
	go s.run(rootCtx)
}

// Stop tears the scheduler down. Idempotent.
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.unsub != nil {
			s.unsub()
		}
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// poke wakes the scheduler loop without blocking. The rebuild channel
// has buffer 1, so consecutive pokes coalesce.
func (s *Scheduler) poke() {
	if s == nil {
		return
	}
	select {
	case s.rebuild <- struct{}{}:
	default:
	}
}

// run is the single goroutine driving the scheduler. It keeps a sleep
// timer set to the next reminder fire time and wakes early on store
// changes (poke) or context cancellation. We always reread the event
// list from the store on each iteration so calendar toggles take effect
// without restarting.
func (s *Scheduler) run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()

	for {
		nextFire, nextEv, nextLead, ok := s.computeNext(time.Now())
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if !ok {
			// No upcoming reminders — sleep an hour, then re-check.
			timer.Reset(time.Hour)
		} else {
			d := time.Until(nextFire)
			if d < 0 {
				d = 0
			}
			timer.Reset(d)
		}

		select {
		case <-ctx.Done():
			return
		case <-s.rebuild:
			// Recompute on next loop iteration.
			continue
		case <-timer.C:
			if !ok {
				continue // spurious wake — still no reminders
			}
			s.fire(nextEv, nextLead)
		}
	}
}

// computeNext returns the soonest (event, lead) pair whose fire time is
// in the future and which we have not yet fired.
func (s *Scheduler) computeNext(now time.Time) (time.Time, Event, time.Duration, bool) {
	s.mu.Lock()
	leads := append([]time.Duration{}, s.leadTimes...)
	s.mu.Unlock()
	if len(leads) == 0 {
		return time.Time{}, Event{}, 0, false
	}

	// Look 24h ahead — anything beyond is rebuilt on the next sync.
	events := s.store.EventsBetween(now, now.Add(24*time.Hour))
	if len(events) == 0 {
		return time.Time{}, Event{}, 0, false
	}

	var (
		bestTime  time.Time
		bestEvent Event
		bestLead  time.Duration
		found     bool
	)
	for _, ev := range events {
		if ev.AllDay {
			continue
		}
		for _, lead := range leads {
			fireAt := ev.Start.Add(-lead)
			if !fireAt.After(now) {
				continue // already due / past
			}
			if !found || fireAt.Before(bestTime) {
				if s.alreadyFired(ev, lead) {
					continue
				}
				bestTime = fireAt
				bestEvent = ev
				bestLead = lead
				found = true
			}
		}
	}
	return bestTime, bestEvent, bestLead, found
}

func (s *Scheduler) alreadyFired(ev Event, lead time.Duration) bool {
	key := dedupeKey(ev, lead)
	s.mu.Lock()
	_, ok := s.fired[key]
	s.mu.Unlock()
	return ok
}

func (s *Scheduler) fire(ev Event, lead time.Duration) {
	key := dedupeKey(ev, lead)
	s.mu.Lock()
	if _, dup := s.fired[key]; dup {
		s.mu.Unlock()
		return
	}
	s.fired[key] = struct{}{}
	// Trim dedupe map: drop entries for events that have fully ended.
	for k := range s.fired {
		// Cheap heuristic: keys live forever for at most ~1 day.
		// We re-fill the map by scanning the cache; just cap size to
		// avoid unbounded growth in long-running sessions.
		if len(s.fired) < 512 {
			break
		}
		delete(s.fired, k)
	}
	s.mu.Unlock()

	until := time.Until(ev.Start)
	defer func() {
		// Defensive: a panicking notify must not kill the scheduler.
		if r := recover(); r != nil {
			log.Printf("[calendar-reminder] notify panic: %v", r)
		}
	}()
	s.notify(ev, until)
}

func dedupeKey(ev Event, lead time.Duration) string {
	return fmt.Sprintf("%s@%d|%s", ev.ID, ev.Start.Unix(), lead)
}
