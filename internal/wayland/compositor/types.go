// Package compositor implements a Wayland compositor using wlroots with support for XDG and XWayland clients.
package compositor

import (
	"image"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"deedles.dev/wlr"
	"deedles.dev/wlr/xkb"
	"fyshos.com/fynedesk/wlipc"
)

// monitorWP holds per-output wallpaper settings (from config).
type monitorWP struct {
	background     string
	backgroundType string
}

// resolvedBinding is a resolved keybinding (XKB sym + wlr modifier mask).
type resolvedBinding struct {
	sym  xkb.KeySym
	mods wlr.KeyboardModifier
}

const (
	barWidth    = 36  // Left bar width (NarrowBarWidth=36 at FYNE_SCALE=1)
	widgetWidth = 196 // Right widget panel width (WidgetPanelWidth=196 at FYNE_SCALE=1)

	// Window decoration dimensions
	titlebarHeight = 28 // Height of titlebar
	borderWidth    = 4  // Width of visible window border
	edgeHitSize    = 4  // Hit zone for resize edges (SSD windows)
	csdEdgeHitSize = 10 // Outer hit zone for CSD windows (no visible border, needs larger grab area)
	buttonSize     = 16 // Close/maximize button diameter
	buttonMargin   = 5  // Margin around buttons
	decoScale      = 2  // Render decorations at 2x for HiDPI quality
	cornerRadius   = 4  // Top corner radius in 1x pixels

	// CSD (client-side decorated) window hit detection
	csdTitlebarHeight = 40 // Height of CSD titlebar area for double-click detection

	// Window cascade placement
	cascadeStep = 30 // Pixel offset between cascaded windows
	maxCascade  = 5  // Reset cascade after this many windows

	// Input: double-click detection
	doubleClickThreshold = 400 * time.Millisecond // Max interval between clicks
	doubleClickDistance  = 5.0                    // Max cursor drift in pixels

	// Input: Super key alone detection
	superAloneTimeout = 400 * time.Millisecond // Max hold duration for Super-alone toggle

	// Input: key repeat timing
	keyRepeatDelay    = 600 * time.Millisecond // Initial delay before key repeat starts
	keyRepeatInterval = 40 * time.Millisecond  // Interval between repeats (25/sec)

	// Panel hotspot (auto-reveal when bar is covered)
	panelEdgeZone      = 4                      // Pixel zone at screen edge for hotspot detection
	panelRevealDelay   = 500 * time.Millisecond // Hover duration before panel reveals

	// Switcher / thumbnail dimensions
	thumbMaxW = 192 // Max thumbnail width in pixels
	thumbMaxH = 140 // Max thumbnail height in pixels

	// Lock screen
	lockAvatarRadius = 40 // Avatar circle radius in pixels
)

// Grab modes for interactive window manipulation
type grabMode int

const (
	grabNone grabMode = iota
	grabMove
	grabResize
)

// Decoration hit zones
type decoZone int

const (
	decoNone decoZone = iota
	decoTitlebar
	decoCloseButton
	decoMaxButton
	decoMinButton
	decoBorder
)

// Edge snap zones
type snapZone int

const (
	snapNone     snapZone = iota
	snapLeft              // Left half of screen
	snapRight             // Right half of screen
	snapTop               // Maximize
	snapTopLeft           // Top-left quarter
	snapTopRight          // Top-right quarter
	snapBottomLeft
	snapBottomRight
)

// OutputModeInfo is exported for JSON serialization
type OutputModeInfo struct {
	Index       int    `json:"index"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	RefreshRate int    `json:"refresh_rate"`
	Current     bool   `json:"current"`
	Custom      bool   `json:"custom,omitempty"`       // true = virtual resolution (scale-based)
	AspectRatio string `json:"aspect_ratio,omitempty"` // e.g. "16:10", "16:9"
}

// CompositorOutputState describes a single output in the compositor state
type CompositorOutputState struct {
	OutputName           string           `json:"output_name"`
	Modes                []OutputModeInfo `json:"modes"`
	PhysWidth            int              `json:"phys_width"`
	PhysHeight           int              `json:"phys_height"`
	Scale                float32          `json:"scale"`
	Width                int              `json:"width"`
	Height               int              `json:"height"`
	X                    int              `json:"x"`
	Y                    int              `json:"y"`
	Primary              bool             `json:"primary"`
	AdaptiveSyncEnabled  bool             `json:"adaptive_sync_enabled"`
	AdaptiveSyncSupported bool            `json:"adaptive_sync_supported"`
}

// CompositorState is written to a file for the panel to read
type CompositorState struct {
	Outputs []CompositorOutputState `json:"outputs"`

	// Legacy single-output fields for backward compatibility with older panels
	OutputName string           `json:"output_name"`
	Modes      []OutputModeInfo `json:"modes"`
	PhysWidth  int              `json:"phys_width"`
	PhysHeight int              `json:"phys_height"`
	Scale      float32          `json:"scale"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
}

