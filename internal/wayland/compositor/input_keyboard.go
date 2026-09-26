package compositor

import (
	"log"
	"os"
	"time"

	"fyshos.com/tyde/wlipc"

	"fyshos.com/tyde/internal/wayland/wlr"
	"fyshos.com/tyde/internal/wayland/wlr/xkb"
)

func (s *server) handleNewInput(device wlr.InputDevice) {
	typeName := "unknown"
	switch device.Type() {
	case wlr.InputDeviceTypeKeyboard:
		typeName = "keyboard"
	case wlr.InputDeviceTypePointer:
		typeName = "pointer"
	case wlr.InputDeviceTypeTouch:
		typeName = "touch"
	case wlr.InputDeviceTypeTablet:
		typeName = "tablet"
	case wlr.InputDeviceTypeTabletPad:
		typeName = "tablet_pad"
	}
	_ = typeName

	switch device.Type() {
	case wlr.InputDeviceTypeKeyboard:
		s.setupKeyboard(device)
	case wlr.InputDeviceTypePointer:
		s.cursor.AttachInputDevice(device)
	case wlr.InputDeviceTypeTouch:
		// Touch devices also act as pointers
		s.cursor.AttachInputDevice(device)
	}

	caps := wlr.SeatCapabilityPointer | wlr.SeatCapabilityKeyboard
	s.seat.SetCapabilities(caps)
}

func (s *server) setupKeyboard(device wlr.InputDevice) {
	keyboard := device.Keyboard()

	// Apply current keyboard layout
	s.applyKeyboardLayoutTo(keyboard)
	keyboard.SetRepeatInfo(25, 600)

	// Store keyboard reference for later layout switching
	s.keyboards = append(s.keyboards, keyboard)

	// Track this keyboard's listeners locally so we can destroy them when the
	// device disappears, instead of growing s.listeners (server-wide) by 3 on
	// every USB hot-plug or XWayland virtual keyboard creation.
	var perDevice wlr.Listeners

	// Remove keyboard from list when device is destroyed (e.g. XWayland virtual keyboards)
	perDevice.Add(device.OnDestroy(func(dev wlr.InputDevice) {
		s.handleKeyboardDestroy(keyboard, &perDevice)
	}))

	perDevice.Add(keyboard.OnKey(func(kb wlr.Keyboard, t time.Time, keyCode uint32, updateState bool, state wlr.KeyState) {
		s.handleKeyboardKey(keyboard, kb, t, keyCode, state)
	}))

	perDevice.Add(keyboard.OnModifiers(func(kb wlr.Keyboard) {
		s.handleKeyboardModifiers(keyboard, kb)
	}))

	s.seat.SetKeyboard(keyboard)
	// A keyboard showing up after a window got focus (hotplug, virtual or
	// on-screen keyboard) must deliver wl_keyboard.enter to that window,
	// which focusing without any keyboard could not do.
	s.syncKeyboardFocus()
}

// handleKeyboardDestroy removes a keyboard from the list and destroys its listeners.
func (s *server) handleKeyboardDestroy(keyboard wlr.Keyboard, perDevice *wlr.Listeners) {
	for i, kb := range s.keyboards {
		if kb == keyboard {
			s.keyboards = append(s.keyboards[:i], s.keyboards[i+1:]...)
			break
		}
	}
	// wlroots asserts that no listener is left on the device (this one
	// included) once the destroy signal has been emitted.
	perDevice.DestroyAll()
}

