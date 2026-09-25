package compositor

/*
#include <stdlib.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_xdg_shell.h>

static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
    return wlr_scene_tree_create(parent);
}
static struct wlr_scene_tree *scene_xdg_surface_create(struct wlr_scene_tree *parent, struct wlr_xdg_surface *surface) {
    return wlr_scene_xdg_surface_create(parent, surface);
}

// Store/retrieve scene tree in xdg_surface->data (for popup parent lookup, like TinyWL)
static void set_xdg_surface_data(struct wlr_xdg_surface *surface, void *data) {
    surface->data = data;
}
static struct wlr_scene_tree *get_xdg_popup_parent_tree(struct wlr_xdg_popup *popup) {
    if (!popup->parent) return NULL;
    struct wlr_xdg_surface *parent = wlr_xdg_surface_try_from_wlr_surface(popup->parent);
    if (!parent || !parent->data) return NULL;
    return (struct wlr_scene_tree *)parent->data;
}

// Walk up a popup chain to the xdg_toplevel it belongs to (NULL if none).
static struct wlr_xdg_toplevel *popup_root_toplevel(struct wlr_xdg_popup *popup) {
    struct wlr_surface *parent = popup->parent;
    for (int depth = 0; parent != NULL && depth < 32; depth++) {
        struct wlr_xdg_surface *xs = wlr_xdg_surface_try_from_wlr_surface(parent);
        if (xs == NULL) return NULL;
        if (xs->role == WLR_XDG_SURFACE_ROLE_TOPLEVEL) return xs->toplevel;
        if (xs->role != WLR_XDG_SURFACE_ROLE_POPUP || xs->popup == NULL) return NULL;
        parent = xs->popup->parent;
    }
    return NULL;
}

static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
    wlr_scene_node_set_position(node, x, y);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
    wlr_scene_node_destroy(node);
}
static void scene_node_reparent(struct wlr_scene_node *node, struct wlr_scene_tree *new_parent) {
	wlr_scene_node_reparent(node, new_parent);
}
*/
import "C"

