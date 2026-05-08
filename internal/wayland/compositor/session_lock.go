package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_session_lock_v1.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_output.h>

// --- Session Lock listeners ---

static struct wlr_session_lock_manager_v1 *lock_manager = NULL;
static struct wl_listener lock_mgr_new_lock_listener;
static struct wl_listener lock_mgr_destroy_listener;

// Per-lock listeners
static struct wl_listener lock_new_surface_listener;
static struct wl_listener lock_unlock_listener;
static struct wl_listener lock_destroy_listener;

// Forward declarations for Go callbacks
extern void goSessionLockNewLock(void *lock);
extern void goSessionLockNewSurface(void *lock_surface);
extern void goSessionLockUnlock(void *lock);
extern void goSessionLockDestroy(void *lock);

static void handle_lock_new_surface(struct wl_listener *listener, void *data) {
	goSessionLockNewSurface(data);
}

static void handle_lock_unlock(struct wl_listener *listener, void *data) {
	goSessionLockUnlock(data);
}

static void handle_lock_destroy(struct wl_listener *listener, void *data) {
	// Remove per-lock listeners to avoid dangling pointers after lock object is freed.
	// Without this, a second lock would try to wl_signal_add with stale link pointers.
	wl_list_remove(&lock_new_surface_listener.link);
	wl_list_remove(&lock_unlock_listener.link);
	wl_list_remove(&lock_destroy_listener.link);
	goSessionLockDestroy(data);
}

static void handle_lock_new_lock(struct wl_listener *listener, void *data) {
	struct wlr_session_lock_v1 *lock = data;

	// Wire per-lock listeners
	lock_new_surface_listener.notify = handle_lock_new_surface;
	wl_signal_add(&lock->events.new_surface, &lock_new_surface_listener);

	lock_unlock_listener.notify = handle_lock_unlock;
	wl_signal_add(&lock->events.unlock, &lock_unlock_listener);

	lock_destroy_listener.notify = handle_lock_destroy;
	wl_signal_add(&lock->events.destroy, &lock_destroy_listener);

	goSessionLockNewLock(lock);
}

static void handle_lock_mgr_destroy(struct wl_listener *listener, void *data) {
	lock_manager = NULL;
}

static void setup_session_lock(struct wl_display *display) {
	lock_manager = wlr_session_lock_manager_v1_create(display);
	lock_mgr_new_lock_listener.notify = handle_lock_new_lock;
	wl_signal_add(&lock_manager->events.new_lock, &lock_mgr_new_lock_listener);
	lock_mgr_destroy_listener.notify = handle_lock_mgr_destroy;
	wl_signal_add(&lock_manager->events.destroy, &lock_mgr_destroy_listener);
}

// cleanup_session_lock removes all manager-level listeners so that
// wlr_session_lock_manager's display-destroy handler finds empty lists.
// Must be called BEFORE wl_display_destroy().
static void cleanup_session_lock(void) {
	wl_list_remove(&lock_mgr_new_lock_listener.link);
	wl_list_remove(&lock_mgr_destroy_listener.link);
	// Also remove per-lock listeners if a lock is still active
	// (link.next != NULL means the listener was wl_signal_add'd)
	if (lock_new_surface_listener.link.next != NULL) {
		wl_list_remove(&lock_new_surface_listener.link);
		lock_new_surface_listener.link.next = NULL;
	}
	if (lock_unlock_listener.link.next != NULL) {
		wl_list_remove(&lock_unlock_listener.link);
		lock_unlock_listener.link.next = NULL;
	}
	if (lock_destroy_listener.link.next != NULL) {
		wl_list_remove(&lock_destroy_listener.link);
		lock_destroy_listener.link.next = NULL;
	}
}

static void configure_lock_surface(struct wlr_session_lock_surface_v1 *lock_surface, uint32_t w, uint32_t h) {
	wlr_session_lock_surface_v1_configure(lock_surface, w, h);
}

