package compositor

/*
#include <time.h>
#include <unistd.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_output.h>

// Go callback for draining mainThreadActions from the event loop thread.
extern void goWakeupDrain();

// Wakeup event source: reads from eventfd, drains pending Go actions, and
// schedules a frame on every connected output. The direct goWakeupDrain()
// call ensures actions are processed even when the backend doesn't deliver
// frame callbacks (e.g. nested Wayland compositor with idle window). The
// Go side iterates outputs at fire time so primary-output changes (monitor
// hot-plug) don't leave wakeups pointing at a freed wlr_output.
static int wakeup_handler(int fd, uint32_t mask, void *data) {
    uint64_t val;
    read(fd, &val, sizeof(val));
    // Drain pending actions and schedule frames on current outputs (Go-side).
    goWakeupDrain();
    return 0;
}
static void setup_wakeup_fd(struct wl_display *display, struct wlr_output *output, int fd) {
    struct wl_event_loop *loop = wl_display_get_event_loop(display);
    // output is unused — kept for ABI continuity with Go-side callers; frame
    // scheduling now happens on the Go side over all outputs.
    (void)output;
    wl_event_loop_add_fd(loop, fd, WL_EVENT_READABLE, wakeup_handler, NULL);
}

static void scene_node_raise_to_top(struct wlr_scene_node *node) {
    wlr_scene_node_raise_to_top(node);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
// scene_output_commit renders and commits the scene output if it needs a
// frame. Screencopy clients (grim) get their frame without extra work: since
// wlroots 0.18 a pending screencopy marks the output as needing a frame.
static int scene_output_commit(struct wlr_scene_output *scene_output) {
    return wlr_scene_output_commit(scene_output, NULL) ? 1 : 0;
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
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"fyshos.com/tyde/internal/wayland/wlr"
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
	if s.seat.KeyboardFocusedSurface().Ptr() == surface.Ptr() {
		return
	}

	// Clear focus first so the next NotifyEnter is not a no-op
	s.seat.KeyboardClearFocus()

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

	// Update scene visibility for all windows based on desktop membership,
	// fullscreen ones included (they stayed over every desktop).
	for _, v := range s.xdgViews {
		if v.mapped {
			setViewSceneEnabled(v.sceneTree, v.onDesk(desk))
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.overrideRedirect {
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
	if !s.noSlideTransition {
		s.startSlideTransition()
	}

	// Update windows state so panel sees new focus
	s.writeWindowsState()
}

// focusTopmostOnDesk focuses the topmost window of the desktop (the
// current one, whose windows are the enabled ones), by the real stacking
// order across XDG and XWayland windows. A fullscreen window, in its own
// layer, comes first; panels, overlays and override-redirect popups never
// take the focus.
func (s *server) focusTopmostOnDesk(desk int) {
	for i := len(s.xdgViews) - 1; i >= 0; i-- {
		if v := s.xdgViews[i]; v.mapped && v.fullscreen && v.onDesk(desk) {
			s.focusXdgView(v)
			return
		}
	}
	for i := len(s.xwayViews) - 1; i >= 0; i-- {
		if v := s.xwayViews[i]; v.mapped && v.fullscreen && focusableXway(v) && v.onDesk(desk) {
			s.focusXwayView(v)
			return
		}
	}
	for _, e := range s.getViewsInZOrder() {
		if v := e.xdg; v != nil && v.mapped && !v.minimized && v.onDesk(desk) {
			s.focusXdgView(v)
			return
		}
		if v := e.xway; v != nil && v.mapped && !v.minimized && focusableXway(v) && v.onDesk(desk) {
			s.focusXwayView(v)
			return
		}
	}
}

// focusAfterHide gives the focus of a window that was hidden (to the tray)
// to the one now on top, or to none: the keys must not go on to the hidden
// window.
func (s *server) focusAfterHide() {
	s.focusTopmostOnDesk(s.currentDesk)
	if s.activeXdg == nil && s.activeXway == nil {
		s.seat.KeyboardClearFocus()
	}
}

// focusableXway reports whether an XWayland window can take the focus:
// not the panel, an overlay or an override-redirect popup (tooltip, menu).
func focusableXway(v *xwayView) bool {
	return !v.isPanel && !v.isOverlay && !v.overrideRedirect
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
	if v == nil || !v.mapped || s.locked.Load() {
		return // while locked, the keyboard belongs to the lock screen only
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
	if v == nil || !v.mapped || v.isPanel || v.isOverlay || s.locked.Load() {
		return // while locked, the keyboard belongs to the lock screen only
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
	s.setFullscreenLayer(focusingFullscreen)
}

// setFullscreenLayer shows or hides the fullscreen scene layer. Everything
// goes through it, so that the cached state stays true.
func (s *server) setFullscreenLayer(on bool) {
	if s.fullscreenTree == nil {
		return
	}
	s.fullscreenLayerEnabled = on
	enabled := C.int(0)
	if on {
		enabled = 1
	}
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, enabled)
}

// anyFullscreen reports whether a window is fullscreen.
func (s *server) anyFullscreen() bool {
	for _, v := range s.xdgViews {
		if v.fullscreen {
			return true
		}
	}
	for _, v := range s.xwayViews {
		if v.fullscreen {
			return true
		}
	}
	return false
}

// runOnMainThread runs fn on the main thread and waits for the result.
// Used by IPC goroutine handlers that need to read state shared with the
// main thread (xdgViews/xwayViews iteration, currentDesk/numDesks reads,
// etc.) — running them inline would race with main-thread mutations.
//
// Returns an error if the main thread is too busy to accept the action
// within 500ms (same budget as enqueueAction). Result type is via generics
// so each call site can return whatever shape it needs.
func runOnMainThread[T any](s *server, fn func() T) (T, error) {
	var zero T
	resultCh := make(chan T, 1)
	if err := s.enqueueAction(func() { resultCh <- fn() }); err != nil {
		return zero, err
	}
	select {
	case result := <-resultCh:
		return result, nil
	case <-time.After(2 * time.Second):
		return zero, fmt.Errorf("main thread did not respond within 2s")
	}
}

// enqueueAction sends an action to the main thread with a timeout.
// Returns an error if the main thread is too busy to accept the action.
// A dropped action is logged with the caller site so operators can spot
// chronic backpressure (e.g., heavy frame stalls vs. transient bursts).
func (s *server) enqueueAction(action func()) error {
	select {
	case s.mainThreadActions <- action:
		s.triggerWakeup()
		return nil
	case <-time.After(500 * time.Millisecond):
		caller := "<unknown>"
		if _, file, line, ok := runtime.Caller(1); ok {
			caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}
		log.Printf("[ENQUEUE] dropped action from %s — main thread busy >500ms (chan len=%d/%d)\n",
			caller, len(s.mainThreadActions), cap(s.mainThreadActions))
		return fmt.Errorf("main thread busy, action dropped")
	}
}

func (s *server) drainMainThreadActions() {
	deadline := time.Now().Add(100 * time.Millisecond)
	ranAny := false
	for {
		select {
		case action := <-s.mainThreadActions:
			ranAny = true
			t0 := time.Now()
			action()
			if d := time.Since(t0); d > 50*time.Millisecond {
				log.Printf("[STALL] mainThreadAction took %v\n", d)
			}
			if time.Now().After(deadline) {
				log.Println("[STALL] drainMainThreadActions hit 100ms budget, deferring remaining actions")
				// Re-trigger wakeup so remaining actions get drained on the next event loop iteration
				s.triggerWakeup()
				if ranAny {
					s.scheduleAllOutputFrames()
				}
				return
			}
		default:
			if ranAny {
				// Ensure the changes from these actions get flushed to the screen
				// even if the actions themselves didn't schedule frames. Iterating
				// here (rather than binding a single output to the wakeup handler)
				// keeps us correct across primary-output hotplug changes.
				s.scheduleAllOutputFrames()
			}
			return
		}
	}
}

// desktopTickMin is the least time between two runs of the desktop's
// per-frame work: less than a refresh at 240 Hz.
const desktopTickMin = 4 * time.Millisecond

func (s *server) renderOutput(output wlr.Output) {
	frameStart := time.Now()
	s.lastLoopTime.Store(frameStart.UnixNano())
	// (No frame for a while is normal now: a quiet desktop renders none.)

	// Execute pending actions from goroutines on the main thread (wlroots is not thread-safe)
	s.drainMainThreadActions()
	s.reapplyViewOpacity()

	// Skip rendering on blanked outputs. After suspend/resume, the DRM swapchain
	// is stale and wlr_scene_output_commit() would crash with a NULL dereference.
	// The unblank path (resetIdleTimer → setDisplayBlanked(false)) re-enables the
	// output and schedules a new frame, restarting the render loop safely.
	if s.displayBlanked.Load() {
		return
	}

	// The work of the whole desktop (IPC flush, animations) runs once per
	// frame, whichever screen's frame comes first: with two screens it ran
	// twice a refresh. The others reuse whether an animation runs.
	var ipcDur time.Duration
	animActive := s.animActive
	if frameStart.Sub(s.lastDesktopTick) >= desktopTickMin {
		s.lastDesktopTick = frameStart

		// Flush debounced IPC writes (batches rapid state changes into one write per frame)
		if s.windowsStateDirty {
			s.windowsStateDirty = false
			t0 := time.Now()
			s.flushWindowsState()
			ipcDur = time.Since(t0)
		}

		// Tick all animations (snap, transitions, close effects, etc.)
		// Each returns true if still running, used to decide frame scheduling.
		animActive = s.tickAnimations()
		s.animActive = animActive
	}
	if s.displayBlanked.Load() {
		return // the curtain came down and the idle action blanked the outputs
	}
	s.updateBlurs(output)

	// Resolve this output's state once — used by the animated wallpaper tick
	// and the page-flip stall tracking around the scene commit below.
	var outState *outputState
	for _, out := range s.outputs {
		if out.output == output {
			outState = out
			break
		}
	}

	// Tick animated wallpaper (before scene commit so pixels are fresh). It
	// changes at 24 fps: it asks for its next frame then, not at every vblank,
	// and not at all while a fullscreen window hides it (leaving fullscreen
	// damages the screen, which brings the frames back).
	if outState != nil && outState.animWallpaper != nil && !s.isOutputOccludedByFullscreen(outState) {
		s.updateAnimatedWallpaper(outState)
		s.scheduleAnimWakeup(outState.lastAnimTick)
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
	if !s.commitScene(output, sceneOutput) {
		// DRM atomic commit failed (EBUSY): a previous page flip is still
		// in-flight. Do NOT schedule another frame here — the page flip
		// completion callback will fire the next frame event naturally.
		// Scheduling here creates a tight retry loop (~6ms) that starves
		// the event loop and causes mouse lag.
		//
		// If the completion event never arrives (i915 loses it across
		// suspend/resume), the failure streak below detects the stall and
		// forces a blocking modeset to restart the chain.
		if outState != nil {
			s.noteCommitFailure(outState)
		}
		return
	}
	if outState != nil {
		s.noteCommitSuccess(outState)
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
	// (Alt-Tab, the overview and the region selection need no frame while
	// they stand still: their animations report themselves, and the pointer
	// or the windows damage the screen when something changes.)
	if animActive || s.transitionActive || s.bootActive {
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

// commitScene draws output and shows it, enlarged when the magnifier is on.
// It reports whether the output took the new picture.
func (s *server) commitScene(output wlr.Output, sceneOutput *C.struct_wlr_scene_output) bool {
	if s.zoomed() {
		return s.commitZoomed(output, sceneOutput)
	}
	return C.scene_output_commit(sceneOutput) != 0
}

// tickAnimations advances every animation and reports whether one is still
// running.
func (s *server) tickAnimations() bool {
	animActive := s.tickViewAnims()
	animActive = s.tickTransition() || animActive
	animActive = s.tickBootSequence() || animActive
	animActive = s.tickCloseAnims() || animActive
	animActive = s.tickOverviewAnim() || animActive
	animActive = s.tickSwitcherFade() || animActive
	animActive = s.tickOpenAnim() || animActive
	animActive = s.tickPenFade() || animActive
	animActive = s.tickAttention() || animActive
	animActive = s.tickWobble() || animActive
	animActive = s.tickGenie() || animActive
	animActive = s.tickFocusDim() || animActive
	animActive = s.tickDeskSwipe() || animActive
	animActive = s.tickCurtain() || animActive
	animActive = s.tickZoom() || animActive
	animActive = s.tickShadows() || animActive
	return animActive
}
