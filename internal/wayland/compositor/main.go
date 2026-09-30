// Package compositor implements a Wayland compositor using wlroots bindings.
// The Tyde UI runs as a separate Fyne client application through XWayland.
package compositor

/*
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wayland-server-core.h>
#include <wlr/xwayland.h>
#include <wlr/types/wlr_compositor.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/gles2.h>
#include <wlr/render/egl.h>
#include <wlr/types/wlr_data_device.h>
#include <wlr/types/wlr_primary_selection.h>
#include <EGL/egl.h>
#include <wlr/types/wlr_screencopy_v1.h>
#include <wlr/types/wlr_presentation_time.h>

// wp_presentation_time: lets clients (video players, browsers) sync to vblank.
// Without this, mpv/Firefox fall back to timer heuristics and stutter. The
// scene reports presentation feedback for its surfaces automatically.
static void create_presentation(struct wl_display *display,
                                struct wlr_backend *backend) {
	wlr_presentation_create(display, backend, 2);
}

// --- Drag and drop support ---
// Auto-accept client drag requests so DnD works in XWayland apps (Firefox, etc.)

static struct wl_listener request_start_drag_listener;
static struct wl_listener start_drag_listener;
static struct wl_listener drag_destroy_listener;
static struct wlr_scene_tree *drag_icon_scene_tree = NULL;
static struct wlr_scene_tree *drag_icon_tree_node = NULL;

static void handle_request_start_drag(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_start_drag_event *event = data;
	struct wlr_seat *seat = event->drag->seat;

	// Try pointer grab serial validation first (most common case).
	if (wlr_seat_validate_pointer_grab_serial(seat, event->origin, event->serial)) {
		wlr_seat_start_pointer_drag(seat, event->drag, event->serial);
		return;
	}
	struct wlr_touch_point *point;
	if (wlr_seat_validate_touch_grab_serial(seat, event->origin, event->serial, &point)) {
		wlr_seat_start_touch_drag(seat, event->drag, event->serial, point);
		return;
	}
	// XWayland clients (Firefox, Chrome) may produce serials that don't
	// pass validation, as XWayland synthesises them independently of the
	// compositor's serial tracker. Their drags are still started, but only
	// while a button is held on a surface of the client asking: any other
	// client would otherwise take the pointer whenever it likes.
	struct wlr_seat_pointer_state *ps = &seat->pointer_state;
	if (ps->button_count > 0 && event->origin && ps->focused_surface &&
		wl_resource_get_client(event->origin->resource) ==
			wl_resource_get_client(ps->focused_surface->resource)) {
		wlr_seat_start_pointer_drag(seat, event->drag, event->serial);
		return;
	}
	if (event->drag->source) {
		wlr_data_source_destroy(event->drag->source);
	}
}

static void handle_drag_destroy(struct wl_listener *listener, void *data) {
	drag_icon_tree_node = NULL;
	wl_list_remove(&drag_destroy_listener.link);
	wl_list_init(&drag_destroy_listener.link);
}

static void handle_start_drag(struct wl_listener *listener, void *data) {
	struct wlr_drag *drag = data;
	if (drag->icon && drag_icon_scene_tree) {
		drag_icon_tree_node = wlr_scene_drag_icon_create(drag_icon_scene_tree, drag->icon);
	} else {
		drag_icon_tree_node = NULL;
	}
	// Listen for drag end to clear the icon pointer before wlroots frees it
	drag_destroy_listener.notify = handle_drag_destroy;
	wl_signal_add(&drag->events.destroy, &drag_destroy_listener);
}

static void setup_drag_handlers(struct wlr_seat *seat, struct wlr_scene_tree *icon_tree) {
	drag_icon_scene_tree = icon_tree;
	wl_list_init(&drag_destroy_listener.link);
	request_start_drag_listener.notify = handle_request_start_drag;
	wl_signal_add(&seat->events.request_start_drag, &request_start_drag_listener);
	start_drag_listener.notify = handle_start_drag;
	wl_signal_add(&seat->events.start_drag, &start_drag_listener);
}

static void update_drag_icon_position(int x, int y) {
	if (drag_icon_tree_node) {
		wlr_scene_node_set_position(&drag_icon_tree_node->node, x, y);
	}
}


// --- Clipboard/Selection support ---
// Approve client clipboard requests so copy/paste works between applications.

#include <unistd.h>
extern void goClipboardChanged(int fd);

static struct wlr_seat *selection_seat = NULL;
static struct wl_listener request_set_selection_listener;
static struct wl_listener request_set_primary_selection_listener;

static void handle_request_set_selection(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_set_selection_event *event = data;
	wlr_seat_set_selection(selection_seat, event->source, event->serial);

	// Capture clipboard text content for history
	if (!event->source) return;

	const char *preferred_mime = NULL;
	char **p;
	wl_array_for_each(p, &event->source->mime_types) {
		if (*p && strcmp(*p, "x-kde-passwordManagerHint") == 0) {
			return; // a password manager asks to be left out of any history
		}
	}
	wl_array_for_each(p, &event->source->mime_types) {
		if (*p) {
			if (strcmp(*p, "text/plain;charset=utf-8") == 0) {
				preferred_mime = "text/plain;charset=utf-8";
				break;
			}
			if (!preferred_mime && (strcmp(*p, "text/plain") == 0 || strcmp(*p, "UTF8_STRING") == 0)) {
				preferred_mime = *p;
			}
		}
	}
	if (!preferred_mime) return;

	int fds[2];
	if (pipe(fds) != 0) return;

	wlr_data_source_send(event->source, preferred_mime, fds[1]);
	close(fds[1]);

	goClipboardChanged(fds[0]);
}

static void handle_request_set_primary_selection(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_set_primary_selection_event *event = data;
	wlr_seat_set_primary_selection(selection_seat, event->source, event->serial);
}

static void setup_selection_handlers(struct wlr_seat *seat) {
	selection_seat = seat;
	request_set_selection_listener.notify = handle_request_set_selection;
	wl_signal_add(&seat->events.request_set_selection, &request_set_selection_listener);
	request_set_primary_selection_listener.notify = handle_request_set_primary_selection;
	wl_signal_add(&seat->events.request_set_primary_selection, &request_set_primary_selection_listener);
}

// The seat asserts that its signals have no listener left when it is
// destroyed (with the display): detach the drag and selection handlers first.
static struct wl_listener seat_destroy_listener;

static void handle_seat_destroy(struct wl_listener *listener, void *data) {
	wl_list_remove(&request_start_drag_listener.link);
	wl_list_remove(&start_drag_listener.link);
	wl_list_remove(&request_set_selection_listener.link);
	wl_list_remove(&request_set_primary_selection_listener.link);
	wl_list_remove(&seat_destroy_listener.link);
	selection_seat = NULL;
	drag_icon_scene_tree = NULL;
}

static void setup_seat_handlers(struct wlr_seat *seat, struct wlr_scene_tree *icon_tree) {
	setup_drag_handlers(seat, icon_tree);
	setup_selection_handlers(seat);
	seat_destroy_listener.notify = handle_seat_destroy;
	wl_signal_add(&seat->events.destroy, &seat_destroy_listener);
}


// --- wlr_scene wrappers ---

static struct wlr_scene *create_scene(void) {
	return wlr_scene_create();
}

// The renderer's EGL display/context, used for off-frame GL operations
// (thumbnail capture). Set from the renderer (NO_* when it is not GLES2).
// Non-static: shared across CGO compilation units (switcher.go reads).
EGLDisplay g_egl_display = EGL_NO_DISPLAY;
EGLContext g_egl_context = EGL_NO_CONTEXT;

static void set_egl_from_renderer(struct wlr_renderer *renderer) {
	g_egl_display = EGL_NO_DISPLAY;
	g_egl_context = EGL_NO_CONTEXT;
	if (renderer == NULL || !wlr_renderer_is_gles2(renderer)) {
		return;
	}
	struct wlr_egl *egl = wlr_gles2_renderer_get_egl(renderer);
	if (egl == NULL) {
		return;
	}
	g_egl_display = wlr_egl_get_display(egl);
	g_egl_context = wlr_egl_get_context(egl);
}

// Tree nodes
static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
	return wlr_scene_tree_create(parent);
}

static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
	wlr_scene_node_set_enabled(node, enabled);
}

static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}

static void scene_node_raise_to_top(struct wlr_scene_node *node) {
	wlr_scene_node_raise_to_top(node);
}

static void compositor_set_renderer(struct wlr_compositor *compositor, struct wlr_renderer *renderer) {
	wlr_compositor_set_renderer(compositor, renderer);
}
*/
import "C"