static void send_locked(struct wlr_session_lock_v1 *lock) {
	wlr_session_lock_v1_send_locked(lock);
}

static void destroy_lock(struct wlr_session_lock_v1 *lock) {
	wlr_session_lock_v1_destroy(lock);
}

static struct wlr_output *lock_surface_get_output(struct wlr_session_lock_surface_v1 *lock_surface) {
	return lock_surface->output;
}

static struct wlr_surface *lock_surface_get_surface(struct wlr_session_lock_surface_v1 *lock_surface) {
	return lock_surface->surface;
}

static struct wlr_scene_tree *create_lock_surface_scene(struct wlr_scene_tree *parent, struct wlr_surface *surface) {
	return wlr_scene_subsurface_tree_create(parent, surface);
}

static struct wlr_scene_rect *create_lock_black_rect(struct wlr_scene_tree *parent, int w, int h) {
	float color[4] = {0.0f, 0.0f, 0.0f, 1.0f};
	return wlr_scene_rect_create(parent, w, h, color);
}

// Heap-allocated per-lock-surface destroy listener.
// Each lock surface gets its own listener that self-cleans via wl_list_remove.
struct lock_surface_listener {
	struct wl_listener listener;
	struct wlr_session_lock_surface_v1 *lock_surface;
};

extern void goSessionLockSurfaceDestroy(void *lock_surface_ptr);

static void handle_lock_surface_destroy(struct wl_listener *listener, void *data) {
	struct lock_surface_listener *lsl = wl_container_of(listener, lsl, listener);
	wl_list_remove(&lsl->listener.link);
	goSessionLockSurfaceDestroy(lsl->lock_surface);
	free(lsl);
}

static void listen_lock_surface_destroy(struct wlr_session_lock_surface_v1 *lock_surface) {
	struct lock_surface_listener *lsl = calloc(1, sizeof(*lsl));
	lsl->lock_surface = lock_surface;
	lsl->listener.notify = handle_lock_surface_destroy;
	wl_signal_add(&lock_surface->events.destroy, &lsl->listener);
}

static void scene_node_set_enabled_c(struct wlr_scene_node *node, int enabled) {
	wlr_scene_node_set_enabled(node, enabled);
}

static void scene_node_set_position_c(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}

static void scene_node_destroy_c(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}

static void scene_rect_set_size_c(struct wlr_scene_rect *rect, int w, int h) {
	wlr_scene_rect_set_size(rect, w, h);
}

static void keyboard_clear_focus_c(struct wlr_seat *seat) {
	wlr_seat_keyboard_clear_focus(seat);
}

static void pointer_clear_focus_c(struct wlr_seat *seat) {
	wlr_seat_pointer_clear_focus(seat);
}
*/
import "C"

import (
	"log"
	"time"
	"unsafe"
)

// Package-level server pointer for //export callbacks (can't be closures)
var lockServer *server

// lockSurfaceState tracks per-output lock surface state
type lockSurfaceState struct {
	sceneTree  unsafe.Pointer // *C.struct_wlr_scene_tree
	surface    unsafe.Pointer // *C.struct_wlr_session_lock_surface_v1
	wlrSurface unsafe.Pointer // *C.struct_wlr_surface
	outputName string
	mapped     bool
}

// setupSessionLock initializes the session lock protocol
func setupSessionLock(s *server) {
	lockServer = s
	s.lockSurfaceStates = make(map[string]*lockSurfaceState)
	s.lockBlackRects = make(map[string]unsafe.Pointer)

	C.setup_session_lock(displayPtr(s.display))
	log.Println("ext-session-lock-v1 protocol registered")
}

// cleanupSessionLock removes all Wayland listeners registered by the session
// lock manager. Must be called BEFORE display.Destroy() to avoid the assertion:
//   wlr_session_lock_v1.c: Assertion 'wl_list_empty(&lock_manager->events.new_lock.listener_list)' failed
func cleanupSessionLock() {
	C.cleanup_session_lock()
}