// ModeRequest is read from a file when panel requests an EDID mode change.
type ModeRequest struct {
	ModeIndex  int    `json:"mode_index"`
	OutputName string `json:"output_name,omitempty"` // empty = primary
}

// ScaleRequest is read from a file when panel requests a scale change
type ScaleRequest struct {
	Scale      float32 `json:"scale"`
	OutputName string  `json:"output_name,omitempty"` // empty = primary
}

// LayoutRequest is read from a file when panel requests output positioning/mirroring/primary changes
type LayoutRequest struct {
	OutputName string `json:"output_name"` // Output to reposition
	Position   string `json:"position"`    // "left","right","above","below","mirror" (empty = primary-only)
	RelativeTo string `json:"relative_to"` // Reference output name
	Primary    bool   `json:"primary"`     // Set as primary output
}

// OutputLayoutConfig persists output layout to disk
type OutputLayoutConfig struct {
	Layouts map[string]OutputLayoutEntry `json:"layouts"`
	Primary string                       `json:"primary"`
}

// OutputLayoutEntry stores a single output's layout position and per-output settings
type OutputLayoutEntry struct {
	Position     string `json:"position"`
	RelativeTo   string `json:"relative_to"`
	AdaptiveSync *bool  `json:"adaptive_sync,omitempty"` // nil = default (off), true/false = explicit
}

// VRRRequest is read from a file when panel requests VRR toggle
type VRRRequest struct {
	OutputName string `json:"output_name"` // empty = primary
	Enabled    bool   `json:"enabled"`
}

// DesktopRequest is read from a file when panel requests a desktop change
type DesktopRequest struct {
	Desktop int `json:"desktop"`
}

// DesktopState is written to a file for the panel to read current desktop
type DesktopState struct {
	Version  int      `json:"version,omitempty"` // IPC schema version (0 == legacy/unset)
	Current  int      `json:"current"`
	NumDesks int      `json:"num_desks"`
	Names    []string `json:"names,omitempty"`
}