import (
	_ "embed"
	"image/color"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/wlipc"
)

//go:embed default_bg.png
var defaultBgPNG []byte

// C pointer accessors for the cgo preambles of this package. The wlr types
// expose their underlying pointer as unsafe.Pointer; these helpers give it the
// C type of this package's cgo namespace.

func outputPtr(o wlr.Output) *C.struct_wlr_output {
	return (*C.struct_wlr_output)(o.Ptr())
}

func displayPtr(d wlr.Display) *C.struct_wl_display {
	return (*C.struct_wl_display)(d.Ptr())
}

func backendPtr(b wlr.Backend) *C.struct_wlr_backend {
	return (*C.struct_wlr_backend)(b.Ptr())
}

func seatPtr(seat wlr.Seat) *C.struct_wlr_seat {
	return (*C.struct_wlr_seat)(seat.Ptr())
}

func cursorPtr(c wlr.Cursor) *C.struct_wlr_cursor {
	return (*C.struct_wlr_cursor)(c.Ptr())
}

func keyboardPtr(k wlr.Keyboard) *C.struct_wlr_keyboard {
	return (*C.struct_wlr_keyboard)(k.Ptr())
}

// getOutputPhysSize returns the physical dimensions in mm from the wlr_output.
func getOutputPhysSize(out *outputState) (int, int) {
	return out.output.PhysicalSize()
}

