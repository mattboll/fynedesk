package compositor

import (
	"math"
	"time"
)

// Switching desktops with three fingers: the windows of the current desktop
// and of the next one follow the fingers side by side; on release the switch
// completes, or the desktop comes back if the swipe was short.
const (
	deskSwipeGain     = 1.6                    // screen pixels per touchpad unit
	deskSwipeStart    = 12.0                   // touchpad units before it starts
	deskSwipeCommit   = 0.2                    // share of the screen past which the switch happens
	deskSwipeDuration = 220 * time.Millisecond // the end of the slide
)

type deskSwipe struct {
	offset float64 // horizontal shift of the current desktop
	width  float64 // of the output under the pointer

	// End of the swipe: the slide to the final offset.
	ending    bool
	commit    bool
	from, to  float64
	startTime time.Time
}

// neighbourDesk returns the desktop that comes in when the current one
// moves by offset: the next one from the right when it moves left.
func (s *server) neighbourDesk(offset float64) int {
	d := s.currentDesk + 1
	if offset > 0 {
		d = s.currentDesk - 1
	}
	return (d + s.numDesks) % s.numDesks
}

// updateDeskSwipe follows the fingers; dx is the horizontal distance
// travelled on the touchpad since the swipe began. It reports whether the
// swipe drives the desktops.
func (s *server) updateDeskSwipe(dx, dy float64) bool {
	if s.reduceMotion || s.overviewActive || s.numDesks < 2 {
		return false
	}
	if s.deskSwipe == nil {
		if math.Abs(dx) < deskSwipeStart || math.Abs(dx) < 1.2*math.Abs(dy) {
			return false
		}
		out := s.getActiveOutput()
		if out == nil {
			return false
		}
		s.deskSwipe = &deskSwipe{width: float64(out.width)}
	}
	sw := s.deskSwipe
	if sw.ending {
		return true
	}
	sw.offset = math.Max(-sw.width, math.Min(sw.width, dx*deskSwipeGain))
	s.showDeskSwipe()
	return true
}

// endDeskSwipe finishes the swipe with a short slide.
func (s *server) endDeskSwipe(cancelled bool) {
	sw := s.deskSwipe
	if sw == nil || sw.ending {
		return
	}
	sw.ending = true
	sw.commit = !cancelled && math.Abs(sw.offset) > sw.width*deskSwipeCommit
	sw.from, sw.to = sw.offset, 0
	if sw.commit {
		sw.to = math.Copysign(sw.width, sw.offset)
	}
	sw.startTime = time.Now()
	for _, o := range s.outputs {
		scheduleOutputFrame(o.output)
	}
}

// tickDeskSwipe runs the end of the swipe and reports whether it goes on.
func (s *server) tickDeskSwipe() bool {
	sw := s.deskSwipe
	if sw == nil || !sw.ending {
		return false
	}
	t := float64(time.Since(sw.startTime)) / float64(deskSwipeDuration)
	if t < 1 {
		sw.offset = sw.from + (sw.to-sw.from)*easeOutCubic(t)
		s.showDeskSwipe()
		return true
	}

	target := s.neighbourDesk(sw.from)
	s.deskSwipe = nil
	s.restoreDeskSwipe()
	if sw.commit {
		s.noSlideTransition = true // the fingers already slid it
		s.switchDesk(target)
		s.noSlideTransition = false
	}
	return false
}

// showDeskSwipe places the windows of both desktops for the current offset.
func (s *server) showDeskSwipe() {
	sw := s.deskSwipe
	target := s.neighbourDesk(sw.offset)
	shift := sw.offset - math.Copysign(sw.width, sw.offset) // of the incoming desktop

	// place returns whether a window shows and its horizontal position.
	place := func(x float64, pinned bool, desk int) (bool, int) {
		switch {
		case pinned:
			return true, int(x) // on every desktop: it stays
		case desk == s.currentDesk:
			return true, int(x + sw.offset)
		case desk == target && sw.offset != 0:
			return true, int(x + shift)
		}
		return false, int(x)
	}
	for _, v := range s.xdgViews {
		if !v.mapped || v.minimized || v.fullscreen || v.sceneTree == nil {
			continue
		}
		enabled, x := place(v.x, v.pinned, v.desk)
		y := int(v.y)
		if v.decorated {
			y -= titlebarHeight
		}
		setViewSceneEnabled(v.sceneTree, enabled)
		setViewScenePosition(v.sceneTree, x, y)
	}
	for _, v := range s.xwayViews {
		if !v.mapped || v.minimized || v.fullscreen || v.isPanel || v.overrideRedirect || v.sceneTree == nil {
			continue
		}
		enabled, x := place(v.x, v.pinned, v.desk)
		y := int(v.y)
		if v.decorated {
			y -= titlebarHeight
		}
		setViewSceneEnabled(v.sceneTree, enabled)
		setViewScenePosition(v.sceneTree, x, y)
	}
}

// restoreDeskSwipe puts the windows back where they are, as the current
// desktop shows them.
func (s *server) restoreDeskSwipe() {
	for _, v := range s.xdgViews {
		if !v.mapped || v.minimized || v.fullscreen || v.sceneTree == nil {
			continue
		}
		setViewSceneEnabled(v.sceneTree, v.onDesk(s.currentDesk))
		setXdgScenePos(v)
	}
	for _, v := range s.xwayViews {
		if !v.mapped || v.minimized || v.fullscreen || v.isPanel || v.overrideRedirect || v.sceneTree == nil {
			continue
		}
		setViewSceneEnabled(v.sceneTree, v.onDesk(s.currentDesk))
		setXwayScenePos(v)
	}
}
