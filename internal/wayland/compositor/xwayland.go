package compositor

/*
#include <stdlib.h>
#include <stdio.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_compositor.h>
#include <wlr/xwayland.h>

static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
    return wlr_scene_tree_create(parent);
}
static struct wlr_scene_tree *scene_subsurface_tree_create(struct wlr_scene_tree *parent, struct wlr_surface *surface) {
    return wlr_scene_subsurface_tree_create(parent, surface);
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
static void scene_node_place_above(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_above(node, sibling);
}
static void scene_node_place_below(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_below(node, sibling);
}

// Debug: check surface opaque region and format
static void debug_surface_info(struct wlr_surface *surface, const char *label) {
    if (!surface) {
        printf("[SURFACE-DEBUG] %s: surface=NULL\n", label);
        fflush(stdout);
        return;
    }
    pixman_region32_t *opaque = &surface->opaque_region;
    int n = 0;
    pixman_box32_t *boxes = pixman_region32_rectangles(opaque, &n);
    printf("[SURFACE-DEBUG] %s: opaque_rects=%d", label, n);
    for (int i = 0; i < n && i < 3; i++) {
        printf(" rect[%d]=(%d,%d,%d,%d)", i, boxes[i].x1, boxes[i].y1, boxes[i].x2, boxes[i].y2);
    }
    printf(" current_w=%d current_h=%d\n", surface->current.width, surface->current.height);
    fflush(stdout);
}

// Debug: dump scene tree child order
static void dump_scene_children(struct wlr_scene_tree *root, const char *label) {
    printf("[SCENE-DEBUG] %s children order (bottom→top):\n", label);
    struct wlr_scene_node *child;
    int idx = 0;
    wl_list_for_each(child, &root->children, link) {
        printf("  [%d] node=%p type=%d enabled=%d\n", idx, child, child->type, child->enabled);
        idx++;
    }
    printf("  total=%d\n", idx);
    fflush(stdout);
}

// Try to map an XWayland surface's wlr_surface if it already has a buffer.
// wlroots maps the wl_surface on the first commit after association (its
// commit listener is registered in xwayland_surface_associate). If the buffer
// was committed before association, the map event never fires for static
// windows (still the case in wlroots 0.20).
static void try_map_xway_surface(struct wlr_surface *surface) {
    if (!surface) return;
    if (wlr_surface_has_buffer(surface) && !surface->mapped) {
        wlr_surface_map(surface);
    }
}

*/
import "C"

