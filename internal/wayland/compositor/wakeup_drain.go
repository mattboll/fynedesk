package compositor

/*
#include <wlr/types/wlr_output.h>
*/
import "C"

import "unsafe"

// Package-level server pointer for //export callback (same pattern as clipServer, gestureServer, etc.)
var wakeupServer *server

//export goWakeupDrain
func goWakeupDrain() {
	if wakeupServer != nil {
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
		outPtr := *(*unsafe.Pointer)(unsafe.Pointer(&out.output))
		if outPtr == unsafe.Pointer(output) {
			wakeupServer.renderOutput(out.output)
			return
		}
	}
}

//export goOnFrameAll
func goOnFrameAll() {
	if wakeupServer == nil {
		return
	}
	// Render all outputs (used by fallback timer in nested mode)
	for _, out := range wakeupServer.outputs {
		wakeupServer.renderOutput(out.output)
	}
}
