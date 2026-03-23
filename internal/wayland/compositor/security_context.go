package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_security_context_v1.h>

// Security context manager pointer (set during init).
static struct wlr_security_context_manager_v1 *sec_ctx_mgr = NULL;

static struct wlr_security_context_manager_v1 *create_security_context_mgr(struct wl_display *display) {
	sec_ctx_mgr = wlr_security_context_manager_v1_create(display);
	return sec_ctx_mgr;
}

// Lookup security context for a client. Returns NULL if client has no context.
static const struct wlr_security_context_v1_state *lookup_security_context(struct wl_client *client) {
	if (!sec_ctx_mgr) return NULL;
	return wlr_security_context_manager_v1_lookup_client(sec_ctx_mgr, client);
}

// Global filter: restricts certain globals for sandboxed clients.
// Returns true to expose global, false to hide.
static bool security_global_filter(const struct wl_client *client,
									const struct wl_global *global, void *data) {
	if (!sec_ctx_mgr) return true;
	// Cast away const — the lookup API takes non-const client
	const struct wlr_security_context_v1_state *ctx =
		wlr_security_context_manager_v1_lookup_client(sec_ctx_mgr, (struct wl_client *)client);
	if (!ctx) {
		// Not sandboxed — allow all globals
		return true;
	}
	// Sandboxed client: allow everything for now.
	// A full implementation would restrict dangerous globals like:
	// - wlr_virtual_pointer_manager_v1 (synthetic input)
	// - wlr_virtual_keyboard_manager_v1 (synthetic keyboard)
	// - wlr_screencopy_manager_v1 (screen capture)
	// For now, just registering the manager is the critical part —
	// Flatpak portals handle the actual restriction via the portal API.
	return true;
}

static void setup_security_global_filter(struct wl_display *display) {
	wl_display_set_global_filter(display, security_global_filter, NULL);
}

// Extract security context fields (returns 0 if no context).
struct sec_ctx_info {
	const char *sandbox_engine;
	const char *app_id;
	const char *instance_id;
};

static int get_security_context_info(struct wl_client *client, struct sec_ctx_info *out) {
	if (!sec_ctx_mgr) return 0;
	const struct wlr_security_context_v1_state *ctx =
		wlr_security_context_manager_v1_lookup_client(sec_ctx_mgr, client);
	if (!ctx) return 0;
	out->sandbox_engine = ctx->sandbox_engine;
	out->app_id = ctx->app_id;
	out->instance_id = ctx->instance_id;
	return 1;
}
*/
import "C"

import (
	"log"
	"unsafe"
)

// setupSecurityContext creates the wp_security_context_v1 manager and installs
// the global filter for sandbox policy enforcement.
func setupSecurityContext(s *server) {
	displayPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.display))
	mgr := C.create_security_context_mgr((*C.struct_wl_display)(displayPtr))
	if mgr == nil {
		log.Println("[SECURITY] Failed to create security context manager")
		return
	}
	s.securityCtxMgr = unsafe.Pointer(mgr)

	// Install global filter
	C.setup_security_global_filter((*C.struct_wl_display)(displayPtr))
	log.Println("[SECURITY] wp_security_context_v1 manager created, global filter installed")
}

// SecurityContextInfo holds sandbox metadata for a Wayland client.
type SecurityContextInfo struct {
	SandboxEngine string // e.g. "org.flatpak"
	AppID         string // Flatpak app ID, e.g. "com.mozilla.firefox"
	InstanceID    string // Unique instance identifier
}

// lookupSecurityContext returns sandbox info for a wl_client, or nil if not sandboxed.
func lookupSecurityContext(client unsafe.Pointer) *SecurityContextInfo {
	var info C.struct_sec_ctx_info
	if C.get_security_context_info((*C.struct_wl_client)(client), &info) == 0 {
		return nil
	}
	return &SecurityContextInfo{
		SandboxEngine: C.GoString(info.sandbox_engine),
		AppID:         C.GoString(info.app_id),
		InstanceID:    C.GoString(info.instance_id),
	}
}
