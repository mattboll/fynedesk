package compositor

/*
#include <wlr/types/wlr_output.h>

static int output_needs_frame(struct wlr_output *output) {
	return output->needs_frame;
}
*/
import "C"

import (
	"time"
	"unsafe"
)

// Package-level server pointer for //export callback (same pattern as clipServer, gestureServer, etc.)
var wakeupServer *server

//export goWakeupDrain
func goWakeupDrain() {
	if wakeupServer != nil {
		wakeupServer.lastLoopTime.Store(time.Now().UnixNano())
		wakeupServer.drainMainThreadActions()
	}
}

//export goOnFrame
func goOnFrame(output *C.struct_wlr_output) {
	if wakeupServer == nil {
		return
	}
	// Find the matching wlr.Output by comparing raw pointers
	for _, out := range wakeupServer.outputs {
		if out.output.Ptr() == unsafe.Pointer(output) {
			wakeupServer.renderOutput(out.output)
			return
		}
	}
}

//export goOnFrameAll
func goOnFrameAll() C.int {
	if wakeupServer == nil {
		return 0
	}
	// Render all outputs (frame watchdog), and report whether one still
	// needs a frame.
	still := C.int(0)
	for _, out := range wakeupServer.outputs {
		wakeupServer.renderOutput(out.output)
		if C.output_needs_frame((*C.struct_wlr_output)(out.output.Ptr())) != 0 {
			still = 1
		}
	}
	return still
}
