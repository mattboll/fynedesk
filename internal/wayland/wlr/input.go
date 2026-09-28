package wlr

/*
#include <linux/input-event-codes.h>
#include <stdlib.h>
#include <wayland-server-protocol.h>
#include <wlr/interfaces/wlr_keyboard.h>
#include <wlr/types/wlr_input_device.h>
#include <wlr/types/wlr_keyboard.h>
#include <wlr/types/wlr_pointer.h>
*/
import "C"

import (
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr/xkb"
)

// InputDeviceType mirrors enum wlr_input_device_type.
type InputDeviceType uint32

const (
	InputDeviceTypeKeyboard  InputDeviceType = C.WLR_INPUT_DEVICE_KEYBOARD
	InputDeviceTypePointer   InputDeviceType = C.WLR_INPUT_DEVICE_POINTER
	InputDeviceTypeTouch     InputDeviceType = C.WLR_INPUT_DEVICE_TOUCH
	InputDeviceTypeTablet    InputDeviceType = C.WLR_INPUT_DEVICE_TABLET
	InputDeviceTypeTabletPad InputDeviceType = C.WLR_INPUT_DEVICE_TABLET_PAD
	InputDeviceTypeSwitch    InputDeviceType = C.WLR_INPUT_DEVICE_SWITCH
)

// InputDevice wraps struct wlr_input_device.
type InputDevice struct {
	p *C.struct_wlr_input_device
}

// InputDeviceFromPtr wraps a struct wlr_input_device pointer.
func InputDeviceFromPtr(p unsafe.Pointer) InputDevice {
	return InputDevice{p: (*C.struct_wlr_input_device)(p)}
}

// Ptr returns the underlying struct wlr_input_device pointer.
func (d InputDevice) Ptr() unsafe.Pointer { return unsafe.Pointer(d.p) }

// Type returns the device type.
func (d InputDevice) Type() InputDeviceType { return InputDeviceType(d.p._type) }

// Name returns the device name (may be empty).
func (d InputDevice) Name() string { return C.GoString(d.p.name) }

// Keyboard returns the keyboard behind a keyboard device.
func (d InputDevice) Keyboard() Keyboard {
	return Keyboard{p: C.wlr_keyboard_from_input_device(d.p)}
}

// Pointer returns the pointer behind a pointer device.
func (d InputDevice) Pointer() Pointer {
	return Pointer{p: C.wlr_pointer_from_input_device(d.p)}
}

// OnDestroy is emitted when the device is removed.
func (d InputDevice) OnDestroy(cb func(InputDevice)) Listener {
	return newListener(&d.p.events.destroy, func(unsafe.Pointer) { cb(d) })
}

// KeyState mirrors enum wl_keyboard_key_state.
type KeyState uint32

const (
	KeyStateReleased KeyState = C.WL_KEYBOARD_KEY_STATE_RELEASED
	KeyStatePressed  KeyState = C.WL_KEYBOARD_KEY_STATE_PRESSED
)

// KeyboardModifier is a bitmask of enum wlr_keyboard_modifier.
type KeyboardModifier uint32

const (
	KeyboardModifierShift KeyboardModifier = C.WLR_MODIFIER_SHIFT
	KeyboardModifierCaps  KeyboardModifier = C.WLR_MODIFIER_CAPS
	KeyboardModifierCtrl  KeyboardModifier = C.WLR_MODIFIER_CTRL
	KeyboardModifierAlt   KeyboardModifier = C.WLR_MODIFIER_ALT
	KeyboardModifierMod2  KeyboardModifier = C.WLR_MODIFIER_MOD2
	KeyboardModifierMod3  KeyboardModifier = C.WLR_MODIFIER_MOD3
	KeyboardModifierLogo  KeyboardModifier = C.WLR_MODIFIER_LOGO
	KeyboardModifierMod5  KeyboardModifier = C.WLR_MODIFIER_MOD5
)

// Keyboard wraps struct wlr_keyboard.
type Keyboard struct {
	p *C.struct_wlr_keyboard
}

// KeyboardFromPtr wraps a struct wlr_keyboard pointer.
func KeyboardFromPtr(p unsafe.Pointer) Keyboard {
	return Keyboard{p: (*C.struct_wlr_keyboard)(p)}
}

// Ptr returns the underlying struct wlr_keyboard pointer.
func (k Keyboard) Ptr() unsafe.Pointer { return unsafe.Pointer(k.p) }

// Valid reports whether k wraps a keyboard.
func (k Keyboard) Valid() bool { return k.p != nil }

// Base returns the keyboard's input device.
func (k Keyboard) Base() InputDevice { return InputDevice{p: &k.p.base} }

// SetKeymap compiles keymap into the keyboard.
func (k Keyboard) SetKeymap(keymap xkb.Keymap) bool {
	return bool(C.wlr_keyboard_set_keymap(k.p, (*C.struct_xkb_keymap)(keymap.Ptr())))
}

// SetRepeatInfo sets the key repeat rate (Hz) and delay (ms).
func (k Keyboard) SetRepeatInfo(rate, delay int32) {
	C.wlr_keyboard_set_repeat_info(k.p, C.int32_t(rate), C.int32_t(delay))
}

// NotifyModifiers sets the keyboard's modifier state (and so its LEDs),
// emitting the modifiers event if it changed.
func (k Keyboard) NotifyModifiers(depressed, latched, locked, group uint32) {
	C.wlr_keyboard_notify_modifiers(k.p, C.uint32_t(depressed), C.uint32_t(latched),
		C.uint32_t(locked), C.uint32_t(group))
}

// NumLockMask returns the mask of Num Lock in the keyboard's keymap, 0 if
// it has none (or no keymap).
func (k Keyboard) NumLockMask() uint32 { return k.modMask(C.XKB_MOD_NAME_NUM) }