// restackXwaylandSurfaceAbove puts an XWayland surface on top of the X11
// stacking order (keeps XWayland input routing in sync with the scene).
func restackXwaylandSurfaceAbove(surface wlr.XwaylandSurface) {
	surface.RestackAbove()
}

// getXdgToplevelAppID returns the app_id of an XDG toplevel (e.g. "org.mozilla.firefox").
func getXdgToplevelAppID(toplevel wlr.XDGToplevel) string {
	return toplevel.AppID()
}

// getXwaylandSurfaceClass returns the WM_CLASS of an XWayland surface.
func getXwaylandSurfaceClass(surface wlr.XwaylandSurface) string {
	return surface.Class()
}

// --- Scene graph helpers ---

// surfaceFromCPtr constructs a wlr.Surface from a C pointer (reverse of surfacePtr).
func surfaceFromCPtr(p *C.struct_wlr_surface) wlr.Surface {
	return wlr.SurfaceFromPtr(unsafe.Pointer(p))
}

// colorToFloat4 converts a color.RGBA to a [4]float32 suitable for wlr_scene_rect.
func colorToFloat4(c color.RGBA) [4]C.float {
	return [4]C.float{
		C.float(float32(c.R) / 255),
		C.float(float32(c.G) / 255),
		C.float(float32(c.B) / 255),
		C.float(float32(c.A) / 255),
	}
}

func xdgSurfacePtr(s wlr.XDGSurface) *C.struct_wlr_xdg_surface {
	return (*C.struct_wlr_xdg_surface)(s.Ptr())
}

func surfacePtr(s wlr.Surface) *C.struct_wlr_surface {
	return (*C.struct_wlr_surface)(s.Ptr())
}

func xdgPopupPtr(p wlr.XDGPopup) *C.struct_wlr_xdg_popup {
	return (*C.struct_wlr_xdg_popup)(p.Ptr())
}

// setViewSceneEnabled toggles the enabled state of a view's scene tree.
func setViewSceneEnabled(sceneTree unsafe.Pointer, enabled bool) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	val := C.int(0)
	if enabled {
		val = 1
	}
	C.scene_node_set_enabled(&tree.node, val)
}

// setViewScenePosition sets the position of a view's scene tree.
func setViewScenePosition(sceneTree unsafe.Pointer, x, y int) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	C.scene_node_set_position(&tree.node, C.int(x), C.int(y))
}

// updateDragIconPos moves the icon of the client drag in progress (if any).
func (s *server) updateDragIconPos(x, y int) {
	C.update_drag_icon_position(C.int(x), C.int(y))
}

// raiseViewSceneToTop raises a view's scene tree to the top of its parent.
func raiseViewSceneToTop(sceneTree unsafe.Pointer) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	C.scene_node_raise_to_top(&tree.node)
}