import (
	"fmt"
	"image"
	"log"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// handleNewXDGToplevel sets up a view for a new xdg_toplevel. The toplevel is
// not configured yet: wlroots requires the first configure to be sent from
// the initial commit (see handleXdgInitialCommit).
func (s *server) handleNewXDGToplevel(toplevel wlr.XDGToplevel) {
	surface := toplevel.Base()

	s.nextViewID++
	initGeo := s.getActiveOutputGeo()
	initCx, initCy, _, _ := s.contentBounds(initGeo)
	v := &xdgView{
		id:          fmt.Sprintf("xdg-%d", s.nextViewID),
		xdgToplevel: toplevel,
		x:           float64(initCx) + 20,
		y:           float64(initCy) + 20,
		decorated:   false, // Default CSD; clients binding xdg-decoration get SSD
		wantsSSD:    false, // Intrinsic preference; flipped to true on ServerSide negotiation
		opacity:     1.0,
	}

	// Create scene tree for this view in the windows layer
	windowsTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	if windowsTree == nil {
		log.Printf("[XDG] ERROR: windowsTree is nil, cannot create view for %s", v.id)
		return
	}
	viewTree := C.scene_tree_create(windowsTree)
	if viewTree == nil {
		log.Printf("[XDG] ERROR: scene_tree_create returned nil for %s", v.id)
		return
	}

	// Create the XDG surface node inside the view tree
	// wlr_scene_xdg_surface_create handles subsurfaces automatically
	xdgSurf := xdgSurfacePtr(surface)
	surfTree := C.scene_xdg_surface_create(viewTree, xdgSurf)
	if surfTree == nil {
		log.Printf("[XDG] ERROR: scene_xdg_surface_create returned nil for %s", v.id)
		C.scene_node_destroy(&viewTree.node)
		return
	}
	v.sceneTree = unsafe.Pointer(viewTree)
	v.surfaceTree = unsafe.Pointer(surfTree)
	s.xdgViews = append(s.xdgViews, v)
	// Store in xdg_surface->data so popups can find their parent scene tree
	C.set_xdg_surface_data(xdgSurf, unsafe.Pointer(surfTree))

	// Store view pointer in the scene node's data for hit-testing
	viewTree.node.data = unsafe.Pointer(v)

	// Start hidden until mapped
	C.scene_node_set_enabled(&viewTree.node, 0)

	// The first configure must be sent from the initial commit.
	v.listeners.Add(surface.Surface().OnCommit(func(wlr.Surface) {
		if surface.InitialCommit() {
			s.handleXdgInitialCommit(v)
		}
	}))

	// Handle surface map/unmap
	v.listeners.Add(surface.Surface().OnMap(func(Surface wlr.Surface) {
		s.handleXdgMap(v, viewTree, toplevel, Surface)
	}))

	v.listeners.Add(surface.Surface().OnUnmap(func(Surface wlr.Surface) {
		s.handleXdgUnmap(v, viewTree)
	}))

	// The toplevel role object is destroyed before its xdg_surface and
	// wl_surface: every listener (including the wl_surface ones) goes here.
	v.listeners.Add(toplevel.OnDestroy(func(wlr.XDGToplevel) {
		s.handleXdgDestroy(v, xdgSurf)
	}))

	// Handle client-initiated move request (titlebar drag).
	// For maximized/fullscreen windows, beginGrabMove enters a resistance
	// phase: the window stays in its filled state until the cursor crosses
	// restoreDragThreshold, then restores under the cursor.
	v.listeners.Add(toplevel.OnRequestMove(func(t wlr.XDGToplevel, client wlr.SeatClient, serial uint32) {
		s.handleXdgRequestMove(v, toplevel)
	}))

	// Handle client-initiated resize request (window border drag)
	v.listeners.Add(toplevel.OnRequestResize(func(t wlr.XDGToplevel, client wlr.SeatClient, serial uint32, edges wlr.Edges) {
		s.handleXdgRequestResize(v, toplevel, edges)
	}))

	// Handle client-initiated maximize request (e.g. GTK headerbar maximize button).
	// The protocol requires a configure in reply even when nothing changes.
	// Before the initial commit the request is honoured by the initial
	// configure instead (handleXdgInitialCommit).
	v.listeners.Add(toplevel.OnRequestMaximize(func(t wlr.XDGToplevel) {
		s.handleXdgRequestMaximize(v, toplevel, t)
	}))

	// Handle client-initiated fullscreen request (e.g. video player fullscreen button)
	v.listeners.Add(toplevel.OnRequestFullscreen(func(t wlr.XDGToplevel) {
		s.handleXdgRequestFullscreen(v, t)
	}))

	// Handle client-initiated minimize request (e.g. GTK headerbar minimize button)
	v.listeners.Add(toplevel.OnRequestMinimize(func(wlr.XDGToplevel) {
		s.handleXdgRequestMinimize(v)
	}))

	// Listen for parent changes (protocol-level transient_for)
	v.listeners.Add(toplevel.OnSetParent(func(t wlr.XDGToplevel) {
		s.handleXdgSetParent(v)
	}))
}

// handleXdgMap shows a toplevel when its surface is mapped: desktop, parent,
// window rules, placement, decorations and focus.
func (s *server) handleXdgMap(v *xdgView, viewTree *C.struct_wlr_scene_tree, toplevel wlr.XDGToplevel, Surface wlr.Surface) {
	if v.sceneTree == nil {
		log.Printf("[XDG] WARNING: OnMap fired but sceneTree is nil for %s, skipping", v.id)
		return
	}
	v.mapped = true
	s.captureShowXdg(v)

	// Assign to current desktop by default
	v.desk = s.currentDesk

	// Read size constraints from toplevel state
	state := toplevel.Current()
	v.minWidth = int(state.MinWidth())
	v.minHeight = int(state.MinHeight())
	v.maxWidth = int(state.MaxWidth())
	v.maxHeight = int(state.MaxHeight())

	// Read protocol-level parent (set_parent signal may have fired before map)
	if parent := toplevel.Parent(); parent.Valid() && v.parent == nil {
		for _, pv := range s.xdgViews {
			if pv.xdgToplevel.Ptr() == parent.Ptr() {
				v.parent = pv
				v.desk = pv.desk
				break
			}
		}
	}
	// Inherit desktop from parent
	if v.parent != nil {
		v.desk = v.parent.desk
	}

	// Apply per-app window rules before positioning
	appID := getXdgToplevelAppID(toplevel)
	if rule := s.matchWindowRule(appID); rule != nil {
		s.applyWindowRuleXdg(v, rule)
	}

	// Restore session window state (position, desktop, maximize)
	if sw := s.matchSessionWindow(appID); sw != nil {
		s.applySessionWindowXdg(v, sw)
	}

	surfState := Surface.Current()
	log.Printf("[DECO] XDG map: app_id=%q title=%q size=%dx%d decorated=%v parent=%v\n",
		appID, toplevel.Title(), surfState.Width(), surfState.Height(), v.decorated, v.parent != nil)

	s.positionNewXdgWindow(v, surfState.Width(), surfState.Height())

	// Apply maximize geometry if set by window rule (rule only sets flag, not geometry)
	if v.maximized {
		s.configureXdgMaximized(v)
	}

	// Determine if open animation will run
	onCurrentDesk := v.pinned || v.desk == s.currentDesk
	willAnimate := onCurrentDesk && !s.reduceMotion && v.parent == nil && !v.fullscreen

	// Enable scene node only if on current desktop AND no animation
	// (animation keeps it hidden until it finishes)
	if onCurrentDesk && !willAnimate {
		C.scene_node_set_enabled(&viewTree.node, 1)
	}
	// If decorated, offset the surface down by titlebarHeight
	if v.decorated {
		surfT := (*C.struct_wlr_scene_tree)(v.surfaceTree)
		C.scene_node_set_position(&surfT.node, 0, C.int(titlebarHeight))
		// Create decoration nodes
		_, _, v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = s.createDecoNodes(viewTree, surfState.Width(), surfState.Height(), false)
		s.updateXdgViewDecorations(v)
	}
	setXdgScenePos(v)

	// Create modal scrim behind dialog windows (parent != nil)
	if v.parent != nil && onCurrentDesk {
		s.createModalScrimXdg(v)
	}

	if onCurrentDesk {
		s.focusXdgView(v)
		if willAnimate {
			s.startOpenAnimXdg(v)
		}
	}
	s.writeWindowsState()
	s.retile()
}

// handleXdgUnmap hides a toplevel when its surface is unmapped.
func (s *server) handleXdgUnmap(v *xdgView, viewTree *C.struct_wlr_scene_tree) {
	s.ensureThumbXdg(v) // capture thumbnail before unmap for close animation
	v.mapped = false
	s.captureHideXdg(v)
	destroyModalScrim(&v.scrimRect)
	C.scene_node_set_enabled(&viewTree.node, 0)
	s.writeWindowsState()
	s.retile()
}

// handleXdgDestroy releases the listeners, scene nodes and server references
// of a destroyed toplevel.
func (s *server) handleXdgDestroy(v *xdgView, xdgSurf *C.struct_wlr_xdg_surface) {
	v.listeners.DestroyAll()
	v.decoListeners.DestroyAll()
	v.decoration = wlr.XDGToplevelDecorationV1{}
	// Popups look their parent tree up through xdg_surface->data, which
	// is about to point at a destroyed scene node.
	C.set_xdg_surface_data(xdgSurf, nil)

	// Destroy modal scrim if any
	destroyModalScrim(&v.scrimRect)
	// Start close glitch animation before destroying the scene node
	s.startCloseAnimXdg(v)

	// Restore output mode if window was fullscreen when destroyed
	if v.fullscreen {
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		if out != nil {
			s.restoreModeAfterFullscreen(out)
		}
	}

	// Clear parent reference on all children before removing
	for _, cv := range s.xdgViews {
		if cv.parent == v {
			cv.parent = nil
		}
	}
	// Scene node cleanup: wlr_scene_node_destroy recursively destroys children
	if v.sceneTree != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(v.sceneTree).node)
		v.sceneTree = nil
		v.surfaceTree = nil
	}
	v.forgetDecorationNodes()
	wasActive := s.activeXdg == v
	for i, view := range s.xdgViews {
		if view == v {
			s.xdgViews = append(s.xdgViews[:i], s.xdgViews[i+1:]...)
			if s.activeXdg == v {
				s.activeXdg = nil
			}
			break
		}
	}
	s.forgetXdgView(v)
	// Focus next window if this was active
	if wasActive {
		s.focusTopmostOnDesk(s.currentDesk)
	}
	s.writeWindowsState()
	s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
	s.retile()
}

