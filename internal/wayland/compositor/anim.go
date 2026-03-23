package compositor

import (
	"math"
	"time"
)

// viewAnim holds state for animating a window's position and opacity.
type viewAnim struct {
	active         bool
	startX, startY float64
	endX, endY     float64
	startTime      time.Time
	duration       time.Duration
	fadeIn          bool    // true = also animate opacity from startOpacity to targetOpacity
	startOpacity    float32 // opacity at start of fade-in
	targetOpacity   float32 // opacity to reach at end of fade-in
}

const animDuration = 250 * time.Millisecond

// easeOutCubic applies an ease-out cubic curve: 1 - (1-t)^3
// Kept for non-spring animations (transitions, boot sequence).
func easeOutCubic(t float64) float64 {
	t = 1 - t
	return 1 - t*t*t
}

// dampedSpring simulates a critically-damped spring with slight overshoot.
// Returns a value that starts at 0, overshoots ~1.05, then settles to 1.0.
// Parameters tuned for snappy window animations: ζ=0.7, ω=12.
func dampedSpring(t float64) float64 {
	if t >= 1 {
		return 1
	}
	const (
		zeta  = 0.7  // damping ratio (<1 = underdamped = overshoot)
		omega = 12.0 // angular frequency (higher = faster oscillation)
	)
	decay := math.Exp(-zeta * omega * t)
	osc := math.Cos(omega * math.Sqrt(1-zeta*zeta) * t)
	return 1 - decay*osc
}

// tickViewAnims advances all active view position animations and returns
// true if any animation is still running (so renderOutput can schedule
// continuous frames).
func (s *server) tickViewAnims() bool {
	if s.reduceMotion {
		// Instantly complete all pending animations
		for _, v := range s.xdgViews {
			if v.anim.active {
				v.x = v.anim.endX
				v.y = v.anim.endY
				if v.anim.fadeIn {
					s.setXdgViewOpacity(v, v.anim.targetOpacity)
				}
				v.anim.active = false
				setXdgScenePos(v)
				s.updateXdgViewDecorations(v)
			}
		}
		for _, v := range s.xwayViews {
			if v.anim.active {
				v.x = v.anim.endX
				v.y = v.anim.endY
				if v.anim.fadeIn {
					s.setXwayViewOpacity(v, v.anim.targetOpacity)
				}
				v.anim.active = false
				setXwayScenePos(v)
				s.updateXwayViewDecorations(v)
			}
		}
		return false
	}

	now := time.Now()
	anyActive := false

	for _, v := range s.xdgViews {
		if !v.anim.active {
			continue
		}
		elapsed := now.Sub(v.anim.startTime)
		if elapsed >= v.anim.duration {
			// Animation complete — snap to final position
			v.x = v.anim.endX
			v.y = v.anim.endY
			v.anim.active = false
			if v.anim.fadeIn {
				s.setXdgViewOpacity(v, v.anim.targetOpacity)
				v.anim.fadeIn = false
				v.hideDecorations = false
			}
			setXdgScenePos(v)
			s.updateXdgViewDecorations(v)
		} else {
			raw := float64(elapsed) / float64(v.anim.duration)
			// Use easeOutCubic for fade-in (visible deceleration),
			// dampedSpring for regular position animations.
			var t float64
			if v.anim.fadeIn {
				t = easeOutCubic(raw)
			} else {
				t = dampedSpring(raw)
			}
			v.x = v.anim.startX + (v.anim.endX-v.anim.startX)*t
			v.y = v.anim.startY + (v.anim.endY-v.anim.startY)*t
			setXdgScenePos(v)
			if v.anim.fadeIn {
				// Opacity ramps from startOpacity to targetOpacity over full duration
				op := v.anim.startOpacity + float32(easeOutCubic(raw))*(v.anim.targetOpacity-v.anim.startOpacity)
				s.setXdgViewOpacity(v, op)
			} else {
				s.updateXdgViewDecorations(v)
			}
			anyActive = true
		}
	}

	for _, v := range s.xwayViews {
		if !v.anim.active {
			continue
		}
		elapsed := now.Sub(v.anim.startTime)
		if elapsed >= v.anim.duration {
			v.x = v.anim.endX
			v.y = v.anim.endY
			v.anim.active = false
			if v.anim.fadeIn {
				s.setXwayViewOpacity(v, v.anim.targetOpacity)
				v.anim.fadeIn = false
				v.hideDecorations = false
			}
			setXwayScenePos(v)
			s.updateXwayViewDecorations(v)
		} else {
			raw := float64(elapsed) / float64(v.anim.duration)
			var t float64
			if v.anim.fadeIn {
				t = easeOutCubic(raw)
			} else {
				t = dampedSpring(raw)
			}
			v.x = v.anim.startX + (v.anim.endX-v.anim.startX)*t
			v.y = v.anim.startY + (v.anim.endY-v.anim.startY)*t
			setXwayScenePos(v)
			if v.anim.fadeIn {
				op := v.anim.startOpacity + float32(easeOutCubic(raw))*(v.anim.targetOpacity-v.anim.startOpacity)
				s.setXwayViewOpacity(v, op)
			} else {
				s.updateXwayViewDecorations(v)
			}
			anyActive = true
		}
	}

	return anyActive
}

// animateXdgPos starts a position animation for an XDG view.
func (s *server) animateXdgPos(v *xdgView, fromX, fromY, toX, toY float64) {
	s.animateXdgPosOpts(v, fromX, fromY, toX, toY, false)
}

// animateXdgPosOpts starts a position animation with optional fade-in.
func (s *server) animateXdgPosOpts(v *xdgView, fromX, fromY, toX, toY float64, fadeIn bool) {
	targetOpacity := v.opacity
	if targetOpacity <= 0 {
		targetOpacity = 1.0
	}
	var startOpacity float32
	if fadeIn {
		startOpacity = 0.4 // Start visible so the slide is perceptible
	}
	v.anim = viewAnim{
		active:        true,
		startX:        fromX,
		startY:        fromY,
		endX:          toX,
		endY:          toY,
		startTime:     time.Now(),
		duration:      animDuration,
		fadeIn:         fadeIn,
		startOpacity:  startOpacity,
		targetOpacity: targetOpacity,
	}
	v.x = fromX
	v.y = fromY
	setXdgScenePos(v)
	s.updateXdgViewDecorations(v)
	if fadeIn {
		s.setXdgViewOpacity(v, startOpacity)
	}
}

// animateXwayPos starts a position animation for an XWayland view.
func (s *server) animateXwayPos(v *xwayView, fromX, fromY, toX, toY float64) {
	s.animateXwayPosOpts(v, fromX, fromY, toX, toY, false)
}

// animateXwayPosOpts starts a position animation with optional fade-in.
func (s *server) animateXwayPosOpts(v *xwayView, fromX, fromY, toX, toY float64, fadeIn bool) {
	targetOpacity := v.opacity
	if targetOpacity <= 0 {
		targetOpacity = 1.0
	}
	var startOpacity float32
	if fadeIn {
		startOpacity = 0.4 // Start visible so the slide is perceptible
	}
	v.anim = viewAnim{
		active:        true,
		startX:        fromX,
		startY:        fromY,
		endX:          toX,
		endY:          toY,
		startTime:     time.Now(),
		duration:      animDuration,
		fadeIn:         fadeIn,
		startOpacity:  startOpacity,
		targetOpacity: targetOpacity,
	}
	v.x = fromX
	v.y = fromY
	setXwayScenePos(v)
	s.updateXwayViewDecorations(v)
	if fadeIn {
		s.setXwayViewOpacity(v, startOpacity)
	}
}
