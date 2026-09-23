package wlr

/*
#include <stdlib.h>
#include <wlr/types/wlr_seat.h>
*/
import "C"

import (
	"time"
	"unsafe"
)

// SeatCapability mirrors enum wl_seat_capability.
type SeatCapability uint32

const (
	SeatCapabilityPointer  SeatCapability = C.WL_SEAT_CAPABILITY_POINTER
	SeatCapabilityKeyboard SeatCapability = C.WL_SEAT_CAPABILITY_KEYBOARD
	SeatCapabilityTouch    SeatCapability = C.WL_SEAT_CAPABILITY_TOUCH
)

// Seat wraps struct wlr_seat.
type Seat struct {
	p *C.struct_wlr_seat
}

// CreateSeat creates a wl_seat global.
func CreateSeat(display Display, name string) Seat {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return Seat{p: C.wlr_seat_create(display.p, cs)}
}

// Ptr returns the underlying struct wlr_seat pointer.
func (s Seat) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// Destroy destroys the seat.
func (s Seat) Destroy() { C.wlr_seat_destroy(s.p) }

// SetCapabilities advertises the seat capabilities.
func (s Seat) SetCapabilities(caps SeatCapability) {
	C.wlr_seat_set_capabilities(s.p, C.uint32_t(caps))
}

// SetKeyboard makes kb the seat's active keyboard.
func (s Seat) SetKeyboard(kb Keyboard) { C.wlr_seat_set_keyboard(s.p, kb.p) }

// Keyboard returns the seat's active keyboard (may be invalid).
func (s Seat) Keyboard() Keyboard { return Keyboard{p: C.wlr_seat_get_keyboard(s.p)} }

// OnRequestSetCursor is emitted when a client sets the cursor image.
func (s Seat) OnRequestSetCursor(cb func(client SeatClient, surface Surface, serial uint32, hotspotX, hotspotY int32)) Listener {
	return newListener(&s.p.events.request_set_cursor, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_seat_pointer_request_set_cursor_event)(data)
		cb(SeatClient{p: event.seat_client}, Surface{p: event.surface},
			uint32(event.serial), int32(event.hotspot_x), int32(event.hotspot_y))
	})
}

// PointerNotifyEnter gives pointer focus to surface.
func (s Seat) PointerNotifyEnter(surface Surface, sx, sy float64) {
	C.wlr_seat_pointer_notify_enter(s.p, surface.p, C.double(sx), C.double(sy))
}

// PointerNotifyMotion sends motion to the focused surface.
func (s Seat) PointerNotifyMotion(t time.Time, sx, sy float64) {
	C.wlr_seat_pointer_notify_motion(s.p, C.uint32_t(t.UnixMilli()), C.double(sx), C.double(sy))
}

// PointerNotifyButton sends a button event and returns its serial.
func (s Seat) PointerNotifyButton(t time.Time, button CursorButton, state ButtonState) uint32 {
	return uint32(C.wlr_seat_pointer_notify_button(s.p, C.uint32_t(t.UnixMilli()),
		C.uint32_t(button), C.enum_wl_pointer_button_state(state)))
}

// PointerNotifyAxis sends a scroll event.
func (s Seat) PointerNotifyAxis(e AxisEvent) {
	C.wlr_seat_pointer_notify_axis(s.p, C.uint32_t(e.Time.UnixMilli()),
		C.enum_wl_pointer_axis(e.Orientation), C.double(e.Delta), C.int32_t(e.DeltaDiscrete),
		C.enum_wl_pointer_axis_source(e.Source),
		C.enum_wl_pointer_axis_relative_direction(e.RelativeDirection))
}

// PointerNotifyFrame ends a group of pointer events.
func (s Seat) PointerNotifyFrame() { C.wlr_seat_pointer_notify_frame(s.p) }

// PointerNotifyClearFocus removes pointer focus.
func (s Seat) PointerNotifyClearFocus() { C.wlr_seat_pointer_notify_clear_focus(s.p) }

// KeyboardNotifyEnter gives keyboard focus to surface.
func (s Seat) KeyboardNotifyEnter(surface Surface, keycodes []uint32, modifiers KeyboardModifiers) {
	var kc *C.uint32_t
	if len(keycodes) > 0 {
		kc = (*C.uint32_t)(unsafe.Pointer(&keycodes[0]))
	}
	C.wlr_seat_keyboard_notify_enter(s.p, surface.p, kc, C.size_t(len(keycodes)), modifiers.p)
}

// KeyboardNotifyModifiers sends the modifier state to the focused client.
func (s Seat) KeyboardNotifyModifiers(modifiers KeyboardModifiers) {
	C.wlr_seat_keyboard_notify_modifiers(s.p, modifiers.p)
}

// KeyboardNotifyKey sends a key event to the focused client.
func (s Seat) KeyboardNotifyKey(t time.Time, keyCode uint32, state KeyState) {
	C.wlr_seat_keyboard_notify_key(s.p, C.uint32_t(t.UnixMilli()), C.uint32_t(keyCode), C.uint32_t(state))
}

// KeyboardClearFocus removes keyboard focus.
func (s Seat) KeyboardClearFocus() { C.wlr_seat_keyboard_clear_focus(s.p) }

// KeyboardFocusedSurface returns the surface with keyboard focus.
func (s Seat) KeyboardFocusedSurface() Surface {
	return Surface{p: s.p.keyboard_state.focused_surface}
}

// PointerFocusedSurface returns the surface with pointer focus.
func (s Seat) PointerFocusedSurface() Surface {
	return Surface{p: s.p.pointer_state.focused_surface}
}

// PointerFocusedClient returns the client with pointer focus.
func (s Seat) PointerFocusedClient() SeatClient {
	return SeatClient{p: s.p.pointer_state.focused_client}
}

// SeatClient wraps struct wlr_seat_client.
type SeatClient struct {
	p *C.struct_wlr_seat_client
}

// Ptr returns the underlying struct wlr_seat_client pointer.
func (c SeatClient) Ptr() unsafe.Pointer { return unsafe.Pointer(c.p) }