import (
	"fmt"
	"log"
	"strings"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// handleNewXwaylandSurface creates the view of a new XWayland surface and
// registers its listeners.
func (s *server) handleNewXwaylandSurface(surface wlr.XwaylandSurface) {
	// Skip if surface is not valid
	if !surface.Valid() {
		log.Println("Warning: Invalid XWayland surface, skipping")
		return
	}

	// For XWayland windows, check MOTIF hints to decide on server-side decorations.
	// Apps with CSD (Chrome, GTK, Electron) set MOTIF hints to no-border + no-title,
	// meaning they draw their own decorations and don't want SSD from the compositor.

	isOR := surface.OverrideRedirect()

	s.nextViewID++
	xwInitGeo := s.getActiveOutputGeo()
	xwCx, xwCy, _, _ := s.contentBounds(xwInitGeo)
	v := &xwayView{
		id:               fmt.Sprintf("xway-%d", s.nextViewID),
		surface:          surface,
		x:                float64(xwCx) + 20,
		y:                float64(xwCy) + 20,
		overrideRedirect: isOR,
		decorated:        !isOR, // No SSD for override-redirect (popups, menus)
		wantsSSD:         !isOR, // Intrinsic preference; refined by MOTIF hints below
		opacity:          1.0,
	}
	s.xwayViews = append(s.xwayViews, v)

	// Choose parent scene tree based on window type
	// We place in windowsTree initially; will reparent to panelTree/overrideTree/overlayTree on map
	parentTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	if isOR {
		parentTree = (*C.struct_wlr_scene_tree)(s.overrideTree)
	}

	viewTree := C.scene_tree_create(parentTree)
	v.sceneTree = unsafe.Pointer(viewTree)
	viewTree.node.data = unsafe.Pointer(v)

	// Non-OR windows start hidden until OnMap fires.
	// OR windows (menus, popups) must stay enabled so wlr_scene sends
	// frame_done events to XWayland, allowing buffer commits and surface mapping.
	if !isOR {
		C.scene_node_set_enabled(&viewTree.node, 0)
	}

	// The wl_surface only exists between the associate and dissociate events;
	// an X11 unmap dissociates it and a remap brings a new one. Everything
	// tied to the wl_surface (scene subtree, map/unmap/commit listeners) is
	// therefore (re)attached on associate and dropped on dissociate.
	attachSurface := func() {
		s.attachXwaySurface(v, surface, viewTree, isOR)
	}

	detachSurface := func() {
		s.detachXwaySurface(v)
	}

	v.listeners.Add(surface.OnAssociate(func(wlr.XwaylandSurface) { attachSurface() }))
	v.listeners.Add(surface.OnDissociate(func(wlr.XwaylandSurface) { detachSurface() }))

	// Handle title changes to detect panel
	v.listeners.Add(surface.OnSetTitle(func(surf wlr.XwaylandSurface, title string) {
		s.handleXwaySetTitle(v, surface, viewTree, title)
	}))

	// Handle runtime MOTIF hint changes (some apps set hints after map)
	v.listeners.Add(surface.OnSetDecorations(func(surf wlr.XwaylandSurface) {
		s.handleXwaySetDecorations(v, surf)
	}))

	// Normally not associated yet at creation time; attach right away if it is.
	attachSurface()

	v.listeners.Add(surface.OnDestroy(func(surf wlr.XwaylandSurface) {
		s.handleXwayDestroy(v, detachSurface)
	}))

	// Handle configure requests
	v.listeners.Add(surface.OnRequestConfigure(func(surf wlr.XwaylandSurface, x, y int16, width, height uint16) {
		s.handleXwayRequestConfigure(v, surface, x, y, width, height)
	}))

	// Handle client-initiated move request (titlebar drag)
	v.listeners.Add(surface.OnRequestMove(func(surf wlr.XwaylandSurface) {
		s.handleXwayRequestMove(v, surf)
	}))

	// Handle client-initiated resize request (window border drag)
	v.listeners.Add(surface.OnRequestResize(func(surf wlr.XwaylandSurface, edges wlr.Edges) {
		s.handleXwayRequestResize(v, edges)
	}))

	// Handle client-initiated fullscreen request (e.g. Firefox video fullscreen button)
	v.listeners.Add(surface.OnRequestFullscreen(func(wlr.XwaylandSurface) {
		if v.isPanel || v.overrideRedirect {
			return
		}
		s.handleXwayRequestFullscreen(v)
	}))

	// Listen for parent changes (X11 transient_for / WM_TRANSIENT_FOR)
	v.listeners.Add(surface.OnSetParent(func(wlr.XwaylandSurface) {
		s.handleXwaySetParent(v)
	}))
}

// attachXwaySurface puts the wl_surface of an XWayland view into its scene
// tree and listens to its commit, map and unmap events.
func (s *server) attachXwaySurface(v *xwayView, surface wlr.XwaylandSurface, viewTree *C.struct_wlr_scene_tree, isOR bool) {
	wlrSurface := surface.Surface()
	if !wlrSurface.Valid() || v.surfaceTree != nil {
		return
	}

	// Create the surface node (subsurface tree for XWayland)
	surfTree := C.scene_subsurface_tree_create(viewTree, surfacePtr(wlrSurface))
	v.surfaceTree = unsafe.Pointer(surfTree)
	// The subsurface tree destroys itself with the wl_surface, which can
	// go away before the dissociate event (e.g. Xwayland exiting): forget
	// it then so detachSurface doesn't destroy it twice.
	v.surfListeners.Add(wlrSurface.OnDestroy(func(wlr.Surface) {
		v.surfaceTree = nil
	}))
	// A remapped decorated window keeps its titlebar above the surface.
	if v.decorated && v.decoBorderL != nil && !v.fullscreen {
		C.scene_node_set_position(&surfTree.node, 0, C.int(titlebarHeight))
	}

	// Listen for surface commits to detect resize (for SSD decoration updates).
	// Skip override-redirect windows (popups/menus don't have decorations).
	if !isOR {
		cur := wlrSurface.Current()
		v.lastCommitW, v.lastCommitH = cur.Width(), cur.Height()
		v.surfListeners.Add(wlrSurface.OnCommit(func(sf wlr.Surface) {
			st := sf.Current()
			w, h := st.Width(), st.Height()
			if w == v.lastCommitW && h == v.lastCommitH {
				return
			}
			v.lastCommitW, v.lastCommitH = w, h
			s.handleXwaySurfaceResized(v, w, h)
		}))
	}

	// Map handler — extracted so it can be called from OnMap callback
	// AND directly when the surface is already mapped at setup time.
	handleMap := func() {
		s.handleXwayMap(v, surface, viewTree)
	}

	v.surfListeners.Add(wlrSurface.OnMap(func(Surface wlr.Surface) {
		handleMap()
		s.captureShowXway(v)
	}))

	v.surfListeners.Add(wlrSurface.OnUnmap(func(Surface wlr.Surface) {
		s.handleXwayUnmap(v, viewTree)
	}))

	// If the surface already has a buffer but isn't mapped yet, trigger the map
	// (see try_map_xway_surface). This emits events.map, which runs handleMap.
	C.try_map_xway_surface(surfacePtr(wlrSurface))
}

// detachXwaySurface drops the wl_surface listeners and scene subtree of an
// XWayland view.
func (s *server) detachXwaySurface(v *xwayView) {
	v.surfListeners.DestroyAll()
	if v.surfaceTree != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node)
		v.surfaceTree = nil
	}
}

