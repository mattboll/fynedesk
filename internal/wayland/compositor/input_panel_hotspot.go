package compositor

import (
	"log"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// checkPanelHotspot implements macOS-style panel auto-reveal when the bar is
// covered by a window (fullscreen, manually moved, or oversized). Hovering the
// cursor at the screen edge for panelRevealDelay raises the panelTree above windowsTree
// so the bar becomes visible and clickable through the transparent panel.
func (s *server) checkPanelHotspot() {
	out := s.primaryOutput()
	if out == nil {
		return
	}

	// Only process panel hotspot when cursor is on the primary output.
	// Without this check, moving the cursor to a secondary output positioned
	// adjacent to the bar edge (e.g., laptop screen below the main screen)
	// falsely triggers the panel reveal because the cursor Y coordinate
	// passes the primary output's bottom edge check.
	if s.getActiveOutput() != out {
		if s.panelRevealed {
			s.hidePanelHotspot()
		}
		s.cancelEdgeHoverTimer()
		return
	}

	outGeo := s.getOutputGeometry(out)
	cx := s.cursor.X()
	cy := s.cursor.Y()

	// Edge zone: panelEdgeZone pixels from screen edge (wider than 1px for easier targeting)
	atEdge := false
	switch s.barPosition {
	case "bottom":
		atEdge = cy >= float64(outGeo.y+outGeo.height-panelEdgeZone)
	default: // "left"
		atEdge = cx <= float64(outGeo.x)+panelEdgeZone
	}

	if atEdge && !s.panelRevealed && s.isBarCovered() {
		// Cursor at edge and bar is covered — start timer if not already running
		if s.edgeHoverTimer == nil {
			s.edgeHoverTimer = time.AfterFunc(panelRevealDelay, func() {
				s.mainThreadActions <- func() {
					s.revealPanelHotspot()
				}
				s.triggerWakeup()
			})
		}
		return
	}

	if !atEdge && !s.panelRevealed {
		// Cursor left edge before timer fired — cancel
		s.cancelEdgeHoverTimer()
		return
	}

	if s.panelRevealed {
		// Panel is revealed — check if cursor is still in the bar interaction zone.
		// Use a generous zone (barH * 1.5) so the user can comfortably
		// move the cursor to click on bar icons without the panel snapping away.
		// If a click occurred (hotspotClickLatched), use an even larger zone
		// so the panel stays visible while the user interacts.
		barH := float64(s.barOverlayHeight())
		zone := barH * 1.5
		if s.hotspotClickLatched {
			zone = barH * 2.5
		}
		inBar := false
		switch s.barPosition {
		case "bottom":
			inBar = cy >= float64(outGeo.y+outGeo.height)-zone &&
				cy <= float64(outGeo.y+outGeo.height)
		default: // "left"
			inBar = cx < float64(outGeo.x)+zone &&
				cx >= float64(outGeo.x)
		}
		if !inBar {
			s.hidePanelHotspot()
		}
	}
}

// isBarCovered checks if a non-panel window is covering any part of the bar area.
// Probes multiple points along the bar to detect partial coverage.
// Only checks the primary output (where the panel lives).
func (s *server) isBarCovered() bool {
	out := s.primaryOutput()
	if out == nil || s.getActiveOutput() != out {
		return false
	}
	outGeo := s.getOutputGeometry(out)
	barH := s.barOverlayHeight()

	// Probe at 25%, 50%, 75% along the bar to detect partial coverage
	for _, frac := range []float64{0.25, 0.5, 0.75} {
		var probeX, probeY float64
		switch s.barPosition {
		case "bottom":
			probeX = float64(outGeo.x) + float64(outGeo.width)*frac
			probeY = float64(outGeo.y+outGeo.height) - float64(barH)/2
		default: // "left"
			probeX = float64(outGeo.x) + float64(barWidth)/2
			probeY = float64(outGeo.y) + float64(outGeo.height)*frac
		}

		xdgV, xwayV, _, _, _ := s.viewAt(probeX, probeY)
		if xdgV != nil {
			return true
		}
		if xwayV != nil && !xwayV.isPanel && !xwayV.isOverlay {
			return true
		}
	}
	return false
}

// revealPanelHotspot raises the panelTree above windowsTree and fullscreenTree
// so the transparent panel (bar + widgets) becomes visible above any covering
// windows. The panel uses ARGB8888 (DMA-BUF) with SetTransparent(true), so
// areas without widgets are alpha-blended naturally — no opaque_region hack needed.
//
// When the output is scaled for fullscreen (e.g., scale 1.839 for a lower-res
// game), the panel (1280x720 XWayland surface) doesn't match the layout
// (696x391). We temporarily restore scale 1.0 so both the panel and the
// fullscreen window render at native resolution.
func (s *server) revealPanelHotspot() {
	if s.panelRevealed {
		return
	}
	if s.panelXway == nil || !s.panelXway.mapped {
		return
	}
	s.panelRevealed = true
	s.hotspotClickLatched = false
	s.edgeHoverTimer = nil

	// If the output is scaled for fullscreen, temporarily restore scale 1.0.
	// This makes the panel (native resolution) and the fullscreen window
	// render at the same size, so alpha compositing works correctly.
	out := s.primaryOutput()
	if out != nil && out.savedScale > 0 {
		currentScale := out.output.Scale()
		if currentScale != 1.0 {
			s.revealRestoreScale = currentScale
			out.output.SetScale(1.0)
			out.output.Commit()
			out.width, out.height = out.output.EffectiveResolution()
			// Tell the fullscreen window to resize to the (now larger) output
			s.resizeFullscreenToOutput(out)
			log.Printf("[HOTSPOT] Output scale restored: %.3f → 1.0, layout=%dx%d",
				currentScale, out.width, out.height)
		}
	}

	// Raise panelTree above overlayTree (topmost layer below switcher/lock)
	// so the bar is visible over any window including fullscreen.
	s.raisePanelAboveOverlay()

	// Tell the panel to hide desktop icons so they don't appear above windows.
	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventPanelHotspot, wlipc.PanelHotspotEvent{Raised: true})
	}

	log.Printf("[HOTSPOT] Panel revealed (z-order raised, blur disabled, barH=%d)\n", s.barOverlayHeight())
}

