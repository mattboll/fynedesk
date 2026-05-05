package compositor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fyshos.com/fynedesk/wlipc"
)

// safeEnv returns a sanitized environment for launching external processes.
// It removes dangerous variables like LD_PRELOAD that could inject code.
func safeEnv() []string {
	safe := []string{}
	for _, e := range os.Environ() {
		key := e[:strings.IndexByte(e, '=')]
		switch key {
		case "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "LD_DEBUG":
			continue // skip dangerous linker variables
		default:
			safe = append(safe, e)
		}
	}
	return safe
}

// findBinary looks for a binary in standard system paths, returning the first found.
// Falls back to the bare name (PATH lookup) if not found in standard locations.
func findBinary(name string) string {
	for _, dir := range []string{"/usr/bin", "/usr/local/bin", "/bin"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return name // fallback to PATH
}

// startPrivateDBus launches a private dbus-daemon session in nested mode.
// This gives the nested compositor its own D-Bus bus so that services like
// org.freedesktop.Notifications don't conflict with the host session.
func (s *server) startPrivateDBus() {
	if !s.nestedMode {
		return
	}

	out, err := exec.Command(findBinary("dbus-daemon"), "--session", "--print-address", "--print-pid", "--fork").Output()
	if err != nil {
		log.Printf("Warning: could not start private D-Bus: %v\n", err)
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 1 || lines[0] == "" {
		log.Println("Warning: dbus-daemon returned empty address")
		return
	}

	os.Setenv("DBUS_SESSION_BUS_ADDRESS", lines[0])

	if len(lines) >= 2 {
		if pid, err := strconv.Atoi(strings.TrimSpace(lines[1])); err == nil {
			s.privateDBusPid = pid
		}
	}

	log.Printf("Private D-Bus session: %s (pid=%d)\n", lines[0], s.privateDBusPid)
}

// stopPrivateDBus kills the private dbus-daemon started for nested mode.
func (s *server) stopPrivateDBus() {
	if s.privateDBusPid > 0 {
		syscall.Kill(s.privateDBusPid, syscall.SIGTERM)
		s.privateDBusPid = 0
	}
}

func (s *server) launchTerminal() {
	// Try common terminal emulators in order of preference
	terminals := []string{"foot", "alacritty", "kitty", "gnome-terminal", "konsole", "xterm"}

	for _, term := range terminals {
		cmd := exec.Command(findBinary(term))
		cmd.Env = safeEnv()
		if err := cmd.Start(); err == nil {
			log.Printf("Launched terminal: %s\n", term)
			return
		}
	}
	log.Println("Warning: Could not find any terminal emulator")
}

func (s *server) takeScreenshot(regionSelect, windowCapture bool) {
	// Generate filename with timestamp
	homeDir, _ := os.UserHomeDir()
	picturesDir := filepath.Join(homeDir, "Pictures")
	os.MkdirAll(picturesDir, 0755)

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(picturesDir, fmt.Sprintf("screenshot_%s.png", timestamp))

	if regionSelect {
		// Enter compositor-side region selection mode (draws overlay for
		// the user to click-drag a rectangle). The actual capture happens
		// in finishRegionSelect when the mouse button is released.
		s.mainThreadActions <- func() { s.startRegionSelect() }
		s.triggerWakeup()
		return
	}

	if windowCapture {
		// Enter window pick mode: next click captures the clicked window
		s.mainThreadActions <- func() { s.startWindowPick() }
		s.triggerWakeup()
		return
	}

	// Full screen capture with grim (run async to avoid blocking event loop)
	cmd := exec.Command(findBinary("grim"), filename)
	cmd.Env = safeEnv()
	if err := cmd.Start(); err != nil {
		log.Println("Screenshot failed: grim is not installed. Install with: apt install grim slurp")
		return
	}
	go func() {
		if err := cmd.Wait(); err == nil {
			log.Printf("Screenshot saved to %s\n", filename)
			s.enqueueAction(func() { s.notifyScreenshot(filename) })
		} else {
			log.Printf("Screenshot failed: grim exited with error: %v\n", err)
		}
	}()
}

// startWindowPick enters a mode where the next click captures the clicked window.
func (s *server) startWindowPick() {
	s.windowPickMode = true
	log.Println("[SCREENSHOT] Window pick mode: click a window to capture")
}

// captureClickedWindow captures the window at the given coordinates.
// Called from the pointer click handler when windowPickMode is active.
func (s *server) captureClickedWindow(x, y float64) {
	s.windowPickMode = false

	// Find the window under the cursor
	xdgV, xwayV, _, _, _ := s.viewAt(x, y)

	var region string
	if xdgV != nil {
		v := xdgV
		surface := v.xdgToplevel.Base().Surface()
		state := surface.Current()
		rx := int(v.x)
		ry := int(v.y)
		w := state.Width()
		h := state.Height()
		if v.decorated {
			ry -= titlebarHeight
			h += titlebarHeight
		}
		region = fmt.Sprintf("%d,%d %dx%d", rx, ry, w, h)
	} else if xwayV != nil && !xwayV.isPanel {
		v := xwayV
		surface := v.surface.Surface()
		state := surface.Current()
		rx := int(v.x)
		ry := int(v.y)
		w := state.Width()
		h := state.Height()
		if v.decorated {
			ry -= titlebarHeight
			h += titlebarHeight
		}
		region = fmt.Sprintf("%d,%d %dx%d", rx, ry, w, h)
	}

	if region == "" {
		log.Println("[SCREENSHOT] No window at click position")
		return
	}

	homeDir, _ := os.UserHomeDir()
	picturesDir := filepath.Join(homeDir, "Pictures")
	os.MkdirAll(picturesDir, 0755)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(picturesDir, fmt.Sprintf("screenshot_%s.png", timestamp))

	grimCmd := exec.Command(findBinary("grim"), "-g", region, filename)
	grimCmd.Env = safeEnv()
	if err := grimCmd.Start(); err != nil {
		log.Printf("[SCREENSHOT] grim failed to start: %v\n", err)
		return
	}
	go func() {
		if err := grimCmd.Wait(); err == nil {
			log.Printf("[SCREENSHOT] Window captured to %s\n", filename)
			s.enqueueAction(func() { s.notifyScreenshot(filename) })
		} else {
			log.Printf("[SCREENSHOT] grim failed: %v\n", err)
		}
	}()
}

// notifyScreenshot sends screenshot event to panel via IPC and copies to clipboard.
func (s *server) notifyScreenshot(filePath string) {
	configDir := s.getConfigDir()
	evt := struct {
		FilePath  string `json:"file_path"`
		Timestamp int64  `json:"timestamp"`
	}{
		FilePath:  filePath,
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(evt)
	writeAtomic(filepath.Join(configDir, "screenshot-event.json"), data)

	// Copy to Wayland clipboard (best-effort)
	go func() {
		cmd := exec.Command(findBinary("wl-copy"), "--type", "image/png")
		f, err := os.Open(filePath)
		if err != nil {
			return
		}
		cmd.Stdin = f
		cmd.Env = safeEnv()
		cmd.Run()
		f.Close()
	}()
}

var dropdownTermVisible bool

func (s *server) resetIdleTimer() {
	s.lastInputTime = time.Now()

	// Don't clear idleLocked if:
	// - a session lock client is active (it handles auth)
	// - a suspend-triggered lock is pending (race between resume input and lock launch)
	if !s.locked && !s.suspendLockPending {
		s.idleLocked = false
	}

	s.idleSuspended = false

	// Unblank display on input
	if s.displayBlanked {
		s.setDisplayBlanked(false)
		// After unblanking while locked, re-focus the lock surface to ensure
		// keyboard events reach it. The DRM disable/enable cycle can cause
		// the seat's keyboard focus to go stale (client may have re-created
		// its surface, or the focus target may have become invalid).
		if s.locked {
			s.focusLockSurface()
		}
	}
}

// initPowerDefaults sets initial power timeout values before settings are loaded.
func (s *server) initPowerDefaults() {
	s.powerLockTimeout = 5
	s.powerBlankTimeout = 6
	s.powerSuspendTimeout = 0
	s.powerSuspendAction = "suspend"
}

func (s *server) watchIdleTimeout() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[IDLE] panic recovered in watchIdleTimeout: %v\n", r)
		}
	}()

	// In nested mode, the parent session handles idle/lock/blank
	if s.nestedMode {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
		}
		if s.shuttingDown.Load() {
			return
		}
		// Skip idle actions if screensaver is inhibited
		if s.screenSaverDBus != nil && s.screenSaverDBus.IsInhibited() {
			continue
		}

		// Route idle checks through mainThreadActions to avoid data races
		// on idleLocked/displayBlanked fields (read/written by main thread).
		s.mainThreadActions <- func() {
			idle := time.Since(s.lastInputTime)

			// Lock screen
			lockTimeout := time.Duration(s.powerLockTimeout) * time.Minute
			if s.powerLockTimeout > 0 && !s.idleLocked && idle >= lockTimeout {
				s.idleLocked = true
				log.Println("Idle timeout reached, locking screen")
				go s.lockScreen()
			}

			// Blank display
			blankTimeout := time.Duration(s.powerBlankTimeout) * time.Minute
			if s.powerBlankTimeout > 0 && !s.displayBlanked && idle >= blankTimeout {
				log.Println("Blank timeout reached, blanking display")
				s.setDisplayBlanked(true)
			}

			// Auto-suspend/hibernate
			suspendTimeout := time.Duration(s.powerSuspendTimeout) * time.Minute
			if s.powerSuspendTimeout > 0 && !s.idleSuspended && idle >= suspendTimeout &&
				s.powerSuspendAction != "nothing" {
				s.idleSuspended = true
				log.Printf("Suspend timeout reached, executing: %s\n", s.powerSuspendAction)
				go s.performSuspendAction(s.powerSuspendAction)
			}
		}
		s.triggerWakeup()
	}
}