// handleKeyboardKey handles a key event of a keyboard.
func (s *server) handleKeyboardKey(keyboard, kb wlr.Keyboard, t time.Time, keyCode uint32, state wlr.KeyState) {
	s.resetIdleTimer()

	// Stop key repeat on any key release
	if state == wlr.KeyStateReleased {
		s.stopKeyRepeat()
	}

	// When locked, check for emergency logout before forwarding to lock client
	if s.locked.Load() {
		s.handleLockedKey(keyboard, kb, t, keyCode, state)
		return
	}

	syms := kb.XKBState().Syms(xkb.KeyCode(keyCode + 8))

	// Handle switcher key events
	if s.switcherActive {
		s.handleSwitcherKeyEvent(kb, syms, state)
		return // Don't forward keys to clients while switcher is active
	}

	// Handle overview key events
	if s.overviewActive {
		s.handleOverviewKeyEvent(syms, state)
		return // Don't forward keys to clients while overview is active
	}

	// Cancel region screenshot selection on Escape
	if s.regionSelectActive && state == wlr.KeyStatePressed {
		for _, sym := range syms {
			if sym == xkb.SymFromName("Escape", xkb.KeySymNoFlags) {
				s.cancelRegionSelect()
				return
			}
		}
	}

	// Super-alone detection: track bare Super press/release for launcher toggle
	isSuperSym := false
	for _, sym := range syms {
		if sym == xkb.SymFromName("Super_L", xkb.KeySymNoFlags) ||
			sym == xkb.SymFromName("Super_R", xkb.KeySymNoFlags) {
			isSuperSym = true
			break
		}
	}

	if state == wlr.KeyStatePressed {
		if isSuperSym {
			// Super pressed alone
			s.superAlonePressed = true
			s.superAloneTime = time.Now()
		} else {
			// Any other key cancels Super-alone
			s.superAlonePressed = false
		}

		mods := kb.GetModifiers()
		for _, sym := range syms {
			if s.handleKeybinding(mods, sym) {
				// Start key repeat for repeatable actions (volume, brightness)
				clean := mods & relevantMods
				if action, ok := s.keybindingMap[resolvedBinding{sym: sym, mods: clean}]; ok && isRepeatableAction(action) {
					s.startKeyRepeat(keyCode, action)
				}
				return
			}
		}
	} else if state == wlr.KeyStateReleased && isSuperSym {
		// Releasing Super starts the felt-tip pen hold-then-fade countdown.
		s.penHandleSuperRelease()
		if s.superAlonePressed {
			s.superAlonePressed = false
			if time.Since(s.superAloneTime) < superAloneTimeout {
				// Super was pressed and released alone within timeout — toggle overview (exposé)
				s.toggleOverview()
				return
			}
		}
	}

	if s.imeGrabKey(kb, t, keyCode, state) {
		return
	}
	s.seat.SetKeyboard(keyboard)
	s.seat.KeyboardNotifyKey(t, keyCode, state)
}

// handleLockedKey handles a key event while the session is locked.
func (s *server) handleLockedKey(keyboard, kb wlr.Keyboard, t time.Time, keyCode uint32, state wlr.KeyState) {
	if state == wlr.KeyStatePressed {
		symsLock := kb.XKBState().Syms(xkb.KeyCode(keyCode + 8))
		mods := kb.GetModifiers()
		for _, sym := range symsLock {
			if s.handleKeybinding(mods, sym) {
				return
			}
		}
	}
	// Built-in lock screen: handle key input directly
	if s.builtinLock != nil && s.builtinLock.active {
		if state == wlr.KeyStatePressed {
			symsLock := kb.XKBState().Syms(xkb.KeyCode(keyCode + 8))
			for _, sym := range symsLock {
				s.handleBuiltinLockKey(sym, uint32(kb.GetModifiers()))
			}
		}
		return
	}
	// External lock client: forward to its lock surface, and to nothing
	// else — a window that took the focus would get the password.
	if s.currentLock != nil && len(s.lockSurfaceStates) > 0 {
		if !s.lockSurfaceFocused() {
			s.focusLockSurface()
		}
		if s.lockSurfaceFocused() {
			s.seat.SetKeyboard(keyboard)
			s.seat.KeyboardNotifyKey(t, keyCode, state)
		}
	}
}

// handleSwitcherKeyEvent handles a key event while the window switcher is active.
func (s *server) handleSwitcherKeyEvent(kb wlr.Keyboard, syms []xkb.KeySym, state wlr.KeyState) {
	if state == wlr.KeyStatePressed {
		for _, sym := range syms {
			s.handleSwitcherKey(sym, kb.GetModifiers())
		}
	} else {
		// On key release, check if WM modifier is no longer held to confirm.
		// Also check if the released key IS the modifier key itself, because
		// wlroots may still report the modifier as held in kb.GetModifiers()
		// at the moment of the modifier key's own release event.
		if s.isSwitcherModReleased(kb.GetModifiers()) || s.isSwitcherModKeySym(syms) {
			s.confirmSwitcher()
			return
		}
	}
}

// handleOverviewKeyEvent handles a key event while the overview is active.
func (s *server) handleOverviewKeyEvent(syms []xkb.KeySym, state wlr.KeyState) {
	isSuperKey := false
	for _, sym := range syms {
		if sym == xkb.SymFromName("Super_L", xkb.KeySymNoFlags) ||
			sym == xkb.SymFromName("Super_R", xkb.KeySymNoFlags) {
			isSuperKey = true
			break
		}
	}
	if state == wlr.KeyStatePressed {
		if isSuperKey {
			s.superAlonePressed = true
			s.superAloneTime = time.Now()
		} else {
			s.superAlonePressed = false
			for _, sym := range syms {
				s.handleOverviewKey(sym)
			}
		}
	} else if state == wlr.KeyStateReleased && isSuperKey && s.superAlonePressed {
		s.superAlonePressed = false
		if time.Since(s.superAloneTime) < superAloneTimeout {
			s.toggleOverview()
		}
	}
}