// stripXwayDecorations removes the decorations and shadows of an XWayland view.
func (s *server) stripXwayDecorations(v *xwayView) {
	s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
	v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
	v.decoTitlebar = nil
	releasePixelBuffer(&v.decoTitlePix)
	s.removeShadow(v)
}

// stripXwayDecorationsAndOffset removes the decorations and shadows of an
// XWayland view and moves its surface back to the top-left corner.
func (s *server) stripXwayDecorationsAndOffset(v *xwayView) {
	s.stripXwayDecorations(v)
	if v.surfaceTree != nil {
		C.scene_node_set_position(&(*C.struct_wlr_scene_tree)(v.surfaceTree).node, 0, 0)
	}
}

// handleXwayMap places, decorates and focuses an XWayland view when its
// wl_surface is mapped.
func (s *server) handleXwayMap(v *xwayView, surface wlr.XwaylandSurface, viewTree *C.struct_wlr_scene_tree) {
	v.mapped = true
	title := surface.Title()
	w, h := surface.Width(), surface.Height()
	// Assign to current desktop by default
	v.desk = s.currentDesk

	// Override-redirect windows (popups, menus, tooltips):
	// Use X11 position from the surface (set by the client at CreateWindow time)
	if v.overrideRedirect {
		s.mapXwayOverrideRedirect(v, viewTree)
		return
	}

	s.adoptXwayParent(v)

	s.classifyXwayOnMap(v, surface, viewTree, title, w, h)

	s.finishXwayMap(v, viewTree, w, h)
}

// mapXwayOverrideRedirect shows a mapped override-redirect window (popup,
// menu, tooltip) at its X11 position.
func (s *server) mapXwayOverrideRedirect(v *xwayView, viewTree *C.struct_wlr_scene_tree) {
	// Read actual X11 position (not content bounds default)
	v.x = float64(v.surface.X())
	v.y = float64(v.surface.Y())
	// Inherit desktop from active window so popup is visible
	if s.activeXway != nil {
		v.desk = s.activeXway.desk
	} else if s.activeXdg != nil {
		v.desk = s.activeXdg.desk
	}
	restackXwaylandSurfaceAbove(v.surface)
	// If there's a pending overlay position request, this is a panel
	// overlay (tooltip, menu) that will be properly positioned when
	// its title is set. Keep it hidden to avoid a flash at (0,0).
	if s.pendingOverlay == nil {
		C.scene_node_set_enabled(&viewTree.node, 1)
	}
	setXwayScenePos(v)
}