// performSuspendAction calls systemctl with the given action (suspend/hibernate/hybrid-sleep).
func (s *server) performSuspendAction(action string) {
	// Lock screen first if not already locked
	if !s.idleLocked {
		s.mainThreadActions <- func() {
			s.idleLocked = true
		}
		s.triggerWakeup()
		s.lockScreen()
		// Small delay to let lock screen start before suspend
		time.Sleep(500 * time.Millisecond)
	}

	cmd := exec.Command(findBinary("systemctl"), action)
	if err := cmd.Run(); err != nil {
		log.Printf("[POWER] systemctl %s failed: %v\n", action, err)
	}
}

// setDisplayBlanked enables or disables all outputs.
// When blanking, DRM CRTCs are disabled to save power.
// When unblanking (e.g. after suspend/resume), we re-enable outputs and
// schedule a frame to restart the render loop with a fresh swapchain.
func (s *server) setDisplayBlanked(blank bool) {
	s.displayBlanked = blank
	if blank {
		for _, out := range s.outputs {
			out.output.Enable(false)
			out.output.Commit()
		}
		log.Println("Display blanked")
		return
	}

	// Unblanking: re-enable outputs and schedule frames immediately.
	// DRM modeset is synchronous (Commit blocks until complete), so no
	// artificial delay is needed. The previous time.Sleep(50ms) was blocking
	// the wlroots event loop, causing input to freeze on wake from blank.
	for _, out := range s.outputs {
		out.output.Enable(true)
		out.output.Commit()
		scheduleOutputFrame(out.output)
	}
	log.Printf("Display unblanked, render loop restarted (locked=%v)\n", s.locked)
}