// handleKeyboardModifiers handles a modifiers change of a keyboard.
func (s *server) handleKeyboardModifiers(keyboard, kb wlr.Keyboard) {
	// Check switcher dismiss in the modifiers callback too, as this fires
	// reliably when modifier state changes and GetModifiers() is accurate here.
	if s.switcherActive && s.isSwitcherModReleased(kb.GetModifiers()) {
		s.confirmSwitcher()
		return
	}
	if !s.locked.Load() && s.imeGrabModifiers(kb) {
		return
	}
	s.seat.SetKeyboard(keyboard)
	s.seat.KeyboardNotifyModifiers(kb.Modifiers())
}

// startKeyRepeat begins auto-repeating a keybinding action after keyRepeatDelay,
// then every keyRepeatInterval, matching the keyboard repeat rate.
func (s *server) startKeyRepeat(keyCode uint32, action string) {
	s.stopKeyRepeat() // cancel any existing repeat
	stop := make(chan struct{})
	s.keyRepeatStop = stop
	s.keyRepeatCode = keyCode

	go func() {
		// Initial delay before repeat starts
		select {
		case <-stop:
			return
		case <-time.After(keyRepeatDelay):
		}

		ticker := time.NewTicker(keyRepeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			// dispatchAction touches wlroots APIs (seat, scene, cursor) that
			// are not thread-safe — run it on the main thread.
			act := action
			select {
			case s.mainThreadActions <- func() { s.dispatchAction(act) }:
				s.triggerWakeup()
			case <-stop:
				return
			}
		}
	}()
}

// stopKeyRepeat cancels any active key repeat.
func (s *server) stopKeyRepeat() {
	if s.keyRepeatStop != nil {
		close(s.keyRepeatStop)
		s.keyRepeatStop = nil
	}
}

// applyKeyboardLayoutTo sets the XKB keymap on a single keyboard based on current layout settings.
func (s *server) applyKeyboardLayoutTo(keyboard wlr.Keyboard) {
	if !keyboardValid(keyboard) {
		return
	}

	layout := os.Getenv("XKB_DEFAULT_LAYOUT")
	variant := os.Getenv("XKB_DEFAULT_VARIANT")

	// Use configured layouts if available
	if len(s.keyboardLayouts) > 0 && s.activeLayoutIndex < len(s.keyboardLayouts) {
		kl := s.keyboardLayouts[s.activeLayoutIndex]
		layout = kl.Layout
		variant = kl.Variant
	}

	if layout == "" {
		layout, variant = systemKeyboardLayout()
	}

	ctx := xkb.NewContext(xkb.ContextNoFlags)
	defer ctx.Unref()

	rules := &xkb.RuleNames{
		Layout:  layout,
		Variant: variant,
	}
	keymap := xkb.NewKeymapFromNames(ctx, rules, xkb.KeymapCompileNoFlags)
	if keymap == (xkb.Keymap{}) {
		log.Printf("Warning: XKB keymap creation failed for layout=%q variant=%q, skipping\n", layout, variant)
		return
	}
	defer keymap.Unref()

	keyboard.SetKeymap(keymap)
}

// applyKeyboardLayout re-applies the current keyboard layout to all connected keyboards
// and notifies the panel via IPC.
func (s *server) applyKeyboardLayout() {
	for _, kb := range s.keyboards {
		s.applyKeyboardLayoutTo(kb)
	}

	// Notify panel of layout state change
	if len(s.keyboardLayouts) > 0 {
		state := wlipc.KeyboardLayoutState{
			ActiveIndex: s.activeLayoutIndex,
			Layouts:     s.keyboardLayouts,
		}
		if err := wlipc.NotifyKeyboardLayoutState(state); err != nil {
			log.Printf("Warning: failed to notify keyboard layout state: %v\n", err)
		}
	}

	if len(s.keyboardLayouts) > 0 && s.activeLayoutIndex < len(s.keyboardLayouts) {
		kl := s.keyboardLayouts[s.activeLayoutIndex]
		log.Printf("Keyboard layout applied: %s:%s (index %d/%d)\n",
			kl.Layout, kl.Variant, s.activeLayoutIndex, len(s.keyboardLayouts))
	}
}
