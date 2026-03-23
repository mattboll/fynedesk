package compositor

import "deedles.dev/wlr"

// hitTestDecoration checks if cursor is in a decoration zone.
// Uses edgeHitSize (not borderWidth) for the interactive hit zone so it matches
// the resize cursor zone in cursorNearBorder.
func (s *server) hitTestDecoration(cursorX, cursorY, viewX, viewY float64, width, height int) decoZone {
	// Titlebar is above the window content
	titlebarY := viewY - float64(titlebarHeight)
	hitSize := float64(edgeHitSize)
	resizeGrip := 5.0 // Top resize grip inside titlebar (like GNOME/Sway)

	// Check top resize edge: above titlebar + first few pixels of titlebar
	if cursorY >= titlebarY-hitSize && cursorY < titlebarY+resizeGrip &&
		cursorX >= viewX-hitSize && cursorX < viewX+float64(width)+hitSize {
		return decoBorder
	}

	// Check if in titlebar area (below the resize grip)
	if cursorY >= titlebarY && cursorY < viewY && cursorX >= viewX && cursorX < viewX+float64(width) {
		// Check buttons (right side of titlebar)
		buttonY := titlebarY + float64(buttonMargin)
		buttonBottom := buttonY + float64(buttonSize)

		if cursorY >= buttonY && cursorY < buttonBottom {
			var closeX, maxX, minX float64
			if s.buttonsOnLeft {
				closeX = viewX + float64(buttonMargin)
				maxX = closeX + float64(buttonSize+buttonMargin)
				minX = maxX + float64(buttonSize+buttonMargin)
			} else {
				closeX = viewX + float64(width) - float64(buttonSize+buttonMargin)
				maxX = closeX - float64(buttonSize+buttonMargin)
				minX = maxX - float64(buttonSize+buttonMargin)
			}

			if cursorX >= closeX && cursorX < closeX+float64(buttonSize) {
				return decoCloseButton
			}
			if cursorX >= maxX && cursorX < maxX+float64(buttonSize) {
				return decoMaxButton
			}
			if cursorX >= minX && cursorX < minX+float64(buttonSize) {
				return decoMinButton
			}
		}

		return decoTitlebar
	}

	// Check side and bottom borders (using edgeHitSize for usable grab zone)
	if cursorX >= viewX-hitSize && cursorX < viewX+float64(width)+hitSize &&
		cursorY >= titlebarY && cursorY < viewY+float64(height)+hitSize {
		// In border area but not in content
		if cursorX < viewX || cursorX >= viewX+float64(width) ||
			cursorY >= viewY+float64(height) {
			return decoBorder
		}
	}

	return decoNone
}

// updateButtonHover checks if the cursor is over a titlebar button and
// triggers a titlebar re-render with glow effect when the hover state changes.
func (s *server) updateButtonHover(cx, cy float64) {
	decoXdg, decoXway, zone := s.viewAtDecoration(cx, cy)

	// Only care about button zones
	var btn decoZone
	switch zone {
	case decoCloseButton, decoMaxButton, decoMinButton:
		btn = zone
	default:
		btn = decoNone
	}

	// Check if hover state changed
	if btn == s.hoverButton && decoXdg == s.hoverXdg && decoXway == s.hoverXway {
		return
	}

	// Clear previous hover — re-render without glow
	if s.hoverXdg != nil && s.hoverXdg.decorated {
		s.updateXdgViewDecorations(s.hoverXdg)
	}
	if s.hoverXway != nil && s.hoverXway.decorated {
		s.updateXwayViewDecorations(s.hoverXway)
	}

	s.hoverButton = btn
	s.hoverXdg = decoXdg
	s.hoverXway = decoXway

	// Render new hover glow
	if btn != decoNone {
		if decoXdg != nil && decoXdg.decorated {
			s.updateXdgViewDecorations(decoXdg)
		}
		if decoXway != nil && decoXway.decorated {
			s.updateXwayViewDecorations(decoXway)
		}
	}
}

func (s *server) computeResizeEdges(cursorX, cursorY, viewX, viewY float64, width, height int) wlr.Edges {
	// Relative position in window (0.0 to 1.0)
	relX := (cursorX - viewX) / float64(width)
	relY := (cursorY - viewY) / float64(height)

	var edges wlr.Edges

	// Horizontal: left third, middle, right third
	if relX < 0.33 {
		edges |= wlr.EdgeLeft
	} else if relX > 0.66 {
		edges |= wlr.EdgeRight
	}

	// Vertical: top third, middle, bottom third
	if relY < 0.33 {
		edges |= wlr.EdgeTop
	} else if relY > 0.66 {
		edges |= wlr.EdgeBottom
	}

	// If in the center, default to bottom-right
	if edges == wlr.EdgeNone {
		edges = wlr.EdgeBottom | wlr.EdgeRight
	}

	return edges
}

// sendPointerEnterIfOver sends a pointer enter event to the surface if the cursor
// is currently within the given bounds. This ensures the first click on a newly
// shown overlay works without requiring mouse movement first.
func (s *server) sendPointerEnterIfOver(viewX, viewY, viewW, viewH float64, surface wlr.Surface) {
	cx, cy := s.cursor.X(), s.cursor.Y()
	if cx >= viewX && cx < viewX+viewW && cy >= viewY && cy < viewY+viewH && surface.Valid() {
		sx := cx - viewX
		sy := cy - viewY
		s.safePointerEnter(surface, sx, sy)
	}
}