func init() {
	// Pin the main goroutine to a single OS thread. EGL requires all calls
	// (eglMakeCurrent, rendering) to happen on the same thread that created
	// the context. Without this, Go's scheduler may migrate the goroutine
	// to a different OS thread, causing EGL_BAD_ACCESS on DRM backends.
	runtime.LockOSThread()
}

// wlrDebugEnabled reports whether wlroots-level debug logging should be turned
// on. True if TYDE_WLR_DEBUG=1 or the marker file ~/.config/tyde/wlr-debug
// exists. The marker file is the reliable toggle for display-manager-launched
// sessions, which receive no command-line flags or environment overrides.
func wlrDebugEnabled() bool {
	if os.Getenv("TYDE_WLR_DEBUG") == "1" {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(home + "/.config/tyde/wlr-debug")
	return err == nil
}

// Run starts the Wayland compositor. It takes over the calling goroutine
// and does not return until the compositor shuts down.
func Run() {
	// Reduce GC frequency to avoid pauses on the main thread (locked to OS
	// thread for EGL). Default GOGC=100 can cause 10-50ms stalls; 200 halves
	// the GC rate at the cost of ~2x heap headroom.
	debug.SetGCPercent(200)

	// Settings written before the project became Tyde (~/.config/fynedesk,
	// Fyne app ID com.fyshos.fynedesk) are carried over once.
	wlipc.MigrateLegacyConfig()

	// Opt-in wlroots-level debug logging (very verbose). Routes wlroots'
	// internal WLR_DEBUG output (e.g. xdg_popup grab dismissal reasons) to
	// stderr -> compositor.log. Gated so normal sessions stay quiet: enabled
	// by TYDE_WLR_DEBUG=1 OR by the marker file ~/.config/tyde/wlr-debug
	// (the marker is the reliable trigger for DM-launched sessions, which get
	// no flags or env). Create/remove the file then re-login to toggle.
	if wlrDebugEnabled() {
		wlr.InitLog(wlr.Debug)
		log.Println("[DIAG] wlroots debug logging enabled")
	}

	// Handle signals for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	srv := newServer()
	srv.useOwnSocketWhenNested()
	srv.initBackend()
	srv.initScene()
	srv.initProtocols()
	srv.initInput()

	// Load user settings BEFORE starting the backend so that s.backgroundType,
	// keybindings, and other fields are ready when the first output arrives
	// (handleNewOutput fires synchronously during backend.Start).
	// Wallpaper image loading is a no-op here (no outputs yet) but will be
	// handled by loadWallpaperForNewOutput when the output appears.
	srv.loadSettings()
	srv.initHotCorners()
	srv.loadClipboardHistory()

	socket := srv.startBackend()

	log.Println("Keybindings: Alt+Escape=quit, Alt+Tab=cycle, F11=fullscreen, Alt+F4=close")
	log.Println("             Ctrl+Alt+Left/Right=switch desktop, Super+1-4=goto desktop")
	log.Println("             Super+T/Return=terminal, Super+`=dropdown terminal")
	log.Println("             PrintScreen=screenshot, Shift+PrintScreen=region screenshot")
	log.Println("             Super+L=lock screen, Ctrl+Alt+Backspace=emergency logout")

	// Set the socket in environment for child processes
	os.Setenv("WAYLAND_DISPLAY", socket)

	// Override XDG_CURRENT_DESKTOP:
	// - "Tyde" identifies our desktop for xdg-open fallback
	// - "wlroots" enables xdg-desktop-portal-wlr backend (ScreenCast, Screenshot)
	//   which Firefox/Chrome use for WebRTC camera/screen sharing via PipeWire
	os.Setenv("XDG_CURRENT_DESKTOP", "Tyde:wlroots")

	// Enable Firefox/Thunderbird to run as native Wayland clients instead of XWayland.
	// This gives better PipeWire integration for WebRTC (camera, mic, screen sharing).
	os.Setenv("MOZ_ENABLE_WAYLAND", "1")

	// Create XDG portal configuration so the portal daemon uses the wlr backend
	// for ScreenCast/Screenshot and GTK backend as fallback for everything else.
	srv.setupPortalConfig()

	// Start gnome-keyring-daemon so apps (Slack, Chrome, etc.) can persist credentials.
	// Only in real session mode — in nested mode the parent session provides it.
	srv.startKeyring()

	// Push our environment to D-Bus so portals (used by Snap/Flatpak apps) and
	// D-Bus-activated services use our DISPLAY/WAYLAND_DISPLAY when opening URLs etc.
	go srv.updateActivationEnvironment()

	// Start the panel process after XWayland is ready
	go srv.startPanelAndRestoreSession()

	// Write initial keyboard layout state for the panel
	if len(srv.keyboardLayouts) > 0 {
		state := wlipc.KeyboardLayoutState{
			ActiveIndex: srv.activeLayoutIndex,
			Layouts:     srv.keyboardLayouts,
		}
		_ = wlipc.NotifyKeyboardLayoutState(state)
	}

	// Restore saved volume level
	srv.restoreVolume()

	// If a previous compositor instance died (crash, kill -9) while the screen
	// was locked, re-lock immediately. Defends against an attacker killing the
	// compositor to bypass the lock screen.
	if srv.wasPreviouslyLocked() {
		log.Println("[LOCK] Previous instance was locked — locking screen immediately on startup")
		srv.idleLocked = true
		go srv.lockScreen()
	}

	// In nested mode, start a private D-Bus session so that child processes
	// (panel, apps) use their own bus instead of the host session bus.
	// This must happen BEFORE registering any D-Bus services.
	srv.startPrivateDBus()

	// Start D-Bus services
	srv.startScreenSaverDBus()
	srv.startNotificationsDBus()
	srv.startPortalDBus()

	// Background IPC flusher: serialize + write windows state off the render thread
	srv.startIPCFlusher()

	// Watch for mode change requests from panel
	go srv.watchModeRequests()
	go srv.watchIdleTimeout()
	go srv.watchSuspendResume()

	// Watchdog: detect event loop stalls and attempt recovery
	go srv.watchdogRecovery()

	// Start UNIX socket IPC server (alongside file-based IPC)
	srv.startSocketIPC()

	// Handle clean shutdown
	go srv.shutdownOnSignal(sigChan)

	// Run event loop
	srv.display.Run()

	srv.finishRun()
}

// newServer creates the server with its default state.
func newServer() *server {
	srv := &server{
		currentDesk:       0,
		numDesks:          4, // Default to 4 virtual desktops
		lastInputTime:     time.Now(),
		wmModifier:        wlr.KeyboardModifierLogo, // Default: Super key
		nestedMode:        os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != "",
		mainThreadActions: make(chan func(), 64),
		shutdown:          make(chan struct{}),
		mirrors:           map[string]*mirrorState{},
		disabledOutputs:   map[string]*outputState{},
		attentionTitles:   map[string]bool{},
		glows:             map[any]*glow{},
		shadows:           map[any]*glow{},
		wobblyWindows:     true,
		numLockOn:         true,
		numLockPref:       true,
		blurBehind:        true,
		windowShadows:     true,
		clockFormat:       "12h", // as the panel shows it by default
	}
	clipServer = srv
	srv.initPowerDefaults()
	return srv
}

// initBackend creates the display, backend, renderer, allocator and the
// core Wayland globals.
func (s *server) initBackend() {
	// Create Wayland display
	s.display = wlr.CreateDisplay()
	if !s.display.Valid() {
		log.Println("Failed to create the Wayland display")
		os.Exit(1)
	}

	// Create backend (auto-detects: nested wayland/x11 or DRM)
	s.backend, s.session = wlr.AutocreateBackend(s.display.EventLoop())
	if !s.backend.Valid() {
		log.Println("Failed to create backend")
		os.Exit(1)
	}

	// Create renderer
	s.renderer = wlr.AutocreateRenderer(s.backend)
	if !s.renderer.Valid() {
		log.Println("Failed to create renderer")
		os.Exit(1)
	}
	if !s.renderer.InitWLDisplay(s.display) {
		log.Println("Failed to initialize renderer buffer protocols")
		os.Exit(1)
	}
	C.set_egl_from_renderer((*C.struct_wlr_renderer)(s.renderer.Ptr()))

	// Create allocator
	s.allocator = wlr.AutocreateAllocator(s.backend, s.renderer)
	if !s.allocator.Valid() {
		log.Println("Failed to create allocator")
		os.Exit(1)
	}

	// Create compositor
	s.compositor = wlr.CreateCompositor(s.display, 6, s.renderer)
	if !s.compositor.Valid() {
		log.Println("Failed to create wl_compositor")
		os.Exit(1)
	}

	// GPU reset: the renderer is unusable and must be replaced.
	s.rendererLost = s.renderer.OnLost(s.handleRendererLost)

	// Create subcompositor for subsurfaces
	if !wlr.CreateSubcompositor(s.display) {
		log.Println("Failed to create wl_subcompositor")
		os.Exit(1)
	}

	// Create data device manager (required for clipboard/GTK apps)
	s.dataDeviceMgr = wlr.CreateDataDeviceManager(s.display)
	if !s.dataDeviceMgr.Valid() {
		log.Println("Failed to create wl_data_device_manager")
		os.Exit(1)
	}

	// Create primary selection manager (required for middle-click paste and XWayland clipboard bridge)
	wlr.CreatePrimarySelectionV1DeviceManager(s.display)

	// Create output layout
	s.outLayout = wlr.CreateOutputLayout(s.display)
	if !s.outLayout.Valid() {
		log.Println("Failed to create the output layout")
		os.Exit(1)
	}
}

// initScene creates the scene graph and its layer trees.
func (s *server) initScene() {
	// Create scene graph (wlr_scene handles damage tracking + rendering)
	s.scene = unsafe.Pointer(C.create_scene())
	if s.scene == nil {
		log.Println("Failed to create the scene")
		os.Exit(1)
	}
	scene := (*C.struct_wlr_scene)(s.scene)
	sceneTree := &scene.tree

	// wp_presentation_time: required for clients (mpv, browsers) to sync to
	// vblank. The scene wires per-surface feedback via wlr_scene_set_presentation.
	C.create_presentation(displayPtr(s.display), backendPtr(s.backend))

	// Create layer trees (render order: background < panel < windows < override < fullscreen < overlay < switcher)
	s.backgroundTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.panelTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.windowsTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.overrideTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.fullscreenTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.overlayTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.switcherTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.penTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.cursorAlertTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	s.lockTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	// Switcher is hidden by default
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.switcherTree).node, 0)
	// Felt-tip pen annotation layer hidden by default (enabled while ink exists)
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.penTree).node, 0)
	// Fullscreen layer hidden by default
	s.setFullscreenLayer(false)
	// Lock layer hidden by default (enabled when lock client connects)
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.lockTree).node, 0)
}