// handleXdgRequestMove starts a client-initiated move.
func (s *server) handleXdgRequestMove(v *xdgView, toplevel wlr.XDGToplevel) {
	if !v.mapped {
		return
	}
	log.Printf("[MOVE] XDG OnRequestMove: app_id=%q maximized=%v fullscreen=%v\n",
		getXdgToplevelAppID(toplevel), v.maximized, v.fullscreen)
	s.focusXdgView(v)
	s.beginGrabMove(v, nil)
}

// handleXdgRequestResize starts a client-initiated resize.
func (s *server) handleXdgRequestResize(v *xdgView, toplevel wlr.XDGToplevel, edges wlr.Edges) {
	if !v.mapped {
		return
	}
	log.Printf("[RESIZE] XDG OnRequestResize: app_id=%q edges=%d\n", getXdgToplevelAppID(toplevel), edges)
	s.focusXdgView(v)
	s.beginGrabResize(v, nil, edges)
}

// handleXdgRequestMaximize handles a client-initiated maximize request.
func (s *server) handleXdgRequestMaximize(v *xdgView, toplevel, t wlr.XDGToplevel) {
	if !t.Base().Initialized() {
		return
	}
	want := t.RequestedMaximized()
	log.Printf("[MAXIMIZE] XDG OnRequestMaximize: app_id=%q title=%q maximized=%v requested=%v\n",
		getXdgToplevelAppID(toplevel), toplevel.Title(), v.maximized, want)
	switch {
	case want == v.maximized:
		t.Base().ScheduleConfigure()
	case !v.mapped:
		// Not shown yet: record the state, the map handler applies it.
		v.maximized = want
		if want {
			s.configureXdgMaximized(v)
		} else {
			t.SetMaximized(false)
		}
	default:
		s.focusXdgView(v)
		s.maximizeXdgWindow(v)
	}
}

