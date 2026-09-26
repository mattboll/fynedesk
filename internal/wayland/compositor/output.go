package compositor

/*
#include <stdlib.h>
#include <string.h>
#include <wayland-server-core.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_output_layout.h>
#include <wlr/types/wlr_fractional_scale_v1.h>
#include <wlr/types/wlr_viewporter.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

#include "pixel_buffer.h"

static struct wlr_scene_output *create_scene_output(struct wlr_scene *scene, struct wlr_output *output) {
    return wlr_scene_output_create(scene, output);
}
static void scene_output_set_position(struct wlr_scene_output *so, int x, int y) {
    wlr_scene_output_set_position(so, x, y);
}
static void schedule_output_frame(struct wlr_output *output) {
    wlr_output_schedule_frame(output);
}
static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
    return wlr_scene_tree_create(parent);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
    wlr_scene_node_set_position(node, x, y);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
    wlr_scene_node_destroy(node);
}
static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
    return wlr_scene_buffer_create(parent, buffer);
}
static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
    wlr_scene_buffer_set_buffer(buf, buffer);
}
static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
    wlr_scene_buffer_set_dest_size(buf, w, h);
}

// C-level per-output frame listener: each output gets its own wl_listener
// so that frame callbacks fire independently for every connected display.
// A single static listener would be moved to the last output on each
// wl_signal_add call, leaving earlier outputs with no frame callbacks.
extern void goOnFrame(struct wlr_output *output);
extern int goOnFrameAll(void);

// Frame-pacing watchdog: catches stalls in the page-flip → frame → render →
// commit → frame_done chain. When the chain breaks (kernel missed a page
// flip, dmabuf damage dropped, DRM EBUSY swallowed, nested host that doesn't
// deliver wl_callback.done reliably), clients waiting on frame_done freeze
// until something external — cursor movement, an animation tick — wakes the
// render loop. Mouse-idle video playback hits this regularly.
//
// When an output needs a frame, the watchdog is armed FRAME_WATCHDOG_MS
// ahead; the frame that answers disarms it. If none comes, the watchdog
// renders anyway (goOnFrameAll), and keeps doing so at ~30 Hz while an output
// still needs a frame — clients stay unblocked even when the chain is fully
// broken, and nothing wakes up on a quiet desktop.
//
// 32ms gives roughly a 30Hz floor, which is well below the freeze-perception
// threshold for video. Mutter/KWin run a similar clock at native refresh rate,
// using presentation feedback to predict vblank — that's the next refinement
// if 30Hz turns out to be visibly choppy under a fully broken backend.
#define FRAME_WATCHDOG_MS 32

// The watchdog is armed when an output needs a frame (damage, a frame
// callback, a scheduled frame) and disarmed by the frame that answers: a
// quiet desktop wakes up for nothing (it used to fire 30 times a second).
static struct wl_event_source *frame_timer = NULL;

// frame_listeners are the listeners of one output.
struct frame_listeners {
    struct wl_listener frame;
    struct wl_listener needs_frame;
};

static void handle_frame(struct wl_listener *listener, void *data) {
    struct wlr_output *output = data;
    // A real frame answered: nothing to watch until a frame is needed again.
    if (frame_timer) {
        wl_event_source_timer_update(frame_timer, 0);
    }
    goOnFrame(output);
}

static void handle_needs_frame(struct wl_listener *listener, void *data) {
    // A frame is wanted: if the backend does not deliver one in time, the
    // watchdog renders anyway, so clients waiting on frame_done go on.
    if (frame_timer) {
        wl_event_source_timer_update(frame_timer, FRAME_WATCHDOG_MS);
    }
}

static int frame_timer_handler(void *data) {
    // No backend frame in FRAME_WATCHDOG_MS although one was needed: the
    // chain stalled; render so that waiting clients are unblocked. While an
    // output still needs a frame (its commit failed: a page flip still in
    // flight, or lost across suspend), keep trying, so the failure streak
    // that triggers the recovery modeset can build up.
    if (goOnFrameAll()) {
        wl_event_source_timer_update(frame_timer, FRAME_WATCHDOG_MS);
    }
    return 0;
}

// Allocate the listeners of an output and connect them to its frame and
// needs_frame events. The returned pointer must be freed with
// destroy_frame_listener on output destroy.
static struct wl_listener *create_frame_listener(struct wl_display *display, struct wlr_output *output) {
    struct frame_listeners *l = calloc(1, sizeof(*l));
    if (!l) return NULL;
    l->frame.notify = handle_frame;
    wl_signal_add(&output->events.frame, &l->frame);
    l->needs_frame.notify = handle_needs_frame;
    wl_signal_add(&output->events.needs_frame, &l->needs_frame);

    // One global watchdog for all outputs, first armed a while after start
    // so the backend can deliver its first frames.
    if (!frame_timer) {
        struct wl_event_loop *loop = wl_display_get_event_loop(display);
        frame_timer = wl_event_loop_add_timer(loop, frame_timer_handler, NULL);
        wl_event_source_timer_update(frame_timer, 500);
    }
    wlr_output_schedule_frame(output);
    return &l->frame;
}

static void destroy_frame_listener(struct wl_listener *listener) {
    if (!listener) return;
    struct frame_listeners *l = wl_container_of(listener, l, frame);
    wl_list_remove(&l->frame.link);
    wl_list_remove(&l->needs_frame.link);
    free(l);
}

// Fractional scaling: create the manager so clients can query precise scale
static struct wlr_fractional_scale_manager_v1 *create_fractional_scale_mgr(struct wl_display *display) {
    return wlr_fractional_scale_manager_v1_create(display, 1);
}

// Viewporter: allows clients to decouple buffer size from surface size
static struct wlr_viewporter *create_viewporter(struct wl_display *display) {
    return wlr_viewporter_create(display);
}
*/
import "C"