// initProtocols creates the protocol globals and the shell handlers.
func (s *server) initProtocols() {
	// Create XDG output manager (required by grim for output geometry)
	wlr.CreateXDGOutputManagerV1(s.display, s.outLayout)

	// Create screencopy manager (allows grim to capture screenshots)
	s.screencopyMgr = wlr.CreateScreencopyManagerV1(s.display)
	setupCapture(s)

	// Clipboard tools without a window (wlr/ext-data-control).
	setupDataControl(s)

	// Enable fractional scaling (wp_fractional_scale_v1 + wp_viewporter)
	s.setupFractionalScaling()

	// Create session lock manager (ext-session-lock-v1 for swaylock, etc.)
	setupSessionLock(s)

	// Create XDG activation manager (xdg-activation-v1 for focus stealing / urgency)
	setupXDGActivation(s)

	// Create text input / input method managers (text-input-v3 + input-method-v2 for IME)
	setupTextInput(s)

	// Create virtual keyboard manager (zwp_virtual_keyboard_v1) so on-screen
	// keyboards, accessibility tools, IME helpers, and QA automation (wtype,
	// dotool) can inject synthetic key events.
	setupVirtualKeyboard(s)

	// Create security context manager (wp_security_context_v1 for Flatpak sandboxing)
	setupSecurityContext(s)

	// Handle new outputs (monitors)
	s.listeners.Add(s.backend.OnNewOutput(s.handleNewOutput))

	// Create XDG shell for native Wayland windows
	s.xdgShell = wlr.CreateXDGShell(s.display, 3)
	s.listeners.Add(s.xdgShell.OnNewToplevel(s.handleNewXDGToplevel))
	s.listeners.Add(s.xdgShell.OnNewPopup(s.handleNewXDGPopup))

	// Create XDG decoration manager (mode negotiation lives in xdg.go)
	s.xdgDecoMgr = wlr.CreateXDGDecorationManagerV1(s.display)
	s.listeners.Add(s.xdgDecoMgr.OnNewToplevelDecoration(s.handleNewToplevelDecoration))
}

