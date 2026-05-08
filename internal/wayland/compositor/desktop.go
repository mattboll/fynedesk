package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#cgo LDFLAGS: -lEGL
#include <time.h>
#include <unistd.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_screencopy_v1.h>
#include <EGL/egl.h>

// Defined in main.go's CGO preamble (non-static globals)
extern EGLDisplay g_egl_display;
extern EGLContext g_egl_context;

// Go callback for draining mainThreadActions from the event loop thread.
extern void goWakeupDrain();

// Screencopy manager pointer, defined in main.go's CGO preamble.
extern struct wlr_screencopy_manager_v1 *g_screencopy_mgr;

// Wakeup event source: reads from eventfd, drains pending Go actions, and
// schedules a frame on the output. The direct goWakeupDrain() call ensures
// actions are processed even when the backend doesn't deliver frame callbacks
// (e.g. nested Wayland compositor with idle window).
static int wakeup_handler(int fd, uint32_t mask, void *data) {
    uint64_t val;
    read(fd, &val, sizeof(val));
    // Drain pending actions directly (runs on main/event-loop thread)
    goWakeupDrain();
    struct wlr_output *output = (struct wlr_output *)data;
    if (output) {
        wlr_output_schedule_frame(output);
    }
    return 0;
}
static void setup_wakeup_fd(struct wl_display *display, struct wlr_output *output, int fd) {
    struct wl_event_loop *loop = wl_display_get_event_loop(display);
    wl_event_loop_add_fd(loop, fd, WL_EVENT_READABLE, wakeup_handler, output);
}

static void keyboard_clear_focus(struct wlr_seat *seat) {
    wlr_seat_keyboard_clear_focus(seat);
}
static void scene_node_raise_to_top(struct wlr_scene_node *node) {
    wlr_scene_node_raise_to_top(node);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
// scene_output_commit commits the scene output. If the initial commit has no
// damage but screencopy (grim) is waiting for a frame capture, force full
// damage and retry so that wlr_output_commit fires the output.commit signal.
static int scene_output_commit(struct wlr_scene_output *scene_output) {
    // Check for pending screencopy frames BEFORE the commit. If pending,
    // force full damage so the commit actually renders and fires output.commit.
    if (g_screencopy_mgr && !wl_list_empty(&g_screencopy_mgr->frames)) {
        wlr_damage_ring_add_whole(&scene_output->damage_ring);
    }
    int ok = wlr_scene_output_commit(scene_output, NULL) ? 1 : 0;
    // Fallback: if first commit had no damage AND screencopy became pending
    // between the check and commit, retry with forced damage.
    if (!ok && g_screencopy_mgr && !wl_list_empty(&g_screencopy_mgr->frames)) {
        wlr_damage_ring_add_whole(&scene_output->damage_ring);
        ok = wlr_scene_output_commit(scene_output, NULL) ? 1 : 0;
    }
    // Capture EGL context for off-frame GL operations (thumbnail capture)
    EGLDisplay d = eglGetCurrentDisplay();
    EGLContext c = eglGetCurrentContext();
    if (d != EGL_NO_DISPLAY && c != EGL_NO_CONTEXT) {
        g_egl_display = d;
        g_egl_context = c;
    }
    return ok;
}
static void scene_output_send_frame_done(struct wlr_scene_output *scene_output) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    wlr_scene_output_send_frame_done(scene_output, &now);
}
static struct wlr_scene_output *scene_get_scene_output(struct wlr_scene *scene, struct wlr_output *output) {
    return wlr_scene_get_scene_output(scene, output);
}
static void schedule_output_frame(struct wlr_output *output) {
    wlr_output_schedule_frame(output);
}
*/
import "C"

import (
	"fmt"
	"log"
	"sort"
	"time"
	"unsafe"

	"deedles.dev/wlr"
	"golang.org/x/sys/unix"
)

// setupWakeup creates an eventfd and registers it with the Wayland event loop.
// When a goroutine writes to the eventfd (via triggerWakeup), the event loop
// wakes up and schedules a frame, ensuring mainThreadActions are drained promptly.
func (s *server) setupWakeup(output wlr.Output) {
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		log.Printf("Warning: could not create eventfd for wakeup: %v\n", err)
		return
	}
	s.wakeupFd.Store(int64(fd))
	wakeupServer = s
	C.setup_wakeup_fd(displayPtr(s.display), outputPtr(output), C.int(fd))
}

