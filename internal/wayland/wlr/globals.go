package wlr

/*
#include <wlr/types/wlr_compositor.h>
#include <wlr/types/wlr_subcompositor.h>
#include <wlr/types/wlr_data_device.h>
#include <wlr/types/wlr_primary_selection_v1.h>
#include <wlr/types/wlr_screencopy_v1.h>
#include <wlr/types/wlr_xdg_output_v1.h>
*/
import "C"

import "unsafe"

// Compositor wraps struct wlr_compositor (wl_compositor global).
type Compositor struct {
	p *C.struct_wlr_compositor
}

// CreateCompositor creates the wl_compositor global.
func CreateCompositor(display Display, version uint32, renderer Renderer) Compositor {
	return Compositor{p: C.wlr_compositor_create(display.p, C.uint32_t(version), renderer.p)}
}

// Ptr returns the underlying struct wlr_compositor pointer.
func (c Compositor) Ptr() unsafe.Pointer { return unsafe.Pointer(c.p) }

// CreateSubcompositor creates the wl_subcompositor global.
func CreateSubcompositor(display Display) {
	C.wlr_subcompositor_create(display.p)
}

// DataDeviceManager wraps struct wlr_data_device_manager.
type DataDeviceManager struct {
	p *C.struct_wlr_data_device_manager
}

// CreateDataDeviceManager creates the wl_data_device_manager global.
func CreateDataDeviceManager(display Display) DataDeviceManager {
	return DataDeviceManager{p: C.wlr_data_device_manager_create(display.p)}
}

// CreatePrimarySelectionV1DeviceManager creates the
// zwp_primary_selection_device_manager_v1 global.
func CreatePrimarySelectionV1DeviceManager(display Display) {
	C.wlr_primary_selection_v1_device_manager_create(display.p)
}

// ScreencopyManagerV1 wraps struct wlr_screencopy_manager_v1.
type ScreencopyManagerV1 struct {
	p *C.struct_wlr_screencopy_manager_v1
}

// CreateScreencopyManagerV1 creates the zwlr_screencopy_manager_v1 global.
func CreateScreencopyManagerV1(display Display) ScreencopyManagerV1 {
	return ScreencopyManagerV1{p: C.wlr_screencopy_manager_v1_create(display.p)}
}

// Ptr returns the underlying struct wlr_screencopy_manager_v1 pointer.
func (m ScreencopyManagerV1) Ptr() unsafe.Pointer { return unsafe.Pointer(m.p) }

// CreateXDGOutputManagerV1 creates the zxdg_output_manager_v1 global.
func CreateXDGOutputManagerV1(display Display, layout OutputLayout) {
	C.wlr_xdg_output_manager_v1_create(display.p, layout.p)
}
