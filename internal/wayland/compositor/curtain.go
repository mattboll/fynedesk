package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

// The curtain lets the clicks through: the first one lifts it and reaches
// the window below.
static bool curtain_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
    return false;
}

// curtain_create makes the curtain: one black pixel, stretched.
static struct wlr_scene_buffer *curtain_create(struct wlr_scene_tree *root) {
    struct pixel_buffer *px = pixel_buffer_create(1, 1);
    if (!px) {
        return NULL;
    }
    const unsigned char black[4] = {0, 0, 0, 255};
    pixel_buffer_update(px, (void *)black, 1, 1);
    struct wlr_scene_buffer *curtain = wlr_scene_buffer_create(root, &px->base);
    wlr_buffer_drop(&px->base); // the scene buffer holds it now
    if (curtain) {
        curtain->point_accepts_input = curtain_no_input;
        wlr_scene_node_set_enabled(&curtain->node, false);
    }
    return curtain;
}

// curtain_place spreads the curtain over the layout, above everything (or
// disables it).
static void curtain_place(struct wlr_scene_buffer *curtain, int x, int y, int w, int h, float alpha) {
    if (alpha <= 0 || w <= 0 || h <= 0) {
        wlr_scene_node_set_enabled(&curtain->node, false);
        return;
    }
    wlr_scene_node_raise_to_top(&curtain->node);
    wlr_scene_node_set_position(&curtain->node, x, y);
    wlr_scene_buffer_set_dest_size(curtain, w, h);
    wlr_scene_buffer_set_opacity(curtain, alpha);
    wlr_scene_node_set_enabled(&curtain->node, true);
}
*/
import "C"

import (
	"time"
	"unsafe"
)

// The idle curtain: shortly before the screen locks, blanks or the machine
// sleeps for want of use, the screen slowly darkens; the first touch of the
// mouse or the keyboard lifts it.
const (
	curtainLead    = 20 * time.Second // how long before the idle action the screen starts to darken
	curtainDarkest = 0.85
	curtainLift    = 300 * time.Millisecond
	curtainHold    = 3 * time.Second // longest the curtain waits for the lock screen, dark
)

// curtainState is the curtain and where it stands.
type curtainState struct {
	node      unsafe.Pointer // *C.struct_wlr_scene_buffer
	alpha     float64
	liftFrom  float64
	liftStart time.Time // lifting since then, if set
	holdUntil time.Time // the idle action is done: dark until the lock screen shows, or then
	timer     *time.Timer
}

// idleStep is an idle action: after how many minutes (0: never) and whether
// it is still to come.
type idleStep struct {
	minutes int
	pending bool
}

// nextIdleTimeout returns the idle time after which the next action comes.
func nextIdleTimeout(steps ...idleStep) (time.Duration, bool) {
	var next time.Duration
	found := false
	for _, st := range steps {
		if st.minutes <= 0 || !st.pending {
			continue
		}
		if d := time.Duration(st.minutes) * time.Minute; !found || d < next {
			next, found = d, true
		}
	}
	return next, found
}

// curtainAlpha returns how dark the curtain is, the next idle action being
// remaining away: slow at first, darker and darker.
func curtainAlpha(remaining time.Duration) float64 {
	if remaining >= curtainLead {
		return 0
	}
	if remaining <= 0 {
		return curtainDarkest
	}
	t := 1 - float64(remaining)/float64(curtainLead)
	return curtainDarkest * t * t
}

// idleRemaining returns how long until the next idle action, if one is to
// come and nothing holds it off.
func (s *server) idleRemaining() (time.Duration, bool) {
	if s.nestedMode || (s.screenSaverDBus != nil && s.screenSaverDBus.IsInhibited()) {
		return 0, false
	}
	timeout, ok := nextIdleTimeout(
		idleStep{s.powerLockTimeout, !s.idleLocked},
		idleStep{s.powerBlankTimeout, !s.displayBlanked.Load()},
		idleStep{s.powerSuspendTimeout, !s.idleSuspended && s.powerSuspendAction != "nothing"},
	)
	return timeout - time.Since(s.lastInputTime), ok
}

// armCurtain makes sure a frame comes when the curtain is to start falling,
// if it does before within.
func (s *server) armCurtain(within time.Duration) {
	remaining, ok := s.idleRemaining()
	if !ok || remaining-curtainLead > within {
		return
	}
	c := &s.curtain
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(max(remaining-curtainLead, 0), func() {
		s.enqueueAction(s.scheduleAllOutputFrames) //nolint:errcheck // the next check arms it again
	})
}

// liftCurtain lifts the curtain, if it is down: the user is back.
func (s *server) liftCurtain() {
	c := &s.curtain
	c.holdUntil = time.Time{}
	if c.alpha <= 0 || !c.liftStart.IsZero() {
		return
	}
	c.liftFrom = c.alpha
	c.liftStart = time.Now()
	s.scheduleAllOutputFrames()
}

// tickCurtain lowers or lifts the curtain and does the idle action when it
// is down; it reports whether the curtain moves.
func (s *server) tickCurtain() bool {
	c := &s.curtain
	now := time.Now()
	if !c.liftStart.IsZero() {
		t := float64(now.Sub(c.liftStart)) / float64(curtainLift)
		if t >= 1 {
			c.liftStart = time.Time{}
			s.showCurtain(0)
			return false
		}
		s.showCurtain(c.liftFrom * (1 - t))
		return true
	}
	if !c.holdUntil.IsZero() {
		if !s.locked.Load() && now.Before(c.holdUntil) {
			return true // the lock screen is on its way
		}
		s.liftCurtain() // it shows through
		return true
	}

	remaining, ok := s.idleRemaining()
	if !ok || remaining >= curtainLead {
		if c.alpha > 0 {
			s.liftCurtain() // inhibited meanwhile
			return true
		}
		return false
	}
	s.showCurtain(curtainAlpha(remaining))
	if remaining <= 0 {
		s.checkIdle()
		c.holdUntil = now.Add(curtainHold)
	}
	return true
}

// showCurtain sets how dark the curtain is.
func (s *server) showCurtain(alpha float64) {
	c := &s.curtain
	c.alpha = alpha
	if c.node == nil {
		if alpha <= 0 {
			return
		}
		scene := (*C.struct_wlr_scene)(s.scene)
		c.node = unsafe.Pointer(C.curtain_create(&scene.tree))
		if c.node == nil {
			return
		}
	}
	lx, ly, lw, lh := s.fullLayoutBounds()
	C.curtain_place((*C.struct_wlr_scene_buffer)(c.node), C.int(lx), C.int(ly), C.int(lw), C.int(lh), C.float(alpha))
}
