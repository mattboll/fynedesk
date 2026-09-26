// Package wm provides window manager utilities including shortcut handling, positioning, borders, D-Bus operations, and notifications.
package wm

import (
	"sync"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde"
)

// shortcutEntry pairs a registered shortcut with the handler to run for it.
type shortcutEntry struct {
	shortcut *tyde.Shortcut
	handler  func()
}

// ShortcutHandler is a simple implementation for tracking registered shortcuts
type ShortcutHandler struct {
	mu    sync.RWMutex
	entry map[string]shortcutEntry
}

// TypedShortcut handle the registered shortcut
func (sh *ShortcutHandler) TypedShortcut(shortcut fyne.Shortcut) {
	sh.mu.RLock()
	entry, ok := sh.entry[shortcut.ShortcutName()]
	sh.mu.RUnlock()
	if !ok {
		return
	}

	entry.handler()
}

// AddShortcut register an handler to be executed when the shortcut action is triggered
func (sh *ShortcutHandler) AddShortcut(shortcut *tyde.Shortcut, handler func()) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.entry == nil {
		sh.entry = make(map[string]shortcutEntry)
	}
	sh.entry[shortcut.ShortcutName()] = shortcutEntry{shortcut: shortcut, handler: handler}
}

// RemoveShortcut forgets a shortcut.
func (sh *ShortcutHandler) RemoveShortcut(shortcut *tyde.Shortcut) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	delete(sh.entry, shortcut.ShortcutName())
}

// Shortcuts returns the list of all registered shortcuts
func (sh *ShortcutHandler) Shortcuts() []*tyde.Shortcut {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	shorts := make([]*tyde.Shortcut, 0, len(sh.entry))
	for _, entry := range sh.entry {
		shorts = append(shorts, entry.shortcut)
	}
	return shorts
}

// ShortcutManager is an interface that we can use to check for the handler capabilities of a desktop
type ShortcutManager interface {
	Shortcuts() []*tyde.Shortcut
	TypedShortcut(fyne.Shortcut)
}