// handleXdgRequestFullscreen handles a client-initiated fullscreen request.
func (s *server) handleXdgRequestFullscreen(v *xdgView, t wlr.XDGToplevel) {
	if !t.Base().Initialized() {
		return
	}
	want := t.RequestedFullscreen()
	log.Printf("[FULLSCREEN] XDG request_fullscreen: app_id=%q fullscreen=%v\n",
		getXdgToplevelAppID(v.xdgToplevel), want)
	if want == v.fullscreen {
		t.Base().ScheduleConfigure()
		return
	}
	// Also fine before the first map: the map handler keeps the
	// fullscreen placement (see positionNewXdgWindow).
	s.fullscreenXdgWindow(v, want)
}

// handleXdgRequestMinimize handles a client-initiated minimize request.
func (s *server) handleXdgRequestMinimize(v *xdgView) {
	if !v.mapped || v.minimized {
		return
	}
	s.minimizeXdgWindow(v)
	if s.activeXdg == v {
		s.focusTopmostOnDesk(s.currentDesk)
	}
	s.writeWindowsState()
}

// handleXdgInitialCommit sends the first configure of a toplevel: decoration
// mode, and the maximized/fullscreen state the client asked for before its
// surface existed. A configure is always scheduled (xdg-shell requires one
// before the client may attach a buffer).
func (s *server) handleXdgInitialCommit(v *xdgView) {
	if v.decoration.Valid() {
		s.applyXdgDecorationMode(v)
	}
	t := v.xdgToplevel
	switch {
	case t.RequestedFullscreen() && !v.fullscreen:
		s.fullscreenXdgWindow(v, true)
	case t.RequestedMaximized() && !v.maximized:
		v.maximized = true
		s.configureXdgMaximized(v)
	}
	t.Base().ScheduleConfigure()
}