//export goSessionLockNewLock
func goSessionLockNewLock(lock unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	lockPtr := (*C.struct_wlr_session_lock_v1)(lock)

	// Reject second lock client while already locked
	if s.locked.Load() && s.currentLock != nil {
		log.Println("[LOCK] Rejecting second lock client — already locked")
		C.destroy_lock(lockPtr)
		return
	}

	s.handleSessionLockNewLock(lockPtr)
}

//export goSessionLockNewSurface
func goSessionLockNewSurface(lockSurface unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}
	s.handleSessionLockNewSurface((*C.struct_wlr_session_lock_surface_v1)(lockSurface))
}

//export goSessionLockUnlock
func goSessionLockUnlock(lock unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}
	s.handleSessionLockUnlock()
}

//export goSessionLockDestroy
func goSessionLockDestroy(lock unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}
	s.handleSessionLockDestroy()
}

//export goSessionLockSurfaceDestroy
func goSessionLockSurfaceDestroy(lockSurfacePtr unsafe.Pointer) {
	s := lockServer
	if s == nil {
		return
	}

	lsPtr := (*C.struct_wlr_session_lock_surface_v1)(lockSurfacePtr)

	// Find and clean up the surface state
	for name, ls := range s.lockSurfaceStates {
		if ls.surface == unsafe.Pointer(lsPtr) {
			if ls.sceneTree != nil {
				C.scene_node_destroy_c(&(*C.struct_wlr_scene_tree)(ls.sceneTree).node)
			}
			delete(s.lockSurfaceStates, name)
			log.Printf("[LOCK] Lock surface destroyed for output %s\n", name)
			break
		}
	}
}

// handleSessionLockNewLock processes a new lock request from a client
func (s *server) handleSessionLockNewLock(lock *C.struct_wlr_session_lock_v1) {
	log.Println("[LOCK] New lock session requested")

	s.locked.Store(true)
	s.lockedSent = false           // Reset for each new lock session
	s.suspendLockPending = false   // Lock acquired — allow normal idle reset
	s.currentLock = unsafe.Pointer(lock)
	s.lockCrashCount = 0 // Reset crash counter on successful lock connection
	s.markLocked()

	// Enable lock layer (above everything)
	lockTree := (*C.struct_wlr_scene_tree)(s.lockTree)
	C.scene_node_set_enabled_c(&lockTree.node, 1)

	// If a previous lock client crashed, its black rects are still parented
	// under lockTree (handleSessionLockDestroy keeps them deliberately so the
	// screen stays black between attempts). Destroy them now before creating
	// the new ones, otherwise rapid lock-relaunch cycles pile orphan rects in
	// the scene graph and eventually crash inside wl_display_run.
	for _, rect := range s.lockBlackRects {
		if rect != nil {
			C.scene_node_destroy_c(&(*C.struct_wlr_scene_rect)(rect).node)
		}
	}
	s.lockBlackRects = map[string]unsafe.Pointer{}

	// Create black fallback rects for each output
	for _, out := range s.outputs {
		name := out.output.Name()
		geo := s.getOutputGeometry(out)
		rect := C.create_lock_black_rect(lockTree, C.int(geo.width), C.int(geo.height))
		C.scene_node_set_position_c(&rect.node, C.int(geo.x), C.int(geo.y))
		s.lockBlackRects[name] = unsafe.Pointer(rect)
	}

	// Cancel switcher if active
	if s.switcherActive {
		s.cancelSwitcher()
	}

	// Close overlay if open
	if s.overlayXway != nil {
		s.closeOverlay()
	}

	// Don't explicitly deactivate the active window — just redirect keyboard
	// focus to the lock surface. Calling wlr_xwayland_surface_activate(false)
	// triggers X11 FocusOut events that can destabilize XWayland clients
	// (especially Firefox) when they're later reactivated on unlock.
	// Keep s.activeXdg/s.activeXway intact so we can restore focus directly.

	// Clear keyboard focus so the lock surface can receive keyboard input
	type seatPtr struct{ p *C.struct_wlr_seat }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	C.keyboard_clear_focus_c(seatP.p)

	log.Println("[LOCK] Lock session active — black fallback rects created")
}