// initInput creates the seat, XWayland, the cursor and the input handlers.
func (s *server) initInput() {
	// Create seat for input BEFORE XWayland (XWayland needs the seat)
	s.seat = wlr.CreateSeat(s.display, "seat0")
	s.listeners.Add(s.seat.OnRequestSetCursor(s.handleSetCursorRequest))

	// Enable drag and drop (auto-accept client drag requests, render drag
	// icons) and clipboard (approve selection requests). Without this,
	// XWayland apps (Firefox, Chrome) silently fail all DnD operations.
	C.setup_seat_handlers(seatPtr(s.seat), (*C.struct_wlr_scene_tree)(s.overlayTree))

	// Create XWayland for X11 apps (including Fyne)
	s.xwayland = wlr.CreateXwayland(s.display, s.compositor, false)
	if s.xwayland.Valid() {
		s.listeners.Add(s.xwayland.OnNewSurface(s.handleNewXwaylandSurface))
		// Set the seat on XWayland - critical for input to work!
		s.xwayland.SetSeat(s.seat)
		log.Println("XWayland initialized with seat")
	} else {
		log.Println("Warning: XWayland not available")
	}

	// Create cursor — set XCURSOR_SIZE + XCURSOR_THEME so XWayland clients
	// (via libXcursor) use the same cursor size as the compositor. Without this,
	// libXcursor computes a default from the X screen height (16*H/480) which
	// can be very different in multi-output layouts.
	os.Setenv("XCURSOR_SIZE", "24")
	os.Setenv("XCURSOR_THEME", "Adwaita")
	s.cursor = wlr.CreateCursor()
	s.cursor.AttachOutputLayout(s.outLayout)
	s.cursorMgr = wlr.CreateXCursorManager("Adwaita", 24)

	// Handle input devices
	s.listeners.Add(s.backend.OnNewInput(s.handleNewInput))

	// Cursor events
	s.listeners.Add(s.cursor.OnMotion(s.handleCursorMotion))
	s.listeners.Add(s.cursor.OnMotionAbsolute(s.handleCursorMotionAbsolute))
	s.listeners.Add(s.cursor.OnButton(s.handleCursorButton))
	s.listeners.Add(s.cursor.OnAxis(s.handleCursorAxis))
	s.listeners.Add(s.cursor.OnFrame(s.handleCursorFrame))

	// Trackpad gesture support (swipe for desktop switching, overview, etc.)
	s.setupGestures()
}