// configureXdgMaximized sizes a view flagged maximized to the content area of
// the active output.
func (s *server) configureXdgMaximized(v *xdgView) {
	outGeo := s.getActiveOutputGeo()
	cx, cy, cw, ch := s.contentBounds(outGeo)
	topMargin := 0
	if v.decorated {
		topMargin = titlebarHeight
	}
	v.x = float64(cx)
	v.y = float64(cy + topMargin)
	v.xdgToplevel.SetSize(int32(cw), int32(ch-topMargin))
	v.xdgToplevel.SetMaximized(true)
	v.configuredW = cw
	v.configuredH = ch - topMargin
}

// handleXdgSetParent follows protocol-level parent (transient_for) changes.
func (s *server) handleXdgSetParent(child *xdgView) {
	parent := child.xdgToplevel.Parent()
	if !parent.Valid() {
		child.parent = nil
		return
	}
	for _, v := range s.xdgViews {
		if v.xdgToplevel.Ptr() == parent.Ptr() {
			child.parent = v
			child.desk = v.desk
			log.Printf("[PARENT] XDG set_parent: child=%s parent=%s\n", child.id, v.id)
			return
		}
	}
}

// forgetXdgView drops every server reference to a destroyed view.
func (s *server) forgetXdgView(v *xdgView) {
	s.captureHideXdg(v)
	if s.prevRealXdg == v {
		s.prevRealXdg = nil
	}
	if s.preOverlayXdg == v {
		s.preOverlayXdg = nil
	}
	if s.switcherOrigXdg == v {
		s.switcherOrigXdg = nil
	}
	if s.hoverXdg == v {
		s.hoverXdg = nil
	}
	if s.grabXdg == v {
		s.grab = grabNone
		s.grabXdg = nil
	}
	if s.implicitGrabXdg == v {
		s.implicitGrabXdg = nil
	}
}

// handleNewXDGPopup adds a popup (menu, tooltip, dropdown) to the scene under
// its parent and configures it, constrained to the output of its toplevel.
func (s *server) handleNewXDGPopup(popup wlr.XDGPopup) {
	base := popup.Base()
	parentTree := C.get_xdg_popup_parent_tree(xdgPopupPtr(popup))
	// Diagnostic: a popup that maps then vanishes ("flash") means wlroots
	// dismissed it via popup_done (grab rejected). Capture the seat state
	// at creation time so it can be correlated with wlroots' own DEBUG log
	// (enable via TYDE_WLR_DEBUG=1). parentTree_nil=true would instead
	// mean the popup was never added to the scene (parent has no surfTree).
	focused := s.seat.PointerFocusedSurface()
	log.Printf("[POPUP] new xdg_popup: parentTree_nil=%v ptrFocusValid=%v btnCount=%d",
		parentTree == nil, focused.Valid(), s.pointerButtonCount)
	if parentTree != nil {
		popupTree := C.scene_xdg_surface_create(parentTree, xdgSurfacePtr(base))
		C.set_xdg_surface_data(xdgSurfacePtr(base), unsafe.Pointer(popupTree))
	}

	var listeners wlr.Listeners
	listeners.Add(base.Surface().OnCommit(func(wlr.Surface) {
		if base.InitialCommit() {
			s.unconstrainXdgPopup(popup)
			base.ScheduleConfigure()
		}
	}))
	listeners.Add(popup.OnDestroy(func(wlr.XDGPopup) {
		listeners.DestroyAll()
	}))
}

// unconstrainXdgPopup keeps a popup inside the output showing its toplevel.
func (s *server) unconstrainXdgPopup(popup wlr.XDGPopup) {
	root := C.popup_root_toplevel(xdgPopupPtr(popup))
	if root == nil {
		return
	}
	for _, v := range s.xdgViews {
		if xdgToplevelPtr(v.xdgToplevel) != root {
			continue
		}
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			return
		}
		og := s.getOutputGeometry(out)
		geo := v.xdgToplevel.Base().Geometry()
		// Box in the toplevel's surface-local coordinates: (v.x, v.y) is where
		// the window geometry origin sits in the layout.
		x := og.x - int(v.x) + geo.Min.X
		y := og.y - int(v.y) + geo.Min.Y
		popup.UnconstrainFromBox(image.Rect(x, y, x+og.width, y+og.height))
		return
	}
}

