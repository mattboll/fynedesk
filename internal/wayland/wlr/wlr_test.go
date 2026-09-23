package wlr

import (
	"testing"

	"fyshos.com/tyde/internal/wayland/wlr/xkb"
)

func TestListenerEmitAndDestroy(t *testing.T) {
	sig := newSignal()
	defer sig.free()

	calls := 0
	lis := sig.listen(func() { calls++ })
	sig.emit()
	sig.emit()
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}

	lis.Destroy()
	if lis.Valid() {
		t.Fatal("listener still valid after Destroy")
	}
	if !sig.empty() {
		t.Fatal("signal still has listeners after Destroy")
	}
	sig.emit()
	if calls != 2 {
		t.Fatalf("destroyed listener was called (calls = %d)", calls)
	}
	lis.Destroy() // idempotent
}

// wlroots destroys objects from their destroy signal: a listener must be able
// to tear down its whole set, itself included, while the signal is emitted.
func TestListenersDestroyAllFromCallback(t *testing.T) {
	sig := newSignal()
	defer sig.free()

	var set Listeners
	first, second := 0, 0
	set.Add(sig.listen(func() {
		first++
		set.DestroyAll()
	}))
	set.Add(sig.listen(func() { second++ }))

	sig.emit()
	if first != 1 || second != 0 {
		t.Fatalf("first=%d second=%d, want 1 and 0 (second removed during emission)", first, second)
	}
	if len(set) != 0 {
		t.Fatalf("set has %d listeners after DestroyAll", len(set))
	}
	if !sig.empty() {
		t.Fatal("signal still has listeners after DestroyAll")
	}
	sig.emit()
	if first != 1 {
		t.Fatalf("listener called after DestroyAll (first = %d)", first)
	}
}

func TestListenerCopiesShareState(t *testing.T) {
	sig := newSignal()
	defer sig.free()

	lis := sig.listen(func() {})
	cp := lis
	cp.Destroy()
	if lis.Valid() {
		t.Fatal("copy of a destroyed listener still reports valid")
	}
	lis.Destroy() // must not double free
	if !sig.empty() {
		t.Fatal("signal still has listeners")
	}
}

func TestOutputStateLifecycle(t *testing.T) {
	st := NewOutputState()
	st.SetEnabled(true)
	st.SetScale(1.5)
	st.SetAdaptiveSyncEnabled(false)
	if !st.Enabled() {
		t.Fatal("state should enable the output")
	}
	st.Finish()
	st.Finish() // idempotent
	var nilState *OutputState
	nilState.Finish()
}

func TestInvalidWrappers(t *testing.T) {
	if (Surface{}).Valid() || (Output{}).Valid() || (Keyboard{}).Valid() ||
		(XDGToplevel{}).Valid() || (XwaylandSurface{}).Valid() || (Texture{}).Valid() {
		t.Fatal("zero wrappers must be invalid")
	}
	if (XCursor{}).ImageCount() != 0 {
		t.Fatal("zero XCursor must have no image")
	}
	// Setters on an unconfigured toplevel are no-ops, never a wlroots assert.
	XDGToplevel{}.SetSize(10, 10)
	XDGToplevel{}.SetActivated(true)
}

func TestEdges(t *testing.T) {
	if EdgeNone != 0 {
		t.Fatal("EdgeNone must be 0")
	}
	all := EdgeTop | EdgeBottom | EdgeLeft | EdgeRight
	for _, e := range []Edges{EdgeTop, EdgeBottom, EdgeLeft, EdgeRight} {
		if all&e == 0 || e&(all&^e) != 0 {
			t.Fatalf("edge %d is not a distinct bit", e)
		}
	}
}

func TestXKB(t *testing.T) {
	if got := xkb.SymFromName("Return", xkb.KeySymNoFlags); got != xkb.KeySymReturn {
		t.Fatalf("SymFromName(Return) = %#x, want %#x", got, xkb.KeySymReturn)
	}
	if got := xkb.SymFromName("return", xkb.KeySymCaseInsensitive); got != xkb.KeySymReturn {
		t.Fatalf("case-insensitive lookup = %#x", got)
	}
	if got := xkb.SymFromName("NotAKeysym", xkb.KeySymNoFlags); got != xkb.KeySymNoSymbol {
		t.Fatalf("unknown keysym = %#x, want NoSymbol", got)
	}

	ctx := xkb.NewContext(xkb.ContextNoFlags)
	if !ctx.Valid() {
		t.Fatal("xkb context creation failed")
	}
	defer ctx.Unref()
	km := xkb.NewKeymapFromNames(ctx, &xkb.RuleNames{Layout: "us"}, xkb.KeymapCompileNoFlags)
	if !km.Valid() {
		t.Skip("no XKB data available to compile a keymap")
	}
	km.Unref()
}
