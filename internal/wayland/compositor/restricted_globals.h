// Globals hidden from sandboxed (Flatpak…) clients: they would let an app
// capture the screen or other windows, or type into them, without going
// through the portal that asks the user.
#ifndef TYDE_RESTRICTED_GLOBALS_H
#define TYDE_RESTRICTED_GLOBALS_H

#include <stdbool.h>
#include <wayland-server-core.h>

// restrict_global marks a global as unavailable to sandboxed clients.
void restrict_global(const struct wl_global *global);

// global_is_restricted reports whether restrict_global was called for global.
bool global_is_restricted(const struct wl_global *global);

#endif
