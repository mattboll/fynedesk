package compositor

import (
	"log"
	"time"

	"fyshos.com/tyde/wlipc"
)

// shareMinDuration is how long a capture session lasts before it counts as
// a screen share: a screenshot takes one frame and ends well before.
const shareMinDuration = time.Second

// shareTracker follows the capture sessions (screen or window), to tell the
// panel when the screen is shared: it then holds the notification popups
// back. Main thread only.
type shareTracker struct {
	seq      uint64
	sessions map[uintptr]uint64 // live session → its number
	sharing  map[uint64]bool    // numbers of the sessions that lasted
	active   bool
}

// started records a session and returns its number.
func (t *shareTracker) started(session uintptr) uint64 {
	if t.sessions == nil {
		t.sessions = map[uintptr]uint64{}
		t.sharing = map[uint64]bool{}
	}
	t.seq++
	t.sessions[session] = t.seq
	return t.seq
}

// lasted marks session number n as a share if it is still live, and reports
// whether the screen is now shared when that changed.
func (t *shareTracker) lasted(n uint64) (active, changed bool) {
	for _, live := range t.sessions {
		if live == n {
			t.sharing[n] = true
			return t.update()
		}
	}
	return t.active, false
}

// ended forgets a session, and reports whether the screen is still shared
// when that changed.
func (t *shareTracker) ended(session uintptr) (active, changed bool) {
	n, ok := t.sessions[session]
	if !ok {
		return t.active, false
	}
	delete(t.sessions, session)
	delete(t.sharing, n)
	return t.update()
}

func (t *shareTracker) update() (bool, bool) {
	active := len(t.sharing) > 0
	changed := active != t.active
	t.active = active
	return active, changed
}

// screencopyGap is how long without a screencopy frame ends a stream of
// them: well under shareMinDuration, so that a screenshot is no share.
const screencopyGap = 400 * time.Millisecond

// screencopySession stands for a stream of wlr-screencopy frames in the
// shareTracker: that protocol has frames, not sessions.
const screencopySession = ^uintptr(0)

// screencopyFrame follows the wlr-screencopy frames: one made (live 1) or
// destroyed (-1). Frames coming one after the other make a stream, which
// ends screencopyGap after the last one went.
func (s *server) screencopyFrame(live int) {
	s.copyFrames += live
	s.copyLast = time.Now()
	if s.copyStreaming {
		return
	}
	s.copyStreaming = true
	s.captureSessionStarted(screencopySession)
	s.watchScreencopyStream()
}

func (s *server) watchScreencopyStream() {
	time.AfterFunc(screencopyGap/2, func() {
		_ = s.enqueueAction(func() {
			if s.copyFrames > 0 || time.Since(s.copyLast) < screencopyGap {
				s.watchScreencopyStream()
				return
			}
			s.copyStreaming = false
			s.captureSessionEnded(screencopySession)
		})
	})
}

func (s *server) captureSessionStarted(session uintptr) {
	n := s.shares.started(session)
	time.AfterFunc(shareMinDuration, func() {
		_ = s.enqueueAction(func() {
			if active, changed := s.shares.lasted(n); changed {
				s.screenSharingChanged(active)
			}
		})
	})
}

func (s *server) captureSessionEnded(session uintptr) {
	if active, changed := s.shares.ended(session); changed {
		s.screenSharingChanged(active)
	}
}

// screenSharingChanged tells the panel the screen started or stopped being
// shared.
func (s *server) screenSharingChanged(active bool) {
	log.Printf("[SHARE] screen sharing: %v", active)
	if s.ipcServer != nil {
		s.ipcServer.Broadcast(wlipc.EventScreenSharing, wlipc.ScreenSharingEvent{Active: active})
	}
}