type server struct {
	display       wlr.Display
	backend       wlr.Backend
	renderer      wlr.Renderer
	allocator     wlr.Allocator
	compositor    wlr.Compositor
	outLayout     wlr.OutputLayout
	seat          wlr.Seat
	xdgShell      wlr.XDGShell
	xwayland      wlr.Xwayland
	cursor        wlr.Cursor
	cursorMgr     wlr.XCursorManager
	screencopyMgr wlr.ScreencopyManagerV1

	// Required for GTK/Qt apps
	dataDeviceMgr wlr.DataDeviceManager
	xdgDecoMgr    wlr.XDGDecorationManagerV1

	outputs            []*outputState
	primaryOutputName  string // Explicit primary output name (empty = first output)
	xdgViews           []*xdgView
	xwayViews          []*xwayView
	activeXdg          *xdgView
	activeXway         *xwayView
	panelXway          *xwayView            // Panel window (XWayland)
	secondaryPanels    map[string]*xwayView // Secondary bar windows keyed by output name
	overlayXway        *xwayView            // Overlay menu window (FyneDesk Menu)
	overlayW, overlayH float64   // Expected overlay dimensions (from IPC)
	preOverlayXdg      *xdgView  // Active XDG view before overlay opened
	preOverlayXway     *xwayView // Active XWayland view before overlay opened
	prevRealXdg        *xdgView  // Previous real (non-panel, non-overlay) focused XDG view
	prevRealXway       *xwayView // Previous real (non-panel, non-overlay) focused XWayland view

	// Cursor output tracking (for scale changes on output boundary crossing)
	lastCursorOutput *outputState

	// Virtual desktops
	currentDesk  int
	numDesks     int
	desktopNames []string // Workspace names (nil = use "1", "2", ...)
	colorScheme  int      // 0=no pref, 1=dark, 2=light (portal color-scheme)

	// Per-app window rules
	windowRules []wlipc.WindowRule

	// Interactive grab state (move/resize)
	grab                  grabMode
	grabXdg               *xdgView
	grabXway              *xwayView
	grabX, grabY          float64   // Cursor position at grab start
	grabViewX, grabViewY  float64   // View position at grab start
	grabWidth, grabHeight int       // View size at grab start (for resize)
	grabEdges             wlr.Edges // Which edges are being resized

	// Double-click tracking
	lastClickTime time.Time
	lastClickX    float64
	lastClickY    float64

	// Edge snap state
	snapZone snapZone // Current snap zone during drag

	// Cursor ownership: when true, compositor controls cursor (SSD border hover)
	ssdBorderHover bool

	// Cascade positioning for new windows (per-output)
	cascadeOffsets map[string]int

	// Idle tracking for screensaver / power management
	lastInputTime    time.Time
	idleLocked       bool
	suspendLockPending bool // true between PrepareForSleep and lock acquisition — prevents resetIdleTimer from clearing idleLocked
	displayBlanked   bool
	idleSuspended    bool // true after auto-suspend initiated, cleared on resume

	// Configurable power timeouts (minutes, 0 = disabled)
	powerLockTimeout    int    // default 5
	powerBlankTimeout   int    // default 6
	powerSuspendTimeout int    // default 0 (disabled)
	powerSuspendAction  string // "suspend", "hibernate", "hybrid-sleep", "nothing"
	nestedMode      bool // true when running inside another compositor (WLR_BACKENDS set)
	privateDBusPid  int  // PID of private dbus-daemon (nested mode only)
	shuttingDown    atomic.Bool
	wantRestart     atomic.Bool   // true if shutdown was triggered by a "restart" IPC; runner will restart on exit code 5
	shutdown        chan struct{} // closed when the compositor is tearing down; long-lived watchers select on it
	screenSaverDBus *screenSaverDBus
	portal          *portalDBus

	// Scene graph (wlr_scene for damage-tracked rendering)
	openAnim *openAnim // Current icon-to-window launch animation (nil = none)

	scene          unsafe.Pointer // *C.struct_wlr_scene
	backgroundTree unsafe.Pointer // *C.struct_wlr_scene_tree — wallpaper layer
	panelTree      unsafe.Pointer // *C.struct_wlr_scene_tree — panel XWayland surface
	windowsTree    unsafe.Pointer // *C.struct_wlr_scene_tree — normal windows (sorted by focus)
	overrideTree   unsafe.Pointer // *C.struct_wlr_scene_tree — override-redirect popups/menus
	fullscreenTree unsafe.Pointer // *C.struct_wlr_scene_tree — fullscreen window layer
	overlayTree    unsafe.Pointer // *C.struct_wlr_scene_tree — overlay menu (FyneDesk Menu)
	switcherTree   unsafe.Pointer // *C.struct_wlr_scene_tree — Alt-Tab overlay
	lockTree       unsafe.Pointer // *C.struct_wlr_scene_tree — Session lock layer (above all)

	// IME (text input) state
	textInputs      []unsafe.Pointer // []*C.struct_wlr_text_input_v3
	activeTextInput unsafe.Pointer   // *C.struct_wlr_text_input_v3 — focused
	activeInputMethod unsafe.Pointer // *C.struct_wlr_input_method_v2 — connected IME

	// Session lock state
	locked            bool
	lockedSent        bool // true after send_locked; prevents double-send crash
	lockSurfaceStates map[string]*lockSurfaceState
	lockBlackRects    map[string]unsafe.Pointer
	currentLock       unsafe.Pointer // *C.struct_wlr_session_lock_v1
	lockCrashCount    int            // Consecutive lock client crashes (reset on successful lock)
	builtinLock       *builtinLockState // Built-in lock screen state (nil when not active)
	lockScreenType    string            // "FyshOS" = built-in, otherwise try external lockers
	lockLabel         string            // Custom lock screen label from settings

	// Settings from Fyne preferences
	buttonsOnLeft      bool                 // true = close/max/min on left (macOS-style)
	wmModifier         wlr.KeyboardModifier // Primary WM modifier (Logo or Alt)
	naturalScroll      bool                 // true = invert scroll direction (like macOS)
	narrowWidgetPanel  bool                 // true = widget panel uses narrow width (36 instead of 196)
	narrowLeftLauncher bool                 // true = vertical bar on left (false = no left bar)
	backgroundType     string               // "image", "matrix", "starfield"
	backgroundPath     string               // Global wallpaper image/directory path
	monitorWallpapers  map[string]monitorWP // Per-output wallpaper overrides (key = output name)
	launcherIconSize    int                  // icon size in pixels (default 48)
	launcherZoomScale   float64              // zoom scale (default 2.0)
	barPosition         string               // "left" or "bottom" — where the panel bar is
	innerGap            int                  // pixel gap between adjacent windows (default 6)
	outerGap            int                  // pixel gap between windows and screen edges (default 6)
	panelRevealed       bool                 // panel is currently raised above windows (hotspot active)
	edgeHoverTimer      *time.Timer          // pending hotspot reveal timer (nil = not hovering edge)
	hotspotClickLatched bool                 // true = click occurred in bar area, keep panel visible
	lastHotspotCheck    time.Time            // throttle: last time checkPanelHotspot() ran
	revealRestoreScale  float32              // output scale to restore when hiding panel (0 = no restore)
	lastFrameTime      time.Time            // diagnostic: last renderOutput call time
	animTimerPending   atomic.Bool          // true = animation wakeup timer already scheduled

	// Hot corners
	hotCornerActions   [4]string // Action per corner (empty = disabled)
	activeHotCorner    int       // Currently hovered corner (-1 = none)
	lastHotCornerTime  time.Time    // Last activation time (cooldown)
	showDesktopActive  bool         // true = all windows minimized via "show desktop"
	focusModeActive    bool         // true = all windows except focused minimized via focus mode

	// Super-alone detection: open launcher on bare Super press+release
	superAlonePressed bool      // Super was pressed without any other key
	superAloneTime    time.Time // When Super was pressed

	// Configurable keybindings: (sym, mods) -> action name
	keybindingMap map[resolvedBinding]string

	// Keyboard layout switching
	keyboardLayouts   []wlipc.KeyboardLayout
	activeLayoutIndex int
	keyboards         []wlr.Keyboard

	// Icon cache: app_id -> wlr texture (nil = lookup failed, don't retry)
	iconCache map[string]*iconEntry

	// Decoration hover state for button glow effect
	hoverButton decoZone  // Which button is hovered (decoNone if none)
	hoverXdg    *xdgView  // View whose titlebar is hovered
	hoverXway   *xwayView // View whose titlebar is hovered

	// App switcher overlay state
	switcherActive     bool          // Whether the switcher overlay is visible
	switcherWindows    []interface{} // Windows in the switcher (xdgView or xwayView)
	switcherIndex      int           // Currently highlighted window index
	switcherOrigXdg    *xdgView      // Original focused XDG window before switcher
	switcherOrigXway   *xwayView     // Original focused XWayland window before switcher
	switcherThumbnails []wlr.Texture // Cached SHM thumbnails (captured from framebuffer)
	switcherThumbImgs  []*image.NRGBA // Captured surface thumbnails for composite
	switcherBuf        unsafe.Pointer // *C.struct_wlr_scene_buffer (composite panel)
	switcherPixBuf     unsafe.Pointer // *C.struct_pixel_buffer (backing pixel data)
	switcherFadeIn     bool          // Fade-in animation active
	switcherFadeOut    bool          // Fade-out animation active
	switcherFadeStart  time.Time     // When the fade started

	// Window overview (Exposé) state — per-window scene buffers
	overviewActive      bool
	overviewEntries     []overviewEntry    // Per-window scene buffers + animation data
	overviewDimRect     unsafe.Pointer     // *C.struct_wlr_scene_rect (full-screen dim overlay)
	overviewAnimActive  bool               // Animation in progress
	overviewAnimStart   time.Time          // Animation start time
	overviewAnimClosing bool               // true = closing animation (grid→real)
	overviewLayout      overviewLayoutData // Cached grid layout for animation

	// Screenshot modes
	windowPickMode     bool // Waiting for click to select window for screenshot
	regionSelectActive bool
	regionAnchorSet    bool    // true after first click places the anchor
	regionStartX       float64
	regionStartY       float64
	regionEndX         float64
	regionEndY         float64
	// Scene rects for region overlay (GPU-native, no pixel manipulation):
	// 4 dim rects around selection + 4 border rects + container tree
	regionTree         unsafe.Pointer // *C.struct_wlr_scene_tree
	regionDimRects     [4]unsafe.Pointer // top, bottom, left, right dim rects
	regionBorderRects  [4]unsafe.Pointer // top, bottom, left, right border rects

	// Thumbnail capture throttle (last time we captured view thumbnails)
	lastThumbCapture         time.Time
	thumbCaptureFailLogged   int // 1 if "EGL not available" was already logged
	thumbCaptureCount        int // total successful capture cycles (for periodic logging)

	// Pending preview captures requested via IPC (taskbar hover).
	// Written from main thread (via mainThreadActions), read from render path.
	previewPendingIDs []string

	// Stable view ID counter (monotonically increasing)
	nextViewID int

	// Global focus ordering (higher = more recently focused)
	nextFocusSeq uint64

	// Listeners (keep references to prevent GC)
	listeners []wlr.Listener

	// UNIX socket IPC server (runs alongside file-based IPC)
	ipcServer *wlipc.IPCServer

	// Panel process
	panelCmd *exec.Cmd

	// Thread-safe action queue: goroutines enqueue, main thread (frame callback) executes
	mainThreadActions chan func()
	wakeupFd          int // eventfd for waking the event loop when actions are queued

	// Debounced IPC write: snapshot on main thread, serialize+write in background
	windowsStateDirty bool
	ipcFlushChan      chan wlipc.WindowsState // buffered(1), main thread sends snapshots

	// Pending overlay position request (from socket IPC)
	pendingOverlay *overlayRequest

	// Session restore: pending windows waiting to be matched on map
	sessionMu      sync.Mutex
	pendingSession []wlipc.SessionWindow

	// Dynamic wallpaper: tracks current time slot to detect changes
	dynamicWallpaperSlot string

	// Auto accent color from wallpaper (Material You style)
	autoAccentColor bool

	// Key repeat for consumed keybindings (volume/brightness etc.)
	keyRepeatStop chan struct{} // closed to cancel current repeat; nil = no repeat active
	keyRepeatCode uint32       // keycode being repeated

	// Font customization
	fontFamily string // Font family name (resolved via fc-match)
	fontSize   int    // Font size in points (default 13)

	// Accessibility settings
	reduceMotion bool // Disable all animations
	highContrast bool // WCAG AA high contrast borders and focus indicators

	// Security context (Flatpak sandboxing)
	securityCtxMgr unsafe.Pointer // *C.struct_wlr_security_context_manager_v1

	// Clipboard history for clipboard manager
	clipboardHistory []wlipc.ClipboardEntry
	nightLight       nightLightState

	// Desktop transition animation (slide or iris)
	transitionActive bool
	transitionStart  time.Time
	transitionBuf    unsafe.Pointer // *C.struct_wlr_scene_buffer
	transitionPixBuf unsafe.Pointer // *C.struct_pixel_buffer
	transitionImg    *image.NRGBA
	slideDirection   int // -1 = slide left, +1 = slide right, 0 = iris

	// Boot sequence animation
	bootActive bool
	bootStart  time.Time
	bootBuf    unsafe.Pointer // *C.struct_wlr_scene_buffer
	bootPixBuf unsafe.Pointer // *C.struct_pixel_buffer
	bootImg    *image.NRGBA

	// Window close glitch animations
	closeAnims []*closeAnim

	// Tiling mode (per-desktop)
	tiling []tilingState

	// Implicit pointer grab: Wayland protocol requires that once a button is
	// pressed on a surface, that surface retains pointer focus until all buttons
	// are released — even if the cursor moves outside the surface bounds.
	pointerButtonCount int       // Number of pointer buttons currently held
	implicitGrabXway   *xwayView // XWayland view that received the button press (for implicit grab motion)
	implicitGrabXdg    *xdgView  // XDG view that received the button press (for implicit grab motion)

	// Trackpad gesture state
	gesture         gestureState
	pointerGestures unsafe.Pointer // *C.struct_wlr_pointer_gestures_v1
	swipeListeners  unsafe.Pointer // *C.struct_swipe_listeners
}