import (
	_ "image/jpeg"
	_ "image/png"
	"log"
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// destroyFrameListener frees a per-output frame listener (see
// create_frame_listener). Callable from non-CGO files.
func destroyFrameListener(listener unsafe.Pointer) {
	C.destroy_frame_listener((*C.struct_wl_listener)(listener))
}

// scheduleOutputFrame requests the next frame for an output.
// Callable from non-CGO files (e.g. system.go).
func scheduleOutputFrame(output wlr.Output) {
	C.schedule_output_frame(outputPtr(output))
}

// scheduleAllOutputFrames requests the next frame for every connected output.
// Used by the animation timer to wake all outputs after the throttle period.
func (s *server) scheduleAllOutputFrames() {
	for _, out := range s.outputs {
		C.schedule_output_frame(outputPtr(out.output))
	}
}

// scheduleAnimWakeup schedules a wakeup timer for the next animation tick.
// Debounced: only one timer runs at a time. When it fires, it enqueues a
// main-thread action that schedules frames for all outputs.
func (s *server) scheduleAnimWakeup(lastTick time.Time) {
	if s.animTimerPending.Load() {
		return
	}
	s.animTimerPending.Store(true)

	remaining := 42*time.Millisecond - time.Since(lastTick)
	if remaining < time.Millisecond {
		remaining = time.Millisecond
	}
	time.AfterFunc(remaining, func() {
		s.animTimerPending.Store(false)
		select {
		case s.mainThreadActions <- func() {
			s.scheduleAllOutputFrames()
		}:
		default:
			// Channel full — animation will skip this frame. Always trigger
			// the wakeup so the next drain at least catches up later.
			log.Printf("[anim] mainThreadActions full, frame schedule dropped")
		}
		s.triggerWakeup()
	})
}

// commitOutput applies the changes made by fill to output in one atomic
// commit and reports whether the backend accepted them.
func commitOutput(output wlr.Output, fill func(st *wlr.OutputState)) bool {
	st := wlr.NewOutputState()
	defer st.Finish()
	fill(st)
	return output.CommitState(st)
}

// enableAdaptiveSync enables or disables VRR/FreeSync on an output.
// After commit, checks the output's adaptive_sync_status to verify success.
func (s *server) enableAdaptiveSync(out *outputState, enable bool) {
	if enable && !out.output.AdaptiveSyncSupported() {
		out.vrrEnabled = out.output.AdaptiveSyncEnabled()
		log.Printf("[VRR] Adaptive sync not supported on %s\n", out.output.Name())
		return
	}
	committed := commitOutput(out.output, func(st *wlr.OutputState) {
		st.SetAdaptiveSyncEnabled(enable)
	})

	// Check actual status after commit (DRM may reject)
	out.vrrEnabled = out.output.AdaptiveSyncEnabled()
	if enable && !out.vrrEnabled {
		log.Printf("[VRR] Adaptive sync rejected on %s (commit ok=%v)\n", out.output.Name(), committed)
	} else {
		log.Printf("[VRR] Adaptive sync %s on %s\n",
			map[bool]string{true: "enabled", false: "disabled"}[out.vrrEnabled], out.output.Name())
	}
}

// setupFractionalScaling registers wp_fractional_scale_v1 and wp_viewporter
// protocols so clients can use precise fractional scales (1.25, 1.5, 1.75).
func (s *server) setupFractionalScaling() {
	C.create_fractional_scale_mgr(displayPtr(s.display))
	C.create_viewporter(displayPtr(s.display))
	log.Println("Fractional scaling protocols registered (wp_fractional_scale_v1 + wp_viewporter)")
}

func (s *server) primaryOutput() *outputState {
	if s.primaryOutputName != "" {
		for _, out := range s.outputs {
			if out.output.Name() == s.primaryOutputName {
				return out
			}
		}
	}
	if len(s.outputs) > 0 {
		return s.outputs[0]
	}
	return nil
}

// isPrimaryOutput returns true if the given output geometry matches the primary output
func (s *server) isPrimaryOutput(outGeo outputGeometry) bool {
	p := s.primaryOutput()
	if p == nil {
		return true // no outputs, treat as primary
	}
	pGeo := s.getOutputGeometry(p)
	return outGeo.x == pGeo.x && outGeo.y == pGeo.y
}

// getActiveOutput returns the output under the cursor, or the first output
func (s *server) getActiveOutput() *outputState {
	output := s.outLayout.OutputAt(s.cursor.X(), s.cursor.Y())
	for _, out := range s.outputs {
		if out.output == output {
			return out
		}
	}
	if len(s.outputs) > 0 {
		return s.outputs[0]
	}
	return nil
}

// getOutputGeometry returns the layout geometry for an output
func (s *server) getOutputGeometry(out *outputState) outputGeometry {
	lo := s.outLayout.Get(out.output)
	return outputGeometry{
		x:      lo.X(),
		y:      lo.Y(),
		width:  out.width,
		height: out.height,
		scale:  out.output.Scale(),
	}
}

// outputNameForPosition returns the name of the output containing the given position.
func (s *server) outputNameForPosition(x, y float64) string {
	if out := s.getOutputForPosition(x, y); out != nil {
		return out.output.Name()
	}
	return ""
}

// getOutputForPosition returns the output containing the given position
func (s *server) getOutputForPosition(x, y float64) *outputState {
	output := s.outLayout.OutputAt(x, y)
	for _, out := range s.outputs {
		if out.output == output {
			return out
		}
	}
	if len(s.outputs) > 0 {
		return s.outputs[0]
	}
	return nil
}

func (s *server) handleNewOutput(output wlr.Output) {
	out := &outputState{output: output, savedMode: -1}

	// Initialize render FIRST (required for DRM backend)
	if !output.InitRender(s.allocator, s.renderer) {
		log.Printf("Output %s: failed to initialize rendering, ignoring it\n", output.Name())
		return
	}

	// Collect all available modes
	preferredMode := output.PreferredMode()
	for mode := range output.Modes() {
		out.modes = append(out.modes, mode)
		// Find index of preferred mode
		if preferredMode.Valid() && mode.Width() == preferredMode.Width() &&
			mode.Height() == preferredMode.Height() &&
			mode.RefreshRate() == preferredMode.RefreshRate() {
			out.currentMode = len(out.modes) - 1
		}
	}

	// Detect physical dimensions for IPC (Settings UI shows DPI info)
	// Scale is NOT applied at startup to avoid DRM modeset issues.
	// Users can change scale from Settings → scale-request.json → setOutputScale().
	physW, _ := getOutputPhysSize(out)
	if preferredMode.Valid() {
		autoScale := calculateScale(int(preferredMode.Width()), physW)
		log.Printf("Output %s: %dx%d px, phys_w=%dmm, suggested-scale=%.1f\n",
			output.Name(), preferredMode.Width(), preferredMode.Height(), physW, autoScale)
	}

	// Check layout config early — if this output is disabled, skip all setup
	config := s.readLayoutConfig()
	if config != nil {
		if entry, ok := config.Layouts[output.Name()]; ok && entry.Position == "disable" {
			log.Printf("Output %s: layout config says disabled, skipping setup\n", output.Name())
			commitOutput(output, func(st *wlr.OutputState) { st.SetEnabled(false) })
			s.trackDisabledOutput(out)
			s.writeCompositorState()
			return
		}
	}

	// An output set to mirror another shows that one's picture instead of
	// joining the desktop (see mirror.go). Like any output out of the layout, it
	// is not advertised to clients.
	if entry, ok := config.entry(output.Name()); ok && entry.Position == "mirror" {
		if src := s.findOutputByName(entry.RelativeTo); src != nil {
			commitOutput(output, func(st *wlr.OutputState) {
				st.SetEnabled(true)
				if preferredMode.Valid() {
					st.SetMode(preferredMode)
				}
			})
			out.width, out.height = output.EffectiveResolution()
			s.startMirror(out, src)
			return
		}
	}

	// Create global so clients can see this output
	output.CreateGlobal(s.display)

	s.addOutputToDesktop(out, config)
	s.startPendingMirrors(out)
}

// addOutputToDesktop makes an enabled output part of the desktop: it gets a
// scene output, a place in the layout, a wallpaper and the panel or a bar.
func (s *server) addOutputToDesktop(out *outputState, config *OutputLayoutConfig) {
	output := out.output
	preferredMode := output.PreferredMode()

	// Create scene output BEFORE commit (DRM needs a framebuffer from the scene)
	scene := (*C.struct_wlr_scene)(s.scene)
	sceneOutput := C.create_scene_output(scene, outputPtr(output))
	out.sceneOutput = unsafe.Pointer(sceneOutput)

	// Set window title for nested mode (ignored for DRM)
	output.SetTitle("Tyde Wayland Compositor")

	// Commit the initial output state (mode + enabled).
	// MUST happen before EffectiveResolution (hotplugged outputs have 0x0 before commit)
	s.enableOutputWithBestMode(out, preferredMode)

	// Now that the output is committed, get the effective resolution
	out.width, out.height = output.EffectiveResolution()
	log.Printf("Output %s committed: %dx%d\n", output.Name(), out.width, out.height)

	// Add to layout — restore saved position if available, otherwise auto-place
	positioned := false
	if config != nil {
		if config.Primary != "" {
			s.primaryOutputName = config.Primary
		}
		if entry, ok := config.Layouts[output.Name()]; ok {
			ref := s.findOutputByName(entry.RelativeTo)
			if ref != nil {
				refGeo := s.getOutputGeometry(ref)
				var newX, newY int
				known := true
				switch entry.Position {
				case "right":
					newX = refGeo.x + refGeo.width
					newY = refGeo.y
				case "left":
					newX = refGeo.x - out.width
					newY = refGeo.y
				case "above":
					newX = refGeo.x
					newY = refGeo.y - out.height
				case "below":
					newX = refGeo.x
					newY = refGeo.y + refGeo.height
				default: // "mirror" of an output that is gone: place it normally
					known = false
				}
				if known {
					s.outLayout.Add(output, newX, newY)
					positioned = true
					log.Printf("Output %s: restored layout [%s %s] at (%d,%d)\n",
						output.Name(), entry.Position, entry.RelativeTo, newX, newY)
				}
			}
		}
	}
	if !positioned {
		// Use explicit Add instead of AddAuto to avoid wlr_output_layout
		// auto-reconfigure moving other outputs when this one is placed.
		autoX := 0
		for _, o := range s.outputs {
			geo := s.getOutputGeometry(o)
			right := geo.x + geo.width
			if right > autoX {
				autoX = right
			}
		}
		s.outLayout.Add(output, autoX, 0)
	}
	s.outputs = append(s.outputs, out)

	// Normalize positions to avoid negative coordinates (e.g. from "above" layout)
	// This also updates cached layoutX/layoutY and scene output positions for ALL outputs.
	s.normalizeOutputPositions()

	// Query the (now normalized) layout position
	lo := s.outLayout.Get(output)
	out.layoutX = lo.X()
	out.layoutY = lo.Y()
	C.scene_output_set_position(sceneOutput, C.int(out.layoutX), C.int(out.layoutY))

	// Load cursor for this output's scale
	scale := float64(output.Scale())
	log.Printf("[CURSOR] Output %s: loading cursor at scale=%.2f (effective=%dpx)\n",
		output.Name(), scale, int(24*scale))
	s.cursorMgr.Load(scale)
	s.cursor.SetXCursor(s.cursorMgr, "default")

	// Setup event loop wakeup for the primary output (allows goroutines to
	// trigger frame processing by writing to the eventfd).
	if s.wakeupFd.Load() == 0 {
		s.setupWakeup(output)
	}

	// Register a per-output frame callback using an individually allocated
	// wl_listener. Each output MUST have its own listener — a single static
	// listener would be moved to the last output on wl_signal_add, leaving
	// earlier outputs with no frame callbacks (black screen).
	out.frameListener = unsafe.Pointer(
		C.create_frame_listener(displayPtr(s.display), outputPtr(output)))

	// Handle output disconnect.
	out.listeners.Add(output.OnDestroy(func(output wlr.Output) {
		s.handleOutputDestroy(out)
	}))

	// Nested backends (Wayland/X11 window) ask for a new state when the host
	// window is resized or on first configure; apply it and re-layout.
	out.listeners.Add(output.OnRequestState(func(o wlr.Output, st *wlr.OutputState) {
		if !o.CommitState(st) {
			log.Printf("Output %s: backend-requested state rejected\n", o.Name())
			return
		}
		s.handleOutputReconfigured(out)
	}))

	// Write compositor state for panel to read available modes
	s.writeCompositorState()

	// Reposition panel (primary may have changed due to restored layout config)
	s.repositionPanel()
	s.repositionSecondaryPanels()
	if s.panelPending {
		s.startPanel()
	}

	// Restore VRR setting from layout config
	if config != nil {
		if entry, ok := config.Layouts[output.Name()]; ok && entry.AdaptiveSync != nil && *entry.AdaptiveSync {
			s.enableAdaptiveSync(out, true)
			s.writeCompositorState() // Update VRR status in IPC
		}
	}

	// Load wallpaper for this output if settings are already loaded
	s.loadWallpaperForNewOutput(out)
	if s.nightLight.enabled {
		s.applyNightLightToOutput(out) // a screen plugged in at night
	}

	// Start boot sequence animation on first output
	if len(s.outputs) == 1 {
		s.startBootSequence()
	} else {
		// A monitor was hotplugged into a running session: re-fit existing
		// windows so they can reclaim space on the new output layout, then
		// put them where they were with these screens.
		s.refitWindowsToOutputs()
		s.screensChanged()
	}
	s.coverNewOutputWhileLocked(out)
}

// enableOutputWithBestMode enables the output with its preferred mode, falling
// back to the other advertised modes (then to the backend default) when the
// commit is rejected, e.g. because of GPU bandwidth limits.
func (s *server) enableOutputWithBestMode(out *outputState, preferred wlr.OutputMode) {
	output := out.output
	try := func(mode wlr.OutputMode) bool {
		return commitOutput(output, func(st *wlr.OutputState) {
			st.SetEnabled(true)
			if mode.Valid() {
				st.SetMode(mode)
			}
		})
	}
	if preferred.Valid() && try(preferred) {
		return
	}
	for i, mode := range out.modes {
		if try(mode) {
			out.currentMode = i
			log.Printf("Output %s: preferred mode rejected, using %dx%d@%dmHz\n",
				output.Name(), mode.Width(), mode.Height(), mode.RefreshRate())
			return
		}
	}
	if len(out.modes) == 0 && try(wlr.OutputMode{}) {
		return
	}
	log.Printf("Output %s: could not enable any mode\n", output.Name())
}

// handleOutputReconfigured refreshes everything derived from an output's size
// after the backend changed it (nested window resized).
func (s *server) handleOutputReconfigured(out *outputState) {
	w, h := out.output.EffectiveResolution()
	if w == out.width && h == out.height {
		return
	}
	log.Printf("Output %s reconfigured: %dx%d -> %dx%d\n", out.output.Name(), out.width, out.height, w, h)
	s.outputResized(out, true)
}

// handleOutputDestroy removes a disconnected output from the server.
// Called from two paths:
//  1. The OnDestroy listener — wlroots is tearing the output down;
//  2. disableOutput — the wlr_output is still alive; we clean up our
//     side preemptively so its later destruction doesn't reach us.
//
// Either way every listener attached to the output is removed here (wlroots
// asserts that none is left when it frees the output).
func (s *server) handleOutputDestroy(out *outputState) {
	wasPrimary := s.primaryOutput() == out

	out.listeners.DestroyAll()
	s.forgetZoomOutput(unsafe.Pointer(outputPtr(out.output)))

	// Clean up per-output frame listener
	if out.frameListener != nil {
		C.destroy_frame_listener((*C.struct_wl_listener)(out.frameListener))
		out.frameListener = nil
	}

	// Clean up per-output wallpaper buffers and animation state
	s.clearAnimWallpaper(out)
	if out.wallpaperBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(out.wallpaperBuf)
		C.scene_node_destroy(&sceneBuf.node)
		out.wallpaperBuf = nil
	}
	if out.wallpaperPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(out.wallpaperPixBuf)
		C.pixel_buffer_release(pixBuf)
		out.wallpaperPixBuf = nil
	}

	// Clear cursor output tracking if it was this output
	if s.lastCursorOutput == out {
		s.lastCursorOutput = nil
	}

	// Remove from outputs list
	for i, o := range s.outputs {
		if o == out {
			s.outputs = append(s.outputs[:i], s.outputs[i+1:]...)
			break
		}
	}

	// Migrate windows from the destroyed output to the primary, preserving their
	// relative position. Fullscreen windows are skipped here and re-fitted by
	// refitWindowsToOutputs below; size clamping (so windows clear the widget
	// panel) is also handled there.
	if p := s.primaryOutput(); p != nil {
		pGeo := s.getOutputGeometry(p)
		outGeo := outputGeometry{x: out.layoutX, y: out.layoutY, width: out.width, height: out.height}
		onOldOutput := func(x, y float64) bool {
			return int(x) >= outGeo.x && int(x) < outGeo.x+outGeo.width &&
				int(y) >= outGeo.y && int(y) < outGeo.y+outGeo.height
		}

		for _, v := range s.xdgViews {
			if v.mapped && !v.fullscreen && onOldOutput(v.x, v.y) {
				v.x = float64(pGeo.x) + (v.x - float64(outGeo.x))
				v.y = float64(pGeo.y) + (v.y - float64(outGeo.y))
			}
		}
		for _, v := range s.xwayViews {
			if v.mapped && !v.isPanel && !v.isOverlay && !v.fullscreen && onOldOutput(v.x, v.y) {
				v.x = float64(pGeo.x) + (v.x - float64(outGeo.x))
				v.y = float64(pGeo.y) + (v.y - float64(outGeo.y))
			}
		}

		// Restart panel if primary was destroyed (new primary may have different dimensions)
		if wasPrimary {
			s.restartPanel()
		}
	}

	// Clean up lock surface state for this output (prevents use-after-free on unlock)
	outName := out.output.Name()
	if ls, ok := s.lockSurfaceStates[outName]; ok {
		if ls.sceneTree != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(ls.sceneTree).node)
		}
		delete(s.lockSurfaceStates, outName)
	}
	if rect, ok := s.lockBlackRects[outName]; ok {
		if rect != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_rect)(rect).node)
		}
		delete(s.lockBlackRects, outName)
	}

	// Re-normalize remaining outputs so positions start at (0,0)
	if len(s.outputs) > 0 {
		s.normalizeOutputPositions()
	}

	// Re-fit all windows to the remaining outputs (panel already restarted above
	// so contentBounds reflects the new primary layout).
	s.refitWindowsToOutputs()
	s.screensChanged()

	log.Printf("Output %s disconnected, %d outputs remaining\n", outName, len(s.outputs))

	// Outputs mirroring this one rejoin the desktop.
	for _, m := range s.mirrors {
		if m.source == out {
			s.stopMirror(m, true, true)
		}
	}
	s.writeCompositorState()
}