func (s *server) lockScreen() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[LOCK] panic recovered in lockScreen: %v\n", r)
		}
	}()

	// If user selected FyshOS (built-in) screensaver, use it directly
	if s.lockScreenType == "FyshOS" {
		log.Println("Using built-in lock screen (FyshOS screensaver setting)")
		s.mainThreadActions <- func() { s.activateBuiltinLock() }
		s.triggerWakeup()
		return
	}

	// Try common Wayland screen lockers
	lockers := [][]string{
		{"swaylock", "-f"}, // swaylock (sway's locker, works with wlroots)
		{"waylock"},        // waylock
		{"gtklock"},        // GTK-based locker
	}

	for _, locker := range lockers {
		cmd := exec.Command(findBinary(locker[0]), locker[1:]...)
		cmd.Env = safeEnv()
		if err := cmd.Start(); err == nil {
			log.Printf("Screen locked with %s\n", locker[0])

			// Safety timeout: if the lock client doesn't connect within 10 seconds,
			// it may have crashed silently (e.g. swaylock -f forks and the child dies).
			// In that case, force-unlock to prevent a permanent black screen.
			// Route through mainThreadActions to avoid data race on idleLocked/locked.
			go func() {
				select {
				case <-s.shutdown:
					return
				case <-time.After(10 * time.Second):
				}
				if s.shuttingDown.Load() {
					return
				}
				select {
				case s.mainThreadActions <- func() {
					if s.idleLocked && !s.locked {
						log.Println("[LOCK] Lock client launched but never connected — clearing idle lock")
						s.idleLocked = false
					}
				}:
					s.triggerWakeup()
				case <-s.shutdown:
				}
			}()
			return
		}
	}
	// No external locker found — fallback to built-in lock screen
	log.Println("No external locker found — using built-in lock screen")
	s.mainThreadActions <- func() { s.activateBuiltinLock() }
	s.triggerWakeup()
}

