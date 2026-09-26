package compositor

/*
#include <wlr/types/wlr_seat.h>

// safe_pointer_notify_enter checks surface != NULL before calling
// wlr_seat_pointer_notify_enter. Returns 0 on success, -1 if surface was NULL.
static int safe_pointer_notify_enter(struct wlr_seat *seat, struct wlr_surface *surface, double sx, double sy) {
	if (!surface) return -1;
	wlr_seat_pointer_notify_enter(seat, surface, sx, sy);
	return 0;
}
*/
import "C"

import (
	"time"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// safePointerEnter wraps PointerNotifyEnter with a C-level NULL check on the
// surface pointer. This closes the TOCTOU race where surface.Valid() returns
// true in Go but the underlying wlr_surface is destroyed before the CGO call.
// It also skips the call when the surface already has pointer focus, avoiding
// redundant wl_pointer.enter events that cause Firefox to dismiss popups.
func (s *server) safePointerEnter(surface wlr.Surface, sx, sy float64) {
	// Skip if the surface already has pointer focus — redundant enter events
	// cause Wayland clients (especially Firefox) to re-process input routing
	// and dismiss context menus, popups, and text selection state.
	if s.seat.PointerFocusedSurface().Ptr() == surface.Ptr() {
		return
	}

	C.safe_pointer_notify_enter(seatPtr(s.seat), surfacePtr(surface), C.double(sx), C.double(sy))
}

func (s *server) handleCursorMotion(p wlr.Pointer, t time.Time, dx, dy float64) {
	s.resetIdleTimer()
	s.cursor.Move(p.Base(), dx, dy)
	s.processCursorMotion(t)
}

func (s *server) handleCursorMotionAbsolute(p wlr.Pointer, t time.Time, x, y float64) {
	s.resetIdleTimer()
	s.cursor.WarpAbsolute(p.Base(), x, y)
	s.processCursorMotion(t)
}

func (s *server) handleCursorButton(p wlr.Pointer, t time.Time, button wlr.CursorButton, state wlr.ButtonState) {
	s.resetIdleTimer()

	// Track button count for Wayland implicit pointer grab.
	// The protocol requires that a surface retains pointer focus from button
	// press until all buttons are released (e.g. CSD window resize/drag).
	s.countPointerButton(state)

	// When locked, forward button events to the lock surface (if it exists)
	if s.locked.Load() {
		if s.currentLock != nil && len(s.lockSurfaceStates) > 0 {
			s.seat.PointerNotifyButton(t, button, state)
			s.seat.PointerNotifyFrame()
		}
		return
	}

	// Modal modes consume all input
	if s.handleRegionClick(button, state) {
		return
	}
	if s.handleWindowPick(button, state) {
		return
	}
	if s.handleOverviewClick(button, state) {
		return
	}

	// Felt-tip pen annotation: Super+Left draws ink on a screen overlay.
	if s.handlePenButton(button, state) {
		return
	}

	// When panel is revealed via hotspot, temporarily disable panelTree during
	// viewAt for non-bar areas so clicks pass through to windows below.
	if s.panelRevealed && !s.isCursorInBarArea() {
		s.setPanelTreeEnabled(false)
		defer s.setPanelTreeEnabled(true)
	}

	// Find view and surface under cursor
	xdgV, xwayV, surface, sx, sy := s.viewAt(s.cursor.X(), s.cursor.Y())
	surface, sx, sy = s.fallbackToMainSurface(xdgV, xwayV, surface, sx, sy)

	// Track which view receives the button press for implicit grab motion.
	// This is critical for overlay/skip windows (sidebar, calendar) that
	// don't update s.activeXway — without this, processImplicitGrabMotion
	// would compute surface-local coordinates from the wrong window.
	s.trackImplicitGrab(state, xdgV, xwayV)

	// Get current keyboard modifiers (may be nil if no keyboard attached yet)
	kb := s.seat.Keyboard()
	var mods wlr.KeyboardModifier
	if keyboardValid(kb) {
		mods = kb.GetModifiers()
	}

	if state == wlr.ButtonPressed && button == 272 { // BTN_LEFT
		surface, sx, sy, xwayV = s.handleOverlayDismiss(xwayV, surface, sx, sy)

		if s.handleDecorationClick(xdgV, xwayV) {
			return
		}
		if s.handleCsdDoubleClick(xdgV, xwayV, surface, sy) {
			return
		}
		if s.handleBorderResizeClick() {
			return
		}
		if s.handleAltClickMove(xdgV, xwayV, mods) {
			return
		}
	}

	if state == wlr.ButtonPressed {
		if s.handleRightClickActions(xdgV, xwayV, button, mods) {
			return
		}
		s.handleSurfaceClick(xdgV, xwayV, surface, sx, sy)
	} else if state == wlr.ButtonReleased {
		s.handleButtonRelease(button)
	}

	// Don't forward button events during grab
	if s.grab != grabNone {
		return
	}

	// Click latch: if the panel is revealed via hotspot and the user clicks
	// in the bar area (the overlay), latch the hotspot so it stays visible
	// while the user interacts with the bar.
	if state == wlr.ButtonPressed && s.panelRevealed && s.isCursorInBarArea() {
		s.latchPanelHotspotClick()
	}

	s.seat.PointerNotifyButton(t, button, state)
	s.seat.PointerNotifyFrame()
}

// countPointerButton updates the number of pointer buttons held down.
func (s *server) countPointerButton(state wlr.ButtonState) {
	if state == wlr.ButtonPressed {
		s.pointerButtonCount++
	} else if state == wlr.ButtonReleased && s.pointerButtonCount > 0 {
		s.pointerButtonCount--
	}
}

// trackImplicitGrab records the view that receives the first button press and
// forgets it once every button is released.
func (s *server) trackImplicitGrab(state wlr.ButtonState, xdgV *xdgView, xwayV *xwayView) {
	if state == wlr.ButtonPressed && s.pointerButtonCount == 1 {
		s.implicitGrabXway = xwayV
		s.implicitGrabXdg = xdgV
	} else if state == wlr.ButtonReleased && s.pointerButtonCount == 0 {
		s.implicitGrabXway = nil
		s.implicitGrabXdg = nil
	}
}

func (s *server) handleCursorFrame() {
	s.seat.PointerNotifyFrame()
}

func (s *server) handleCursorAxis(e wlr.AxisEvent) {
	if s.naturalScroll {
		// Inverting the scroll direction is exactly what relative_direction
		// reports, so clients relying on the physical direction stay correct.
		e.Delta = -e.Delta
		e.DeltaDiscrete = -e.DeltaDiscrete
		if e.RelativeDirection == wlr.AxisRelativeDirectionInverted {
			e.RelativeDirection = wlr.AxisRelativeDirectionIdentical
		} else {
			e.RelativeDirection = wlr.AxisRelativeDirectionInverted
		}
	}
	delta := e.Delta

	if s.locked.Load() {
		return
	}

	// WM modifier + scroll = per-window opacity adjustment on window under cursor
	kb := s.seat.Keyboard()
	if keyboardValid(kb) {
		mods := kb.GetModifiers()
		if (mods&s.wmModifier) != 0 && (mods&wlr.KeyboardModifierAlt) != 0 && s.wmModifier != wlr.KeyboardModifierAlt {
			s.zoomByScroll(e) // WM modifier + Alt + scroll = magnifier
			return
		}
		if (mods & s.wmModifier) != 0 {
			step := float32(0.05)
			if delta > 0 {
				step = -step // scroll down = less opaque
			}
			xdgV, xwayV, _, _, _ := s.viewAt(s.cursor.X(), s.cursor.Y())
			if xdgV != nil {
				s.setXdgViewOpacity(xdgV, xdgV.opacity+step)
			} else if xwayV != nil && !xwayV.isPanel && !xwayV.overrideRedirect {
				s.setXwayViewOpacity(xwayV, xwayV.opacity+step)
			}
			return // don't forward scroll to the client
		}
	}

	s.seat.PointerNotifyAxis(e)
}

func (s *server) handleSetCursorRequest(client wlr.SeatClient, surface wlr.Surface, serial uint32, hotspotX, hotspotY int32) {
	// Don't let client override cursor when compositor controls it (SSD border/grab)
	if s.ssdBorderHover || s.grab != grabNone {
		return
	}
	focusedClient := s.seat.PointerFocusedClient()
	if focusedClient == client {
		s.cursor.SetSurface(surface, hotspotX, hotspotY)
	}
}

func (s *server) processCursorMotion(t time.Time) {
	// Felt-tip pen annotation: while a stroke is active, lay down ink and
	// suppress all normal pointer processing (focus, hover, grabs).
	if s.penDrawing {
		s.extendPenStroke()
		return
	}

	// Update drag icon position (if a client drag is in progress)
	s.updateDragIconPos(int(s.cursor.X()), int(s.cursor.Y()))

	// Update region screenshot selection visual as cursor moves
	if s.regionSelectActive {
		s.updateRegionSelect()
		return
	}

	// When locked, route pointer to the lock surface under cursor
	if s.locked.Load() {
		s.processLockedCursorMotion(t)
		return
	}

	// Implicit pointer grab: when buttons are held and no compositor grab is
	// active, keep delivering events to the focused surface without changing
	// focus. This is required by the Wayland protocol and fixes CSD window
	// resize (e.g. Android Studio/JBR) where the cursor moves outside the
	// surface during drag. Skip during DnD — wlroots handles drag focus.
	if s.pointerButtonCount > 0 && s.grab == grabNone && !s.isDragActive() {
		s.updateOutputCursorScale()
		s.processImplicitGrabMotion(t)
		return
	}

	// Check hot corners (overview, launcher, etc.)
	s.checkHotCorners()

	// Check panel hotspot for auto-reveal when bar is covered (throttled to avoid
	// expensive viewAt scene graph traversal on every cursor motion event).
	if time.Since(s.lastHotspotCheck) > 100*time.Millisecond {
		s.checkPanelHotspot()
		s.lastHotspotCheck = time.Now()
	}

	// When panel is revealed via hotspot (z-order raised), temporarily disable
	// panelTree during hit-testing for non-bar areas so clicks pass through to
	// windows below. In bar areas, viewAt naturally hits the raised panel.
	if s.panelRevealed && !s.isCursorInBarArea() {
		s.setPanelTreeEnabled(false)
		defer s.setPanelTreeEnabled(true)
	}

	s.updateOutputCursorScale()

	// Handle interactive grab (move/resize)
	if s.grab == grabMove {
		s.processGrabMove()
		return
	}
	if s.grab == grabResize {
		s.processGrabResize()
		return
	}

	// Find surface under cursor first (respects client input region via SurfaceAt)
	xdgV, xwayV, surface, sx, sy := s.viewAt(s.cursor.X(), s.cursor.Y())

	// Optimization: skip the expensive border/decoration scene-tree traversals
	// when the cursor is clearly inside a window's content area. This is the
	// common case during text editing and eliminates 2 extra scene walks per
	// cursor motion event (cursorNearBorder + viewAtDecoration).
	if !s.isCursorNearAnyEdge(xdgV, xwayV) {
		if s.ssdBorderHover {
			s.ssdBorderHover = false
			// Reset to default cursor; the client will override via SetCursor
			// on the next motion event. Don't call PointerNotifyClearFocus() —
			// the leave/enter cycle causes Firefox to dismiss popups.
			s.cursor.SetXCursor(s.cursorMgr, "default")
		}
		s.clearButtonHover()
		s.routePointerMotion(t, xdgV, xwayV, surface, sx, sy)
		return
	}

	if s.processBorderHover(t, xdgV, xwayV, surface, sx, sy) {
		return
	}

	// Track button hover state for glow effect on titlebar buttons.
	s.updateButtonHover(s.cursor.X(), s.cursor.Y())

	// Route pointer events to the surface under cursor
	s.routePointerMotion(t, xdgV, xwayV, surface, sx, sy)
}

// updateOutputCursorScale reloads the cursor scale when the cursor crosses
// an output boundary. The cursor image is updated immediately so it doesn't
// disappear during the transition. Pointer focus is NOT cleared here — the
// subsequent routePointerMotion() call handles the focus transition naturally,
// which avoids a gap where neither old nor new surface has pointer focus
// (causing the cursor to vanish briefly).
func (s *server) updateOutputCursorScale() {
	curOut := s.getActiveOutput()
	if curOut == nil || curOut == s.lastCursorOutput {
		return
	}
	s.lastCursorOutput = curOut
	scale := float64(curOut.output.Scale())
	s.cursorMgr.Load(scale)
	s.cursor.SetXCursor(s.cursorMgr, "default")
}

// processBorderHover checks if the cursor is near a window border and shows the
// resize cursor if so. Returns true if border hover was handled (caller should return).
func (s *server) processBorderHover(t time.Time, xdgV *xdgView, xwayV *xwayView, surface wlr.Surface, sx, sy float64) bool {
	prevBorderHover := s.ssdBorderHover
	var edges wlr.Edges
	if !s.isCursorInBarArea() && !s.isCursorOverOverlay(xdgV, xwayV) {
		edges, _, _ = s.cursorNearBorder(s.cursor.X(), s.cursor.Y())
	}
	s.ssdBorderHover = (edges != wlr.EdgeNone)

	if edges != wlr.EdgeNone {
		// Near border — show resize cursor (ssdBorderHover blocks client override)
		s.cursor.SetXCursor(s.cursorMgr, s.resizeCursorName(edges))
		// Still forward pointer events so the client keeps its focus up-to-date.
		if (xdgV != nil || xwayV != nil) && surface.Valid() {
			s.safePointerEnter(surface, sx, sy)
			s.seat.PointerNotifyMotion(t, sx, sy)
		}
		return true
	}

	// Leaving border hover — reset to default cursor. The client will
	// override via SetCursor on the next motion event. Don't clear pointer
	// focus (leave/enter cycle causes Firefox to dismiss popups).
	if prevBorderHover {
		s.cursor.SetXCursor(s.cursorMgr, "default")
	}
	return false
}

// isCursorNearAnyEdge returns true if the cursor is close enough to the
// current view's border or titlebar that border/decoration hover detection
// is needed. When the cursor is deep inside a window's content area, border
// and decoration scene traversals can be skipped entirely (2 fewer per frame).
func (s *server) isCursorNearAnyEdge(xdgV *xdgView, xwayV *xwayView) bool {
	const margin = 12 // max(edgeHitSize=4, csdEdgeHitSize=10) + 2px safety
	cx, cy := s.cursor.X(), s.cursor.Y()

	if xdgV != nil && xdgV.mapped {
		geo := xdgV.xdgToplevel.Base().Geometry()
		left := xdgV.x
		top := xdgV.y
		if xdgV.decorated {
			top -= float64(titlebarHeight)
		}
		right := left + float64(geo.Dx())
		bottom := xdgV.y + float64(geo.Dy())
		if cx >= left+margin && cx <= right-margin && cy >= top+margin && cy <= bottom-margin {
			return false
		}
	} else if xwayV != nil && xwayV.mapped && !xwayV.isPanel && !xwayV.overrideRedirect {
		left := xwayV.x
		top := xwayV.y
		if xwayV.decorated {
			top -= float64(titlebarHeight)
		}
		right := left + float64(xwayV.surface.Width())
		bottom := xwayV.y + float64(xwayV.surface.Height())
		if cx >= left+margin && cx <= right-margin && cy >= top+margin && cy <= bottom-margin {
			return false
		}
	}
	return true
}

// clearButtonHover resets the titlebar button hover state when the cursor
// moves deep inside a window, without doing a full decoration scene traversal.
func (s *server) clearButtonHover() {
	if s.hoverButton == decoNone && s.hoverXdg == nil && s.hoverXway == nil {
		return
	}
	if s.hoverXdg != nil && s.hoverXdg.decorated {
		s.updateXdgViewDecorations(s.hoverXdg)
	}
	if s.hoverXway != nil && s.hoverXway.decorated {
		s.updateXwayViewDecorations(s.hoverXway)
	}
	s.hoverButton = decoNone
	s.hoverXdg = nil
	s.hoverXway = nil
}

// isCursorOverOverlay returns true if the view under the cursor is an overlay
// window (sidebar, menus). When true, border resize detection should be skipped
// so the overlay doesn't grab resize handles from windows underneath.
func (s *server) isCursorOverOverlay(xdgV *xdgView, xwayV *xwayView) bool {
	if xwayV != nil && xwayV.isOverlay {
		return true
	}
	// Also check the compositor overlay menu bounds
	if s.overlayXway != nil && s.overlayW > 0 && s.overlayH > 0 {
		ov := s.overlayXway
		cx, cy := s.cursor.X(), s.cursor.Y()
		if cx >= ov.x && cx < ov.x+s.overlayW && cy >= ov.y && cy < ov.y+s.overlayH {
			return true
		}
	}
	return false
}

// routePointerMotion forwards pointer motion events to the correct surface.
// Handles direct surface hits, CSD titlebar fallbacks, and empty desktop.
func (s *server) routePointerMotion(t time.Time, xdgV *xdgView, xwayV *xwayView, surface wlr.Surface, sx, sy float64) {
	// Over a window surface — route events, let client control cursor
	if (xdgV != nil || xwayV != nil) && surface.Valid() {
		s.safePointerEnter(surface, sx, sy)
		s.seat.PointerNotifyMotion(t, sx, sy)
		return
	}

	// View found but no valid surface (e.g. CSD titlebar area in JBR/IntelliJ
	// where the scene node isn't a wlr_scene_surface). Route events to the
	// view's primary surface so motion on CSD titlebars still reaches the client.
	if xwayV != nil && !xwayV.isPanel {
		mainSurf := xwayV.surface.Surface()
		if mainSurf.Valid() {
			lsx := s.cursor.X() - xwayV.x
			lsy := s.cursor.Y() - xwayV.y
			s.safePointerEnter(mainSurf, lsx, lsy)
			s.seat.PointerNotifyMotion(t, lsx, lsy)
			return
		}
	}
	if xdgV != nil {
		mainSurf := xdgV.xdgToplevel.Base().Surface()
		if mainSurf.Valid() {
			geo := xdgV.xdgToplevel.Base().Geometry()
			lsx := s.cursor.X() - xdgV.x + float64(geo.Min.X)
			lsy := s.cursor.Y() - xdgV.y + float64(geo.Min.Y)
			s.safePointerEnter(mainSurf, lsx, lsy)
			s.seat.PointerNotifyMotion(t, lsx, lsy)
			return
		}
	}

	// Over empty space (desktop background) — default cursor, clear focus
	s.cursor.SetXCursor(s.cursorMgr, "default")
	s.seat.PointerNotifyClearFocus()
}

// processImplicitGrabMotion sends pointer motion to the currently focused
// surface without changing focus. This implements the Wayland implicit grab:
// while any pointer button is held, the surface that had focus at button press
// time continues to receive all pointer events — even if the cursor moves
// outside the surface bounds. This is critical for CSD window resize/drag
// (e.g. Android Studio/JBR, GTK CSD) where the cursor leaves the surface edge.
func (s *server) processImplicitGrabMotion(t time.Time) {
	focused := s.seat.PointerFocusedSurface()
	if !focused.Valid() {
		return
	}
	// Use the view that was under the cursor at button-press time.
	// We can't use s.activeXway because overlay/skip windows (sidebar,
	// calendar, etc.) don't update it — they'd compute surface-local
	// coordinates from the wrong window, breaking drag on overlays.
	cx, cy := s.cursor.X(), s.cursor.Y()
	if s.implicitGrabXway != nil && s.implicitGrabXway.mapped {
		sx := cx - s.implicitGrabXway.x
		sy := cy - s.implicitGrabXway.y
		s.seat.PointerNotifyMotion(t, sx, sy)
	} else if s.implicitGrabXdg != nil && s.implicitGrabXdg.mapped {
		geo := s.implicitGrabXdg.xdgToplevel.Base().Geometry()
		sx := cx - s.implicitGrabXdg.x + float64(geo.Min.X)
		sy := cy - s.implicitGrabXdg.y + float64(geo.Min.Y)
		s.seat.PointerNotifyMotion(t, sx, sy)
	} else if s.activeXway != nil && s.activeXway.mapped && !s.activeXway.isPanel {
		// Fallback to activeXway for clicks that didn't go through handleCursorButton
		sx := cx - s.activeXway.x
		sy := cy - s.activeXway.y
		s.seat.PointerNotifyMotion(t, sx, sy)
	} else if s.activeXdg != nil && s.activeXdg.mapped {
		geo := s.activeXdg.xdgToplevel.Base().Geometry()
		sx := cx - s.activeXdg.x + float64(geo.Min.X)
		sy := cy - s.activeXdg.y + float64(geo.Min.Y)
		s.seat.PointerNotifyMotion(t, sx, sy)
	}
}
