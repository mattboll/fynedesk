package calendar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// Store persists the list of configured accounts and per-account event
// caches. It is safe for concurrent use; readers see a consistent snapshot
// because all writes go through atomic temp+rename.
type Store struct {
	mu       sync.RWMutex
	dir      string                  // <config>/calendar
	accounts []Account               // sorted by Provider, then Email
	cache    map[string][]Event      // accountID → events sorted by Start
	cals     map[string][]Calendar   // accountID → calendars
	listeners map[int]func()         // change subscribers (UI repaint, …)
	nextID   int
}

// OpenStore loads the persisted state from <config>/calendar/ and returns a
// ready-to-use Store. The directory is created if missing.
func OpenStore() (*Store, error) {
	dir := filepath.Join(wlipc.ConfigDir(), "calendar")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("calendar store mkdir: %w", err)
	}
	s := &Store{
		dir:       dir,
		cache:     make(map[string][]Event),
		cals:      make(map[string][]Calendar),
		listeners: make(map[int]func()),
	}
	if err := s.loadAccountsLocked(); err != nil {
		return nil, err
	}
	for _, acc := range s.accounts {
		_ = s.loadCacheLocked(acc.ID) // best effort; missing cache is fine
	}
	return s, nil
}

// Accounts returns a copy of the configured accounts.
func (s *Store) Accounts() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, len(s.accounts))
	copy(out, s.accounts)
	return out
}

// AccountByID returns the account with the given ID, or false.
func (s *Store) AccountByID(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

// PutAccount inserts or replaces an account by ID.
func (s *Store) PutAccount(acc Account) error {
	s.mu.Lock()
	replaced := false
	for i := range s.accounts {
		if s.accounts[i].ID == acc.ID {
			s.accounts[i] = acc
			replaced = true
			break
		}
	}
	if !replaced {
		s.accounts = append(s.accounts, acc)
	}
	s.sortAccountsLocked()
	err := s.persistAccountsLocked()
	listeners := s.listenerSnapshotLocked()
	s.mu.Unlock()
	notify(listeners)
	return err
}

// RemoveAccount drops an account and its cached events.
func (s *Store) RemoveAccount(id string) error {
	s.mu.Lock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			break
		}
	}
	delete(s.cache, id)
	delete(s.cals, id)
	_ = os.Remove(filepath.Join(s.dir, cacheFilename(id)))
	err := s.persistAccountsLocked()
	listeners := s.listenerSnapshotLocked()
	s.mu.Unlock()
	notify(listeners)
	return err
}

// PutEvents replaces the cached events for an account and persists.
// The slice is sorted in place by Start ascending.
func (s *Store) PutEvents(accountID string, events []Event) error {
	sort.Slice(events, func(i, j int) bool { return events[i].Start.Before(events[j].Start) })
	s.mu.Lock()
	s.cache[accountID] = events
	err := s.persistCacheLocked(accountID)
	listeners := s.listenerSnapshotLocked()
	s.mu.Unlock()
	notify(listeners)
	return err
}

// PutCalendars replaces the calendar list for an account.
func (s *Store) PutCalendars(accountID string, cals []Calendar) {
	s.mu.Lock()
	s.cals[accountID] = cals
	listeners := s.listenerSnapshotLocked()
	s.mu.Unlock()
	notify(listeners)
}

// CalendarsFor returns the cached calendar list for an account.
func (s *Store) CalendarsFor(accountID string) []Calendar {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cals := s.cals[accountID]
	out := make([]Calendar, len(cals))
	copy(out, cals)
	return out
}

// EventsBetween returns events from all enabled calendars across all
// accounts whose Start is in [from, to), sorted ascending.
func (s *Store) EventsBetween(from, to time.Time) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Event
	for _, acc := range s.accounts {
		for _, ev := range s.cache[acc.ID] {
			if ev.End.Before(from) || !ev.Start.Before(to) {
				continue
			}
			if prefs, ok := acc.Calendars[ev.CalendarID]; ok && !prefs.Enabled {
				continue
			}
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// NextEvent returns the next event starting at or after t across all
// enabled calendars, and reports whether one was found.
func (s *Store) NextEvent(t time.Time) (Event, bool) {
	upcoming := s.EventsBetween(t, t.Add(7*24*time.Hour))
	for _, ev := range upcoming {
		if ev.Start.Before(t) {
			continue
		}
		return ev, true
	}
	return Event{}, false
}

// Subscribe registers a callback fired (without holding the store lock)
// whenever accounts or events change. Returns an unsubscribe function.
func (s *Store) Subscribe(fn func()) func() {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// --- internal ---

func (s *Store) listenerSnapshotLocked() []func() {
	out := make([]func(), 0, len(s.listeners))
	for _, fn := range s.listeners {
		out = append(out, fn)
	}
	return out
}

func notify(listeners []func()) {
	for _, fn := range listeners {
		fn()
	}
}

func (s *Store) sortAccountsLocked() {
	sort.Slice(s.accounts, func(i, j int) bool {
		if s.accounts[i].Provider != s.accounts[j].Provider {
			return s.accounts[i].Provider < s.accounts[j].Provider
		}
		return strings.ToLower(s.accounts[i].Email) < strings.ToLower(s.accounts[j].Email)
	})
}

func (s *Store) loadAccountsLocked() error {
	path := filepath.Join(s.dir, "accounts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read accounts: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var accs []Account
	if err := json.Unmarshal(data, &accs); err != nil {
		return fmt.Errorf("parse accounts: %w", err)
	}
	s.accounts = accs
	s.sortAccountsLocked()
	return nil
}

func (s *Store) persistAccountsLocked() error {
	path := filepath.Join(s.dir, "accounts.json")
	data, err := json.MarshalIndent(s.accounts, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

func (s *Store) loadCacheLocked(accountID string) error {
	path := filepath.Join(s.dir, cacheFilename(accountID))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		return err
	}
	s.cache[accountID] = events
	return nil
}

func (s *Store) persistCacheLocked(accountID string) error {
	path := filepath.Join(s.dir, cacheFilename(accountID))
	data, err := json.MarshalIndent(s.cache[accountID], "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o600)
}

// cacheFilename derives a safe filename from an account ID. GOA IDs contain
// '/' so we replace path separators.
func cacheFilename(accountID string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(accountID)
	return "cache_" + safe + ".json"
}

// atomicWrite writes data to path atomically (temp + rename) with the given
// mode. Refuses to overwrite a symlink — the parent directory should be 0700
// but defense in depth costs nothing.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink: %s", path)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cal-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
