package wlr

/*
#include <stdlib.h>
#include <wlr/backend/wayland.h>
#include <wlr/backend/x11.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_output_layout.h>
*/
import "C"

import (
	"errors"
	"iter"
	"unsafe"
)

// Output wraps struct wlr_output.
type Output struct {
	p *C.struct_wlr_output
}

// OutputFromPtr wraps a struct wlr_output pointer.
func OutputFromPtr(p unsafe.Pointer) Output {
	return Output{p: (*C.struct_wlr_output)(p)}
}

// Ptr returns the underlying struct wlr_output pointer.
func (o Output) Ptr() unsafe.Pointer { return unsafe.Pointer(o.p) }

// Valid reports whether o wraps an output.
func (o Output) Valid() bool { return o.p != nil }

// Name returns the output connector name (e.g. "eDP-1").
func (o Output) Name() string { return C.GoString(o.p.name) }

// Enabled reports whether the output is currently enabled.
func (o Output) Enabled() bool { return bool(o.p.enabled) }

// Scale returns the current output scale.
func (o Output) Scale() float32 { return float32(o.p.scale) }

// Width returns the current mode width in physical pixels.
func (o Output) Width() int { return int(o.p.width) }

// Height returns the current mode height in physical pixels.
func (o Output) Height() int { return int(o.p.height) }

// Refresh returns the current refresh rate in mHz (may be 0).
func (o Output) Refresh() int { return int(o.p.refresh) }

// PhysicalSize returns the physical size in millimetres.
func (o Output) PhysicalSize() (int, int) {
	return int(o.p.phys_width), int(o.p.phys_height)
}

// CurrentMode returns the current mode (invalid for custom modes or backends
// without modes).
func (o Output) CurrentMode() OutputMode { return OutputMode{p: o.p.current_mode} }

// AdaptiveSyncSupported reports whether changing adaptive sync may succeed.
func (o Output) AdaptiveSyncSupported() bool { return bool(o.p.adaptive_sync_supported) }

// AdaptiveSyncEnabled reports whether adaptive sync is currently enabled.
func (o Output) AdaptiveSyncEnabled() bool {
	return o.p.adaptive_sync_status == C.WLR_OUTPUT_ADAPTIVE_SYNC_ENABLED
}

// EffectiveResolution returns the resolution in layout coordinates (after
// transform and scale).
func (o Output) EffectiveResolution() (int, int) {
	var w, h C.int
	C.wlr_output_effective_resolution(o.p, &w, &h)
	return int(w), int(h)
}

// InitRender makes the output use the given allocator and renderer. Must be
// called once before the first commit.
func (o Output) InitRender(a Allocator, r Renderer) bool {
	return bool(C.wlr_output_init_render(o.p, a.p, r.p))
}

// CreateGlobal advertises the output as a wl_output global.
func (o Output) CreateGlobal(display Display) {
	C.wlr_output_create_global(o.p, display.p)
}

// Modes iterates over the output's advertised modes.
func (o Output) Modes() iter.Seq[OutputMode] {
	offset := unsafe.Offsetof(C.struct_wlr_output_mode{}.link)
	return func(yield func(OutputMode) bool) {
		head := &o.p.modes
		for pos := head.next; pos != head; pos = pos.next {
			if !yield(OutputMode{p: container[C.struct_wlr_output_mode](pos, offset)}) {
				return
			}
		}
	}
}

// PreferredMode returns the preferred mode (invalid if none).
func (o Output) PreferredMode() OutputMode {
	return OutputMode{p: C.wlr_output_preferred_mode(o.p)}
}

// TestState checks whether st could be committed without applying it.
func (o Output) TestState(st *OutputState) bool {
	return bool(C.wlr_output_test_state(o.p, st.p))
}

// CommitState atomically applies st to the output.
func (o Output) CommitState(st *OutputState) bool {
	return bool(C.wlr_output_commit_state(o.p, st.p))
}

// ScheduleFrame requests a frame event.
func (o Output) ScheduleFrame() { C.wlr_output_schedule_frame(o.p) }

// SetTitle sets the window title of a nested (Wayland or X11) output.
func (o Output) SetTitle(title string) error {
	cs := C.CString(title)
	defer C.free(unsafe.Pointer(cs))
	switch {
	case bool(C.wlr_output_is_wl(o.p)):
		C.wlr_wl_output_set_title(o.p, cs)
	case bool(C.wlr_output_is_x11(o.p)):
		C.wlr_x11_output_set_title(o.p, cs)
	default:
		return errors.New("this output type cannot have a title")
	}
	return nil
}

// OnFrame is emitted when the output is ready for a new frame.
func (o Output) OnFrame(cb func(Output)) Listener {
	return newListener(&o.p.events.frame, func(unsafe.Pointer) { cb(o) })
}

// OnRequestState is emitted when the backend asks for a new state (e.g. a
// nested window was resized). The handler should commit st, possibly amended.
func (o Output) OnRequestState(cb func(o Output, st *OutputState)) Listener {
	return newListener(&o.p.events.request_state, func(data unsafe.Pointer) {
		event := (*C.struct_wlr_output_event_request_state)(data)
		cb(o, &OutputState{p: (*C.struct_wlr_output_state)(unsafe.Pointer(event.state))})
	})
}

// OnDestroy is emitted when the output is destroyed.
func (o Output) OnDestroy(cb func(Output)) Listener {
	return newListener(&o.p.events.destroy, func(unsafe.Pointer) { cb(o) })
}