// triggerWakeup wakes the Wayland event loop from a goroutine.
// Thread-safe: eventfd writes are atomic and safe from any goroutine.
// The atomic.Load on wakeupFd avoids a data race against setupWakeup
// (which may run on the main thread while goroutines are already firing
// triggerWakeup at startup).
func (s *server) triggerWakeup() {
	fd := int(s.wakeupFd.Load())
	if fd > 0 {
		buf := [8]byte{1, 0, 0, 0, 0, 0, 0, 0} // uint64(1) little-endian
		unix.Write(fd, buf[:])
	}
}

// syncKeyboardFocus forces a keyboard leave+enter cycle on the focused surface.
// wlroots skips re-enter when the surface is already focused, so we must clear
// focus first. This re-sends wl_keyboard.enter with the current (clean) keycodes
// array, clearing any stale pressed keys the client saw during Alt-Tab.
func (s *server) syncKeyboardFocus() {
	keyboard := s.seat.Keyboard()
	if !keyboardValid(keyboard) {
		return
	}

	var surface wlr.Surface
	if s.activeXdg != nil && s.activeXdg.mapped {
		surface = s.activeXdg.xdgToplevel.Base().Surface()
	} else if s.activeXway != nil && s.activeXway.mapped {
		surface = s.activeXway.surface.Surface()
	}

	if !surface.Valid() {
		return
	}

	// Skip if keyboard focus is already on the correct surface. The
	// clear+enter cycle sends XFocusOut/XFocusIn to XWayland clients,
	// which causes Firefox to dismiss modals, popups, and blur inputs.
	type surfPtr struct{ p *C.struct_wlr_surface }
	focused := s.seat.KeyboardState().FocusedSurface()
	surfP := (*surfPtr)(unsafe.Pointer(&surface))
	focusP := (*surfPtr)(unsafe.Pointer(&focused))
	if surfP.p == focusP.p {
		return
	}

	// Clear focus first so the next NotifyEnter is not a no-op
	type seatPtr struct{ p *C.struct_wlr_seat }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	C.keyboard_clear_focus(seatP.p)

	s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
}

// destroySwitcherThumbnails frees cached thumbnail textures
func (s *server) destroySwitcherThumbnails() {
	for _, t := range s.switcherThumbnails {
		if t.Valid() {
			t.Destroy()
		}
	}
	s.switcherThumbnails = nil
}