// adoptXwayParent links a mapped XWayland view to its X11 parent and puts it
// on the parent's desktop.
func (s *server) adoptXwayParent(v *xwayView) {
	// Read protocol-level parent (set_parent signal may have fired before map)
	if parent := v.surface.Parent(); parent.Valid() && v.parent == nil {
		for _, pv := range s.xwayViews {
			if pv.surface.Ptr() == parent.Ptr() {
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
}

// classifyXwayOnMap sets up a mapped XWayland view according to its title:
// panel, secondary bar, panel utility window, overlay or regular window.
func (s *server) classifyXwayOnMap(v *xwayView, surface wlr.XwaylandSurface, viewTree *C.struct_wlr_scene_tree, title string, w, h int) {
	// Check if this is the panel
	if strings.Contains(title, "Tyde:Panel") {
		v.isPanel = true
		v.decorated = false
		v.wantsSSD = false
		s.panelXway = v
		log.Printf("[PANEL] Panel detected on map: title=%q mapped=%v sceneTree=%v\n",
			title, v.mapped, v.sceneTree != nil)
		// Remove any leftover decorations (shadows, titlebar, borders)
		// created before the window was identified as the panel.
		s.stripXwayDecorationsAndOffset(v)
		// Reparent to panelTree
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
		// Panel uses ARGB8888 DMA-BUF with empty opaque_region,
		// so alpha blending works natively — no commit listener needed.
		// Position panel on primary output (both XWayland configure + scene node)
		s.repositionPanel()
		s.addBlur(v)
	} else if strings.HasPrefix(title, "Tyde:Bar:") {
		// Secondary bar window for a non-primary output
		outputName := strings.TrimPrefix(title, "Tyde:Bar:")
		v.isPanel = true
		v.decorated = false
		v.wantsSSD = false
		s.stripXwayDecorationsAndOffset(v)
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
		if s.secondaryPanels == nil {
			s.secondaryPanels = make(map[string]*xwayView)
		}
		s.secondaryPanels[outputName] = v
		log.Printf("[PANEL] Secondary bar detected on map: output=%q title=%q", outputName, title)
		s.repositionSecondaryPanel(outputName, v)
		s.addBlur(v)
	} else if strings.Contains(title, "Tyde:skip") {
		// Panel utility window (app launcher, etc.) — no decorations
		v.decorated = false
		v.wantsSSD = false
		v.isOverlay = true
		log.Printf("[OVERLAY] map: title=%q surfW=%d surfH=%d", title, surface.Width(), surface.Height())
		// Remove any decorations/shadows that may have been created before identification
		s.stripXwayDecorations(v)
		// Reparent to overlayTree so it appears above normal windows
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
		restackXwaylandSurfaceAbove(v.surface)
		// Enable scene node (may have been hidden by override-redirect handler
		// when pendingOverlay was set, to avoid flash at wrong position)
		C.scene_node_set_enabled(&viewTree.node, 1)
		s.positionOverlay(v, surface)
		s.addBlur(v)
		if !strings.Contains(title, "Tyde:nofocus") {
			s.focusOverlayKeyboard(v)
		}
	} else if title == "Tyde Menu" || strings.Contains(title, "Tyde:EmojiPicker") {
		// Overlay window (context menu or emoji picker) — position from IPC
		// Save pre-overlay focus so refocusPreOverlayWindow can restore it
		s.preOverlayXdg = s.activeXdg
		s.preOverlayXway = s.activeXway
		// Close any existing overlay first
		s.closeOverlay()
		v.decorated = false
		v.wantsSSD = false
		v.isOverlay = true
		s.overlayXway = v
		// Remove any decorations/shadows that may have been created before identification
		s.stripXwayDecorations(v)
		// Reparent to overlayTree
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
		s.positionOverlay(v, surface)
		s.addBlur(v)
		restackXwaylandSurfaceAbove(v.surface)
		s.focusOverlayKeyboard(v)
		// Set pointer focus so the first click works without mouse movement
		s.sendPointerEnterIfOver(v.x, v.y, float64(w), float64(h), v.surface.Surface())
	} else if !v.isPanel {
		s.mapXwayRegular(v, surface, title, w, h)
	}
}

// mapXwayRegular decorates and places a mapped regular XWayland window.
func (s *server) mapXwayRegular(v *xwayView, surface wlr.XwaylandSurface, title string, w, h int) {
	// Check MOTIF hints BEFORE positioning so decorated flag is correct
	// (affects titlebar offset in positioning)
	decoHints := surface.Decorations()
	hasCSD := decoHints&wlr.XwaylandSurfaceDecorationsNoBorder != 0 &&
		decoHints&wlr.XwaylandSurfaceDecorationsNoTitle != 0
	v.wantsSSD = !hasCSD
	if hasCSD {
		v.decorated = false
	}
	xwayClass := getXwaylandSurfaceClass(surface)
	log.Printf("[DECO] XWayland map: class=%q title=%q size=%dx%d hints=0x%x hasCSD=%v → decorated=%v\n",
		xwayClass, title, w, h, decoHints, hasCSD, v.decorated)

	ruled := false
	if !v.everMapped {
		// Apply per-app window rules before positioning
		if rule := s.matchWindowRule(xwayClass); rule != nil {
			s.applyWindowRuleXway(v, rule)
			ruled = true
		}

		// Restore session window state (position, desktop, maximize)
		if sw := s.matchSessionWindow(xwayClass); sw != nil {
			s.applySessionWindowXway(v, sw)
		}

		// Regular window - position in content area
		s.positionNewXwayWindow(v)
	} else {
		// Remap (X11 unmap + map, e.g. an app restored from the
		// tray): keep the window where it was.
		v.surface.Configure(int16(v.x), int16(v.y), uint16(w), uint16(h))
	}

	// Apply maximize geometry if set by window rule (rule only sets flag, not geometry)
	if v.maximized {
		outGeo := s.getActiveOutputGeo()
		cx, cy, cw, ch := s.contentBounds(outGeo)
		topMargin := 0
		if v.decorated {
			topMargin = titlebarHeight
		}
		targetX := float64(cx)
		targetY := float64(cy + topMargin)
		v.x, v.y = targetX, targetY
		v.surface.Configure(int16(targetX), int16(targetY), uint16(cw), uint16(ch-topMargin))
	}
	if !v.everMapped && v.parent == nil {
		s.placeNewWindow(placeable{xway: v}, ruled) // where it was with these screens
	}

	onCurrentDesk := v.pinned || v.desk == s.currentDesk
	// Create modal scrim behind dialog windows (parent != nil)
	if v.parent != nil && onCurrentDesk {
		s.createModalScrimXway(v)
	}
	s.writeWindowsState()
	s.retile()
}

// finishXwayMap shows, decorates, focuses and animates a mapped XWayland view
// once it has been classified.
func (s *server) finishXwayMap(v *xwayView, viewTree *C.struct_wlr_scene_tree, w, h int) {
	// Determine if open animation will run (only on first map, not remaps).
	// Remaps happen when apps hide to tray then reappear (e.g. Slack).
	isNormalWindow := !v.isPanel && !v.isOverlay && !v.overrideRedirect
	onCurrentDesk := v.pinned || v.desk == s.currentDesk
	isFirstMap := !v.everMapped
	willAnimate := isNormalWindow && onCurrentDesk && !s.reduceMotion && v.parent == nil && !v.fullscreen && isFirstMap
	v.everMapped = true

	// Enable scene node only if on current desktop AND no animation
	onDesk := v.isPanel || v.isOverlay || v.overrideRedirect || v.pinned || v.desk == s.currentDesk
	if onDesk && !willAnimate {
		C.scene_node_set_enabled(&viewTree.node, 1)
	}

	// If decorated, offset the surface down by titlebarHeight
	if v.decorated && !v.isPanel && isFirstMap {
		surfT := (*C.struct_wlr_scene_tree)(v.surfaceTree)
		C.scene_node_set_position(&surfT.node, 0, C.int(titlebarHeight))
		_, _, v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = s.createDecoNodes(viewTree, w, h, false)
		s.updateXwayViewDecorations(v)
	}
	setXwayScenePos(v)

	// Focus AFTER decorations are set up
	if isNormalWindow && onCurrentDesk {
		s.focusXwayView(v)
		if willAnimate {
			s.startOpenAnimXway(v)
		}
	}
}

// handleXwayUnmap hides an XWayland view whose wl_surface was unmapped.
func (s *server) handleXwayUnmap(v *xwayView, viewTree *C.struct_wlr_scene_tree) {
	s.ensureThumbXway(v) // capture thumbnail before unmap for close animation
	v.mapped = false
	s.captureHideXway(v)
	destroyModalScrim(&v.scrimRect)
	C.scene_node_set_enabled(&viewTree.node, 0)
	if v.isOverlay && s.overlayXway == v {
		s.overlayXway = nil
		s.overlayW = 0
		s.overlayH = 0
		// Refocus the window that was active before the overlay
		s.refocusPreOverlayWindow()
		// Check if there's a pending paste request (emoji or clipboard)
		s.handleEmojiPaste()
		s.handleClipboardPaste()
	}
	if !v.isPanel && !v.isOverlay {
		s.writeWindowsState()
		s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
		s.retile()
	}
}

// handleXwaySetTitle turns an XWayland view into a panel, secondary bar,
// panel utility window or overlay when its title identifies it as one.
func (s *server) handleXwaySetTitle(v *xwayView, surface wlr.XwaylandSurface, viewTree *C.struct_wlr_scene_tree, title string) {
	defer s.captureUpdateXway(v)
	if strings.Contains(title, "Tyde:Panel") {
		v.isPanel = true
		v.decorated = false
		s.panelXway = v
		log.Printf("[PANEL] Panel detected on title: title=%q mapped=%v\n", title, v.mapped)
		// Remove any leftover decorations
		s.stripXwayDecorationsAndOffset(v)
		// Reparent to panelTree if not already
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
		// Position panel on primary output (both XWayland configure + scene node)
		s.repositionPanel()
		s.addBlur(v)
	} else if strings.HasPrefix(title, "Tyde:Bar:") {
		outputName := strings.TrimPrefix(title, "Tyde:Bar:")
		v.isPanel = true
		v.decorated = false
		s.stripXwayDecorationsAndOffset(v)
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.panelTree))
		if s.secondaryPanels == nil {
			s.secondaryPanels = make(map[string]*xwayView)
		}
		s.secondaryPanels[outputName] = v
		log.Printf("[PANEL] Secondary bar detected on title: output=%q title=%q", outputName, title)
		s.repositionSecondaryPanel(outputName, v)
		s.addBlur(v)
	} else if strings.Contains(title, "Tyde:skip") && v.mapped {
		// Panel utility window title set after map
		v.decorated = false
		v.isOverlay = true
		// Remove any leftover decorations
		s.stripXwayDecorationsAndOffset(v)
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
		// Enable now — the node may have been kept hidden at map time
		// (pending overlay position). positionOverlay will set the correct pos.
		C.scene_node_set_enabled(&viewTree.node, 1)
		restackXwaylandSurfaceAbove(v.surface)
		s.positionOverlay(v, surface)
		s.addBlur(v)
		if strings.Contains(title, "Tyde:nofocus") {
			// No focus — e.g. toast notifications
		} else {
			s.focusOverlayKeyboard(v)
		}
	} else if (title == "Tyde Menu" || strings.Contains(title, "Tyde:EmojiPicker")) && v.mapped {
		// Overlay window title set after map — reposition from IPC
		// Save pre-overlay focus (use prevReal since this window already stole focus on map)
		if s.preOverlayXdg == nil && s.preOverlayXway == nil {
			s.preOverlayXdg = s.prevRealXdg
			s.preOverlayXway = s.prevRealXway
		}
		s.closeOverlay()
		v.decorated = false
		// Remove any leftover decorations
		s.stripXwayDecorationsAndOffset(v)
		v.isOverlay = true
		s.overlayXway = v
		C.scene_node_reparent(&viewTree.node, (*C.struct_wlr_scene_tree)(s.overlayTree))
		s.positionOverlay(v, surface)
		s.addBlur(v)
		restackXwaylandSurfaceAbove(v.surface)
		s.focusOverlayKeyboard(v)
		// Set pointer focus so the first click works without mouse movement
		w, h := surface.Width(), surface.Height()
		s.sendPointerEnterIfOver(v.x, v.y, float64(w), float64(h), v.surface.Surface())
	}
}

// handleXwaySetDecorations follows the MOTIF hints a mapped regular XWayland
// window sets at runtime.
func (s *server) handleXwaySetDecorations(v *xwayView, surf wlr.XwaylandSurface) {
	if !v.mapped || v.isPanel || v.isOverlay || v.overrideRedirect {
		return
	}
	decoHints := surf.Decorations()
	hasCSD := decoHints&wlr.XwaylandSurfaceDecorationsNoBorder != 0 &&
		decoHints&wlr.XwaylandSurfaceDecorationsNoTitle != 0
	log.Printf("[DECO] XWayland OnSetDecorations: class=%q hints=0x%x hasCSD=%v was_decorated=%v\n",
		getXwaylandSurfaceClass(surf), decoHints, hasCSD, v.decorated)
	// Record the intrinsic preference and let reconcile create/tear down the
	// SSD nodes. Reconcile is fullscreen-safe (a hint change while fullscreen
	// is recorded but stays a no-op until the window leaves fullscreen).
	v.wantsSSD = !hasCSD
	s.reconcileXwayDecorations(v)
}

// handleXwayDestroy releases an XWayland view whose X11 surface is destroyed
// and hands the focus to the next window.
func (s *server) handleXwayDestroy(v *xwayView, detachSurface func()) {
	// wlroots asserts that no listener is left on the X11 surface (and on
	// the wl_surface) when it frees them.
	v.listeners.DestroyAll()
	detachSurface()

	// Destroy modal scrim if any
	destroyModalScrim(&v.scrimRect)
	// Start close glitch animation before destroying the scene node
	s.startCloseAnimXway(v)

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

	// Handle overlay destruction (process killed without unmap)
	if v.isOverlay && s.overlayXway == v {
		s.overlayXway = nil
		s.overlayW = 0
		s.overlayH = 0
		s.refocusPreOverlayWindow()
		s.handleEmojiPaste()
		s.handleClipboardPaste()
	}
	// Clear parent reference on all children before removing
	for _, cv := range s.xwayViews {
		if cv.parent == v {
			cv.parent = nil
		}
	}
	// Scene node cleanup
	if v.sceneTree != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_tree)(v.sceneTree).node)
		v.sceneTree = nil
	}
	v.forgetDecorationNodes()
	delete(s.shadows, v) // it went with the tree
	s.removeBlur(v)
	wasActive := s.activeXway == v && !v.isPanel && !v.overrideRedirect && !v.isOverlay
	wasPanel := v.isPanel
	s.removeXwayView(v)
	s.forgetXwayView(v)
	// Focus next window if this was active (not for override-redirect popups or overlays)
	if wasActive {
		s.focusTopmostOnDesk(s.currentDesk)
	}
	if !wasPanel && !v.overrideRedirect {
		s.writeWindowsState()
		s.scheduleAllOutputFrames() // ensure IPC flush happens even without scene damage
		s.retile()
	}
}

