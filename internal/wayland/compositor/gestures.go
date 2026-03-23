package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_cursor.h>
#include <wlr/types/wlr_pointer.h>
#include <wlr/types/wlr_pointer_gestures_v1.h>
#include <wlr/types/wlr_seat.h>

// --- Swipe gesture listeners (heap-allocated, self-cleaning) ---

struct swipe_listeners {
	struct wl_listener swipe_begin;
	struct wl_listener swipe_update;
	struct wl_listener swipe_end;
};

extern void goSwipeBegin(uint32_t time_msec, uint32_t fingers);
extern void goSwipeUpdate(uint32_t time_msec, uint32_t fingers, double dx, double dy);
extern void goSwipeEnd(uint32_t time_msec, int cancelled);

static void handle_swipe_begin(struct wl_listener *listener, void *data) {
	struct wlr_pointer_swipe_begin_event *event = data;
	goSwipeBegin(event->time_msec, event->fingers);
}

static void handle_swipe_update(struct wl_listener *listener, void *data) {
	struct wlr_pointer_swipe_update_event *event = data;
	goSwipeUpdate(event->time_msec, event->fingers, event->dx, event->dy);
}

static void handle_swipe_end(struct wl_listener *listener, void *data) {
	struct wlr_pointer_swipe_end_event *event = data;
	goSwipeEnd(event->time_msec, event->cancelled ? 1 : 0);
}

static struct swipe_listeners *setup_swipe_listeners(struct wlr_cursor *cursor) {
	struct swipe_listeners *sl = calloc(1, sizeof(struct swipe_listeners));
	sl->swipe_begin.notify = handle_swipe_begin;
	wl_signal_add(&cursor->events.swipe_begin, &sl->swipe_begin);
	sl->swipe_update.notify = handle_swipe_update;
	wl_signal_add(&cursor->events.swipe_update, &sl->swipe_update);
	sl->swipe_end.notify = handle_swipe_end;
	wl_signal_add(&cursor->events.swipe_end, &sl->swipe_end);
	return sl;
}

static struct wlr_pointer_gestures_v1 *create_pointer_gestures(struct wl_display *display) {
	return wlr_pointer_gestures_v1_create(display);
}

static void gestures_send_swipe_begin(struct wlr_pointer_gestures_v1 *gestures,
		struct wlr_seat *seat, uint32_t time_msec, uint32_t fingers) {
	wlr_pointer_gestures_v1_send_swipe_begin(gestures, seat, time_msec, fingers);
}

static void gestures_send_swipe_update(struct wlr_pointer_gestures_v1 *gestures,
		struct wlr_seat *seat, uint32_t time_msec, double dx, double dy) {
	wlr_pointer_gestures_v1_send_swipe_update(gestures, seat, time_msec, dx, dy);
}

static void gestures_send_swipe_end(struct wlr_pointer_gestures_v1 *gestures,
		struct wlr_seat *seat, uint32_t time_msec, int cancelled) {
	wlr_pointer_gestures_v1_send_swipe_end(gestures, seat, time_msec, cancelled);
}
*/
import "C"
import (
	"math"
	"unsafe"
)

// Gesture state
type gestureState struct {
	active  bool
	fingers uint32
	dx, dy  float64 // Accumulated delta since gesture start
}

// Global reference (only one compositor instance)
var gestureServer *server

// setupGestures initializes pointer gesture support.
func (s *server) setupGestures() {
	gestureServer = s

	// Extract underlying C pointers from Go wlr wrapper structs.
	// These structs are all { p *C.struct_... } — a single pointer field.
	displayPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.display))
	cursorPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.cursor))

	// Create pointer-gestures-unstable-v1 protocol for forwarding to clients
	s.pointerGestures = unsafe.Pointer(C.create_pointer_gestures(
		(*C.struct_wl_display)(displayPtr)))

	// Register swipe listeners on the cursor
	s.swipeListeners = unsafe.Pointer(C.setup_swipe_listeners(
		(*C.struct_wlr_cursor)(cursorPtr)))
}