func (s *server) switchDesk(desk int) {
	// Wrap around
	if desk < 0 {
		desk = s.numDesks - 1
	} else if desk >= s.numDesks {
		desk = 0
	}

	if desk == s.currentDesk {
		return
	}

	oldDesk := s.currentDesk
	s.currentDesk = desk

	// Update desktop state file for panel
	s.writeDesktopState()

	// Update scene visibility for all windows based on desktop membership
	for _, v := range s.xdgViews {
		if v.mapped && !v.fullscreen {
			setViewSceneEnabled(v.sceneTree, v.onDesk(desk))
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.overrideRedirect && !v.fullscreen {
			setViewSceneEnabled(v.sceneTree, v.onDesk(desk))
		}
	}

	// Clear focus when switching desktops
	if s.activeXdg != nil && !s.activeXdg.onDesk(desk) {
		s.activeXdg.xdgToplevel.SetActivated(false)
		s.activeXdg = nil
	}
	if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.onDesk(desk) {
		s.activeXway.surface.Activate(false)
		s.activeXway = nil
	}

	// Focus the topmost window on the new desktop
	s.focusTopmostOnDesk(desk)

	// Determine slide direction based on desktop index change.
	// Handles wraparound: going from desk 3→0 slides right, 0→3 slides left.
	diff := desk - oldDesk
	if diff > s.numDesks/2 {
		diff -= s.numDesks
	} else if diff < -s.numDesks/2 {
		diff += s.numDesks
	}
	if diff > 0 {
		s.slideDirection = 1 // slide right (new desk enters from right)
	} else {
		s.slideDirection = -1 // slide left (new desk enters from left)
	}
	s.startSlideTransition()

	// Update windows state so panel sees new focus
	s.writeWindowsState()
}

// focusTopmostOnDesk focuses the topmost window on the specified desktop
func (s *server) focusTopmostOnDesk(desk int) {
	// Check XDG views (top to bottom)
	for i := len(s.xdgViews) - 1; i >= 0; i-- {
		v := s.xdgViews[i]
		if v.mapped && v.onDesk(desk) {
			s.focusXdgView(v)
			return
		}
	}
	// Check XWayland views (skip panel and overlay windows like tooltips)
	for i := len(s.xwayViews) - 1; i >= 0; i-- {
		v := s.xwayViews[i]
		if v.mapped && !v.isPanel && !v.isOverlay && v.onDesk(desk) {
			s.focusXwayView(v)
			return
		}
	}
}

// moveWindowToDesk moves the active window to the specified desktop
func (s *server) moveWindowToDesk(desk int) {
	// Wrap around
	if desk < 0 {
		desk = s.numDesks - 1
	} else if desk >= s.numDesks {
		desk = 0
	}

	if s.activeXdg != nil {
		s.activeXdg.desk = desk
		// Also move all children to the same desktop
		for _, child := range s.xdgDescendants(s.activeXdg) {
			child.desk = desk
		}
		// Follow the window to the new desktop
		s.switchDesk(desk)
	} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
		s.activeXway.desk = desk
		// Also move all children to the same desktop
		for _, child := range s.xwayDescendants(s.activeXway) {
			child.desk = desk
		}
		// Follow the window to the new desktop
		s.switchDesk(desk)
	}
}

// xdgRootAncestor walks up the parent chain to find the root (parentless) ancestor.
func xdgRootAncestor(v *xdgView) *xdgView {
	for depth := 0; v.parent != nil && depth < 20; depth++ {
		v = v.parent
	}
	return v
}

// xwayRootAncestor walks up the parent chain to find the root (parentless) ancestor.
func xwayRootAncestor(v *xwayView) *xwayView {
	for depth := 0; v.parent != nil && depth < 20; depth++ {
		v = v.parent
	}
	return v
}

// xdgDescendants collects all XDG views that are descendants of root, sorted by focusSeq ascending.
func (s *server) xdgDescendants(root *xdgView) []*xdgView {
	var result []*xdgView
	for _, v := range s.xdgViews {
		if v != root && s.isXdgDescendantOf(v, root) {
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].focusSeq < result[j].focusSeq
	})
	return result
}

// xwayDescendants collects all XWayland views that are descendants of root, sorted by focusSeq ascending.
func (s *server) xwayDescendants(root *xwayView) []*xwayView {
	var result []*xwayView
	for _, v := range s.xwayViews {
		if v != root && s.isXwayDescendantOf(v, root) {
			result = append(result, v)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].focusSeq < result[j].focusSeq
	})
	return result
}

// isXdgDescendantOf returns true if v is a descendant of ancestor (walks parent chain).
func (s *server) isXdgDescendantOf(v, ancestor *xdgView) bool {
	cur := v
	for depth := 0; cur != nil && depth < 20; depth++ {
		if cur.parent == ancestor {
			return true
		}
		cur = cur.parent
	}
	return false
}

// isXwayDescendantOf returns true if v is a descendant of ancestor (walks parent chain).
func (s *server) isXwayDescendantOf(v, ancestor *xwayView) bool {
	cur := v
	for depth := 0; cur != nil && depth < 20; depth++ {
		if cur.parent == ancestor {
			return true
		}
		cur = cur.parent
	}
	return false
}

