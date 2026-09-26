package compositor

import (
	"log"
	"strings"
	"time"

	"fyshos.com/tyde/internal/wayland/wlr"
)

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
			geo := xdgV.xdgToplevel.Base().Geometry()
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
			s.minimizeWithEffect(decoXdg, nil)
		} else if decoXway != nil {
			s.minimizeWithEffect(decoXway, nil)
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
			geo := xdgV.xdgToplevel.Base().Geometry()
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
		if !xwayV.isOverlay && !strings.Contains(xwayV.surface.Title(), "Tyde:skip") {
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