type outputState struct {
	output      wlr.Output
	sceneOutput unsafe.Pointer // *C.struct_wlr_scene_output
	width       int
	height      int
	layoutX     int // position in output layout (from outLayout.Get)
	layoutY     int
	modes       []wlr.OutputMode
	currentMode int
	savedMode   int     // Mode index before fullscreen switch (-1 = no saved mode)
	savedScale  float32 // Scale before fullscreen switch (0 = no saved scale)
	listeners     []wlr.Listener
	frameListener unsafe.Pointer // *C.struct_wl_listener (per-output frame callback)
	vrrEnabled    bool           // Adaptive sync (VRR/FreeSync) enabled on this output
	// Per-output wallpaper
	wallpaperBuf    unsafe.Pointer   // *C.struct_wlr_scene_buffer
	wallpaperPixBuf unsafe.Pointer   // *C.struct_pixel_buffer
	// Per-output animated wallpaper
	animWallpaper animatedWallpaper // Current animation (nil = static image)
	animBuf       *image.NRGBA      // Reusable NRGBA buffer for animation
	lastAnimTick  time.Time         // Last animation frame timestamp
}

type xdgView struct {
	id          string // Stable unique ID (e.g. "xdg-1")
	xdgToplevel wlr.XDGToplevel
	x, y        float64
	mapped      bool
	decorated   bool // Whether to draw server-side decorations
	maximized   bool
	fullscreen  bool
	minimized   bool
	snapped     snapZone // Current snap state (snapNone, snapLeft, snapRight)
	pinned      bool     // Show on all desktops
	floating    bool     // Exempted from tiling layout
	urgent      bool     // Requesting user attention (XDG activation)
	desk        int      // Virtual desktop this window belongs to
	parent      *xdgView // Parent window for transient/dialog windows
	// Size constraints from client
	minWidth, minHeight int
	maxWidth, maxHeight int
	// Saved geometry for restore after maximize/fullscreen/snap
	savedX, savedY           float64
	savedWidth, savedHeight  int
	savedDecorated           bool
	configuredW, configuredH int      // Pending configured size (for decorations before client commits)
	focusSeq                 uint64   // Global focus ordering
	opacity                  float32  // Per-window opacity [0.1, 1.0]
	anim                     viewAnim // Position animation state
	hideDecorations          bool     // Suppress decorations during open-anim fade-in

	listeners []wlr.Listener

	// Scene graph nodes
	sceneTree    unsafe.Pointer // *C.struct_wlr_scene_tree — container for this view
	surfaceTree  unsafe.Pointer // *C.struct_wlr_scene_tree — XDG surface + subsurfaces
	decoTitlebar unsafe.Pointer // *C.struct_wlr_scene_buffer — titlebar composite
	decoTitlePix unsafe.Pointer // *C.struct_pixel_buffer — titlebar pixel data
	decoBorderT  unsafe.Pointer // *C.struct_wlr_scene_rect — top border (above titlebar)
	decoBorderB  unsafe.Pointer // *C.struct_wlr_scene_rect — bottom border
	decoBorderL  unsafe.Pointer // *C.struct_wlr_scene_rect — left border
	decoBorderR  unsafe.Pointer // *C.struct_wlr_scene_rect — right border
	decoCornerBL unsafe.Pointer // *C.struct_wlr_scene_buffer — bottom-left rounded corner
	decoCornerBR unsafe.Pointer // *C.struct_wlr_scene_buffer — bottom-right rounded corner
	decoCornerPL unsafe.Pointer // *C.struct_pixel_buffer — bottom-left pixel data
	decoCornerPR unsafe.Pointer // *C.struct_pixel_buffer — bottom-right pixel data
	decoIconBuf  unsafe.Pointer // *C.struct_wlr_scene_buffer — app icon overlay
	decoIconPix  unsafe.Pointer // *C.struct_pixel_buffer — icon pixel data
	// Shadow layers (3 semi-transparent rects for soft drop shadow)
	shadowRects [shadowLayers]unsafe.Pointer // *C.struct_wlr_scene_rect
	// Modal scrim: semi-transparent overlay behind dialog windows
	scrimRect unsafe.Pointer // *C.struct_wlr_scene_rect (nil = no scrim)

	// Cached thumbnail for Alt-Tab switcher (captured during render pass)
	cachedThumb *image.NRGBA
}