// barOverlayHeight returns the height (or width for left bar) of the bar area
// in pixels. For bottom bar, this is iconSize*zoomScale to accommodate
// the zoom effect. For left bar, this is the narrow bar width (36px).
func (s *server) barOverlayHeight() int {
	if s.barPosition == "bottom" {
		h := int(float64(s.launcherIconSize) * s.launcherZoomScale)
		if h < s.launcherIconSize {
			h = s.launcherIconSize
		}
		return h
	}
	return barWidth
}

// hidePanelHotspot restores the panelTree to its normal z-order (below windowsTree).
func (s *server) hidePanelHotspot() {
	if !s.panelRevealed {
		return
	}
	s.panelRevealed = false
	s.hotspotClickLatched = false
	s.cancelEdgeHoverTimer()

	// Restore panelTree below windowsTree (original z-order:
	// background < panel < windows < ... )
	s.lowerPanelBelowWindows()

	// If we temporarily restored the output scale during reveal,
	// re-apply the fullscreen scale now.
	if s.revealRestoreScale > 0 {
		out := s.primaryOutput()
		if out != nil {
			out.output.SetScale(s.revealRestoreScale)
			out.output.Commit()
			out.width, out.height = out.output.EffectiveResolution()
			// Resize fullscreen window to match the scaled output
			s.resizeFullscreenToOutput(out)
			log.Printf("[HOTSPOT] Output scale restored: 1.0 → %.3f, layout=%dx%d",
				s.revealRestoreScale, out.width, out.height)
		}
		s.revealRestoreScale = 0
	}

	// Tell the panel to show desktop icons again.
	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventPanelHotspot, wlipc.PanelHotspotEvent{Raised: false})
	}

	log.Printf("[HOTSPOT] Panel hidden (z-order restored)\n")
	s.dumpSceneOrder("after-hide")
}

// resizeFullscreenToOutput finds the active fullscreen window and resizes it
// to match the current output geometry. Used when the output scale changes
// during panel reveal/hide.
func (s *server) resizeFullscreenToOutput(out *outputState) {
	outGeo := s.getOutputGeometry(out)
	for _, v := range s.xdgViews {
		if v.fullscreen && v.mapped {
			v.x = float64(outGeo.x)
			v.y = float64(outGeo.y)
			v.xdgToplevel.SetSize(int32(outGeo.width), int32(outGeo.height))
			setXdgScenePos(v)
			log.Printf("[HOTSPOT] Resized fullscreen XDG %q to %dx%d",
				v.id, outGeo.width, outGeo.height)
			return
		}
	}
	for _, v := range s.xwayViews {
		if v.fullscreen && v.mapped && !v.isPanel {
			v.x = float64(outGeo.x)
			v.y = float64(outGeo.y)
			v.surface.Configure(int16(v.x), int16(v.y),
				uint16(outGeo.width), uint16(outGeo.height))
			setXwayScenePos(v)
			log.Printf("[HOTSPOT] Resized fullscreen XWayland %q to %dx%d",
				v.id, outGeo.width, outGeo.height)
			return
		}
	}
}

// isCursorInBarArea returns true if the cursor is within the bar's physical area
// on the primary output. Returns false if the cursor is on a different output.
func (s *server) isCursorInBarArea() bool {
	out := s.primaryOutput()
	if out == nil {
		return false
	}
	// Only consider the bar area when cursor is on the primary output
	if s.getActiveOutput() != out {
		return false
	}
	outGeo := s.getOutputGeometry(out)
	cx := s.cursor.X()
	cy := s.cursor.Y()
	switch s.barPosition {
	case "bottom":
		barH := s.barOverlayHeight()
		return cy >= float64(outGeo.y+outGeo.height-barH) &&
			cy <= float64(outGeo.y+outGeo.height)
	default: // "left"
		return cx <= float64(outGeo.x)+float64(barWidth) &&
			cx >= float64(outGeo.x)
	}
}

// latchPanelHotspotClick marks that a click occurred while the panel was revealed.
// This keeps the panel visible with a larger interaction zone until the cursor
// moves well away from the bar.
func (s *server) latchPanelHotspotClick() {
	if s.panelRevealed {
		s.hotspotClickLatched = true
	}
}

// cancelEdgeHoverTimer stops and clears the pending edge hover timer.
func (s *server) cancelEdgeHoverTimer() {
	if s.edgeHoverTimer != nil {
		s.edgeHoverTimer.Stop()
		s.edgeHoverTimer = nil
	}
}