// startBackend starts the backend and returns the Wayland socket name.
func (s *server) startBackend() string {
	// Start backend
	if err := s.backend.Start(); err != nil {
		log.Println("Failed to start backend:", err)
		os.Exit(1)
	}

	// Get socket name and print it
	socket, err := s.display.AddSocketAuto()
	if err != nil {
		log.Println("Failed to create Wayland socket:", err)
		os.Exit(1)
	}
	log.Printf("Tyde Wayland compositor running on WAYLAND_DISPLAY=%s\n", socket)

	// Set XWayland cursor
	if s.xwayland.Valid() {
		xdisplay := s.xwayland.DisplayName()
		log.Printf("XWayland display: %s\n", xdisplay)
		os.Setenv("DISPLAY", xdisplay)

		// Set cursor for XWayland
		s.cursorMgr.Load(1.0)
		xcursor := s.cursorMgr.GetXCursor("default", 1.0)
		if xcursor.ImageCount() > 0 {
			img := xcursor.Image(0)
			hx, hy := img.Hotspot()
			log.Printf("[CURSOR] XCursor 'default' at scale=1.0: actual image=%dx%d, hotspot=(%d,%d)\n",
				img.Width(), img.Height(), hx, hy)
			s.xwayland.SetCursor(img)
		}
	}

	return socket
}