// handleNewToplevelDecoration tracks an xdg-decoration object so the
// negotiated mode can be sent once the toplevel is initialized.
func (s *server) handleNewToplevelDecoration(deco wlr.XDGToplevelDecorationV1) {
	var view *xdgView
	for _, v := range s.xdgViews {
		if v.xdgToplevel.Ptr() == deco.Toplevel().Ptr() {
			view = v
			break
		}
	}
	if view == nil {
		log.Println("[DECO] xdg-decoration for an unknown toplevel, ignoring")
		return
	}
	view.decoListeners.DestroyAll()
	view.decoration = deco
	view.decoListeners.Add(deco.OnRequestMode(func(wlr.XDGToplevelDecorationV1) {
		s.applyXdgDecorationMode(view)
	}))
	view.decoListeners.Add(deco.OnDestroy(func(wlr.XDGToplevelDecorationV1) {
		view.decoListeners.DestroyAll()
		view.decoration = wlr.XDGToplevelDecorationV1{}
	}))
	s.applyXdgDecorationMode(view)
}

// applyXdgDecorationMode respects the client's decoration preference:
//   - CSD apps (Chrome, Electron) request ClientSide → honor it
//   - SSD apps (Qt, SDL, mpv) request ServerSide → provide SSD
//   - No preference → default to SSD
//
// The mode is only sent once the toplevel is initialized (SetMode is a no-op
// before); the initial commit handler calls this again.
func (s *server) applyXdgDecorationMode(v *xdgView) {
	d := v.decoration
	if !d.Valid() {
		return
	}
	ssd := d.RequestedMode() != wlr.XDGToplevelDecorationV1ModeClientSide
	if ssd {
		d.SetMode(wlr.XDGToplevelDecorationV1ModeServerSide)
	} else {
		d.SetMode(wlr.XDGToplevelDecorationV1ModeClientSide)
	}
	v.wantsSSD = ssd
	// Reconcile drives the live decorated flag from wantsSSD; a mode
	// change while fullscreen is recorded but stays a visual no-op until
	// the window leaves fullscreen. Before map the scene nodes don't
	// exist yet, so just set the derived flag for the map handler.
	if v.mapped {
		s.reconcileXdgDecorations(v)
	} else {
		v.decorated = ssd && !v.fullscreen
	}
}

func (s *server) positionNewXdgWindow(v *xdgView, winWidth, winHeight int) {
	// Don't reposition maximized or fullscreen windows (already placed)
	if v.maximized || v.fullscreen {
		return
	}
	// Place window on the output under the cursor
	outGeo := s.getActiveOutputGeo()
	cx, cy, cw, ch := s.contentBounds(outGeo)

	// A window of fixed size is a dialog of its own (a chooser, a prompt):
	// it goes in the middle, where the user looks.
	if isFixedSize(v) && winWidth > 0 && winHeight > 0 && winWidth <= cw && winHeight <= ch {
		top := 0
		if v.decorated {
			top = titlebarHeight
		}
		v.x = float64(cx + (cw-winWidth)/2)
		v.y = float64(cy + top + (ch-top-winHeight)/2)
		return
	}

	contentX := cx + 20
	contentY := cy + 20
	contentWidth := cw - 40
	contentHeight := ch - 40

	// Reserve space for SSD titlebar so it doesn't go off-screen
	if v.decorated {
		contentY += titlebarHeight
		contentHeight -= titlebarHeight
	}

	// Calculate cascade position (per-output)
	outName := s.getActiveOutput().output.Name()
	if s.cascadeOffsets == nil {
		s.cascadeOffsets = map[string]int{}
	}
	offset := s.cascadeOffsets[outName] * cascadeStep

	// Reset cascade if it would put window too far
	if offset > contentWidth/3 || offset > contentHeight/3 {
		s.cascadeOffsets[outName] = 0
		offset = 0
	}

	// Request smaller size if window exceeds content area
	resized := false
	if winWidth > contentWidth {
		winWidth = contentWidth
		resized = true
	}
	if winHeight > contentHeight {
		winHeight = contentHeight
		resized = true
	}
	if resized {
		v.xdgToplevel.SetSize(int32(winWidth), int32(winHeight))
	}

	if winWidth > 0 && winHeight > 0 {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
		if int(v.x)+winWidth > contentX+contentWidth {
			v.x = float64(contentX)
		}
		if int(v.y)+winHeight > contentY+contentHeight {
			v.y = float64(contentY)
		}
	} else {
		v.x = float64(contentX + offset)
		v.y = float64(contentY + offset)
	}

	s.cascadeOffsets[outName] = (s.cascadeOffsets[outName] + 1) % maxCascade
}