func (s *server) adjustVolume(delta int) {
	// Try wpctl first (PipeWire), fallback to pactl (PulseAudio)
	sign := "+"
	if delta < 0 {
		sign = "-"
		delta = -delta
	}
	cmd := exec.Command(findBinary("wpctl"), "set-volume", "@DEFAULT_AUDIO_SINK@", fmt.Sprintf("%d%%", delta)+sign)
	if err := cmd.Run(); err != nil {
		// Fallback to pactl
		pSign := "+"
		if sign == "-" {
			pSign = "-"
		}
		cmd2 := exec.Command(findBinary("pactl"), "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%s%d%%", pSign, delta))
		if err2 := cmd2.Run(); err2 != nil {
			log.Printf("Failed to adjust volume: wpctl: %v, pactl: %v\n", err, err2)
		}
	}
	// Notify panel to refresh sound widget
	s.writeVolumeEvent()
	go s.saveVolumeState()
}

func (s *server) toggleMute() {
	// Try wpctl first (PipeWire), fallback to pactl (PulseAudio)
	cmd := exec.Command(findBinary("wpctl"), "set-mute", "@DEFAULT_AUDIO_SINK@", "toggle")
	if err := cmd.Run(); err != nil {
		cmd2 := exec.Command(findBinary("pactl"), "set-sink-mute", "@DEFAULT_SINK@", "toggle")
		if err2 := cmd2.Run(); err2 != nil {
			log.Printf("Failed to toggle mute: wpctl: %v, pactl: %v\n", err, err2)
		}
	}
	// Notify panel to refresh sound widget
	s.writeVolumeEvent()
	go s.saveVolumeState()
}

func (s *server) adjustBrightness(delta int) {
	sign := "+"
	if delta < 0 {
		sign = "-"
		delta = -delta
	}
	// Try brightnessctl first, then xbacklight
	cmd := exec.Command(findBinary("brightnessctl"), "set", fmt.Sprintf("%d%%%s", delta, sign))
	if err := cmd.Run(); err != nil {
		cmd2 := exec.Command(findBinary("xbacklight"), sign+fmt.Sprintf("%d", delta))
		if err2 := cmd2.Run(); err2 != nil {
			log.Printf("Failed to adjust brightness: brightnessctl: %v, xbacklight: %v\n", err, err2)
			return
		}
	}
	wlipc.NotifyBrightnessEvent()
}

