package compositor

import (
	"image"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/FyshOS/appie"

	"deedles.dev/wlr"
	"deedles.dev/wlr/xkb"

	"golang.org/x/image/draw"
)

// --- App Switcher Overlay ---

const switcherIconSize = 24 // pixels for switcher overlay icons

// openSwitcher enters switcher mode, collecting visible windows sorted by MRU order
func (s *server) openSwitcher() {
	if s.overviewActive {
		return // Don't open switcher while overview is active
	}

	type switcherEntry struct {
		view     interface{}
		focusSeq uint64
	}

	var entries []switcherEntry
	for _, v := range s.xdgViews {
		if v.mapped && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, switcherEntry{view: v, focusSeq: v.focusSeq})
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, switcherEntry{view: v, focusSeq: v.focusSeq})
		}
	}
	if len(entries) < 2 {
		return
	}

	// Sort by focusSeq descending (most recently focused first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].focusSeq > entries[j].focusSeq
	})

	s.switcherWindows = nil
	for _, e := range entries {
		s.switcherWindows = append(s.switcherWindows, e.view)
	}

	// Save original focus for cancel
	s.switcherOrigXdg = s.activeXdg
	s.switcherOrigXway = s.activeXway

	// Use cached thumbnails (captured continuously during render pass)
	s.switcherThumbImgs = s.collectSwitcherThumbs()

	// Start selection on 2nd item (the previous window)
	s.switcherIndex = 1
	s.switcherActive = true
	s.focusSwitcherSelection()
	s.updateSwitcherScene()

	// Start fade-in animation (skip if reduce motion)
	if !s.reduceMotion {
		s.switcherFadeIn = true
		s.switcherFadeOut = false
		s.switcherFadeStart = time.Now()
		s.scheduleAnimWakeup(time.Now())
	}
}

// switcherCycleNext moves selection to the next window
func (s *server) switcherCycleNext() {
	if len(s.switcherWindows) == 0 {
		return
	}
	s.switcherIndex = (s.switcherIndex + 1) % len(s.switcherWindows)
	s.focusSwitcherSelection()
	s.updateSwitcherScene()
}

// switcherCyclePrev moves selection to the previous window
func (s *server) switcherCyclePrev() {
	if len(s.switcherWindows) == 0 {
		return
	}
	s.switcherIndex = (s.switcherIndex - 1 + len(s.switcherWindows)) % len(s.switcherWindows)
	s.focusSwitcherSelection()
	s.updateSwitcherScene()
}

// focusSwitcherSelection sends keyboard focus to the currently selected window
// WITHOUT changing the z-order or view slice order. This prevents intermediate
// Tab presses from reshuffling the window stack while the switcher is open.
// The final raise happens in confirmSwitcher() when the modifier is released.
func (s *server) focusSwitcherSelection() {
	if s.switcherIndex < 0 || s.switcherIndex >= len(s.switcherWindows) {
		return
	}
	w := s.switcherWindows[s.switcherIndex]
	switch v := w.(type) {
	case *xdgView:
		if !v.mapped {
			s.removeSwitcherWindow(s.switcherIndex)
			return
		}
		// Deactivate previous
		if s.activeXdg != nil && s.activeXdg != v {
			s.activeXdg.xdgToplevel.SetActivated(false)
		}
		if s.activeXway != nil {
			s.activeXway.surface.Activate(false)
			s.activeXway = nil
		}
		s.activeXdg = v
		v.xdgToplevel.SetActivated(true)
		// Send keyboard focus but do NOT raise or reorder
		keyboard := s.seat.Keyboard()
		if keyboardValid(keyboard) {
			surface := v.xdgToplevel.Base().Surface()
			s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
		}
	case *xwayView:
		if !v.mapped {
			s.removeSwitcherWindow(s.switcherIndex)
			return
		}
		if s.activeXdg != nil {
			s.activeXdg.xdgToplevel.SetActivated(false)
			s.activeXdg = nil
		}
		if s.activeXway != nil && s.activeXway != v {
			s.activeXway.surface.Activate(false)
		}
		s.activeXway = v
		v.surface.Activate(true)
		keyboard := s.seat.Keyboard()
		if keyboardValid(keyboard) {
			surface := v.surface.Surface()
			if surface.Valid() {
				s.seat.KeyboardNotifyEnter(surface, keyboard.Keycodes(), keyboard.Modifiers())
			}
		}
	}
}