// OutputState wraps struct wlr_output_state: a set of pending changes applied
// atomically by Output.CommitState.
//
// States created with NewOutputState are allocated in C memory and must be
// released with Finish.
type OutputState struct {
	p     *C.struct_wlr_output_state
	owned bool
}

// NewOutputState returns an empty state. Call Finish when done.
func NewOutputState() *OutputState {
	p := (*C.struct_wlr_output_state)(C.calloc(1, C.sizeof_struct_wlr_output_state))
	if p == nil {
		panic("wlr: out of memory allocating output state")
	}
	C.wlr_output_state_init(p)
	return &OutputState{p: p, owned: true}
}

// Finish releases the state. It is a no-op for states owned by wlroots.
func (s *OutputState) Finish() {
	if s == nil || !s.owned || s.p == nil {
		return
	}
	C.wlr_output_state_finish(s.p)
	C.free(unsafe.Pointer(s.p))
	s.p = nil
}

// Ptr returns the underlying struct wlr_output_state pointer.
func (s *OutputState) Ptr() unsafe.Pointer { return unsafe.Pointer(s.p) }

// SetEnabled enables or disables the output.
func (s *OutputState) SetEnabled(enabled bool) {
	C.wlr_output_state_set_enabled(s.p, C.bool(enabled))
}

// SetMode selects one of the output's advertised modes.
func (s *OutputState) SetMode(mode OutputMode) {
	C.wlr_output_state_set_mode(s.p, mode.p)
}

// SetCustomMode requests a mode that is not advertised (refresh in mHz,
// 0 = let the backend pick).
func (s *OutputState) SetCustomMode(width, height, refresh int) {
	C.wlr_output_state_set_custom_mode(s.p, C.int32_t(width), C.int32_t(height), C.int32_t(refresh))
}

// SetScale sets the output scale.
func (s *OutputState) SetScale(scale float32) {
	C.wlr_output_state_set_scale(s.p, C.float(scale))
}

// SetAdaptiveSyncEnabled toggles variable refresh rate.
func (s *OutputState) SetAdaptiveSyncEnabled(enabled bool) {
	C.wlr_output_state_set_adaptive_sync_enabled(s.p, C.bool(enabled))
}

// Enabled reports whether the state enables the output (only meaningful when
// the state carries an enabled field).
func (s *OutputState) Enabled() bool { return bool(s.p.enabled) }

// OutputMode wraps struct wlr_output_mode.
type OutputMode struct {
	p *C.struct_wlr_output_mode
}

// Valid reports whether m wraps a mode.
func (m OutputMode) Valid() bool { return m.p != nil }

// Width returns the mode width in pixels.
func (m OutputMode) Width() int32 { return int32(m.p.width) }

// Height returns the mode height in pixels.
func (m OutputMode) Height() int32 { return int32(m.p.height) }

// RefreshRate returns the refresh rate in mHz.
func (m OutputMode) RefreshRate() int32 { return int32(m.p.refresh) }

// Preferred reports whether the mode is the output's preferred one.
func (m OutputMode) Preferred() bool { return bool(m.p.preferred) }

// OutputLayout wraps struct wlr_output_layout.
type OutputLayout struct {
	p *C.struct_wlr_output_layout
}

// CreateOutputLayout creates an output layout.
func CreateOutputLayout(display Display) OutputLayout {
	return OutputLayout{p: C.wlr_output_layout_create(display.p)}
}

// Ptr returns the underlying struct wlr_output_layout pointer.
func (l OutputLayout) Ptr() unsafe.Pointer { return unsafe.Pointer(l.p) }

// Add places output at (lx, ly), or moves it there if already present.
func (l OutputLayout) Add(output Output, lx, ly int) OutputLayoutOutput {
	return OutputLayoutOutput{p: C.wlr_output_layout_add(l.p, output.p, C.int(lx), C.int(ly))}
}

// AddAuto places output to the right of the existing ones.
func (l OutputLayout) AddAuto(output Output) OutputLayoutOutput {
	return OutputLayoutOutput{p: C.wlr_output_layout_add_auto(l.p, output.p)}
}

// Remove removes output from the layout.
func (l OutputLayout) Remove(output Output) {
	C.wlr_output_layout_remove(l.p, output.p)
}

// Get returns the layout entry for output (invalid if absent).
func (l OutputLayout) Get(output Output) OutputLayoutOutput {
	return OutputLayoutOutput{p: C.wlr_output_layout_get(l.p, output.p)}
}

// OutputAt returns the output at layout coordinates (x, y), if any.
func (l OutputLayout) OutputAt(x, y float64) Output {
	return Output{p: C.wlr_output_layout_output_at(l.p, C.double(x), C.double(y))}
}

// OutputLayoutOutput wraps struct wlr_output_layout_output.
type OutputLayoutOutput struct {
	p *C.struct_wlr_output_layout_output
}

// Ptr returns the underlying struct wlr_output_layout_output pointer.
func (o OutputLayoutOutput) Ptr() unsafe.Pointer { return unsafe.Pointer(o.p) }

// Valid reports whether the entry exists.
func (o OutputLayoutOutput) Valid() bool { return o.p != nil }

// X returns the output's x position in the layout.
func (o OutputLayoutOutput) X() int { return int(o.p.x) }

// Y returns the output's y position in the layout.
func (o OutputLayoutOutput) Y() int { return int(o.p.y) }