// isFixedSize reports whether a window cannot be resized: the minimum and
// maximum sizes the client set are the same.
func isFixedSize(v *xdgView) bool {
	return v.parent == nil && v.minWidth > 0 && v.minHeight > 0 &&
		v.minWidth == v.maxWidth && v.minHeight == v.maxHeight
}

func (s *server) closeXdgWindow(v *xdgView) {
	s.ensureThumbXdg(v) // capture thumbnail for close animation
	v.xdgToplevel.SendClose()
}

func (s *server) maximizeXdgWindow(v *xdgView) {
	oldX, oldY := v.x, v.y

	if v.maximized {
		// Restore
		log.Printf("[MAXIMIZE] Restoring XDG: app_id=%q saved=(%v,%v %dx%d)\n",
			getXdgToplevelAppID(v.xdgToplevel), v.savedX, v.savedY, v.savedWidth, v.savedHeight)

		// If saved size is 0 (maximize before map), use a reasonable default
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)
		restW, restH := v.savedWidth, v.savedHeight
		if restW <= 0 || restH <= 0 {
			restW = cw * 2 / 3
			restH = ch * 2 / 3
		}

		// Validate restored position — if saved position is outside content area
		// (e.g. 0,0 behind the bar), center the window instead
		restX, restY := v.savedX, v.savedY
		if int(restX) < cx || int(restY) < cy ||
			int(restX)+restW > cx+cw || int(restY)+restH > cy+ch {
			restX = float64(cx + (cw-restW)/2)
			restY = float64(cy + (ch-restH)/2)
		}

		v.xdgToplevel.SetSize(int32(restW), int32(restH))
		v.xdgToplevel.SetMaximized(false)
		v.maximized = false
		v.snapped = snapNone
		v.configuredW = restW
		v.configuredH = restH
		s.animateXdgPos(v, oldX, oldY, restX, restY)
	} else {
		// Save current geometry only from normal state (preserve across snap→maximize)
		if v.snapped == snapNone {
			geo := v.xdgToplevel.Base().Geometry()
			v.savedX = v.x
			v.savedY = v.y
			v.savedWidth = geo.Dx()
			v.savedHeight = geo.Dy()
		}
		v.snapped = snapNone

		// Maximize to the output the window is on
		outGeo := s.getOutputGeoForView(v.x, v.y)
		cx, cy, cw, ch := s.contentBounds(outGeo)

		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		targetX := float64(cx)
		targetY := float64(cy + topMargin)
		targetW, targetH := cw, ch-topMargin
		v.xdgToplevel.SetSize(int32(targetW), int32(targetH))
		v.xdgToplevel.SetMaximized(true)
		v.maximized = true
		v.configuredW = targetW
		v.configuredH = targetH
		s.animateXdgPos(v, oldX, oldY, targetX, targetY)
	}
}

func (s *server) minimizeXdgWindow(v *xdgView) {
	v.minimized = true
	v.mapped = false
	setViewSceneEnabled(v.sceneTree, false)
}