// removeXwayView takes a destroyed view out of the view list and of the
// active, panel, overlay and secondary bar slots.
func (s *server) removeXwayView(v *xwayView) {
	for i, view := range s.xwayViews {
		if view == v {
			s.xwayViews = append(s.xwayViews[:i], s.xwayViews[i+1:]...)
			if s.activeXway == v {
				s.activeXway = nil
			}
			if s.panelXway == v {
				s.panelXway = nil
			}
			if s.overlayXway == v {
				s.overlayXway = nil
			}
			// Remove from secondary panels if applicable
			for name, sv := range s.secondaryPanels {
				if sv == v {
					delete(s.secondaryPanels, name)
					log.Printf("[PANEL] Secondary bar removed: output=%q", name)
					break
				}
			}
			break
		}
	}
}

// handleXwayRequestConfigure answers a configure request: panels are put back
// in place, other windows are kept within their output.
func (s *server) handleXwayRequestConfigure(v *xwayView, surface wlr.XwaylandSurface, x, y int16, width, height uint16) {
	if v.isPanel {
		if v == s.panelXway {
			s.repositionPanel()
		} else {
			// Secondary bar — reposition on its target output
			for name, sv := range s.secondaryPanels {
				if sv == v {
					s.repositionSecondaryPanel(name, v)
					break
				}
			}
		}
	} else {
		// Constrain to output bounds (prevent overflow causing off-screen buttons).
		// Use the view's current position if already mapped — the request's (x,y)
		// may be (0,0) which resolves to the primary output even when the window
		// is visually on a secondary screen.
		lookupX, lookupY := float64(x), float64(y)
		if v.mapped && (v.x != 0 || v.y != 0) {
			lookupX, lookupY = v.x, v.y
		}
		outGeo := s.getOutputGeoForView(lookupX, lookupY)
		_, _, cw, ch := s.contentBounds(outGeo)
		if int(width) > cw {
			width = uint16(cw)
		}
		if int(height) > ch {
			height = uint16(ch)
		}
		// Track position for hit-testing (especially for popups)
		v.x = float64(x)
		v.y = float64(y)
		surface.Configure(x, y, width, height)
		setXwayScenePos(v)
		if v.decorated {
			s.updateXwayViewDecorations(v)
		}
	}
}

