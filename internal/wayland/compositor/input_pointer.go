package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
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
	"log"
	"strings"
	"time"
	"unsafe"

	"deedles.dev/wlr"
)

// safePointerEnter wraps PointerNotifyEnter with a C-level NULL check on the
// surface pointer. This closes the TOCTOU race where surface.Valid() returns
// true in Go but the underlying wlr_surface is destroyed before the CGO call.
// It also skips the call when the surface already has pointer focus, avoiding
// redundant wl_pointer.enter events that cause Firefox to dismiss popups.
func (s *server) safePointerEnter(surface wlr.Surface, sx, sy float64) {
	type surfPtr struct{ p *C.struct_wlr_surface }

	// Skip if the surface already has pointer focus — redundant enter events
	// cause Wayland clients (especially Firefox) to re-process input routing
	// and dismiss context menus, popups, and text selection state.
	focused := s.seat.PointerState().FocusedSurface()
	surfP := (*surfPtr)(unsafe.Pointer(&surface))
	focusP := (*surfPtr)(unsafe.Pointer(&focused))
	if surfP.p == focusP.p {
		return
	}

	type seatPtr struct{ p *C.struct_wlr_seat }
	seatP := (*seatPtr)(unsafe.Pointer(&s.seat))
	C.safe_pointer_notify_enter(seatP.p, surfP.p, C.double(sx), C.double(sy))
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
	if state == wlr.ButtonPressed {
		s.pointerButtonCount++
	} else if state == wlr.ButtonReleased && s.pointerButtonCount > 0 {
		s.pointerButtonCount--
	}

	// When locked, forward button events to the lock surface (if it exists)
	if s.locked {
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
	if state == wlr.ButtonPressed && s.pointerButtonCount == 1 {
		s.implicitGrabXway = xwayV
		s.implicitGrabXdg = xdgV
	} else if state == wlr.ButtonReleased && s.pointerButtonCount == 0 {
		s.implicitGrabXway = nil
		s.implicitGrabXdg = nil
	}

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

// handleRegionClick handles clicks during region screenshot selection mode.
// Supports two interaction patterns:
//   - Click-drag: press, hold, drag to second corner, release
//   - Click-click: click to place first corner, move, click to place second corner
//
// Returns true if the event was consumed.
func (s *server) handleRegionClick(button wlr.CursorButton, state wlr.ButtonState) bool {
	if !s.regionSelectActive {
		return false
	}
	if button == 272 {
		if state == wlr.ButtonPressed {
			if s.regionAnchorSet {
				// Second click (click-click mode): finish the selection
				s.finishRegionSelect()
				return true
			}
			// First click: place anchor
			s.regionStartX = s.cursor.X()
			s.regionStartY = s.cursor.Y()
			s.regionAnchorSet = true
			return true
		}
		if state == wlr.ButtonReleased {
			// Check if user dragged far enough for click-drag mode
			dx := s.cursor.X() - s.regionStartX
			dy := s.cursor.Y() - s.regionStartY
			if dx*dx+dy*dy >= 25 { // >= 5px distance
				s.finishRegionSelect()
			}
			// Otherwise: too small, stay in selection mode (click-click)
			return true
		}
	}
	// Right-click cancels
	if state == wlr.ButtonPressed && button == 273 {
		s.cancelRegionSelect()
		return true
	}
	return true // consume all events in this mode
}

// handleWindowPick handles clicks during window-pick screenshot mode.
// Returns true if the event was consumed.
func (s *server) handleWindowPick(button wlr.CursorButton, state wlr.ButtonState) bool {
	if !s.windowPickMode {
		return false
	}
	if state == wlr.ButtonPressed && button == 272 {
		s.captureClickedWindow(s.cursor.X(), s.cursor.Y())
		return true
	}
	if state == wlr.ButtonPressed && button == 273 {
		s.windowPickMode = false
		log.Println("[SCREENSHOT] Window pick cancelled")
		return true
	}
	return true // consume all events in this mode
}

// handleOverviewClick handles left-clicks during overview/expose mode.
// Returns true if the event was consumed.
func (s *server) handleOverviewClick(button wlr.CursorButton, state wlr.ButtonState) bool {
	if s.overviewActive && state == wlr.ButtonPressed && button == 272 {
		s.overviewClick(int(s.cursor.X()), int(s.cursor.Y()))
		return true
	}
	return false
}

// fallbackToMainSurface resolves cases where viewAt found a view but no valid
// surface (e.g. CSD titlebar area in JBR/IntelliJ). Routes to the view's
// primary surface so clicks on CSD titlebars work.
func (s *server) fallbackToMainSurface(xdgV *xdgView, xwayV *xwayView, surface wlr.Surface, sx, sy float64) (wlr.Surface, float64, float64) {
	if surface.Valid() {
		return surface, sx, sy
	}
	if xwayV != nil && !xwayV.isPanel {
		mainSurf := xwayV.surface.Surface()
		if mainSurf.Valid() {
			return mainSurf, s.cursor.X() - xwayV.x, s.cursor.Y() - xwayV.y
		}
	} else if xdgV != nil {
		mainSurf := xdgV.xdgToplevel.Base().Surface()
		if mainSurf.Valid() {
			geo := xdgV.xdgToplevel.Base().GetGeometry()
			return mainSurf, s.cursor.X() - xdgV.x + float64(geo.Min.X), s.cursor.Y() - xdgV.y + float64(geo.Min.Y)
		}
	}
	return surface, sx, sy
}

// handleOverlayDismiss checks if a left-click should dismiss or redirect to the
// overlay menu. Returns potentially updated surface/coordinates and xwayV.
func (s *server) handleOverlayDismiss(xwayV *xwayView, surface wlr.Surface, sx, sy float64) (wlr.Surface, float64, float64, *xwayView) {
	if s.overlayXway == nil || xwayV == s.overlayXway {
		return surface, sx, sy, xwayV
	}
	// Don't interfere with client override-redirect popups (e.g. Firefox
	// autocomplete, Bitwarden extension). Only manage compositor overlays.
	if xwayV != nil && xwayV.overrideRedirect && !xwayV.isOverlay {
		return surface, sx, sy, xwayV
	}
	// The overlay surface may not have committed its first buffer yet,
	// so viewAt() might return the panel instead. Check cursor against
	// the overlay's known bounds before dismissing.
	ov := s.overlayXway
	cx, cy := s.cursor.X(), s.cursor.Y()
	inOverlay := s.overlayW > 0 && s.overlayH > 0 &&
		cx >= ov.x && cx < ov.x+s.overlayW &&
		cy >= ov.y && cy < ov.y+s.overlayH
	if inOverlay {
		ovSurf := ov.surface.Surface()
		if ovSurf.Valid() {
			return ovSurf, cx - ov.x, cy - ov.y, ov
		}
	} else {
		s.closeOverlay()
	}
	return surface, sx, sy, xwayV
}

// handleDecorationClick handles clicks on SSD decoration elements (close, max,
// min buttons, titlebar drag/double-click, border resize). Returns true if consumed.
func (s *server) handleDecorationClick(xdgV *xdgView, xwayV *xwayView) bool {
	decoXdg, decoXway, zone := s.viewAtDecoration(s.cursor.X(), s.cursor.Y())
	if zone != decoNone {
		var title string
		if decoXway != nil {
			title = decoXway.surface.Title()
		} else if decoXdg != nil {
			title = decoXdg.xdgToplevel.Title()
		}
		log.Printf("[DECO-CLICK] viewAtDecoration: zone=%d title=%q viewAt=(xdg=%v xway=%v)",
			zone, title, xdgV != nil, xwayV != nil)
	}

	// Safety net: if viewAt() found a different view than viewAtDecoration(),
	// the decoration click is from a background window — suppress it.
	if zone != decoNone {
		decoView := decoXdg != nil || decoXway != nil
		surfView := xdgV != nil || xwayV != nil
		isSameView := (decoXdg != nil && decoXdg == xdgV) || (decoXway != nil && decoXway == xwayV)
		if decoView && surfView && !isSameView {
			var decoTitle, surfTitle string
			if decoXway != nil {
				decoTitle = decoXway.surface.Title()
			}
			if xwayV != nil {
				surfTitle = xwayV.surface.Title()
			}
			log.Printf("[DECO-CLICK] SUPPRESSED zone=%d: deco=%q vs surf=%q (xdg: deco=%v surf=%v)",
				zone, decoTitle, surfTitle, decoXdg != nil, xdgV != nil)
			zone = decoNone
		}
	}

	if zone == decoNone {
		return false
	}

	switch zone {
	case decoCloseButton:
		if decoXdg != nil {
			log.Printf("[DECO-CLICK] Close XDG: app_id=%q", getXdgToplevelAppID(decoXdg.xdgToplevel))
			s.closeXdgWindow(decoXdg)
		} else if decoXway != nil {
			log.Printf("[DECO-CLICK] Close XWay: title=%q class=%q", decoXway.surface.Title(), getXwaylandSurfaceClass(decoXway.surface))
			s.closeXwayWindow(decoXway)
		}
	case decoMaxButton:
		if decoXdg != nil {
			s.focusXdgView(decoXdg)
			s.maximizeXdgWindow(decoXdg)
		} else if decoXway != nil {
			s.focusXwayView(decoXway)
			s.maximizeXwayWindow(decoXway)
		}
	case decoMinButton:
		if decoXdg != nil {
			s.minimizeXdgWindow(decoXdg)
		} else if decoXway != nil {
			s.minimizeXwayWindow(decoXway)
		}
	case decoTitlebar:
		s.handleTitlebarClick(decoXdg, decoXway)
	case decoBorder:
		edges, _, _ := s.cursorNearBorder(s.cursor.X(), s.cursor.Y())
		if edges != wlr.EdgeNone {
			if decoXdg != nil {
				s.focusXdgView(decoXdg)
				s.beginGrabResize(decoXdg, nil, edges)
			} else if decoXway != nil {
				restackXwaylandSurfaceAbove(decoXway.surface)
				s.focusXwayView(decoXway)
				s.beginGrabResize(nil, decoXway, edges)
			}
		}
	}
	return true
}

// handleTitlebarClick processes a click on an SSD titlebar: double-click
// toggles maximize, single click begins a move grab.
func (s *server) handleTitlebarClick(decoXdg *xdgView, decoXway *xwayView) {
	now := time.Now()
	isDoubleClick := now.Sub(s.lastClickTime) < doubleClickThreshold &&
		abs(s.cursor.X()-s.lastClickX) < doubleClickDistance &&
		abs(s.cursor.Y()-s.lastClickY) < doubleClickDistance

	s.lastClickTime = now
	s.lastClickX = s.cursor.X()
	s.lastClickY = s.cursor.Y()

	if isDoubleClick {
		if decoXdg != nil {
			s.focusXdgView(decoXdg)
			s.maximizeXdgWindow(decoXdg)
		} else if decoXway != nil {
			s.focusXwayView(decoXway)
			s.maximizeXwayWindow(decoXway)
		}
	} else {
		if decoXdg != nil {
			s.focusXdgView(decoXdg)
			s.beginGrabMove(decoXdg, nil)
		} else if decoXway != nil {
			restackXwaylandSurfaceAbove(decoXway.surface)
			s.focusXwayView(decoXway)
			s.beginGrabMove(nil, decoXway)
		}
	}
}

// handleCsdDoubleClick detects double-clicks in the CSD titlebar area (top 40px)
// of undecorated windows to toggle maximize. Returns true if consumed.
func (s *server) handleCsdDoubleClick(xdgV *xdgView, xwayV *xwayView, surface wlr.Surface, sy float64) bool {
	if !surface.Valid() {
		return false
	}
	csdView := xdgV != nil && !xdgV.decorated
	csdXwayView := xwayV != nil && !xwayV.decorated && !xwayV.isPanel
	if !csdView && !csdXwayView {
		return false
	}
	if sy >= csdTitlebarHeight {
		return false
	}

	now := time.Now()
	isDblClick := now.Sub(s.lastClickTime) < doubleClickThreshold &&
		abs(s.cursor.X()-s.lastClickX) < doubleClickDistance &&
		abs(s.cursor.Y()-s.lastClickY) < doubleClickDistance
	s.lastClickTime = now
	s.lastClickX = s.cursor.X()
	s.lastClickY = s.cursor.Y()

	if !isDblClick {
		return false
	}
	if xdgV != nil && xdgV.maximized {
		s.focusXdgView(xdgV)
		s.maximizeXdgWindow(xdgV) // toggle -> restore
		return true
	} else if xwayV != nil && xwayV.maximized {
		s.focusXwayView(xwayV)
		s.maximizeXwayWindow(xwayV) // toggle -> restore
		return true
	}
	return false
}

// handleBorderResizeClick checks if the cursor is near a window border and
// begins a resize grab if so. Returns true if consumed.
func (s *server) handleBorderResizeClick() bool {
	if s.isCursorInBarArea() {
		return false
	}
	// Don't grab resize handles from windows underneath overlays (e.g. sidebar).
	// viewAt() already found the overlay surface, so border resize should not
	// consume the click that belongs to the overlay.
	xdgV, xwayV, _, _, _ := s.viewAt(s.cursor.X(), s.cursor.Y())
	if s.isCursorOverOverlay(xdgV, xwayV) {
		return false
	}
	edges, borderXdg, borderXway := s.cursorNearBorder(s.cursor.X(), s.cursor.Y())
	if edges == wlr.EdgeNone {
		return false
	}
	if borderXdg != nil {
		s.focusXdgView(borderXdg)
		s.beginGrabResize(borderXdg, nil, edges)
		return true
	} else if borderXway != nil && !borderXway.isPanel {
		restackXwaylandSurfaceAbove(borderXway.surface)
		s.focusXwayView(borderXway)
		s.beginGrabResize(nil, borderXway, edges)
		return true
	}
	return false
}

// handleAltClickMove starts a move grab when Alt+Left click is detected.
// Unmaximizes the window first if needed. Returns true if consumed.
func (s *server) handleAltClickMove(xdgV *xdgView, xwayV *xwayView, mods wlr.KeyboardModifier) bool {
	if mods&wlr.KeyboardModifierAlt == 0 {
		return false
	}
	if xdgV != nil {
		s.focusXdgView(xdgV)
		if xdgV.maximized {
			s.maximizeXdgWindow(xdgV) // toggle -> restore
		}
		s.beginGrabMove(xdgV, nil)
		return true
	} else if xwayV != nil && !xwayV.isPanel {
		restackXwaylandSurfaceAbove(xwayV.surface)
		s.focusXwayView(xwayV)
		if xwayV.maximized {
			s.maximizeXwayWindow(xwayV) // toggle -> restore
		}
		s.beginGrabMove(nil, xwayV)
		return true
	}
	return false
}

// handleRightClickActions handles right-click on titlebar (context menu) and
// Alt+Right click (resize grab). Returns true if consumed.
func (s *server) handleRightClickActions(xdgV *xdgView, xwayV *xwayView, button wlr.CursorButton, mods wlr.KeyboardModifier) bool {
	if button != 273 { // BTN_RIGHT
		return false
	}

	// Right-click on titlebar = window context menu (no modifiers)
	if mods == 0 {
		decoXdg, decoXway, zone := s.viewAtDecoration(s.cursor.X(), s.cursor.Y())
		if zone == decoTitlebar {
			surfView := xdgV != nil || xwayV != nil
			isSameView := (decoXdg != nil && decoXdg == xdgV) || (decoXway != nil && decoXway == xwayV)
			if surfView && !isSameView {
				zone = decoNone
			}
		}
		if zone == decoTitlebar {
			s.showWindowContextMenu(decoXdg, decoXway)
			return true
		}
	}

	// Alt + Right click = start resize grab (unmaximize first if maximized)
	if mods&wlr.KeyboardModifierAlt != 0 {
		if xdgV != nil {
			s.focusXdgView(xdgV)
			if xdgV.maximized {
				s.maximizeXdgWindow(xdgV) // toggle -> restore
			}
			geo := xdgV.xdgToplevel.Base().GetGeometry()
			edges := s.computeResizeEdges(s.cursor.X(), s.cursor.Y(), xdgV.x, xdgV.y, geo.Dx(), geo.Dy())
			s.beginGrabResize(xdgV, nil, edges)
			return true
		} else if xwayV != nil && !xwayV.isPanel {
			restackXwaylandSurfaceAbove(xwayV.surface)
			s.focusXwayView(xwayV)
			if xwayV.maximized {
				s.maximizeXwayWindow(xwayV) // toggle -> restore
			}
			edges := s.computeResizeEdges(s.cursor.X(), s.cursor.Y(), xwayV.x, xwayV.y, xwayV.surface.Width(), xwayV.surface.Height())
			s.beginGrabResize(nil, xwayV, edges)
			return true
		}
	}
	return false
}

// handleSurfaceClick ensures pointer focus and keyboard focus are set correctly
// when a button press hits a surface.
func (s *server) handleSurfaceClick(xdgV *xdgView, xwayV *xwayView, surface wlr.Surface, sx, sy float64) {
	// Focus window (but not override-redirect popups — they receive input
	// without stealing focus from their parent)
	if xdgV != nil {
		if s.activeXdg == xdgV {
			// Same window — pointer focus is already correct from motion events.
			// Don't send redundant wl_pointer.enter (causes XWayland clients to
			// dismiss popups) or keyboard leave+enter cycles.
			// But sync keyboard focus in case it was lost to an overlay.
			s.syncKeyboardFocus()
		} else {
			if surface.Valid() {
				s.safePointerEnter(surface, sx, sy)
			}
			s.focusXdgView(xdgV)
		}
	} else if xwayV != nil && !xwayV.isPanel && !xwayV.overrideRedirect {
		// Skip restack/focus for overlay, panel utility, and already-active windows.
		if !xwayV.isOverlay && !strings.Contains(xwayV.surface.Title(), "FyneDesk:skip") {
			if s.activeXway != xwayV {
				if surface.Valid() {
					s.safePointerEnter(surface, sx, sy)
				}
				restackXwaylandSurfaceAbove(xwayV.surface)
				s.focusXwayView(xwayV)
			} else {
				// Already-active window: don't restack or send pointer enter
				// (would dismiss Firefox popups), but sync keyboard focus in case
				// it was stolen by an overlay/menu that didn't restore properly.
				s.syncKeyboardFocus()
			}
		} else {
			// Overlay/skip windows: update keyboard focus without restacking.
			keyboard := s.seat.Keyboard()
			if keyboardValid(keyboard) {
				surf := xwayV.surface.Surface()
				if surf.Valid() {
					s.seat.KeyboardNotifyEnter(surf, keyboard.Keycodes(), keyboard.Modifiers())
				}
			}
		}
	} else if xwayV != nil && xwayV.overrideRedirect && !xwayV.isOverlay && !xwayV.isPanel {
		// Override-redirect popups (e.g. Firefox autocomplete, Bitwarden extension):
		// Don't send pointer enter — it causes Firefox to dismiss the popup.
		// Pointer focus is already on the correct surface from motion events.
		// Only ensure keyboard focus goes to the parent so key events reach the app.
		keyboard := s.seat.Keyboard()
		if keyboardValid(keyboard) {
			var focusSurf wlr.Surface
			if xwayV.parent != nil && xwayV.parent.mapped {
				focusSurf = xwayV.parent.surface.Surface()
			} else if s.activeXway != nil && s.activeXway.mapped {
				focusSurf = s.activeXway.surface.Surface()
			} else {
				focusSurf = xwayV.surface.Surface()
			}
			if focusSurf.Valid() {
				s.seat.KeyboardNotifyEnter(focusSurf, keyboard.Keycodes(), keyboard.Modifiers())
			}
		}
	} else {
		// Click on panel, desktop background, or unhandled surface type:
		// update pointer focus so the button event reaches the right client.
		if surface.Valid() {
			s.safePointerEnter(surface, sx, sy)
		}
	}
}

// handleButtonRelease ends interactive grabs on button release.
func (s *server) handleButtonRelease(button wlr.CursorButton) {
	if s.grab == grabMove && button == 272 { // BTN_LEFT ends move
		s.endGrab()
	} else if s.grab == grabResize && (button == 272 || button == 273) { // Either button ends resize
		s.endGrab()
	}
}

func (s *server) handleCursorFrame() {
	s.seat.PointerNotifyFrame()
}

func (s *server) handleCursorAxis(p wlr.Pointer, t time.Time, source wlr.AxisSource, orientation wlr.AxisOrientation, delta float64, deltaDiscrete int32) {
	if s.naturalScroll {
		delta = -delta
		deltaDiscrete = -deltaDiscrete
	}

	if s.locked {
		return
	}

	// WM modifier + scroll = per-window opacity adjustment on window under cursor
	kb := s.seat.Keyboard()
	if keyboardValid(kb) {
		mods := kb.GetModifiers()
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

	s.seat.PointerNotifyAxis(t, orientation, delta, deltaDiscrete, source)
}

func (s *server) handleSetCursorRequest(client wlr.SeatClient, surface wlr.Surface, serial uint32, hotspotX, hotspotY int32) {
	// Don't let client override cursor when compositor controls it (SSD border/grab)
	if s.ssdBorderHover || s.grab != grabNone {
		return
	}
	focusedClient := s.seat.PointerState().FocusedClient()
	if focusedClient == client {
		s.cursor.SetSurface(surface, hotspotX, hotspotY)
	}
}

func (s *server) processCursorMotion(t time.Time) {
	// Update drag icon position (if a client drag is in progress)
	s.updateDragIconPos(int(s.cursor.X()), int(s.cursor.Y()))

	// Update region screenshot selection visual as cursor moves
	if s.regionSelectActive {
		s.updateRegionSelect()
		return
	}

	// When locked, route pointer to the lock surface under cursor
	if s.locked {
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

// processGrabMove updates the grabbed view's position during an interactive move,
// applying edge magnetism and updating the snap preview.
func (s *server) processGrabMove() {
	dx := s.cursor.X() - s.grabX
	dy := s.cursor.Y() - s.grabY
	newX := s.grabViewX + dx
	newY := s.grabViewY + dy

	// Apply edge magnetism (snap to screen edges and other windows)
	newX, newY = s.applyEdgeMagnet(newX, newY)

	if s.grabXdg != nil {
		s.grabXdg.x = newX
		s.grabXdg.y = newY
		setXdgScenePos(s.grabXdg)
	} else if s.grabXway != nil {
		s.grabXway.x = newX
		s.grabXway.y = newY
		s.grabXway.surface.Configure(int16(newX), int16(newY),
			uint16(s.grabXway.surface.Width()), uint16(s.grabXway.surface.Height()))
		setXwayScenePos(s.grabXway)
	}

	// Update snap zone for edge snap on release
	s.updateSnapPreview()
}

// clampSize enforces min/max size constraints, adjusting position for left/top
// edge resizes so the opposite edge stays anchored.
func (s *server) clampSize(newX, newY float64, newWidth, newHeight, minW, minH, maxW, maxH int) (float64, float64, int, int) {
	if newWidth < minW {
		newWidth = minW
		if s.grabEdges&wlr.EdgeLeft != 0 {
			newX = s.grabViewX + float64(s.grabWidth-minW)
		}
	}
	if maxW > 0 && newWidth > maxW {
		newWidth = maxW
		if s.grabEdges&wlr.EdgeLeft != 0 {
			newX = s.grabViewX + float64(s.grabWidth-maxW)
		}
	}
	if newHeight < minH {
		newHeight = minH
		if s.grabEdges&wlr.EdgeTop != 0 {
			newY = s.grabViewY + float64(s.grabHeight-minH)
		}
	}
	if maxH > 0 && newHeight > maxH {
		newHeight = maxH
		if s.grabEdges&wlr.EdgeTop != 0 {
			newY = s.grabViewY + float64(s.grabHeight-maxH)
		}
	}
	return newX, newY, newWidth, newHeight
}

// processGrabResize updates the grabbed view's size during an interactive resize,
// enforcing client size constraints.
func (s *server) processGrabResize() {
	dx := s.cursor.X() - s.grabX
	dy := s.cursor.Y() - s.grabY

	newX := s.grabViewX
	newY := s.grabViewY
	newWidth := s.grabWidth
	newHeight := s.grabHeight

	// Adjust based on which edges are being resized
	if s.grabEdges&wlr.EdgeLeft != 0 {
		newX = s.grabViewX + dx
		newWidth = s.grabWidth - int(dx)
	} else if s.grabEdges&wlr.EdgeRight != 0 {
		newWidth = s.grabWidth + int(dx)
	}

	if s.grabEdges&wlr.EdgeTop != 0 {
		newY = s.grabViewY + dy
		newHeight = s.grabHeight - int(dy)
	} else if s.grabEdges&wlr.EdgeBottom != 0 {
		newHeight = s.grabHeight + int(dy)
	}

	const fallbackMinSize = 50

	if s.grabXdg != nil {
		minW, minH := s.grabXdg.minWidth, s.grabXdg.minHeight
		maxW, maxH := s.grabXdg.maxWidth, s.grabXdg.maxHeight
		if minW == 0 {
			minW = fallbackMinSize
		}
		if minH == 0 {
			minH = fallbackMinSize
		}
		newX, newY, newWidth, newHeight = s.clampSize(newX, newY, newWidth, newHeight, minW, minH, maxW, maxH)

		s.grabXdg.x = newX
		s.grabXdg.y = newY
		s.grabXdg.configuredW = newWidth
		s.grabXdg.configuredH = newHeight
		s.grabXdg.xdgToplevel.SetSize(int32(newWidth), int32(newHeight))
		setXdgScenePos(s.grabXdg)
		s.updateXdgViewDecorations(s.grabXdg)
	} else if s.grabXway != nil {
		minW, minH := s.grabXway.minWidth, s.grabXway.minHeight
		maxW, maxH := s.grabXway.maxWidth, s.grabXway.maxHeight
		if minW == 0 {
			minW = fallbackMinSize
		}
		if minH == 0 {
			minH = fallbackMinSize
		}
		newX, newY, newWidth, newHeight = s.clampSize(newX, newY, newWidth, newHeight, minW, minH, maxW, maxH)

		s.grabXway.x = newX
		s.grabXway.y = newY
		s.grabXway.surface.Configure(int16(newX), int16(newY), uint16(newWidth), uint16(newHeight))
		setXwayScenePos(s.grabXway)
		s.updateXwayViewDecorations(s.grabXway)
	}
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
		geo := xdgV.xdgToplevel.Base().GetGeometry()
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
			geo := xdgV.xdgToplevel.Base().GetGeometry()
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
	focused := s.seat.PointerState().FocusedSurface()
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
		geo := s.implicitGrabXdg.xdgToplevel.Base().GetGeometry()
		sx := cx - s.implicitGrabXdg.x + float64(geo.Min.X)
		sy := cy - s.implicitGrabXdg.y + float64(geo.Min.Y)
		s.seat.PointerNotifyMotion(t, sx, sy)
	} else if s.activeXway != nil && s.activeXway.mapped && !s.activeXway.isPanel {
		// Fallback to activeXway for clicks that didn't go through handleCursorButton
		sx := cx - s.activeXway.x
		sy := cy - s.activeXway.y
		s.seat.PointerNotifyMotion(t, sx, sy)
	} else if s.activeXdg != nil && s.activeXdg.mapped {
		geo := s.activeXdg.xdgToplevel.Base().GetGeometry()
		sx := cx - s.activeXdg.x + float64(geo.Min.X)
		sy := cy - s.activeXdg.y + float64(geo.Min.Y)
		s.seat.PointerNotifyMotion(t, sx, sy)
	}
}
