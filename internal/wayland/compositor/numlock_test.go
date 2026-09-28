package compositor

import (
	"testing"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/internal/wayland/wlr/xkb"
	"fyshos.com/tyde/wlipc"
)

func locks(kb wlr.Keyboard) (num, caps bool) {
	locked := kb.Modifiers().Locked()
	return locked&kb.NumLockMask() != 0, locked&kb.CapsLockMask() != 0
}

func TestNumLockSharedAndKept(t *testing.T) {
	ctx := xkb.NewContext(xkb.ContextNoFlags)
	km := xkb.NewKeymapFromNames(ctx, &xkb.RuleNames{Layout: "us"}, xkb.KeymapCompileNoFlags)
	ctx.Unref()
	if !km.Valid() {
		t.Skip("no XKB data available to compile a keymap")
	}
	km.Unref()

	s := &server{numLockOn: true, numLockPref: true}
	s.keyboardLayouts = []wlipc.KeyboardLayout{{Layout: "us"}, {Layout: "fr"}}
	a, b := wlr.NewStandaloneKeyboard(), wlr.NewStandaloneKeyboard()
	defer wlr.FreeStandaloneKeyboard(a)
	defer wlr.FreeStandaloneKeyboard(b)

	// Keyboards come up with Num Lock on.
	for _, kb := range []wlr.Keyboard{a, b} {
		s.applyKeyboardLayoutTo(kb)
		s.keyboards = append(s.keyboards, kb)
		if num, caps := locks(kb); !num || caps {
			t.Fatalf("new keyboard: num=%v caps=%v, want num only", num, caps)
		}
	}

	// A layout change keeps Num Lock and Caps Lock.
	a.NotifyModifiers(0, 0, a.Modifiers().Locked()|a.CapsLockMask(), 0)
	s.activeLayoutIndex = 1
	s.applyKeyboardLayoutTo(a)
	if num, caps := locks(a); !num || !caps {
		t.Fatalf("after layout change: num=%v caps=%v, want both", num, caps)
	}

	// Turning it off by hand turns it off everywhere, and it stays off.
	a.NotifyModifiers(0, 0, a.Modifiers().Locked()&^a.NumLockMask(), 0)
	s.followNumLock(a)
	if num, _ := locks(b); num || s.numLockOn {
		t.Fatal("Num Lock turned off on a should be off on b")
	}
	s.applyKeyboardLayoutTo(b)
	if num, _ := locks(b); num {
		t.Fatal("a layout change turned Num Lock back on")
	}
	// Saving another setting leaves it alone; changing the option does not.
	s.applyKeyboardLayoutPrefs(map[string]interface{}{"numlock": true})
	if num, _ := locks(a); num {
		t.Fatal("an unchanged option turned Num Lock back on")
	}
	s.applyKeyboardLayoutPrefs(map[string]interface{}{"numlock": false})
	s.applyKeyboardLayoutPrefs(map[string]interface{}{"numlock": true})
	if num, _ := locks(a); !num {
		t.Fatal("turning the option on should turn Num Lock on")
	}
	if _, caps := locks(a); !caps {
		t.Fatal("the option changed Caps Lock")
	}
}
