package xkb

import (
	"slices"
	"testing"
)

func TestLevelSyms(t *testing.T) {
	ctx := NewContext(ContextNoFlags)
	if !ctx.Valid() {
		t.Skip("no xkb context")
	}
	defer ctx.Unref()
	one := SymFromName("1", KeySymNoFlags)
	const key1 = KeyCode(10) // <AE01>, the "1" key
	for layout, other := range map[string]string{"us": "exclam", "fr": "ampersand"} {
		keymap := NewKeymapFromNames(ctx, &RuleNames{Layout: layout}, KeymapCompileNoFlags)
		if !keymap.Valid() {
			t.Skipf("no %s keymap", layout)
		}
		state := NewState(keymap)
		syms := state.LevelSyms(key1)
		if !slices.Contains(syms, one) || !slices.Contains(syms, SymFromName(other, KeySymNoFlags)) {
			t.Errorf("%s: LevelSyms of the 1 key = %v, want 1 and %s", layout, syms, other)
		}
		state.Unref()
		keymap.Unref()
	}
}