// CapsLockMask returns the mask of Caps Lock in the keyboard's keymap, 0 if
// it has none (or no keymap).
func (k Keyboard) CapsLockMask() uint32 { return k.modMask(C.XKB_MOD_NAME_CAPS) }

func (k Keyboard) modMask(name string) uint32 {
	if k.p.keymap == nil {
		return 0
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	idx := C.xkb_keymap_mod_get_index(k.p.keymap, cname)
	if idx == C.XKB_MOD_INVALID || idx >= 32 {
		return 0
	}
	return 1 << uint32(idx)
}

// XKBState returns the keyboard's xkb state.
func (k Keyboard) XKBState() xkb.State {
	return xkb.StateFromPtr(unsafe.Pointer(k.p.xkb_state))
}

// GetModifiers returns the currently active modifiers.
func (k Keyboard) GetModifiers() KeyboardModifier {
	return KeyboardModifier(C.wlr_keyboard_get_modifiers(k.p))
}

// Modifiers returns the keyboard's serialized modifier state.
func (k Keyboard) Modifiers() KeyboardModifiers {
	return KeyboardModifiers{p: &k.p.modifiers}
}

// Keycodes returns the currently pressed keycodes. The slice aliases wlroots
// memory and is only valid until the next keyboard event.
func (k Keyboard) Keycodes() []uint32 {
	return unsafe.Slice((*uint32)(unsafe.Pointer(&k.p.keycodes[0])), int(k.p.num_keycodes))
}

// OnKey is emitted before the xkb state is updated for the key.
func (k Keyboard) OnKey(cb func(keyboard Keyboard, t time.Time, keyCode uint32, updateState bool, state KeyState)) Listener {
	return newListener(&k.p.events.key, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_keyboard_key_event)(data)
		cb(k, time.UnixMilli(int64(event.time_msec)), uint32(event.keycode),
			bool(event.update_state), KeyState(event.state))
	})
}

// OnModifiers is emitted after the modifier state changed.
func (k Keyboard) OnModifiers(cb func(Keyboard)) Listener {
	return newListener(&k.p.events.modifiers, func(unsafe.Pointer) { cb(k) })
}

// KeyboardModifiers wraps struct wlr_keyboard_modifiers.
type KeyboardModifiers struct {
	p *C.struct_wlr_keyboard_modifiers
}

// Ptr returns the underlying struct wlr_keyboard_modifiers pointer.
func (m KeyboardModifiers) Ptr() unsafe.Pointer { return unsafe.Pointer(m.p) }

// Depressed returns the mask of the modifiers held down.
func (m KeyboardModifiers) Depressed() uint32 { return uint32(m.p.depressed) }

// Latched returns the mask of the latched modifiers.
func (m KeyboardModifiers) Latched() uint32 { return uint32(m.p.latched) }

// Locked returns the mask of the locked modifiers (Caps Lock, Num Lock).
func (m KeyboardModifiers) Locked() uint32 { return uint32(m.p.locked) }

// Group returns the active layout group.
func (m KeyboardModifiers) Group() uint32 { return uint32(m.p.group) }

// CursorButton is a Linux input event button code (BTN_*).
type CursorButton uint32

const (
	BtnLeft   CursorButton = C.BTN_LEFT
	BtnRight  CursorButton = C.BTN_RIGHT
	BtnMiddle CursorButton = C.BTN_MIDDLE
)

// ButtonState mirrors enum wl_pointer_button_state.
type ButtonState uint32

const (
	ButtonReleased ButtonState = C.WL_POINTER_BUTTON_STATE_RELEASED
	ButtonPressed  ButtonState = C.WL_POINTER_BUTTON_STATE_PRESSED
)

// AxisSource mirrors enum wl_pointer_axis_source.
type AxisSource uint32

const (
	AxisSourceWheel      AxisSource = C.WL_POINTER_AXIS_SOURCE_WHEEL
	AxisSourceFinger     AxisSource = C.WL_POINTER_AXIS_SOURCE_FINGER
	AxisSourceContinuous AxisSource = C.WL_POINTER_AXIS_SOURCE_CONTINUOUS
	AxisSourceWheelTilt  AxisSource = C.WL_POINTER_AXIS_SOURCE_WHEEL_TILT
)

// AxisOrientation mirrors enum wl_pointer_axis.
type AxisOrientation uint32

const (
	AxisOrientationVertical   AxisOrientation = C.WL_POINTER_AXIS_VERTICAL_SCROLL
	AxisOrientationHorizontal AxisOrientation = C.WL_POINTER_AXIS_HORIZONTAL_SCROLL
)

// AxisRelativeDirection mirrors enum wl_pointer_axis_relative_direction.
type AxisRelativeDirection uint32

const (
	AxisRelativeDirectionIdentical AxisRelativeDirection = C.WL_POINTER_AXIS_RELATIVE_DIRECTION_IDENTICAL
	AxisRelativeDirectionInverted  AxisRelativeDirection = C.WL_POINTER_AXIS_RELATIVE_DIRECTION_INVERTED
)

// AxisEvent carries a wlr_pointer_axis_event.
type AxisEvent struct {
	Pointer           Pointer
	Time              time.Time
	Source            AxisSource
	Orientation       AxisOrientation
	RelativeDirection AxisRelativeDirection
	Delta             float64
	DeltaDiscrete     int32
}

// Pointer wraps struct wlr_pointer.
type Pointer struct {
	p *C.struct_wlr_pointer
}

// Ptr returns the underlying struct wlr_pointer pointer.
func (p Pointer) Ptr() unsafe.Pointer { return unsafe.Pointer(p.p) }

// Base returns the pointer's input device.
func (p Pointer) Base() InputDevice { return InputDevice{p: &p.p.base} }