func (s *server) focusXdgView(v *xdgView) {
	if v == nil || !v.mapped {
		return
	}

	surface := v.xdgToplevel.Base().Surface()

	// Track previous real (non-panel, non-overlay) window for overlay refocus
	if s.activeXdg != nil && s.activeXdg != v {
		s.prevRealXdg = s.activeXdg
	}
	if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
		s.prevRealXway = s.activeXway
	}

	// Deactivate previous and update its decorations
	prevXdg := s.activeXdg
	if s.activeXdg != nil && s.activeXdg != v {
		s.activeXdg.xdgToplevel.SetActivated(false)
	}
	prevXway := s.activeXway
	if s.activeXway != nil {
		s.activeXway.surface.Activate(false)
		s.activeXway = nil
	}

	// Move to top
	for i, view := range s.xdgViews {
		if view == v {
			s.xdgViews = append(s.xdgViews[:i], s.xdgViews[i+1:]...)
			s.xdgViews = append(s.xdgViews, v)
			break
		}
	}

	s.activeXdg = v
	v.xdgToplevel.SetActivated(true)
	v.focusSeq = s.nextFocusSeq
	s.nextFocusSeq++
	v.urgent = false // Clear urgency on focus

	// Toggle fullscreen layer visibility based on whether this view is fullscreen
	s.updateFullscreenLayerVisibility(v.fullscreen)

	// Group-aware raise: raise root ancestor + all descendants
	root := xdgRootAncestor(v)
	raiseViewSceneToTop(root.sceneTree)
	for _, child := range s.xdgDescendants(root) {
		raiseViewSceneToTop(child.sceneTree)
	}

	// Update decorations for active/inactive state
	if prevXdg != nil && prevXdg != v && prevXdg.decorated {
		s.updateXdgViewDecorations(prevXdg)
	}
	if prevXway != nil && prevXway.decorated {
		s.updateXwayViewDecorations(prevXway)
	}
	if v.decorated {
		s.updateXdgViewDecorations(v)
	}

	keyboard := s.seat.Keyboard()
	if keyboardValid(keyboard) {
		s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
	}

	// Update text input focus for IME
	s.handleTextInputFocusChange(surfacePtr(surface))

	s.writeWindowsState()
}

// setSceneEnabled enables or disables a scene tree node.
// Safe to call with nil sceneTree.
func (s *server) setSceneEnabled(sceneTree unsafe.Pointer, enabled bool) {
	if sceneTree == nil {
		return
	}
	val := C.int(0)
	if enabled {
		val = 1
	}
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(sceneTree).node, val)
}

// focusPanelKeyboard sends keyboard focus to the panel surface without
// modifying the active-window state, scene ordering, or decorations.
// This is used when the panel needs keyboard input (launcher, emoji picker, etc.).
func (s *server) focusPanelKeyboard() {
	if s.panelXway == nil || !s.panelXway.mapped {
		return
	}
	s.focusSurfaceKeyboard(s.panelXway.surface.Surface())
}

// focusOverlayKeyboard sends keyboard focus to an overlay (XWayland) surface
// without modifying active-window state, scene ordering, or decorations.
// focusXwayView intentionally skips overlays to avoid disrupting window
// management, so this lightweight variant is needed for overlays that require
// keyboard input (clipboard picker, emoji picker, etc.).
func (s *server) focusOverlayKeyboard(v *xwayView) {
	if v == nil || !v.mapped {
		return
	}
	// Activate sends XSetInputFocus to the X11 window via XWayland,
	// which is required for GLFW to receive FocusIn and route key events.
	v.surface.Activate(true)
	s.focusSurfaceKeyboard(v.surface.Surface())
}

// focusSurfaceKeyboard sends a keyboard enter event to the given wlr surface.
func (s *server) focusSurfaceKeyboard(surface wlr.Surface) {
	if !surface.Valid() {
		return
	}
	keyboard := s.seat.Keyboard()
	if keyboardValid(keyboard) {
		s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
	}
}