// handleSessionLockNewSurface processes a new lock surface for an output
func (s *server) handleSessionLockNewSurface(lockSurface *C.struct_wlr_session_lock_surface_v1) {
	cOutput := C.lock_surface_get_output(lockSurface)
	cSurface := C.lock_surface_get_surface(lockSurface)

	// Find matching output
	var matchedOut *outputState
	for _, out := range s.outputs {
		if outputPtr(out.output) == cOutput {
			matchedOut = out
			break
		}
	}

	if matchedOut == nil {
		log.Println("[LOCK] Warning: lock surface for unknown output")
		return
	}

	outName := matchedOut.output.Name()
	geo := s.getOutputGeometry(matchedOut)

	log.Printf("[LOCK] Lock surface for output %s (%dx%d)\n", outName, geo.width, geo.height)

	// Configure the lock surface with output dimensions
	C.configure_lock_surface(lockSurface, C.uint32_t(geo.width), C.uint32_t(geo.height))

	// Create scene tree for the lock surface in lockTree
	lockTree := (*C.struct_wlr_scene_tree)(s.lockTree)
	sceneTree := C.create_lock_surface_scene(lockTree, cSurface)
	C.scene_node_set_position_c(&sceneTree.node, C.int(geo.x), C.int(geo.y))

	// Track this surface state
	ls := &lockSurfaceState{
		sceneTree:  unsafe.Pointer(sceneTree),
		surface:    unsafe.Pointer(lockSurface),
		wlrSurface: unsafe.Pointer(cSurface),
		outputName: outName,
		mapped:     true, // Will render on next commit
	}
	s.lockSurfaceStates[outName] = ls

	// Listen for surface destroy
	C.listen_lock_surface_destroy(lockSurface)

	// Check if all outputs have lock surfaces — if so, send locked
	s.checkAllLockSurfacesMapped()
}

// checkAllLockSurfacesMapped checks if every output has a lock surface and sends locked if so
func (s *server) checkAllLockSurfacesMapped() {
	if s.lockedSent {
		return // Already sent locked to client
	}

	for _, out := range s.outputs {
		name := out.output.Name()
		if _, ok := s.lockSurfaceStates[name]; !ok {
			return // Not all outputs covered yet
		}
	}

	// All outputs have lock surfaces — send locked
	if s.currentLock != nil {
		lockPtr := (*C.struct_wlr_session_lock_v1)(s.currentLock)
		C.send_locked(lockPtr)
		s.lockedSent = true
		log.Println("[LOCK] All outputs covered — sent locked to client")

		// Focus the primary output's lock surface for keyboard input
		s.focusLockSurface()
	}
}

// focusLockSurface gives keyboard focus to the primary output's lock surface
func (s *server) focusLockSurface() {
	primary := s.primaryOutput()
	if primary == nil {
		log.Println("[LOCK] WARNING: No primary output — cannot focus lock surface")
		return
	}

	name := primary.output.Name()
	ls, ok := s.lockSurfaceStates[name]
	if !ok {
		// Try any lock surface
		for _, v := range s.lockSurfaceStates {
			ls = v
			break
		}
	}
	if ls == nil || ls.wlrSurface == nil {
		log.Println("[LOCK] WARNING: No lock surface available — keyboard focus not set")
		return
	}

	surface := surfaceFromCPtr((*C.struct_wlr_surface)(ls.wlrSurface))
	kb := s.seat.Keyboard()
	if !keyboardValid(kb) {
		log.Println("[LOCK] WARNING: No valid keyboard — lock surface focus not set")
		return
	}
	s.seat.KeyboardNotifyEnter(surface, kb.Keycodes(), kb.Modifiers())
	log.Printf("[LOCK] Keyboard focus set to lock surface on %s\n", ls.outputName)
}