// handleXwayRequestMove starts moving an XWayland window dragged by its
// client-side titlebar.
func (s *server) handleXwayRequestMove(v *xwayView, surf wlr.XwaylandSurface) {
	log.Printf("[MOVE] XWayland OnRequestMove: class=%q title=%q\n",
		getXwaylandSurfaceClass(surf), surf.Title())
	if !v.isPanel {
		restackXwaylandSurfaceAbove(v.surface)
		s.focusXwayView(v)
		s.beginGrabMove(nil, v)
	}
}

// handleXwayRequestResize starts resizing an XWayland window dragged by its
// client-side border.
func (s *server) handleXwayRequestResize(v *xwayView, edges wlr.Edges) {
	if !v.isPanel {
		restackXwaylandSurfaceAbove(v.surface)
		s.focusXwayView(v)
		s.beginGrabResize(nil, v, edges)
	}
}

// forgetXwayView drops every server reference to a destroyed view.
func (s *server) forgetXwayView(v *xwayView) {
	s.captureHideXway(v)
	if s.prevRealXway == v {
		s.prevRealXway = nil
	}
	if s.preOverlayXway == v {
		s.preOverlayXway = nil
	}
	if s.switcherOrigXway == v {
		s.switcherOrigXway = nil
	}
	if s.hoverXway == v {
		s.hoverXway = nil
	}
	if s.grabXway == v {
		s.grab = grabNone
		s.grabXway = nil
	}
	if s.implicitGrabXway == v {
		s.implicitGrabXway = nil
	}
}

