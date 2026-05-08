package compositor

import (
	"log"

	"deedles.dev/wlr"
	"deedles.dev/wlr/xkb"

	"fyshos.com/fynedesk/wlipc"
)

// cleanMods strips non-modifier bits (caps lock, num lock, etc.) from a modifier mask.
const relevantMods = wlr.KeyboardModifierShift | wlr.KeyboardModifierCtrl | wlr.KeyboardModifierAlt | wlr.KeyboardModifierLogo

// isRepeatableAction returns true for actions that should auto-repeat when held.
func isRepeatableAction(action string) bool {
	switch action {
	case wlipc.ActionVolumeUp, wlipc.ActionVolumeDown,
		wlipc.ActionBrightnessUp, wlipc.ActionBrightnessDown:
		return true
	}
	return false
}

func (s *server) handleKeybinding(mods wlr.KeyboardModifier, sym xkb.KeySym) bool {
	clean := mods & relevantMods
	action, ok := s.keybindingMap[resolvedBinding{sym: sym, mods: clean}]
	if !ok {
		if clean != 0 {
			log.Printf("[KEYBIND] no match for sym=0x%x mods=0x%x (clean=0x%x, wmMod=0x%x)", sym, mods, clean, s.wmModifier)
		}
		return false
	}

	// While locked, only allow emergency logout (prevents permanent lockout
	// if the lock client crashes and can't be relaunched)
	if s.locked.Load() {
		if action == wlipc.ActionEmergencyLogout {
			return s.dispatchAction(action)
		}
		return false
	}

	return s.dispatchAction(action)
}