// repositionPanel moves the panel to the current primary output.
// It updates both the XWayland surface configuration (client-side position)
// AND the scene tree node position (compositor-side rendering position).
//
// Strategy: move panelTree itself to the output position, keep panel view at (0,0).
// This ensures the panel layer always aligns with the primary output.
func (s *server) repositionPanel() {
	if s.panelXway == nil || !s.panelXway.mapped {
		log.Printf("[PANEL] repositionPanel: skipped (panelXway=%v mapped=%v)\n",
			s.panelXway != nil, s.panelXway != nil && s.panelXway.mapped)
		return
	}
	p := s.primaryOutput()
	if p == nil {
		log.Println("[PANEL] repositionPanel: no primary output!")
		return
	}
	pGeo := s.getOutputGeometry(p)
	log.Printf("[PANEL] repositionPanel: output=%s geo=(%d,%d %dx%d) sceneTree=%v\n",
		p.output.Name(), pGeo.x, pGeo.y, pGeo.width, pGeo.height, s.panelXway.sceneTree != nil)

	// Tell XWayland client its position and size
	s.panelXway.surface.Configure(int16(pGeo.x), int16(pGeo.y),
		uint16(pGeo.width), uint16(pGeo.height))

	// Move panelTree layer to primary output position
	if s.panelTree != nil {
		panelTreeC := (*C.struct_wlr_scene_tree)(s.panelTree)
		C.scene_node_set_position(&panelTreeC.node, C.int(pGeo.x), C.int(pGeo.y))
	}
	// Panel view tree stays at (0,0) within panelTree
	s.panelXway.x = float64(pGeo.x)
	s.panelXway.y = float64(pGeo.y)
	setViewScenePosition(s.panelXway.sceneTree, 0, 0)
}

