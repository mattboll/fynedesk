package wm

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyshos.com/tyde"
	"github.com/stretchr/testify/assert"
)

func TestShortcutHandler_Shortcuts(t *testing.T) {
	m := &ShortcutHandler{}
	assert.Equal(t, 0, len(m.Shortcuts()))

	m.AddShortcut(tyde.NewShortcut("Hint", fyne.KeyB, fyne.KeyModifierSuper), func() {})
	assert.Equal(t, 1, len(m.Shortcuts()))
}

// Module instances are rebuilt on every settings change and re-register their
// shortcuts, so a repeat registration must replace the old handler - not leave
// the previous (destroyed) instance's closure to be picked instead.
func TestShortcutHandler_ReRegister(t *testing.T) {
	m := &ShortcutHandler{}
	stale := false
	m.AddShortcut(tyde.NewShortcut("Hint", fyne.KeyH, fyne.KeyModifierSuper), func() {
		stale = true
	})

	fresh := false
	key := tyde.NewShortcut("Hint", fyne.KeyH, fyne.KeyModifierSuper)
	m.AddShortcut(key, func() {
		fresh = true
	})

	assert.Equal(t, 1, len(m.Shortcuts()))
	m.TypedShortcut(key)
	assert.True(t, fresh)
	assert.False(t, stale)
}

func TestShortcutHandler_TypedShortcut(t *testing.T) {
	m := &ShortcutHandler{}
	called := false
	key := tyde.NewShortcut("Hint", fyne.KeyH, fyne.KeyModifierSuper)
	m.AddShortcut(key, func() {
		called = true
	})
	m.TypedShortcut(key)
	assert.True(t, called)
}

func TestRemoveShortcut(t *testing.T) {
	sh := &ShortcutHandler{}
	s := &tyde.Shortcut{Name: "Test", KeyName: fyne.KeyA, Modifier: fyne.KeyModifierSuper}
	called := false
	sh.AddShortcut(s, func() { called = true })
	sh.RemoveShortcut(s)
	sh.TypedShortcut(s)
	if called || len(sh.Shortcuts()) != 0 {
		t.Error("a removed shortcut still runs")
	}
}