// removeSwitcherWindow removes a window from the switcher at the given index
func (s *server) removeSwitcherWindow(idx int) {
	s.switcherWindows = append(s.switcherWindows[:idx], s.switcherWindows[idx+1:]...)
	if len(s.switcherWindows) < 2 {
		s.confirmSwitcher()
		return
	}
	if s.switcherIndex >= len(s.switcherWindows) {
		s.switcherIndex = 0
	}
	s.focusSwitcherSelection()
	s.updateSwitcherScene()
}

// confirmSwitcher closes the switcher, raises the selected window to top,
// and updates the stacking order. Only the final selection changes z-order.
func (s *server) confirmSwitcher() {
	s.switcherActive = false
	s.switcherFadeIn = false

	// Start fade-out if not reduce motion
	if !s.reduceMotion && s.switcherBuf != nil {
		s.switcherFadeOut = true
		s.switcherFadeStart = time.Now()
		s.scheduleAnimWakeup(time.Now())
	} else {
		// Immediate close
		s.switcherWindows = nil
		s.switcherOrigXdg = nil
		s.switcherOrigXway = nil
		s.switcherThumbImgs = nil
		s.destroySwitcherThumbnails()
		s.updateSwitcherScene()
	}

	// Now raise the selected window via the normal focus path, which handles
	// view slice reorder, scene raise, focusSeq update, and decorations.
	if s.activeXdg != nil {
		s.focusXdgView(s.activeXdg)
	} else if s.activeXway != nil {
		s.focusXwayView(s.activeXway)
	}

	// Re-sync keyboard state with the focused client. During Alt-Tab,
	// KeyboardNotifyEnter was sent with Alt+Tab in the keycodes (physically held).
	// Those key releases were consumed by the switcher, never forwarded to the client.
	// Re-entering now sends the current (clean) key state, clearing the stale keys.
	s.syncKeyboardFocus()
}

// finishSwitcherClose is called after fade-out completes to clean up resources.
func (s *server) finishSwitcherClose() {
	s.switcherWindows = nil
	s.switcherOrigXdg = nil
	s.switcherOrigXway = nil
	s.switcherThumbImgs = nil
	s.destroySwitcherThumbnails()
	s.updateSwitcherScene()
}

// cancelSwitcher closes the switcher and restores original focus
func (s *server) cancelSwitcher() {
	s.switcherActive = false
	s.switcherFadeIn = false

	// Restore original focus (focusXdg/XwayView calls KeyboardNotifyEnter)
	if s.switcherOrigXdg != nil {
		s.focusXdgView(s.switcherOrigXdg)
	} else if s.switcherOrigXway != nil {
		s.focusXwayView(s.switcherOrigXway)
	}

	// Start fade-out if not reduce motion
	if !s.reduceMotion && s.switcherBuf != nil {
		s.switcherFadeOut = true
		s.switcherFadeStart = time.Now()
		s.scheduleAnimWakeup(time.Now())
	} else {
		s.switcherOrigXdg = nil
		s.switcherOrigXway = nil
		s.switcherThumbImgs = nil
		s.destroySwitcherThumbnails()
		s.updateSwitcherScene()
	}

	// Re-sync to clear stale Alt/Tab key state (same issue as confirmSwitcher)
	s.syncKeyboardFocus()
}

// isSwitcherModReleased returns true if the WM modifier key (Super or Alt
// depending on user preference) is no longer held, based on actual keyboard state.
func (s *server) isSwitcherModReleased(mods wlr.KeyboardModifier) bool {
	if s.wmModifier == wlr.KeyboardModifierAlt {
		return mods&wlr.KeyboardModifierAlt == 0
	}
	return mods&wlr.KeyboardModifierLogo == 0
}

// isSwitcherModKeySym returns true if any of the given key syms correspond to
// the WM modifier key (Alt_L/Alt_R when wmModifier is Alt, Super_L/Super_R
// when wmModifier is Logo). This is used as a fallback check on key release
// because wlroots may still report the modifier as held in GetModifiers()
// at the moment the modifier key itself is released.
func (s *server) isSwitcherModKeySym(syms []xkb.KeySym) bool {
	altL := xkb.SymFromName("Alt_L", xkb.KeySymNoFlags)
	altR := xkb.SymFromName("Alt_R", xkb.KeySymNoFlags)
	superL := xkb.SymFromName("Super_L", xkb.KeySymNoFlags)
	superR := xkb.SymFromName("Super_R", xkb.KeySymNoFlags)

	for _, sym := range syms {
		if s.wmModifier == wlr.KeyboardModifierAlt {
			if sym == altL || sym == altR {
				return true
			}
		} else {
			if sym == superL || sym == superR {
				return true
			}
		}
	}
	return false
}

