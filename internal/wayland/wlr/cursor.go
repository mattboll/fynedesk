package wlr

/*
#include <stdlib.h>
#include <wlr/types/wlr_cursor.h>
#include <wlr/types/wlr_pointer.h>
#include <wlr/types/wlr_xcursor_manager.h>
#include <wlr/xcursor.h>
*/
import "C"

import (
	"time"
	"unsafe"
)

// Cursor wraps struct wlr_cursor.
type Cursor struct {
	p *C.struct_wlr_cursor
}

// CreateCursor creates a cursor.
func CreateCursor() Cursor { return Cursor{p: C.wlr_cursor_create()} }

// Ptr returns the underlying struct wlr_cursor pointer.
func (c Cursor) Ptr() unsafe.Pointer { return unsafe.Pointer(c.p) }

// Destroy destroys the cursor.
func (c Cursor) Destroy() { C.wlr_cursor_destroy(c.p) }

// X returns the cursor x position in layout coordinates.
func (c Cursor) X() float64 { return float64(c.p.x) }

// Y returns the cursor y position in layout coordinates.
func (c Cursor) Y() float64 { return float64(c.p.y) }

// AttachOutputLayout constrains the cursor to layout.
func (c Cursor) AttachOutputLayout(layout OutputLayout) {
	C.wlr_cursor_attach_output_layout(c.p, layout.p)
}

// AttachInputDevice routes dev's pointer events through the cursor.
func (c Cursor) AttachInputDevice(dev InputDevice) {
	C.wlr_cursor_attach_input_device(c.p, dev.p)
}

// Move moves the cursor by (dx, dy), honouring dev's constraints.
func (c Cursor) Move(dev InputDevice, dx, dy float64) {
	C.wlr_cursor_move(c.p, dev.p, C.double(dx), C.double(dy))
}

// WarpAbsolute moves the cursor to normalized [0,1] coordinates.
func (c Cursor) WarpAbsolute(dev InputDevice, x, y float64) {
	C.wlr_cursor_warp_absolute(c.p, dev.p, C.double(x), C.double(y))
}

// SetSurface uses a client surface as the cursor image.
func (c Cursor) SetSurface(surface Surface, hotspotX, hotspotY int32) {
	C.wlr_cursor_set_surface(c.p, surface.p, C.int32_t(hotspotX), C.int32_t(hotspotY))
}

// SetXCursor uses a named cursor from the xcursor theme.
func (c Cursor) SetXCursor(m XCursorManager, name string) {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	C.wlr_cursor_set_xcursor(c.p, m.p, cs)
}

// OnMotion is emitted for relative pointer motion.
func (c Cursor) OnMotion(cb func(p Pointer, t time.Time, dx, dy float64)) Listener {
	return newListener(&c.p.events.motion, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_pointer_motion_event)(data)
		cb(Pointer{p: event.pointer}, time.UnixMilli(int64(event.time_msec)),
			float64(event.delta_x), float64(event.delta_y))
	})
}

// OnMotionAbsolute is emitted for absolute pointer motion (x, y in [0,1]).
func (c Cursor) OnMotionAbsolute(cb func(p Pointer, t time.Time, x, y float64)) Listener {
	return newListener(&c.p.events.motion_absolute, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_pointer_motion_absolute_event)(data)
		cb(Pointer{p: event.pointer}, time.UnixMilli(int64(event.time_msec)),
			float64(event.x), float64(event.y))
	})
}

// OnButton is emitted for pointer button presses and releases.
func (c Cursor) OnButton(cb func(p Pointer, t time.Time, button CursorButton, state ButtonState)) Listener {
	return newListener(&c.p.events.button, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_pointer_button_event)(data)
		cb(Pointer{p: event.pointer}, time.UnixMilli(int64(event.time_msec)),
			CursorButton(event.button), ButtonState(event.state))
	})
}

// OnAxis is emitted for scroll events.
func (c Cursor) OnAxis(cb func(AxisEvent)) Listener {
	return newListener(&c.p.events.axis, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_pointer_axis_event)(data)
		cb(AxisEvent{
			Pointer:           Pointer{p: event.pointer},
			Time:              time.UnixMilli(int64(event.time_msec)),
			Source:            AxisSource(event.source),
			Orientation:       AxisOrientation(event.orientation),
			RelativeDirection: AxisRelativeDirection(event.relative_direction),
			Delta:             float64(event.delta),
			DeltaDiscrete:     int32(event.delta_discrete),
		})
	})
}

// OnFrame is emitted after a group of pointer events.
func (c Cursor) OnFrame(cb func()) Listener {
	return newListener(&c.p.events.frame, func(unsafe.Pointer) { cb() })
}

// XCursorManager wraps struct wlr_xcursor_manager.
type XCursorManager struct {
	p *C.struct_wlr_xcursor_manager
}

// CreateXCursorManager creates a manager for the named theme ("" = default).
func CreateXCursorManager(name string, size uint32) XCursorManager {
	var cs *C.char
	if name != "" {
		cs = C.CString(name)
		defer C.free(unsafe.Pointer(cs))
	}
	return XCursorManager{p: C.wlr_xcursor_manager_create(cs, C.uint32_t(size))}
}

// Ptr returns the underlying struct wlr_xcursor_manager pointer.
func (m XCursorManager) Ptr() unsafe.Pointer { return unsafe.Pointer(m.p) }

// Destroy destroys the manager.
func (m XCursorManager) Destroy() { C.wlr_xcursor_manager_destroy(m.p) }

// Load ensures the theme is loaded at the given scale.
func (m XCursorManager) Load(scale float64) bool {
	return bool(C.wlr_xcursor_manager_load(m.p, C.float(scale)))
}

// GetXCursor returns the named cursor at the given scale (invalid if absent).
func (m XCursorManager) GetXCursor(name string, scale float32) XCursor {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return XCursor{p: C.wlr_xcursor_manager_get_xcursor(m.p, cs, C.float(scale))}
}

// XCursor wraps struct wlr_xcursor.
type XCursor struct {
	p *C.struct_wlr_xcursor
}

// Valid reports whether the cursor exists.
func (c XCursor) Valid() bool { return c.p != nil }

// ImageCount returns the number of animation frames.
func (c XCursor) ImageCount() int {
	if c.p == nil {
		return 0
	}
	return int(c.p.image_count)
}

// Image returns frame i.
func (c XCursor) Image(i int) XCursorImage {
	images := unsafe.Slice(c.p.images, c.ImageCount())
	return XCursorImage{p: images[i]}
}

// XCursorImage wraps struct wlr_xcursor_image.
type XCursorImage struct {
	p *C.struct_wlr_xcursor_image
}

// Width returns the image width in pixels.
func (i XCursorImage) Width() int { return int(i.p.width) }

// Height returns the image height in pixels.
func (i XCursorImage) Height() int { return int(i.p.height) }

// Hotspot returns the image hotspot.
func (i XCursorImage) Hotspot() (int, int) { return int(i.p.hotspot_x), int(i.p.hotspot_y) }