type xwayView struct {
	id               string // Stable unique ID (e.g. "xway-1")
	surface          wlr.XwaylandSurface
	x, y             float64
	mapped           bool
	everMapped       bool // true after first successful map (skip open anim on remap)
	isPanel          bool
	isOverlay        bool // Overlay menu (FyneDesk Menu)
	overrideRedirect bool // X11 override-redirect (popups, menus, tooltips)
	decorated        bool // Whether to draw server-side decorations
	maximized        bool
	fullscreen       bool
	minimized        bool
	snapped          snapZone  // Current snap state (snapNone, snapLeft, snapRight)
	pinned           bool      // Show on all desktops
	floating         bool      // Exempted from tiling layout
	urgent           bool      // Requesting user attention (XDG activation)
	desk             int       // Virtual desktop this window belongs to
	parent           *xwayView // Parent window for transient/dialog windows
	mapListenerSetup bool
	pendingSetup     func() // Deferred setup for when wlr_surface isn't ready yet (associate event)
	// Size constraints from client
	minWidth, minHeight int
	maxWidth, maxHeight int
	// Saved geometry for restore after maximize/fullscreen/snap
	savedX, savedY          float64
	savedWidth, savedHeight int
	focusSeq                uint64   // Global focus ordering
	opacity                 float32  // Per-window opacity [0.1, 1.0]
	anim                    viewAnim // Position animation state
	hideDecorations         bool     // Suppress decorations during open-anim fade-in

	listeners []wlr.Listener

	// Scene graph nodes
	sceneTree    unsafe.Pointer // *C.struct_wlr_scene_tree — container for this view
	surfaceTree  unsafe.Pointer // *C.struct_wlr_scene_tree — XWayland surface + subsurfaces
	decoTitlebar unsafe.Pointer // *C.struct_wlr_scene_buffer — titlebar composite
	decoTitlePix unsafe.Pointer // *C.struct_pixel_buffer — titlebar pixel data
	decoBorderT  unsafe.Pointer // *C.struct_wlr_scene_rect — top border (above titlebar)
	decoBorderB  unsafe.Pointer // *C.struct_wlr_scene_rect — bottom border
	decoBorderL  unsafe.Pointer // *C.struct_wlr_scene_rect — left border
	decoBorderR  unsafe.Pointer // *C.struct_wlr_scene_rect — right border
	decoCornerBL unsafe.Pointer // *C.struct_wlr_scene_buffer — bottom-left rounded corner
	decoCornerBR unsafe.Pointer // *C.struct_wlr_scene_buffer — bottom-right rounded corner
	decoCornerPL unsafe.Pointer // *C.struct_pixel_buffer — bottom-left pixel data
	decoCornerPR unsafe.Pointer // *C.struct_pixel_buffer — bottom-right pixel data
	decoIconBuf  unsafe.Pointer // *C.struct_wlr_scene_buffer — app icon overlay
	decoIconPix  unsafe.Pointer // *C.struct_pixel_buffer — icon pixel data
	// Shadow layers (3 semi-transparent rects for soft drop shadow)
	shadowRects [shadowLayers]unsafe.Pointer // *C.struct_wlr_scene_rect
	// Modal scrim: semi-transparent overlay behind dialog windows
	scrimRect unsafe.Pointer // *C.struct_wlr_scene_rect (nil = no scrim)

	// Cached thumbnail for Alt-Tab switcher (captured during render pass)
	cachedThumb *image.NRGBA
}

