package calendar

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeProvider serves two calendars; the second fails when told to.
type fakeProvider struct {
	mu       sync.Mutex
	fail     bool
	onEvents func() // called while listing events: the user acts meanwhile
}

func (p *fakeProvider) Name() string                                    { return "fake" }
func (p *fakeProvider) ListAccounts(context.Context) ([]Account, error) { return nil, nil }

func (p *fakeProvider) ListCalendars(context.Context, Account) ([]Calendar, error) {
	return []Calendar{{ID: "work"}, {ID: "home"}}, nil
}

func (p *fakeProvider) ListEvents(_ context.Context, _ Account, cal string, _, _ time.Time) ([]Event, error) {
	p.mu.Lock()
	fail, hook := p.fail, p.onEvents
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	if cal == "home" && fail {
		return nil, errors.New("503")
	}
	return []Event{{ID: cal + "-1", CalendarID: cal, Start: time.Now().Add(time.Hour)}}, nil
}

func testStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutAccount(Account{ID: "acc", Email: "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSyncKeepsEventsOfAFailingCalendar(t *testing.T) {
	s := testStore(t)
	p := &fakeProvider{}
	sy := &Syncer{Store: s, Provider: p, Lookahead: 24 * time.Hour}
	sy.syncOnce(context.Background(), "acc")
	if n := len(s.cachedEvents("acc")); n != 2 {
		t.Fatalf("first sync: %d events", n)
	}
	p.fail = true
	sy.syncOnce(context.Background(), "acc")
	if n := len(s.cachedEvents("acc")); n != 2 {
		t.Errorf("a failing calendar lost its events: %d left", n)
	}
}

func TestSyncKeepsAChoiceMadeMeanwhile(t *testing.T) {
	s := testStore(t)
	p := &fakeProvider{}
	sy := &Syncer{Store: s, Provider: p, Lookahead: 24 * time.Hour}
	sy.syncOnce(context.Background(), "acc") // discovers both calendars
	var once sync.Once
	p.onEvents = func() {
		once.Do(func() { // the user hides "home" during the next sync
			acc, _ := s.AccountByID("acc")
			acc.Calendars["home"] = CalendarPrefs{Enabled: false}
			_ = s.PutAccount(acc)
		})
	}
	sy.syncOnce(context.Background(), "acc")
	acc, _ := s.AccountByID("acc")
	if acc.Calendars["home"].Enabled {
		t.Error("the sync undid the choice made meanwhile")
	}
}

func TestAccountCopiesDoNotShareMaps(t *testing.T) {
	s := testStore(t)
	s.AddCalendars("acc", []string{"work"})
	acc, _ := s.AccountByID("acc")
	acc.Calendars["work"] = CalendarPrefs{Enabled: false}
	again, _ := s.AccountByID("acc")
	if !again.Calendars["work"].Enabled {
		t.Error("changing a copy changed the store")
	}
}