func (s *server) focusXwayView(v *xwayView) {
	if v == nil || !v.mapped || v.isPanel || v.isOverlay {
		return
	}

	surface := v.surface.Surface()
	if !surface.Valid() {
		return
	}

	// Track previous real (non-panel, non-overlay) window for overlay refocus
	if s.activeXdg != nil {
		s.prevRealXdg = s.activeXdg
	}
	if s.activeXway != nil && s.activeXway != v && !s.activeXway.isPanel && !s.activeXway.isOverlay {
		s.prevRealXway = s.activeXway
	}

	// Deactivate previous and update decorations
	prevXdg := s.activeXdg
	if s.activeXdg != nil {
		s.activeXdg.xdgToplevel.SetActivated(false)
		s.activeXdg = nil
	}
	prevXway := s.activeXway
	if s.activeXway != nil && s.activeXway != v {
		s.activeXway.surface.Activate(false)
	}

	// Move to top
	for i, view := range s.xwayViews {
		if view == v {
			s.xwayViews = append(s.xwayViews[:i], s.xwayViews[i+1:]...)
			s.xwayViews = append(s.xwayViews, v)
			break
		}
	}

	s.activeXway = v
	v.surface.Activate(true)
	v.focusSeq = s.nextFocusSeq
	s.nextFocusSeq++
	v.urgent = false // Clear urgency on focus

	// Toggle fullscreen layer visibility based on whether this view is fullscreen
	s.updateFullscreenLayerVisibility(v.fullscreen)

	// Group-aware raise: raise root ancestor + all descendants
	// Also restack in X11 so XWayland input routing matches scene order
	root := xwayRootAncestor(v)
	raiseViewSceneToTop(root.sceneTree)
	restackXwaylandSurfaceAbove(root.surface)
	descendants := s.xwayDescendants(root)
	for _, child := range descendants {
		raiseViewSceneToTop(child.sceneTree)
		restackXwaylandSurfaceAbove(child.surface)
	}

	// Update decorations for active/inactive state
	if prevXdg != nil && prevXdg.decorated {
		s.updateXdgViewDecorations(prevXdg)
	}
	if prevXway != nil && prevXway != v && prevXway.decorated {
		s.updateXwayViewDecorations(prevXway)
	}
	if v.decorated {
		s.updateXwayViewDecorations(v)
	}

	keyboard := s.seat.Keyboard()
	if keyboardValid(keyboard) {
		s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
	}

	// Update text input focus for IME
	s.handleTextInputFocusChange(surfacePtr(surface))

	s.writeWindowsState()
}

// updateFullscreenLayerVisibility shows or hides the fullscreen scene layer.
// When focusing a non-fullscreen window, the fullscreen layer is hidden so the
// normal windows tree is visible and the user can interact with non-fullscreen
// windows (e.g. Steam while a game is fullscreen). When refocusing a fullscreen
// window, the layer is re-enabled.
func (s *server) updateFullscreenLayerVisibility(focusingFullscreen bool) {
	if s.fullscreenTree == nil {
		return
	}
	// Skip the CGO call (and the log) when nothing changes — every focus
	// change used to fire this, producing 12+ identical log lines per
	// second during e.g. a desktop-switch stress and one redundant
	// scene_node_set_enabled per call.
	if s.fullscreenLayerEnabled == focusingFullscreen {
		return
	}
	s.fullscreenLayerEnabled = focusingFullscreen
	if focusingFullscreen {
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 1)
	} else {
		log.Printf("[FULLSCREEN] Disabling fullscreenTree (focusingFullscreen=%v)", focusingFullscreen)
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 0)
	}
}

// enqueueAction sends an action to the main thread with a timeout.
// Returns an error if the main thread is too busy to accept the action.
func (s *server) enqueueAction(action func()) error {
	select {
	case s.mainThreadActions <- action:
		s.triggerWakeup()
		return nil
	case <-time.After(500 * time.Millisecond):
		return fmt.Errorf("main thread busy, action dropped")
	}
}

func (s *server) drainMainThreadActions() {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		select {
		case action := <-s.mainThreadActions:
			t0 := time.Now()
			action()
			if d := time.Since(t0); d > 50*time.Millisecond {
				log.Printf("[STALL] mainThreadAction took %v\n", d)
			}
			if time.Now().After(deadline) {
				log.Println("[STALL] drainMainThreadActions hit 100ms budget, deferring remaining actions")
				// Re-trigger wakeup so remaining actions get drained on the next event loop iteration
				s.triggerWakeup()
				return
			}
		default:
			return
		}
	}
}