// onDesk returns true if the xdg view should be shown on the given desktop
func (v *xdgView) onDesk(desk int) bool {
	return v.pinned || v.desk == desk
}

// onDesk returns true if the xway view should be shown on the given desktop
func (v *xwayView) onDesk(desk int) bool {
	return v.pinned || v.desk == desk
}

// outputGeometry represents the position and size of an output in the layout
type outputGeometry struct {
	x, y          int
	width, height int
	scale         float32
}

// overviewLayoutData holds the computed grid layout for the overview.
type overviewLayoutData struct {
	screenW, screenH int
	screenX, screenY int
	cols, rows       int
	cellW, cellH     int
	thumbH           int
	margin, gap      int
	titleH           int
}

// overviewEntry holds per-window state during overview mode.
// Each window gets its own wlr_scene_buffer for GPU-quality scaling.
type overviewEntry struct {
	view      interface{}    // *xdgView or *xwayView
	sceneBuf  unsafe.Pointer // *C.struct_wlr_scene_buffer (thumbnail)
	pixBuf    unsafe.Pointer // *C.struct_pixel_buffer
	titleBuf  unsafe.Pointer // *C.struct_wlr_scene_buffer (title label)
	titlePix  unsafe.Pointer // *C.struct_pixel_buffer
	sceneTree unsafe.Pointer // view's original sceneTree (to re-enable on close)
	realX, realY int         // Window's actual screen position
	realW, realH int         // Window's actual size
	gridX, gridY int         // Target grid position
	gridW, gridH int         // Target grid cell size for thumbnail
}