// handleSessionLockUnlock processes client unlock request
func (s *server) handleSessionLockUnlock() {
	log.Println("[LOCK] Client requested unlock")
	s.cleanupLock()

	// Unblank display if blanked
	if s.displayBlanked {
		s.setDisplayBlanked(false)
	}
}

// maxLockCrashes is the maximum number of lock client crashes before
// force-unlocking. This prevents a permanent black screen when no locker
// can stay alive (e.g. missing GPU driver, broken Wayland socket).
const maxLockCrashes = 3

// handleSessionLockDestroy processes lock object destruction
func (s *server) handleSessionLockDestroy() {
	if s.locked.Load() {
		log.Println("[LOCK] Lock client crashed while still locked")

		// Clean up surface scene nodes.
		// IMPORTANT: Delete map entries BEFORE destroying nodes to prevent
		// re-entrant double-free. wlr_scene_node_destroy may synchronously
		// fire the surface destroy callback (goSessionLockSurfaceDestroy),
		// which would try to destroy the same node again if it finds it in the map.
		var nodesToDestroy []unsafe.Pointer
		for name, ls := range s.lockSurfaceStates {
			if ls.sceneTree != nil {
				nodesToDestroy = append(nodesToDestroy, ls.sceneTree)
			}
			delete(s.lockSurfaceStates, name)
		}
		for _, tree := range nodesToDestroy {
			C.scene_node_destroy_c(&(*C.struct_wlr_scene_tree)(tree).node)
		}
		// Keep s.locked = true, black rects remain visible
		s.currentLock = nil

		// Clear stale keyboard focus (lock surface was just destroyed)
		type seatPtr struct{ p *C.struct_wlr_seat }
		seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
		C.keyboard_clear_focus_c(seatP.p)

		s.lockCrashCount++

		// After too many crashes, fall back to built-in lock screen instead of
		// unlocking. This keeps the session secure while still allowing the user
		// to authenticate. Ctrl+Alt+Backspace remains available as emergency logout.
		if s.lockCrashCount >= maxLockCrashes {
			log.Printf("[LOCK] Lock client crashed %d times — falling back to built-in lock screen\n", s.lockCrashCount)
			// Clean up ext-session-lock black rects but keep s.locked = true
			for _, rect := range s.lockBlackRects {
				if rect != nil {
					C.scene_node_destroy_c(&(*C.struct_wlr_scene_rect)(rect).node)
				}
			}
			s.lockBlackRects = nil
			// Activate built-in lock (checks if already active, sets s.locked = true)
			s.activateBuiltinLock()
			return
		}

		// Relaunch the lock client so the user can authenticate.
		delay := time.Duration(s.lockCrashCount) * time.Second // 1s, 2s, 3s...
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		log.Printf("[LOCK] Relaunching lock client in %v (attempt %d/%d)\n", delay, s.lockCrashCount, maxLockCrashes)
		go func() {
			// Honor shutdown so we don't fight teardown. Without this, a
			// stress run that kills the lock client repeatedly produced a
			// pile of pending relaunch goroutines and eventually segfaulted
			// inside wl_display_run when several of them fired together.
			select {
			case <-s.shutdown:
				return
			case <-time.After(delay):
			}
			if s.shuttingDown.Load() {
				return
			}
			// If something else already re-locked the screen meanwhile
			// (user pressed lock again, ext-session-lock client reconnected,
			// fallback built-in lock kicked in), don't pile on a second one.
			if s.currentLock != nil || (s.builtinLock != nil && s.builtinLock.active) {
				return
			}
			s.lockScreen()
		}()
		return
	}

	// Not locked — just clean up
	s.currentLock = nil
	log.Println("[LOCK] Lock object destroyed (already unlocked)")
}

