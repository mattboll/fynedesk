#include "restricted_globals.h"

// A handful of globals, all created at startup and living as long as the
// display: a small fixed array is enough.
#define MAX_RESTRICTED_GLOBALS 16

static const struct wl_global *restricted[MAX_RESTRICTED_GLOBALS];
static int restricted_count;

void restrict_global(const struct wl_global *global) {
	if (global && restricted_count < MAX_RESTRICTED_GLOBALS) {
		restricted[restricted_count++] = global;
	}
}

bool global_is_restricted(const struct wl_global *global) {
	for (int i = 0; i < restricted_count; i++) {
		if (restricted[i] == global) {
			return true;
		}
	}
	return false;
}