func (s *server) fullscreenXdgWindow(v *xdgView, enable bool) {
	log.Printf("[FULLSCREEN] fullscreenXdgWindow: enable=%v current=%v sceneTree=%v", enable, v.fullscreen, v.sceneTree != nil)
	if enable == v.fullscreen {
		log.Printf("[FULLSCREEN] SKIPPED — already in desired state")
		return
	}

	if enable {
		log.Printf("[FULLSCREEN] enable branch entered")
		// Save current geometry
		geo := v.xdgToplevel.Base().Geometry()
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = geo.Dx()
		v.savedHeight = geo.Dy()
		log.Printf("[FULLSCREEN] saved geo: %dx%d at (%v,%v)", geo.Dx(), geo.Dy(), v.x, v.y)

		// Fullscreen on the output the window is on
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		log.Printf("[FULLSCREEN] output=%v", out != nil)
		outGeo := s.getOutputGeometry(out)
		log.Printf("[FULLSCREEN] outGeo=%dx%d at (%d,%d)", outGeo.width, outGeo.height, outGeo.x, outGeo.y)

		// Only switch the output mode if the client demands a HIGHER
		// resolution than the output offers — that's the legacy game case
		// (e.g. native 1920x1080 mode on a 4K screen). For everything else
		// the client will simply expand to fill the output we configure it
		// to, so swapping modes here would just churn the panel layout for
		// no reason and trigger a heavy panel restart on unfullscreen.
		clientW, clientH := geo.Dx(), geo.Dy()
		if clientW > outGeo.width || clientH > outGeo.height {
			outGeo = s.switchModeForFullscreen(out, clientW, clientH)
		}

		v.x = float64(outGeo.x)
		v.y = float64(outGeo.y)
		log.Printf("[FULLSCREEN] Setting XDG fullscreen geometry: %dx%d at (%d,%d)", outGeo.width, outGeo.height, outGeo.x, outGeo.y)
		v.xdgToplevel.SetSize(int32(outGeo.width), int32(outGeo.height))
		v.xdgToplevel.SetFullscreen(true)
		v.fullscreen = true

		// Reparent to fullscreen layer and enable it
		log.Printf("[FULLSCREEN] sceneTree=%v for %q", v.sceneTree != nil, getXdgToplevelAppID(v.xdgToplevel))
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.fullscreenTree))
			C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 1)
			// Position the fullscreen view at (0,0) on the output
			C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, C.int(outGeo.x), C.int(outGeo.y))
			log.Printf("[FULLSCREEN] Enabled fullscreenTree for %q, sceneTree pos=(%d,%d)", getXdgToplevelAppID(v.xdgToplevel), outGeo.x, outGeo.y)
			// Hide deco surface offset (CSD shadows)
			if v.surfaceTree != nil {
				C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
			}
		}
	} else {
		// Restore mode before restoring window geometry
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}

		v.x = v.savedX
		v.y = v.savedY
		v.xdgToplevel.SetSize(int32(v.savedWidth), int32(v.savedHeight))
		v.xdgToplevel.SetFullscreen(false)
		v.fullscreen = false
		v.configuredW = v.savedWidth
		v.configuredH = v.savedHeight

		// Reparent back to windowsTree (reconcile below restores the surface
		// offset and recreates decorations from wantsSSD).
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.windowsTree))
			C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.fullscreenTree).node, 0)
		}

		// Restore output mode after reparenting
		s.restoreModeAfterFullscreen(out)

		// Reset panel hotspot — panel returns to normal z-order
		s.hidePanelHotspot()
	}
	// Single source of truth: reconcile derives decorated from wantsSSD &&
	// !fullscreen, recreating or tearing down SSD nodes as needed. Runs after
	// the reparent and after v.fullscreen is set so scene positions are correct.
	s.reconcileXdgDecorations(v)
}

func (s *server) restoreXdgWindow(v *xdgView) {
	v.minimized = false
	v.mapped = true
	setViewSceneEnabled(v.sceneTree, true)
	s.focusXdgView(v)
}

func xdgToplevelPtr(t wlr.XDGToplevel) *C.struct_wlr_xdg_toplevel {
	return (*C.struct_wlr_xdg_toplevel)(t.Ptr())
}