// repositionSecondaryPanel positions a secondary bar window on its target output.
func (s *server) repositionSecondaryPanel(outputName string, v *xwayView) {
	if v == nil || !v.mapped {
		return
	}
	// Find the output by name
	var target *outputState
	for _, out := range s.outputs {
		if out.output.Name() == outputName {
			target = out
			break
		}
	}
	if target == nil {
		log.Printf("[PANEL] repositionSecondaryPanel: output %q not found", outputName)
		return
	}
	geo := s.getOutputGeometry(target)
	log.Printf("[PANEL] repositionSecondaryPanel: output=%s geo=(%d,%d %dx%d)",
		outputName, geo.x, geo.y, geo.width, geo.height)

	// Scene position is relative to panelTree (positioned at the primary output).
	// Subtract panelTree's position so the secondary bar lands on the correct output.
	pGeo := s.getOutputGeometry(s.primaryOutput())
	sceneX := geo.x - pGeo.x
	sceneY := geo.y - pGeo.y

	// For bottom bar, position at the bottom of the output (bar-sized window)
	if s.barPosition == "bottom" {
		barH := int(float64(s.launcherIconSize)*s.launcherZoomScale) + 10
		configY := geo.y + geo.height - barH
		v.surface.Configure(int16(geo.x), int16(configY),
			uint16(geo.width), uint16(barH))
		v.x = float64(geo.x)
		v.y = float64(configY)
		setViewScenePosition(v.sceneTree, sceneX, sceneY+geo.height-barH)
	} else {
		v.surface.Configure(int16(geo.x), int16(geo.y),
			uint16(geo.width), uint16(geo.height))
		v.x = float64(geo.x)
		v.y = float64(geo.y)
		setViewScenePosition(v.sceneTree, sceneX, sceneY)
	}
	log.Printf("[PANEL] repositionSecondaryPanel: scenePos=(%d,%d) barPos=%s",
		sceneX, sceneY, s.barPosition)
}

