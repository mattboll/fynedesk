// Package notes provides a quick notes / todo list module for the widget panel.
package notes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Note is a single note with a title and body.
// Lines starting with "[ ] " or "[x] " are rendered as checkboxes in the UI.
type Note struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Store manages note persistence to ~/.config/fynedesk/notes.json.
type Store struct {
	mu       sync.Mutex
	notes    []Note
	path     string
	saveTimer *time.Timer
}

// NewStore loads (or creates) the notes store.
func NewStore() *Store {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".config", "fynedesk")
	os.MkdirAll(dir, 0755)

	s := &Store{
		path: filepath.Join(dir, "notes.json"),
	}
	s.load()

	// Create a default note if empty
	if len(s.notes) == 0 {
		s.notes = []Note{{
			ID:        genID(),
			Title:     "Notes",
			Body:      "",
			CreatedAt: time.Now().UnixMilli(),
			UpdatedAt: time.Now().UnixMilli(),
		}}
		s.saveNow()
	}
	return s
}

// Notes returns a copy of all notes.
func (s *Store) Notes() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Note, len(s.notes))
	copy(out, s.notes)
	return out
}

// Get returns a note by ID.
func (s *Store) Get(id string) (Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.notes {
		if n.ID == id {
			return n, true
		}
	}
	return Note{}, false
}

// Update modifies an existing note's body (and optionally title) and schedules a save.
func (s *Store) Update(id, title, body string) {
	s.mu.Lock()
	for i, n := range s.notes {
		if n.ID == id {
			s.notes[i].Body = body
			if title != "" {
				s.notes[i].Title = title
			}
			s.notes[i].UpdatedAt = time.Now().UnixMilli()
			break
		}
	}
	s.mu.Unlock()
	s.scheduleSave()
}

// Add creates a new note and returns it.
func (s *Store) Add(title string) Note {
	n := Note{
		ID:        genID(),
		Title:     title,
		Body:      "",
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}
	s.mu.Lock()
	s.notes = append(s.notes, n)
	s.mu.Unlock()
	s.scheduleSave()
	return n
}

// Delete removes a note by ID.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	for i, n := range s.notes {
		if n.ID == id {
			s.notes = append(s.notes[:i], s.notes[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.scheduleSave()
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // file doesn't exist yet
	}
	var notes []Note
	if err := json.Unmarshal(data, &notes); err != nil {
		log.Printf("[notes] failed to parse %s: %v", s.path, err)
		return
	}
	s.notes = notes
}

func (s *Store) scheduleSave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	s.saveTimer = time.AfterFunc(500*time.Millisecond, func() {
		s.saveNow()
	})
}

func (s *Store) saveNow() {
	s.mu.Lock()
	data, err := json.MarshalIndent(s.notes, "", "  ")
	s.mu.Unlock()
	if err != nil {
		log.Printf("[notes] failed to marshal: %v", err)
		return
	}
	// Atomic write: temp file + rename
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("[notes] failed to write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("[notes] failed to rename %s: %v", s.path, err)
	}
}

// Flush forces an immediate save (call on module destroy).
func (s *Store) Flush() {
	s.mu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
		s.saveTimer = nil
	}
	s.mu.Unlock()
	s.saveNow()
}

func genID() string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return time.Now().Format("20060102150405.000") + "-" + hex.EncodeToString(suffix[:])
}