// finishRun writes the shutdown marker and releases resources once the
// event loop has returned.
func (s *server) finishRun() {
	// Write shutdown marker so the runner knows this is an intentional exit.
	// If cleanup code crashes (segfault in wlroots), the runner won't restart.
	if s.shuttingDown.Load() {
		homeDir, _ := os.UserHomeDir()
		markerPath := filepath.Join(homeDir, ".cache", "fyne", "com.fyshos.tyde", "shutdown-marker")
		_ = os.MkdirAll(filepath.Dir(markerPath), 0o700)
		if err := atomicWriteFile(markerPath, []byte("shutdown")); err != nil {
			log.Printf("Warning: could not write shutdown marker: %v", err)
		}
	}

	// Cleanup
	if s.ipcServer != nil {
		s.ipcServer.Close()
	}
	if s.panelCmd != nil && s.panelCmd.Process != nil {
		s.panelCmd.Process.Kill()
	}
	s.stopPrivateDBus()
	s.teardown()

	// If we reached this point through a clean shutdown (logout/restart), the
	// user-initiated path: the lock-state marker is no longer relevant and
	// would otherwise re-lock the next session unnecessarily after gdm auth.
	// Crashes never reach here, so the marker survives those.
	s.markUnlocked()

	log.Println("Compositor terminated")

	// If a "restart" IPC arrived (instead of a clean shutdown), exit with the
	// runner's restart sentinel so tyde_runner relaunches us. Doing this
	// after Destroy() — instead of os.Exit(5) from the IPC handler — means
	// wlroots/X resources are released cleanly.
	if s.wantRestart.Load() {
		os.Exit(5)
	}
}

// screencastConfigMarker starts the xdg-desktop-portal-wlr configuration
// written by Tyde, which is only ever replaced while it still carries it.
const screencastConfigMarker = "# Written by Tyde"

// handleRendererLost replaces the renderer and allocator after a GPU reset
// (wlroots emits renderer.events.lost; the old renderer can't render anymore).
// Mirrors sway: create new ones, re-init every output, then drop the old ones.
func (s *server) handleRendererLost() {
	log.Println("[GPU] Renderer lost (GPU reset), recreating renderer")
	renderer := wlr.AutocreateRenderer(s.backend)
	if !renderer.Valid() {
		log.Println("[GPU] Failed to recreate renderer")
		return
	}
	allocator := wlr.AutocreateAllocator(s.backend, renderer)
	if !allocator.Valid() {
		log.Println("[GPU] Failed to recreate allocator")
		renderer.Destroy()
		return
	}

	oldRenderer, oldAllocator := s.renderer, s.allocator
	s.rendererLost.Destroy()
	s.renderer, s.allocator = renderer, allocator
	s.rendererLost = renderer.OnLost(s.handleRendererLost)

	C.compositor_set_renderer((*C.struct_wlr_compositor)(s.compositor.Ptr()),
		(*C.struct_wlr_renderer)(renderer.Ptr()))
	for _, out := range s.outputs {
		if !out.output.InitRender(allocator, renderer) {
			log.Printf("[GPU] Output %s: failed to re-init rendering\n", out.output.Name())
		}
	}

	// Textures and GL objects created with the old renderer die with it.
	s.destroySwitcherThumbnails()
	resetThumbGL()
	s.dropBlursGL()
	s.dropWobbleGL()
	s.dropTransitionGL()
	s.dropMatrixWallsGL()
	s.dropZoomBuffers()
	C.set_egl_from_renderer((*C.struct_wlr_renderer)(renderer.Ptr()))

	oldAllocator.Destroy()
	oldRenderer.Destroy()
	s.scheduleAllOutputFrames()
	log.Println("[GPU] Renderer recreated")
}

// teardown releases wlroots objects after the event loop returned.
//
// wlroots (0.19+) asserts that no listener is left on a signal when the
// object emitting it is destroyed, so the order matters: our global
// listeners go first, then X11 windows and Wayland clients are destroyed
// (their views remove their own listeners from the destroy handlers), then
// outputs and input devices with the backend, and finally the display with
// the protocol globals (whose C listeners detach on the globals' destroy
// events).
func (s *server) teardown() {
	s.listeners.DestroyAll()
	s.rendererLost.Destroy()

	if s.xwayland.Valid() {
		s.xwayland.Destroy()
	}
	s.display.DestroyClients()

	// Outputs are about to be destroyed with the backend: drop our per-output
	// hooks first so that no re-layout/panel logic runs during shutdown.
	for _, out := range s.outputs {
		out.listeners.DestroyAll()
		if out.frameListener != nil {
			destroyFrameListener(out.frameListener)
			out.frameListener = nil
		}
	}
	s.outputs = nil
	s.backend.Destroy()

	s.display.Destroy()
}
