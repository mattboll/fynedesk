package compositor

import "log"

// Tiling mode: optional master+stack layout.
//
// When tiling is enabled on a desktop, windows are arranged automatically:
//   - 1 window: fills the content area
//   - 2+ windows: master on the left (configurable ratio), stack on the right
//   - Windows can be individually floated with a toggle
//   - Adding/removing/closing windows retiles automatically
//
// Keybindings (all require WM modifier):
//   Super+Shift+T  — toggle tiling on/off for current desktop
//   Super+Shift+J  — swap master (promote focused stack window)
//   Super+Shift+H  — shrink master ratio
//   Super+Shift+L  — grow master ratio

const (
	defaultMasterRatio = 0.55 // Master takes 55% of content width
	masterRatioStep    = 0.05 // Each step changes ratio by 5%
	masterRatioMin     = 0.25
	masterRatioMax     = 0.80
	tileGapDefault     = 6 // Default pixel gap between tiled windows (overridden by settings)
)

// tilingState holds per-desktop tiling configuration.
type tilingState struct {
	enabled     bool
	masterRatio float64
}

// initTiling initializes tiling state for all desktops.
func (s *server) initTiling() {
	s.tiling = make([]tilingState, s.numDesks)
	for i := range s.tiling {
		s.tiling[i].masterRatio = defaultMasterRatio
	}
}

// toggleTiling enables/disables tiling on the current desktop.
func (s *server) toggleTiling() {
	if s.tiling == nil {
		s.initTiling()
	}
	desk := s.currentDesk
	if desk < 0 || desk >= len(s.tiling) {
		// Grow the slice if numDesks changed since init
		if desk >= 0 && desk < s.numDesks {
			for len(s.tiling) <= desk {
				s.tiling = append(s.tiling, tilingState{masterRatio: defaultMasterRatio})
			}
		} else {
			log.Printf("[TILING] toggle ignored: desk=%d out of range (len=%d, numDesks=%d)", desk, len(s.tiling), s.numDesks)
			return
		}
	}
	s.tiling[desk].enabled = !s.tiling[desk].enabled
	log.Printf("[TILING] desktop %d tiling=%v", desk, s.tiling[desk].enabled)
	if s.tiling[desk].enabled {
		s.retile()
	}
}

// isTilingEnabled returns whether tiling is active on the current desktop.
func (s *server) isTilingEnabled() bool {
	if s.tiling == nil {
		return false
	}
	desk := s.currentDesk
	if desk < 0 || desk >= len(s.tiling) {
		return false
	}
	return s.tiling[desk].enabled
}

