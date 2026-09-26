package compositor

import "fyshos.com/tyde/internal/wayland/wlr/xkb"

// Keysyms the key handlers compare against, resolved once: SymFromName goes
// through libxkbcommon on every call, and they ran on every key event.
var (
	symEscape    = xkb.SymFromName("Escape", xkb.KeySymNoFlags)
	symReturn    = xkb.SymFromName("Return", xkb.KeySymNoFlags)
	symKPEnter   = xkb.SymFromName("KP_Enter", xkb.KeySymNoFlags)
	symBackSpace = xkb.SymFromName("BackSpace", xkb.KeySymNoFlags)
	symSuperL    = xkb.SymFromName("Super_L", xkb.KeySymNoFlags)
	symSuperR    = xkb.SymFromName("Super_R", xkb.KeySymNoFlags)
	symAltL      = xkb.SymFromName("Alt_L", xkb.KeySymNoFlags)
	symAltR      = xkb.SymFromName("Alt_R", xkb.KeySymNoFlags)
)