func (s *server) dispatchAction(action string) bool {
	// Security: block all actions while locked except emergency logout.
	// Defense-in-depth — callers (handleKeybinding, IPC) should also check.
	if s.locked.Load() && action != wlipc.ActionEmergencyLogout {
		log.Printf("[DISPATCH] blocked while locked: %q", action)
		return false
	}
	log.Printf("[DISPATCH] action=%q activeXdg=%v activeXway=%v", action, s.activeXdg != nil, s.activeXway != nil)

	// Hide any lingering panel tooltips before dispatching window actions.
	// Tooltips are lightweight overlays that should never interfere with
	// keybinding-driven window management (snap, maximize, close, etc.).
	s.hideTooltips()

	switch action {
	case wlipc.ActionQuit:
		s.saveSessionState()
		s.display.Terminate()
	case wlipc.ActionEmergencyLogout:
		s.display.Terminate()

	case wlipc.ActionSwitchAppNext:
		if !s.switcherActive {
			s.openSwitcher()
		} else {
			s.switcherCycleNext()
		}
	case wlipc.ActionSwitchAppPrev:
		if !s.switcherActive {
			s.openSwitcher()
		} else {
			s.switcherCyclePrev()
		}
	case wlipc.ActionConfirmSwitcher:
		if s.switcherActive {
			s.confirmSwitcher()
		}
	case wlipc.ActionCancelSwitcher:
		if s.switcherActive {
			s.cancelSwitcher()
		}

	case wlipc.ActionToggleFullscreen:
		if s.activeXdg != nil {
			s.fullscreenXdgWindow(s.activeXdg, !s.activeXdg.fullscreen)
		} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
			s.fullscreenXwayWindow(s.activeXway, !s.activeXway.fullscreen)
		} else {
			return false
		}

	case wlipc.ActionMaximize:
		if s.activeXdg != nil {
			s.maximizeXdgWindow(s.activeXdg)
		} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
			s.maximizeXwayWindow(s.activeXway)
		} else {
			return false
		}

	case wlipc.ActionMinimize:
		if s.activeXdg != nil {
			s.minimizeXdgWindow(s.activeXdg)
			s.focusTopmostOnDesk(s.currentDesk)
			s.writeWindowsState()
		} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
			s.minimizeXwayWindow(s.activeXway)
			s.focusTopmostOnDesk(s.currentDesk)
			s.writeWindowsState()
		} else {
			return false
		}

	case wlipc.ActionSnapLeft:
		s.snapActiveWindow(snapLeft)
	case wlipc.ActionSnapRight:
		s.snapActiveWindow(snapRight)

	case wlipc.ActionCloseWindow:
		if s.activeXdg != nil {
			s.closeXdgWindow(s.activeXdg)
		} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
			s.closeXwayWindow(s.activeXway)
		} else {
			return false
		}

	case wlipc.ActionOpenTerminal:
		go s.launchTerminal()

	case wlipc.ActionPrevDesktop:
		s.switchDesk(s.currentDesk - 1)
	case wlipc.ActionNextDesktop:
		s.switchDesk(s.currentDesk + 1)

	case wlipc.ActionSwitchDesk1:
		s.switchDesk(0)
	case wlipc.ActionSwitchDesk2:
		s.switchDesk(1)
	case wlipc.ActionSwitchDesk3:
		s.switchDesk(2)
	case wlipc.ActionSwitchDesk4:
		s.switchDesk(3)

	case wlipc.ActionMoveToDesk1:
		s.moveWindowToSpecificDesk(0)
	case wlipc.ActionMoveToDesk2:
		s.moveWindowToSpecificDesk(1)
	case wlipc.ActionMoveToDesk3:
		s.moveWindowToSpecificDesk(2)
	case wlipc.ActionMoveToDesk4:
		s.moveWindowToSpecificDesk(3)

	case wlipc.ActionMoveToPrevDesktop:
		s.moveWindowToDesk(s.currentDesk - 1)
	case wlipc.ActionMoveToNextDesktop:
		s.moveWindowToDesk(s.currentDesk + 1)

	case wlipc.ActionScreenshotFull:
		go s.takeScreenshot(false, false)
	case wlipc.ActionScreenshotRegion:
		go s.takeScreenshot(true, false)
	case wlipc.ActionScreenshotWindow:
		go s.takeScreenshot(false, true)

	case wlipc.ActionToggleDropdown:
		go s.toggleDropdownTerminal()
	case wlipc.ActionLockScreen:
		go s.lockScreen()
	case wlipc.ActionShowLauncher:
		s.requestLauncher()
	case wlipc.ActionShowEmojiPicker:
		s.requestEmojiPicker()
	case wlipc.ActionShowClipboard:
		s.requestShowClipboard()
	case wlipc.ActionCommandPalette:
		s.requestCommandPalette()

	case wlipc.ActionVolumeUp:
		go s.adjustVolume(5)
	case wlipc.ActionVolumeDown:
		go s.adjustVolume(-5)
	case wlipc.ActionVolumeMute:
		go s.toggleMute()

	case wlipc.ActionBrightnessUp:
		go s.adjustBrightness(5)
	case wlipc.ActionBrightnessDown:
		go s.adjustBrightness(-5)

	case wlipc.ActionCalculator:
		go s.launchCalculator()

	case wlipc.ActionToggleSidebar:
		s.requestSidebar()

	case wlipc.ActionWindowOverview:
		s.toggleOverview()

	case wlipc.ActionToggleTiling:
		s.toggleTiling()
	case wlipc.ActionSwapMaster:
		s.swapMaster()
	case wlipc.ActionShrinkMaster:
		s.adjustMasterRatio(-masterRatioStep)
	case wlipc.ActionGrowMaster:
		s.adjustMasterRatio(masterRatioStep)
	case wlipc.ActionToggleFloat:
		s.toggleWindowFloat()
	case wlipc.ActionToggleNightLight:
		s.toggleNightLight()
	case wlipc.ActionFocusMode:
		s.toggleFocusMode()
	case wlipc.ActionShowDesktop:
		s.toggleShowDesktop()

	case "debug_reveal_panel":
		s.revealPanelHotspot()
	case "debug_hide_panel":
		s.hidePanelHotspot()
	case "debug_disable_panel":
		s.debugSetPanelEnabled(false)
	case "debug_enable_panel":
		s.debugSetPanelEnabled(true)

	default:
		return false
	}
	return true
}

// moveWindowToSpecificDesk moves the active window to the given desktop index.
func (s *server) moveWindowToSpecificDesk(deskTarget int) {
	if s.activeXdg != nil {
		s.activeXdg.desk = deskTarget
		s.writeWindowsState()
		s.switchDesk(deskTarget)
	} else if s.activeXway != nil && !s.activeXway.isPanel && !s.activeXway.isOverlay {
		s.activeXway.desk = deskTarget
		s.writeWindowsState()
		s.switchDesk(deskTarget)
	}
}

