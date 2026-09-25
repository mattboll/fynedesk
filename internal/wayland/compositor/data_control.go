package compositor

/*
#include <wlr/types/wlr_data_control_v1.h>
#include <wlr/types/wlr_ext_data_control_v1.h>
#include "restricted_globals.h"

// create_data_control lets clipboard tools (wl-copy, wl-paste, clipboard
// managers) read and set the clipboard without a window of their own; the
// sandboxed clients do not get it.
static void create_data_control(struct wl_display *display) {
    struct wlr_data_control_manager_v1 *wlr = wlr_data_control_manager_v1_create(display);
    if (wlr) {
        restrict_global(wlr->global);
    }
    struct wlr_ext_data_control_manager_v1 *ext = wlr_ext_data_control_manager_v1_create(display, 1);
    if (ext) {
        restrict_global(ext->global);
    }
}
*/
import "C"

// setupDataControl offers the data-control protocols (wlr and ext). Without
// them wl-copy opens a small window to get the focus, which blinked in and
// out like a new application each time a terminal program copied text.
func setupDataControl(s *server) {
	C.create_data_control(displayPtr(s.display))
}