func (s *server) renderOutput(output wlr.Output) {
	frameStart := time.Now()

	// Diagnostic: detect event loop stalls (>1s between frames)
	if !s.lastFrameTime.IsZero() {
		gap := frameStart.Sub(s.lastFrameTime)
		if gap > 1*time.Second {
			log.Printf("[STALL] Event loop gap: %v (no frame for %v)\n", gap, gap)
		}
	}
	s.lastFrameTime = frameStart

	// Execute pending actions from goroutines on the main thread (wlroots is not thread-safe)
	s.drainMainThreadActions()

	// Skip rendering on blanked outputs. After suspend/resume, the DRM swapchain
	// is stale and wlr_scene_output_commit() would crash with a NULL dereference.
	// The unblank path (resetIdleTimer → setDisplayBlanked(false)) re-enables the
	// output and schedules a new frame, restarting the render loop safely.
	if s.displayBlanked {
		return
	}

	// Flush debounced IPC writes (batches rapid state changes into one write per frame)
	var ipcDur time.Duration
	if s.windowsStateDirty {
		s.windowsStateDirty = false
		t0 := time.Now()
		s.flushWindowsState()
		ipcDur = time.Since(t0)
	}

	// Tick all animations (snap, transitions, close effects, etc.)
	// Each returns true if still running, used to decide frame scheduling.
	animActive := s.tickViewAnims()
	animActive = s.tickTransition() || animActive
	animActive = s.tickBootSequence() || animActive
	animActive = s.tickCloseAnims() || animActive
	animActive = s.tickOverviewAnim() || animActive
	animActive = s.tickSwitcherFade() || animActive
	animActive = s.tickOpenAnim() || animActive

	// Tick animated wallpaper (before scene commit so pixels are fresh).
	animWallpaperActive := false
	for _, out := range s.outputs {
		if out.output == output && out.animWallpaper != nil {
			if !s.isOutputOccludedByFullscreen(out) {
				s.updateAnimatedWallpaper(out)
			}
			animWallpaperActive = true
			break
		}
	}

	// Find the scene output for this output
	scene := (*C.struct_wlr_scene)(s.scene)
	sceneOutput := C.scene_get_scene_output(scene, outputPtr(output))
	if sceneOutput == nil {
		return
	}

	// Commit the scene output (damage tracking + rendering happens automatically).
	// When the DRM atomic commit fails (EBUSY), a previous page flip is still
	// in progress. Its completion will fire the next frame event, so we skip
	// this frame to avoid a tight retry loop.
	commitStart := time.Now()
	if C.scene_output_commit(sceneOutput) == 0 {
		// DRM atomic commit failed (EBUSY): a previous page flip is still
		// in-flight. Do NOT schedule another frame here — the page flip
		// completion callback will fire the next frame event naturally.
		// Scheduling here creates a tight retry loop (~6ms) that starves
		// the event loop and causes mouse lag.
		return
	}
	commitDur := time.Since(commitStart)

	// Send frame done IMMEDIATELY so clients (video players) can submit new
	// frames without waiting for thumbnail capture or switcher recomposite.
	C.scene_output_send_frame_done(sceneOutput)

	// Capture thumbnails and refresh switcher overlay in the dead time after
	// frame_done. Only recomposite the switcher when thumbnails actually changed.
	thumbStart := time.Now()
	thumbCaptured := s.captureViewThumbnails()
	thumbDur := time.Since(thumbStart)
	if thumbCaptured && s.switcherActive {
		s.switcherThumbImgs = s.collectSwitcherThumbs()
		s.updateSwitcherScene()
	}

	// Schedule the next frame only when there's active work requiring continuous
	// rendering. For idle desktops, the scene's damage tracking sets
	// output->needs_frame when clients commit new buffers, which triggers the
	// next frame callback via the DRM backend automatically.
	if animActive || animWallpaperActive || s.switcherActive || s.overviewActive ||
		s.transitionActive || s.bootActive || s.regionSelectActive {
		C.schedule_output_frame(outputPtr(output))
	}

	// Log severe stalls only (>1s indicates a real problem).
	totalDur := time.Since(frameStart)
	if totalDur > 1*time.Second {
		drainDur := totalDur - ipcDur - commitDur - thumbDur
		log.Printf("[STALL] frame took %v — drain=%v ipc=%v commit=%v thumb=%v\n",
			totalDur, drainDur, ipcDur, commitDur, thumbDur)
	}
}