func (s *server) launchCalculator() {
	calcs := []string{"gnome-calculator", "kcalc", "mate-calc", "galculator", "xcalc"}
	for _, calc := range calcs {
		cmd := exec.Command(findBinary(calc))
		cmd.Env = safeEnv()
		if err := cmd.Start(); err == nil {
			return
		}
	}
	log.Println("Warning: No calculator application found")
}

func (s *server) writeVolumeEvent() {
	configDir := s.getConfigDir()
	eventPath := filepath.Join(configDir, "volume-event.json")
	data := fmt.Sprintf(`{"timestamp":%d}`, time.Now().UnixNano())
	writeAtomic(eventPath, []byte(data))
}

// saveVolumeState persists current volume and mute state to disk
func (s *server) saveVolumeState() {
	// Query current volume via wpctl
	out, err := exec.Command(findBinary("wpctl"), "get-volume", "@DEFAULT_AUDIO_SINK@").Output()
	if err != nil {
		return
	}
	line := strings.TrimSpace(string(out))
	// Format: "Volume: 0.50" or "Volume: 0.50 [MUTED]"
	muted := strings.Contains(line, "[MUTED]")
	line = strings.TrimPrefix(line, "Volume: ")
	line = strings.TrimSuffix(line, " [MUTED]")
	vol, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
	if err != nil {
		return
	}

	state := struct {
		Volume float64 `json:"volume"`
		Muted  bool    `json:"muted"`
	}{Volume: vol, Muted: muted}
	data, _ := json.Marshal(state)
	writeAtomic(filepath.Join(s.getConfigDir(), "volume-state.json"), data)
}

