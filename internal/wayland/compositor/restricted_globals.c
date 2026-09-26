#include <stdlib.h>

#include "restricted_globals.h"

// The globals restricted so far, all created at startup and living as long
// as the display. The list grows as needed: a global left out of it would be
// open to sandboxed clients.
static const struct wl_global **restricted;
static int restricted_count, restricted_cap;

void restrict_global(const struct wl_global *global) {
	if (!global) {
		return;
	}
	if (restricted_count == restricted_cap) {
		int cap = restricted_cap ? restricted_cap * 2 : 16;
		const struct wl_global **grown = realloc(restricted, cap * sizeof(*grown));
		if (!grown) {
			abort(); // failing open would hand capture or typing to any sandboxed app
		}
		restricted = grown;
		restricted_cap = cap;
	}
	restricted[restricted_count++] = global;
}

bool global_is_restricted(const struct wl_global *global) {
	for (int i = 0; i < restricted_count; i++) {
		if (restricted[i] == global) {
			return true;
		}
	}
	return false;
}