// repositionSecondaryPanels repositions all secondary bar windows.
func (s *server) repositionSecondaryPanels() {
	for name, v := range s.secondaryPanels {
		s.repositionSecondaryPanel(name, v)
	}
}

// normalizeOutputPositions shifts all outputs so the minimum X,Y is (0,0).
// This avoids negative coordinates which can cause issues with scene rendering.
// It updates the wlr_output_layout, scene outputs, wallpapers, and cached positions.
func (s *server) normalizeOutputPositions() {
	if len(s.outputs) == 0 {
		return
	}

	// Find minimum X and Y across all outputs (initialize from first output)
	lo0 := s.outLayout.Get(s.outputs[0].output)
	minX, minY := lo0.X(), lo0.Y()
	for _, o := range s.outputs[1:] {
		lo := s.outLayout.Get(o.output)
		x, y := lo.X(), lo.Y()
		if x < minX {
			minX = x
		}
		if y < minY {
			minY = y
		}
	}

	// If minimum is already (0,0), just update cached positions
	if minX == 0 && minY == 0 {
		for _, o := range s.outputs {
			lo := s.outLayout.Get(o.output)
			o.layoutX = lo.X()
			o.layoutY = lo.Y()
			sceneOutput := (*C.struct_wlr_scene_output)(o.sceneOutput)
			C.scene_output_set_position(sceneOutput, C.int(o.layoutX), C.int(o.layoutY))
			if o.wallpaperBuf != nil {
				sceneBuf := (*C.struct_wlr_scene_buffer)(o.wallpaperBuf)
				C.scene_node_set_position(&sceneBuf.node, C.int(o.layoutX), C.int(o.layoutY))
			}
		}
		return
	}

	// Shift all outputs so minimum is (0,0)
	log.Printf("[LAYOUT] Normalizing positions: shifting by (%d,%d)\n", -minX, -minY)
	for _, o := range s.outputs {
		lo := s.outLayout.Get(o.output)
		newX := lo.X() - minX
		newY := lo.Y() - minY
		s.outLayout.Add(o.output, newX, newY)
	}

	// Update all cached positions, scene outputs, and wallpapers
	for _, o := range s.outputs {
		lo := s.outLayout.Get(o.output)
		o.layoutX = lo.X()
		o.layoutY = lo.Y()
		sceneOutput := (*C.struct_wlr_scene_output)(o.sceneOutput)
		C.scene_output_set_position(sceneOutput, C.int(o.layoutX), C.int(o.layoutY))
		if o.wallpaperBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(o.wallpaperBuf)
			C.scene_node_set_position(&sceneBuf.node, C.int(o.layoutX), C.int(o.layoutY))
		}
		log.Printf("[LAYOUT] Output %s → (%d,%d)\n", o.output.Name(), o.layoutX, o.layoutY)
	}

	// The windows and the panel move with their screens.
	s.shiftViews(float64(-minX), float64(-minY))
	s.repositionPanel()
	s.repositionSecondaryPanels()
}