// handleSwitcherKey handles keypresses while the switcher overlay is active
func (s *server) handleSwitcherKey(sym xkb.KeySym, mods wlr.KeyboardModifier) {
	switch sym {
	case xkb.KeySymEscape:
		s.cancelSwitcher()
	case xkb.KeySymReturn:
		s.confirmSwitcher()
	case xkb.KeySymTab:
		if mods&wlr.KeyboardModifierShift != 0 {
			s.switcherCyclePrev()
		} else {
			s.switcherCycleNext()
		}
	case xkb.KeySymISO_Left_Tab: // Shift+Tab generates ISO_Left_Tab
		s.switcherCyclePrev()
	case xkb.KeySymLeft:
		s.switcherCyclePrev()
	case xkb.KeySymRight:
		s.switcherCycleNext()
	}
}

// switcherWindowTitle returns the title for a switcher window entry
func (s *server) switcherWindowTitle(w interface{}) string {
	switch v := w.(type) {
	case *xdgView:
		return v.xdgToplevel.Title()
	case *xwayView:
		return v.surface.Title()
	}
	return ""
}

// switcherDisplayTitle returns a distinguishable display title for the switcher.
// If the raw title is too long and starts with a common prefix (e.g. username@host),
// it uses "AppName — suffix" to make windows easier to tell apart.
func (s *server) switcherDisplayTitle(w interface{}) string {
	title := s.switcherWindowTitle(w)
	if title == "" {
		return s.switcherWindowAppID(w)
	}
	appID := s.switcherWindowAppID(w)

	// If the title is short enough, use it directly
	if len(title) <= 30 {
		return title
	}

	// Try to extract the distinguishing part.
	// Many terminals use "user@host: path" — extract just the path part.
	if idx := strings.LastIndex(title, ":"); idx >= 0 && idx < len(title)-1 {
		suffix := strings.TrimSpace(title[idx+1:])
		if suffix != "" && appID != "" {
			return appID + " — " + suffix
		}
	}

	// For browsers etc: "Page Title — AppName" → use "Page Title"
	for _, sep := range []string{" — ", " - ", " – "} {
		if idx := strings.LastIndex(title, sep); idx > 0 {
			return title[:idx]
		}
	}

	return title
}

// switcherWindowAppID returns the app ID for a switcher window entry
func (s *server) switcherWindowAppID(w interface{}) string {
	switch v := w.(type) {
	case *xdgView:
		return getXdgToplevelAppID(v.xdgToplevel)
	case *xwayView:
		return getXwaylandSurfaceClass(v.surface)
	}
	return ""
}

// loadSwitcherIcon loads a 24px icon for the switcher overlay
func (s *server) loadSwitcherIcon(appID string) *iconEntry {
	if s.switcherIconCache == nil {
		s.switcherIconCache = make(map[string]*iconEntry)
	}
	if entry, ok := s.switcherIconCache[appID]; ok {
		return entry
	}

	iconPath := appie.FdoLookupIconPath("", switcherIconSize, strings.ToLower(appID))
	if iconPath == "" {
		iconPath = appie.FdoLookupIconPath("", switcherIconSize, appID)
	}
	if iconPath == "" {
		s.switcherIconCache[appID] = nil
		return nil
	}

	f, err := os.Open(iconPath)
	if err != nil {
		s.switcherIconCache[appID] = nil
		return nil
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		s.switcherIconCache[appID] = nil
		return nil
	}

	scaled := image.NewNRGBA(image.Rect(0, 0, switcherIconSize, switcherIconSize))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	texture := wlr.TextureFromImage(s.renderer, scaled)
	if !texture.Valid() {
		s.switcherIconCache[appID] = nil
		return nil
	}

	entry := &iconEntry{texture: texture, w: switcherIconSize, h: switcherIconSize}
	s.switcherIconCache[appID] = entry
	return entry
}