// swipeThreshold is the minimum delta (pixels) to trigger an action
const swipeThreshold = 100.0

//export goSwipeBegin
func goSwipeBegin(timeMsec C.uint32_t, fingers C.uint32_t) {
	s := gestureServer
	if s == nil {
		return
	}
	s.resetIdleTimer()
	s.gesture.active = true
	s.gesture.fingers = uint32(fingers)
	s.gesture.dx = 0
	s.gesture.dy = 0

	// Forward 2-finger gestures to clients
	if uint32(fingers) == 2 && s.pointerGestures != nil {
		seatPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.seat))
		C.gestures_send_swipe_begin(
			(*C.struct_wlr_pointer_gestures_v1)(s.pointerGestures),
			(*C.struct_wlr_seat)(seatPtr),
			C.uint32_t(timeMsec), C.uint32_t(fingers))
	}
}

//export goSwipeUpdate
func goSwipeUpdate(timeMsec C.uint32_t, fingers C.uint32_t, dx, dy C.double) {
	s := gestureServer
	if s == nil || !s.gesture.active {
		return
	}
	s.gesture.dx += float64(dx)
	s.gesture.dy += float64(dy)

	// Forward 2-finger gestures to clients
	if uint32(fingers) == 2 && s.pointerGestures != nil {
		seatPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.seat))
		C.gestures_send_swipe_update(
			(*C.struct_wlr_pointer_gestures_v1)(s.pointerGestures),
			(*C.struct_wlr_seat)(seatPtr),
			C.uint32_t(timeMsec), C.double(dx), C.double(dy))
	}
}

//export goSwipeEnd
func goSwipeEnd(timeMsec C.uint32_t, cancelled C.int) {
	s := gestureServer
	if s == nil || !s.gesture.active {
		return
	}

	// Forward 2-finger gestures to clients
	if s.gesture.fingers == 2 && s.pointerGestures != nil {
		seatPtr := *(*unsafe.Pointer)(unsafe.Pointer(&s.seat))
		C.gestures_send_swipe_end(
			(*C.struct_wlr_pointer_gestures_v1)(s.pointerGestures),
			(*C.struct_wlr_seat)(seatPtr),
			C.uint32_t(timeMsec), C.int(cancelled))
	}

	if cancelled == 0 {
		s.processSwipeGesture()
	}

	s.gesture.active = false
	s.gesture.fingers = 0
	s.gesture.dx = 0
	s.gesture.dy = 0
}

// processSwipeGesture interprets the completed swipe gesture.
func (s *server) processSwipeGesture() {
	if s.locked {
		return
	}

	dx := s.gesture.dx
	dy := s.gesture.dy
	fingers := s.gesture.fingers

	absDx := math.Abs(dx)
	absDy := math.Abs(dy)

	// 3-finger swipe: desktop navigation and overview
	if fingers == 3 {
		if absDx > absDy && absDx > swipeThreshold {
			// Horizontal swipe: switch desktop
			if dx < 0 {
				s.switchDesk(s.currentDesk + 1) // Swipe left → next desktop
			} else {
				s.switchDesk(s.currentDesk - 1) // Swipe right → prev desktop
			}
		} else if absDy > absDx && absDy > swipeThreshold {
			if dy < 0 {
				// Swipe up: window overview
				if !s.overviewActive {
					s.openOverview()
				}
			} else {
				// Swipe down: close overview (if open)
				if s.overviewActive {
					s.closeOverview()
				}
			}
		}
	}

	// 4-finger swipe: show/hide launcher
	if fingers == 4 {
		if absDy > absDx && absDy > swipeThreshold {
			if dy < 0 {
				// Swipe up: show app launcher
				s.requestLauncher()
			}
		}
	}
}