// tileableWindows returns windows eligible for tiling on the current desktop
// and on the given output. Excludes panels, overlays, dialogs, fullscreen, and floating windows.
func (s *server) tileableWindows(outGeo outputGeometry) (xdgs []*xdgView, xways []*xwayView) {
	for _, v := range s.xdgViews {
		if v.mapped && v.parent == nil && !v.fullscreen && !v.floating &&
			v.onDesk(s.currentDesk) && s.viewOnOutput(v.x, v.y, outGeo) {
			xdgs = append(xdgs, v)
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.isOverlay && v.parent == nil &&
			!v.fullscreen && !v.floating && v.onDesk(s.currentDesk) &&
			s.viewOnOutput(v.x, v.y, outGeo) {
			xways = append(xways, v)
		}
	}
	return
}

// viewOnOutput returns true if the view's position is within the given output bounds.
func (s *server) viewOnOutput(vx, vy float64, outGeo outputGeometry) bool {
	x, y := int(vx), int(vy)
	return x >= outGeo.x && x < outGeo.x+outGeo.width &&
		y >= outGeo.y && y < outGeo.y+outGeo.height
}

// retile arranges all tileable windows in master+stack layout on the active output.
func (s *server) retile() {
	if !s.isTilingEnabled() {
		return
	}

	outGeo := s.getActiveOutputGeo()
	xdgs, xways := s.tileableWindows(outGeo)
	n := len(xdgs) + len(xways)
	if n == 0 {
		return
	}

	cx, cy, cw, ch := s.contentBounds(outGeo)

	// Build ordered list: focused window first (master), rest in stack
	type tileView struct {
		xdg  *xdgView
		xway *xwayView
	}

	views := make([]tileView, 0, n)

	// Master = active window (if tileable), else first window
	var masterSet bool
	if s.activeXdg != nil && !s.activeXdg.floating && !s.activeXdg.fullscreen &&
		s.activeXdg.mapped && s.activeXdg.parent == nil && s.activeXdg.onDesk(s.currentDesk) {
		views = append(views, tileView{xdg: s.activeXdg})
		masterSet = true
	} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay &&
		!s.activeXway.floating && !s.activeXway.fullscreen &&
		s.activeXway.mapped && s.activeXway.parent == nil && s.activeXway.onDesk(s.currentDesk) {
		views = append(views, tileView{xway: s.activeXway})
		masterSet = true
	}

	// Add remaining windows (preserving order)
	for _, v := range xdgs {
		if masterSet && v == s.activeXdg {
			continue // Already master
		}
		views = append(views, tileView{xdg: v})
	}
	for _, v := range xways {
		if masterSet && v == s.activeXway {
			continue
		}
		views = append(views, tileView{xway: v})
	}

	ratio := s.tiling[s.currentDesk].masterRatio
	o := s.outerGap // gap from screen edges
	i := s.innerGap // gap between adjacent windows

	for idx, tv := range views {
		var x, y float64
		var w, h int
		var topMargin int

		if (tv.xdg != nil && tv.xdg.decorated) || (tv.xway != nil && tv.xway.decorated) {
			topMargin = titlebarHeight
		}

		if n == 1 {
			// Single window: fill entire content area with outer gaps
			x = float64(cx + o)
			y = float64(cy + topMargin + o)
			w = cw - o*2
			h = ch - topMargin - o*2
		} else if idx == 0 {
			// Master: left portion with outer gap on left, inner gap on right
			masterW := int(float64(cw) * ratio)
			x = float64(cx + o)
			y = float64(cy + topMargin + o)
			w = masterW - o - i/2
			h = ch - topMargin - o*2
		} else {
			// Stack: right portion, divided vertically
			stackCount := n - 1
			stackIdx := idx - 1

			masterW := int(float64(cw) * ratio)
			stackX := cx + masterW + i/2
			stackW := cw - masterW - i/2 - o

			stackTotalH := ch - topMargin - o*2
			perH := (stackTotalH - i*(stackCount-1)) / stackCount
			stackY := cy + topMargin + o + stackIdx*(perH+i)

			x = float64(stackX)
			y = float64(stackY)
			w = stackW
			h = perH
		}

		// Apply layout
		if tv.xdg != nil {
			v := tv.xdg
			oldX, oldY := v.x, v.y
			v.snapped = snapNone
			v.maximized = false
			v.configuredW = w
			v.configuredH = h
			v.xdgToplevel.SetSize(int32(w), int32(h))
			s.animateXdgPos(v, oldX, oldY, x, y)
		} else if tv.xway != nil {
			v := tv.xway
			oldX, oldY := v.x, v.y
			v.snapped = snapNone
			v.maximized = false
			v.surface.Configure(int16(x), int16(y), uint16(w), uint16(h))
			s.animateXwayPos(v, oldX, oldY, x, y)
		}
	}

	s.writeWindowsState()
}

// swapMaster promotes the focused stack window to master position.
func (s *server) swapMaster() {
	if !s.isTilingEnabled() {
		return
	}
	// retile already puts the focused window in master position
	s.retile()
}

// adjustMasterRatio changes the master width ratio.
func (s *server) adjustMasterRatio(delta float64) {
	if !s.isTilingEnabled() {
		return
	}
	desk := s.currentDesk
	if desk < 0 || desk >= len(s.tiling) {
		return
	}
	r := s.tiling[desk].masterRatio + delta
	if r < masterRatioMin {
		r = masterRatioMin
	}
	if r > masterRatioMax {
		r = masterRatioMax
	}
	s.tiling[desk].masterRatio = r
	s.retile()
}

// toggleWindowFloat makes the focused window float or un-float in tiling mode.
func (s *server) toggleWindowFloat() {
	if !s.isTilingEnabled() {
		return
	}
	if s.activeXdg != nil {
		s.activeXdg.floating = !s.activeXdg.floating
		if !s.activeXdg.floating {
			// Unfloated: retile to include it
			s.retile()
		}
	} else if s.activeXway != nil && !s.activeXway.isPanel {
		s.activeXway.floating = !s.activeXway.floating
		if !s.activeXway.floating {
			s.retile()
		}
	}
}
