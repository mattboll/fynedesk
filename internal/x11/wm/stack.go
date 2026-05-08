//go:build linux || openbsd || freebsd || netbsd
// +build linux openbsd freebsd netbsd

package wm

import (
	"sync"

	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil/ewmh"

	"fyne.io/fyne/v2"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/x11"
)

// stack tracks managed windows in stacking order.
//
// Concurrency: clients/mappingOrder are mutated by the X11 event loop AND
// by frameExisting (run in a goroutine at startup) plus by various module
// callbacks. mu guards every read or mutation of clients/mappingOrder.
// Listeners are invoked AFTER mu is released so they can safely re-enter
// the public API (e.g. WindowAdded → Windows()).
type stack struct {
	mu           sync.RWMutex
	clients      []fynedesk.Window
	mappingOrder []fynedesk.Window

	listeners []fynedesk.StackListener
}

func (s *stack) AddWindow(win fynedesk.Window) {
	if win == nil {
		return
	}
	s.mu.Lock()
	s.addToStackLocked(win)
	listeners := s.listeners
	s.mu.Unlock()

	for _, l := range listeners {
		l.WindowAdded(win)
	}
}

func (s *stack) RaiseToTop(win fynedesk.Window) {
	if win.Iconic() {
		return
	}
	// indexForWinLocked / removeFromStackLocked / addToStackLocked all need
	// the lock; capture the listeners snapshot under the same lock so we
	// don't race a listener add/remove either.
	s.mu.Lock()
	if len(s.clients) > 1 {
		top := s.topWindowLocked()
		s.mu.Unlock()
		// RaiseAbove may call back into the X server but does not touch
		// stack state — safe to release before this call.
		win.RaiseAbove(top)
		s.mu.Lock()
	}

	if s.indexForWinLocked(win) == -1 {
		s.mu.Unlock()
		return
	}
	s.removeFromStackLocked(win)
	s.addToStackLocked(win)
	listeners := s.listeners
	s.mu.Unlock()

	if wm, ok := fynedesk.Instance().WindowManager().(*x11WM); ok {
		windowClientListStackingUpdate(wm)
	}

	for _, l := range listeners {
		l.WindowOrderChanged()
	}
}

func (s *stack) RemoveWindow(win fynedesk.Window) {
	s.mu.Lock()
	s.removeFromStackLocked(win)
	top := s.topWindowLocked()
	listeners := s.listeners
	s.mu.Unlock()

	if top != nil {
		top.Focus()
	} else {
		// focus root
		if wm, ok := fynedesk.Instance().WindowManager().(*x11WM); ok && wm.X() != nil {
			err := ewmh.ActiveWindowReq(wm.X(), wm.rootID)
			if err != nil {
				fyne.LogError("There was an error trying to remove the window ", err)
			}
		}
	}

	for _, l := range listeners {
		l.WindowRemoved(win)
	}
}

func (s *stack) TopWindow() fynedesk.Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.topWindowLocked()
}

func (s *stack) Windows() []fynedesk.Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ret []fynedesk.Window
	for i := len(s.clients) - 1; i >= 0; i-- {
		ret = append(ret, s.clients[i])
	}
	return ret
}

// addToStackLocked appends win to clients/mappingOrder; caller must hold mu.
func (s *stack) addToStackLocked(win fynedesk.Window) {
	s.clients = append(s.clients, win)
	s.mappingOrder = append(s.mappingOrder, win.(x11.XWin))
}

func (s *stack) clientForWin(id xproto.Window) x11.XWin {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.clients {
		if w.(x11.XWin).FrameID() == id || w.(x11.XWin).ChildID() == id {
			return w.(x11.XWin)
		}
	}
	return nil
}

func (s *stack) getWindowsFromClients(clients []fynedesk.Window) []xproto.Window {
	var wins []xproto.Window
	for _, cli := range clients {
		wins = append(wins, cli.(x11.XWin).ChildID())
	}
	return wins
}

// indexForWinLocked returns the slice index of win in clients, or -1.
// Caller must hold mu.
func (s *stack) indexForWinLocked(win fynedesk.Window) int {
	pos := -1
	for i, w := range s.clients {
		if w == win {
			pos = i
		}
	}
	return pos
}

func (s *stack) publishWindowChange(win fynedesk.Window) {
	s.mu.RLock()
	listeners := s.listeners
	s.mu.RUnlock()
	for _, l := range listeners {
		l.WindowStateChanged(win)
	}
}

// removeFromStackLocked removes win from clients and mappingOrder; caller
// must hold mu.
func (s *stack) removeFromStackLocked(win fynedesk.Window) {
	pos := s.indexForWinLocked(win)
	if pos == -1 {
		return
	}
	s.clients = append(s.clients[:pos], s.clients[pos+1:]...)

	pos = -1
	for i, w := range s.mappingOrder {
		if w == win {
			pos = i
		}
	}
	if pos == -1 {
		return
	}
	s.mappingOrder = append(s.mappingOrder[:pos], s.mappingOrder[pos+1:]...)
}

// topWindowLocked returns the top window or nil; caller must hold mu.
func (s *stack) topWindowLocked() fynedesk.Window {
	if len(s.clients) == 0 {
		return nil
	}
	return s.clients[len(s.clients)-1]
}

// clientsSnapshot returns a copy of clients in stacking order (bottom-first,
// the original ordering). Use this in package code that needs to iterate
// the client list from goroutines other than the X11 event loop thread.
func (s *stack) clientsSnapshot() []fynedesk.Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]fynedesk.Window, len(s.clients))
	copy(out, s.clients)
	return out
}

// clientsLen returns len(clients) under read lock.
func (s *stack) clientsLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// mappingOrderSnapshot returns a copy of mappingOrder.
func (s *stack) mappingOrderSnapshot() []fynedesk.Window {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]fynedesk.Window, len(s.mappingOrder))
	copy(out, s.mappingOrder)
	return out
}
