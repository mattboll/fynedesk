// Package xkb wraps the few libxkbcommon entry points the compositor uses.
package xkb

/*
#cgo pkg-config: xkbcommon

#include <stdlib.h>
#include <xkbcommon/xkbcommon.h>
*/
import "C"

import "unsafe"

// KeyCode is an xkb keycode (evdev keycode + 8).
type KeyCode uint32

// KeySym is an xkb keysym.
type KeySym uint32

// Keysyms referenced directly by the compositor. Everything else is resolved
// by name with SymFromName.
const (
	KeySymNoSymbol     KeySym = C.XKB_KEY_NoSymbol
	KeySymEscape       KeySym = C.XKB_KEY_Escape
	KeySymTab          KeySym = C.XKB_KEY_Tab
	KeySymISO_Left_Tab KeySym = C.XKB_KEY_ISO_Left_Tab
	KeySymReturn       KeySym = C.XKB_KEY_Return
	KeySymLeft         KeySym = C.XKB_KEY_Left
	KeySymRight        KeySym = C.XKB_KEY_Right
	KeySymw            KeySym = C.XKB_KEY_w
)

// KeySymFlags mirrors enum xkb_keysym_flags.
type KeySymFlags uint32

const (
	KeySymNoFlags         KeySymFlags = C.XKB_KEYSYM_NO_FLAGS
	KeySymCaseInsensitive KeySymFlags = C.XKB_KEYSYM_CASE_INSENSITIVE
)

// SymFromName resolves a keysym name ("Return", "XF86AudioMute", ...).
// It returns KeySymNoSymbol for unknown names.
func SymFromName(name string, flags KeySymFlags) KeySym {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return KeySym(C.xkb_keysym_from_name(cs, C.enum_xkb_keysym_flags(flags)))
}

// ContextFlags mirrors enum xkb_context_flags.
type ContextFlags uint32

const ContextNoFlags ContextFlags = C.XKB_CONTEXT_NO_FLAGS

// Context wraps struct xkb_context.
type Context struct {
	p *C.struct_xkb_context
}

// NewContext creates an xkb context.
func NewContext(flags ContextFlags) Context {
	return Context{p: C.xkb_context_new(C.enum_xkb_context_flags(flags))}
}

// Valid reports whether the context was created.
func (c Context) Valid() bool { return c.p != nil }

// Unref drops the caller's reference.
func (c Context) Unref() { C.xkb_context_unref(c.p) }

// RuleNames selects an XKB keymap (empty fields use the defaults).
type RuleNames struct {
	Rules   string
	Model   string
	Layout  string
	Variant string
	Options string
}

// KeymapCompileFlags mirrors enum xkb_keymap_compile_flags.
type KeymapCompileFlags uint32

const KeymapCompileNoFlags KeymapCompileFlags = C.XKB_KEYMAP_COMPILE_NO_FLAGS

// Keymap wraps struct xkb_keymap.
type Keymap struct {
	p *C.struct_xkb_keymap
}

// NewKeymapFromNames compiles a keymap from RMLVO names. The returned keymap
// is invalid if compilation failed.
func NewKeymapFromNames(ctx Context, names *RuleNames, flags KeymapCompileFlags) Keymap {
	var cn C.struct_xkb_rule_names
	cstr := func(s string) *C.char {
		if s == "" {
			return nil
		}
		return C.CString(s)
	}
	cn.rules = cstr(names.Rules)
	cn.model = cstr(names.Model)
	cn.layout = cstr(names.Layout)
	cn.variant = cstr(names.Variant)
	cn.options = cstr(names.Options)
	defer func() {
		for _, p := range []*C.char{cn.rules, cn.model, cn.layout, cn.variant, cn.options} {
			if p != nil {
				C.free(unsafe.Pointer(p))
			}
		}
	}()
	return Keymap{p: C.xkb_keymap_new_from_names(ctx.p, &cn, C.enum_xkb_keymap_compile_flags(flags))}
}

// Ptr returns the underlying struct xkb_keymap pointer.
func (m Keymap) Ptr() unsafe.Pointer { return unsafe.Pointer(m.p) }

// Valid reports whether the keymap was compiled.
func (m Keymap) Valid() bool { return m.p != nil }

// Unref drops the caller's reference.
func (m Keymap) Unref() { C.xkb_keymap_unref(m.p) }

// State wraps struct xkb_state.
type State struct {
	p *C.struct_xkb_state
}

// StateFromPtr wraps a struct xkb_state pointer.
func StateFromPtr(p unsafe.Pointer) State { return State{p: (*C.struct_xkb_state)(p)} }

// Syms returns the keysyms produced by keyCode in the current state.
func (s State) Syms(keyCode KeyCode) []KeySym {
	if s.p == nil {
		return nil
	}
	var syms *C.xkb_keysym_t
	n := int(C.xkb_state_key_get_syms(s.p, C.xkb_keycode_t(keyCode), &syms))
	if n <= 0 || syms == nil {
		return nil
	}
	out := make([]KeySym, n)
	for i, sym := range unsafe.Slice(syms, n) {
		out[i] = KeySym(sym)
	}
	return out
}