// shiftViews moves every window by (dx, dy) in the layout, as the screens
// under them moved: where they were saved to be restored, too.
func (s *server) shiftViews(dx, dy float64) {
	s.shiftViewsOn(nil, dx, dy)
}

// shiftViewsOn is shiftViews for the windows on one screen (by its
// geometry), or all of them if on is nil.
func (s *server) shiftViewsOn(on *outputGeometry, dx, dy float64) {
	inside := func(x, y float64) bool {
		return on == nil || (int(x) >= on.x && int(x) < on.x+on.width && int(y) >= on.y && int(y) < on.y+on.height)
	}
	for _, v := range s.xdgViews {
		if !inside(v.x, v.y) {
			continue
		}
		v.x, v.y = v.x+dx, v.y+dy
		v.savedX, v.savedY = v.savedX+dx, v.savedY+dy
		if v.mapped {
			setXdgScenePos(v)
		}
	}
	for _, v := range s.xwayViews {
		if v.isPanel || !inside(v.x, v.y) {
			continue // the panel: repositionPanel
		}
		v.x, v.y = v.x+dx, v.y+dy
		v.savedX, v.savedY = v.savedX+dx, v.savedY+dy
		if v.mapped {
			setXwayScenePos(v)
			v.surface.Configure(int16(v.x), int16(v.y), uint16(v.surface.Width()), uint16(v.surface.Height()))
		}
	}
}