func (s *server) fullscreenXwayWindow(v *xwayView, enable bool) {
	if enable == v.fullscreen {
		return
	}

	if enable {
		// Save current geometry
		v.savedX = v.x
		v.savedY = v.y
		v.savedWidth = v.surface.Width()
		v.savedHeight = v.surface.Height()

		// Fullscreen on the output the window is on
		out := s.getOutputForPosition(v.x, v.y)
		if out == nil {
			out = s.primaryOutput()
		}
		outGeo := s.getOutputGeometry(out)

		// Only switch output mode if the surface demands a HIGHER resolution
		// than the output offers (legacy XWayland games). Surfaces smaller
		// than the output will be told to grow to fill it; switching modes
		// for them would churn the panel layout for nothing.
		surfW, surfH := v.surface.Width(), v.surface.Height()
		if surfW > outGeo.width || surfH > outGeo.height {
			outGeo = s.switchModeForFullscreen(out, surfW, surfH)
		}

		v.x = float64(outGeo.x)
		v.y = float64(outGeo.y)
		v.surface.Configure(int16(v.x), int16(v.y), uint16(outGeo.width), uint16(outGeo.height))
		v.surface.SetFullscreen(true)
		v.fullscreen = true

		// Reparent to fullscreen layer
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.fullscreenTree))
			s.setFullscreenLayer(true)
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
		v.surface.Configure(int16(v.savedX), int16(v.savedY), uint16(v.savedWidth), uint16(v.savedHeight))
		v.surface.SetFullscreen(false)
		v.fullscreen = false

		// Reparent back to windowsTree (reconcile below restores the surface
		// offset and recreates decorations from wantsSSD).
		if v.sceneTree != nil {
			C.scene_node_reparent(&(*C.struct_wlr_scene_tree)(v.sceneTree).node, (*C.struct_wlr_scene_tree)(s.windowsTree))
			s.setFullscreenLayer(s.anyFullscreen()) // another screen may still have one
		}

		// Restore output mode after reparenting
		s.restoreModeAfterFullscreen(out)

		// Reset panel hotspot — panel returns to normal z-order
		s.hidePanelHotspot()
	}
	// Single source of truth: tears down SSD nodes while fullscreen, recreates
	// them from wantsSSD on exit. Runs after reparent and after v.fullscreen is
	// set so scene positions/offsets are correct.
	s.reconcileXwayDecorations(v)
}

func (s *server) debugSetPanelEnabled(enabled bool) {
	if enabled {
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.panelTree).node, 1)
	} else {
		C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(s.panelTree).node, 0)
	}
}

func (s *server) dumpSceneOrder(label string) {
	// wlr_scene.tree is the first field
	sceneTree := &(*C.struct_wlr_scene)(s.scene).tree
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.dump_scene_children(sceneTree, cLabel)
}

func xwaySurfacePtr(s wlr.XwaylandSurface) *C.struct_wlr_xwayland_surface {
	return (*C.struct_wlr_xwayland_surface)(s.Ptr())
}