// cleanupLock restores the compositor to unlocked state
func (s *server) cleanupLock() {
	s.locked.Store(false)
	s.lockedSent = false
	s.markUnlocked()

	// Clear BOTH keyboard and pointer focus from lock surfaces BEFORE destroying
	// scene nodes. This ensures the seat doesn't reference surfaces about to be freed.
	type seatPtr struct{ p *C.struct_wlr_seat }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	C.keyboard_clear_focus_c(seatP.p)
	C.pointer_clear_focus_c(seatP.p)

	// Destroy lock surface scene nodes.
	// Delete map entries BEFORE destroying nodes to prevent re-entrant double-free
	// from goSessionLockSurfaceDestroy callback fired during scene_node_destroy.
	var surfaceNodesToDestroy []unsafe.Pointer
	for name, ls := range s.lockSurfaceStates {
		if ls.sceneTree != nil {
			surfaceNodesToDestroy = append(surfaceNodesToDestroy, ls.sceneTree)
		}
		delete(s.lockSurfaceStates, name)
	}
	for _, tree := range surfaceNodesToDestroy {
		C.scene_node_destroy_c(&(*C.struct_wlr_scene_tree)(tree).node)
	}

	// Destroy black rects
	for name, rect := range s.lockBlackRects {
		if rect != nil {
			C.scene_node_destroy_c(&(*C.struct_wlr_scene_rect)(rect).node)
		}
		delete(s.lockBlackRects, name)
	}

	// Disable lock layer
	if s.lockTree != nil {
		lockTree := (*C.struct_wlr_scene_tree)(s.lockTree)
		C.scene_node_set_enabled_c(&lockTree.node, 0)
	}

	s.currentLock = nil

	// Restore focus directly to the window that was active before lock.
	// Since we never deactivated it (no Activate(false) during lock), we just
	// need to re-enter keyboard focus. This avoids the X11 FocusOut→FocusIn
	// roundtrip that was causing Firefox to crash.
	restored := false
	if s.activeXway != nil && s.activeXway.mapped && s.activeXway.onDesk(s.currentDesk) {
		surface := s.activeXway.surface.Surface()
		if surface.Valid() {
			kb := s.seat.Keyboard()
			if keyboardValid(kb) {
				s.seat.KeyboardNotifyEnter(surface, kb.Keycodes(), kb.Modifiers())
			}
			log.Printf("[LOCK] Focus restored to xway %s (%s)\n",
				s.activeXway.id, getXwaylandSurfaceClass(s.activeXway.surface))
			restored = true
		}
	} else if s.activeXdg != nil && s.activeXdg.mapped && s.activeXdg.onDesk(s.currentDesk) {
		surface := s.activeXdg.xdgToplevel.Base().Surface()
		kb := s.seat.Keyboard()
		if keyboardValid(kb) {
			s.seat.KeyboardNotifyEnter(surface, kb.Keycodes(), kb.Modifiers())
		}
		log.Printf("[LOCK] Focus restored to xdg %s (%s)\n",
			s.activeXdg.id, getXdgToplevelAppID(s.activeXdg.xdgToplevel))
		restored = true
	}
	if !restored {
		s.focusTopmostOnDesk(s.currentDesk)
		log.Println("[LOCK] Focus restored to topmost window (no previous active)")
	}

	log.Println("[LOCK] Session unlocked — compositor restored")
}

// processLockedCursorMotion routes pointer events to the lock surface under the cursor
func (s *server) processLockedCursorMotion(t time.Time) {
	// Find which output the cursor is on
	curOut := s.getActiveOutput()
	if curOut == nil {
		return
	}

	name := curOut.output.Name()
	ls, ok := s.lockSurfaceStates[name]
	if !ok || ls.wlrSurface == nil {
		return
	}

	surface := surfaceFromCPtr((*C.struct_wlr_surface)(ls.wlrSurface))
	if !surface.Valid() {
		return
	}

	// Compute surface-local coordinates
	geo := s.getOutputGeometry(curOut)
	sx := s.cursor.X() - float64(geo.x)
	sy := s.cursor.Y() - float64(geo.y)

	s.safePointerEnter(surface, sx, sy)
	s.seat.PointerNotifyMotion(t, sx, sy)
}