// restoreVolume restores saved volume and mute state at startup
func (s *server) restoreVolume() {
	data, err := os.ReadFile(filepath.Join(s.getConfigDir(), "volume-state.json"))
	if err != nil {
		return
	}
	var state struct {
		Volume float64 `json:"volume"`
		Muted  bool    `json:"muted"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	// Set volume (wpctl uses 0.0-1.0 range)
	volStr := fmt.Sprintf("%.2f", state.Volume)
	cmd := exec.Command(findBinary("wpctl"), "set-volume", "@DEFAULT_AUDIO_SINK@", volStr)
	if err := cmd.Run(); err != nil {
		// Fallback to pactl (uses percentage)
		pctStr := fmt.Sprintf("%d%%", int(state.Volume*100))
		exec.Command(findBinary("pactl"), "set-sink-volume", "@DEFAULT_SINK@", pctStr).Run()
	}

	// Restore mute state
	if state.Muted {
		cmd := exec.Command(findBinary("wpctl"), "set-mute", "@DEFAULT_AUDIO_SINK@", "1")
		if err := cmd.Run(); err != nil {
			exec.Command(findBinary("pactl"), "set-sink-mute", "@DEFAULT_SINK@", "1").Run()
		}
	} else {
		cmd := exec.Command(findBinary("wpctl"), "set-mute", "@DEFAULT_AUDIO_SINK@", "0")
		if err := cmd.Run(); err != nil {
			exec.Command(findBinary("pactl"), "set-sink-mute", "@DEFAULT_SINK@", "0").Run()
		}
	}
	log.Printf("Volume restored: %.0f%%%s\n", state.Volume*100, map[bool]string{true: " (muted)", false: ""}[state.Muted])
}

func (s *server) toggleDropdownTerminal() {
	// Try to find existing dropdown terminal window
	for _, v := range s.xwayViews {
		if v.mapped && strings.Contains(strings.ToLower(v.surface.Title()), "dropdown") {
			if dropdownTermVisible {
				// Hide by minimizing
				v.minimized = true
				dropdownTermVisible = false
				setViewSceneEnabled(v.sceneTree, false)
				// Refocus the topmost window on the current desktop
				s.focusTopmostOnDesk(s.currentDesk)
			} else {
				// Show
				v.minimized = false
				ddGeo := s.getActiveOutputGeo()
				ddCx, ddCy, _, _ := s.contentBounds(ddGeo)
				v.y = float64(ddCy)
				v.x = float64(ddCx)
				dropdownTermVisible = true
				setViewSceneEnabled(v.sceneTree, true)
				setXwayScenePos(v)
				s.focusXwayView(v)
			}
			return
		}
	}
	for _, v := range s.xdgViews {
		if v.mapped && strings.Contains(strings.ToLower(v.xdgToplevel.Title()), "dropdown") {
			if dropdownTermVisible {
				v.minimized = true
				dropdownTermVisible = false
				setViewSceneEnabled(v.sceneTree, false)
			} else {
				v.minimized = false
				ddGeo2 := s.getActiveOutputGeo()
				ddCx2, ddCy2, _, _ := s.contentBounds(ddGeo2)
				v.y = float64(ddCy2)
				v.x = float64(ddCx2)
				dropdownTermVisible = true
				setViewSceneEnabled(v.sceneTree, true)
				setXdgScenePos(v)
				s.focusXdgView(v)
			}
			return
		}
	}

	// No dropdown terminal found, launch one
	dropdownTermVisible = true
	terminals := []string{"foot", "alacritty", "kitty", "gnome-terminal", "xterm"}

	for _, term := range terminals {
		var cmd *exec.Cmd
		bin := findBinary(term)
		switch term {
		case "foot":
			cmd = exec.Command(bin, "--title=Dropdown Terminal")
		case "alacritty":
			cmd = exec.Command(bin, "--title=Dropdown Terminal")
		case "kitty":
			cmd = exec.Command(bin, "--title=Dropdown Terminal")
		default:
			cmd = exec.Command(bin, "-title", "Dropdown Terminal")
		}
		cmd.Env = safeEnv()
		if err := cmd.Start(); err == nil {
			log.Printf("Launched dropdown terminal: %s\n", term)
			return
		}
	}
	log.Println("Warning: Could not find any terminal emulator for dropdown")
}

func (s *server) startPanel() {
	// Check if we have valid screen dimensions
	p := s.primaryOutput()
	if p != nil {
		pGeo := s.getOutputGeometry(p)
		log.Printf("[PANEL-START] primary=%s geo=(%d,%d %dx%d)\n",
			p.output.Name(), pGeo.x, pGeo.y, pGeo.width, pGeo.height)
	}
	if p == nil || p.width <= 0 || p.height <= 0 {
		w, h := 0, 0
		if p != nil {
			w, h = p.width, p.height
		}
		log.Printf("ERROR: Invalid screen dimensions %dx%d, cannot start panel\n", w, h)
		return
	}

	// Check if XWayland is ready
	if !s.xwayland.Valid() {
		log.Println("ERROR: XWayland not valid, cannot start panel")
		return
	}

	// Find the panel binary (check local build first for development)
	home := os.Getenv("HOME")
	panelPath := "./fynedesk-panel" // root dir: always the freshest go build output
	if _, err := os.Stat(panelPath); os.IsNotExist(err) {
		panelPath = "cmd/fynedesk-panel/fynedesk-panel"
	}
	if _, err := os.Stat(panelPath); os.IsNotExist(err) {
		panelPath = home + "/.local/bin/fynedesk-panel"
	}
	if _, err := os.Stat(panelPath); os.IsNotExist(err) {
		panelPath = "/tmp/fynedesk-panel"
	}
	if _, err := os.Stat(panelPath); os.IsNotExist(err) {
		log.Println("Warning: Panel binary not found at", panelPath)
		log.Println("Launching fallback terminal instead")
		s.launchTerminal()
		return
	}

	pGeo2 := s.getOutputGeometry(p)
	log.Printf("Starting panel: %s (%dx%d at %d,%d)\n", panelPath, p.width, p.height, pGeo2.x, pGeo2.y)

	// Set XWayland DPI to 96 (standard) to prevent HiDPI scaling
	xrandrCmd := exec.Command(findBinary("xrandr"), "--dpi", "96")
	xrandrCmd.Env = safeEnv()
	if err := xrandrCmd.Run(); err != nil {
		_ = err // xrandr --dpi 96 may fail in nested mode
	}

	s.panelCmd = exec.Command(panelPath, fmt.Sprintf("%d", p.width), fmt.Sprintf("%d", p.height),
		fmt.Sprintf("%d", pGeo2.x), fmt.Sprintf("%d", pGeo2.y))
	// Panel uses DISPLAY (X11 via XWayland) since Fyne doesn't have native Wayland support
	s.panelCmd.Env = safeEnv()

	// Ensure XDG directories are properly set for application discovery
	// This is critical when running from TTY where these may not be set
	xdgDataDirs := os.Getenv("XDG_DATA_DIRS")
	if xdgDataDirs == "" {
		xdgDataDirs = "/usr/local/share:/usr/share"
	}
	// Also include user's local share and flatpak/snap locations
	xdgDataDirs = home + "/.local/share:" + xdgDataDirs + ":/var/lib/flatpak/exports/share:/var/lib/snapd/desktop"
	s.panelCmd.Env = append(s.panelCmd.Env, "XDG_DATA_DIRS="+xdgDataDirs)

	// Disable HiDPI scaling everywhere - we want 1:1 pixel mapping.
	// The compositor constants (barWidth=36, widgetWidth=196) assume Scale=1.0.
	// FYNE_SCALE=1 sets the user scale, but Fyne also detects DPI from the
	// monitor's physical dimensions (EDID). In DRM mode this can yield Scale>1.0,
	// making the panel bar wider than the compositor expects.
	// FYNE_DISABLE_DPI_DETECTION prevents Fyne from reading monitor physical size.
	s.panelCmd.Env = append(s.panelCmd.Env, "FYNE_SCALE=1")
	s.panelCmd.Env = append(s.panelCmd.Env, "FYNE_DISABLE_DPI_DETECTION=1")
	s.panelCmd.Env = append(s.panelCmd.Env, "GDK_SCALE=1")
	s.panelCmd.Env = append(s.panelCmd.Env, "GDK_DPI_SCALE=1")
	s.panelCmd.Env = append(s.panelCmd.Env, "QT_SCALE_FACTOR=1")
	s.panelCmd.Env = append(s.panelCmd.Env, "QT_AUTO_SCREEN_SCALE_FACTOR=0")

	// Set XDG_RUNTIME_DIR if not set (needed for Wayland/X11)
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		s.panelCmd.Env = append(s.panelCmd.Env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", os.Getuid()))
	}

	s.panelCmd.Stdout = os.Stdout
	s.panelCmd.Stderr = os.Stderr

	if err := s.panelCmd.Start(); err != nil {
		log.Printf("ERROR: Could not start panel: %v\n", err)
		log.Println("Launching fallback terminal instead")
		s.launchTerminal()
		return
	}

	// Watch for panel crash and auto-restart
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANEL] panic recovered in restart loop: %v\n", r)
			}
		}()

		err := s.panelCmd.Wait()
		if err != nil {
			log.Printf("Panel exited unexpectedly: %v\n", err)
		} else {
			log.Println("Panel exited cleanly")
		}
		// Restart after a short delay unless compositor is shutting down
		time.Sleep(1 * time.Second)
		if !s.shuttingDown.Load() {
			log.Println("Restarting panel...")
			s.startPanel()
		}
	}()
}
