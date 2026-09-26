package compositor

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// startKeyring starts gnome-keyring-daemon outside nested mode.
func (s *server) startKeyring() {
	if !s.nestedMode {
		if _, err := exec.LookPath("gnome-keyring-daemon"); err == nil {
			keyringCmd := exec.Command("gnome-keyring-daemon", "--start", "--components=secrets,pkcs11")
			keyringCmd.Env = safeEnv()
			if out, err := keyringCmd.Output(); err == nil {
				// Parse output lines like "GNOME_KEYRING_CONTROL=/run/user/1000/keyring"
				for _, line := range strings.Split(string(out), "\n") {
					if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
						os.Setenv(parts[0], parts[1])
					}
				}
				log.Println("gnome-keyring-daemon started")
			} else {
				log.Printf("Warning: gnome-keyring-daemon failed: %v\n", err)
			}
		}
	}
}

// updateActivationEnvironment pushes the session environment to D-Bus and
// restarts the portal daemons.
func (s *server) updateActivationEnvironment() {
	args := []string{"WAYLAND_DISPLAY", "DISPLAY", "XDG_CURRENT_DESKTOP", "MOZ_ENABLE_WAYLAND"}
	// Only add --systemd flag if systemd is present (not on FreeBSD, non-systemd Linux)
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		args = append([]string{"--systemd"}, args...)
	}
	dbusCmd := exec.Command("dbus-update-activation-environment", args...)
	dbusCmd.Env = safeEnv()
	if err := dbusCmd.Run(); err != nil {
		log.Printf("Warning: dbus-update-activation-environment failed: %v\n", err)
	}
	log.Println("D-Bus activation environment updated (DISPLAY, WAYLAND_DISPLAY, XDG_CURRENT_DESKTOP, MOZ_ENABLE_WAYLAND)")

	// Restart portal daemons so they pick up the new WAYLAND_DISPLAY and
	// XDG_CURRENT_DESKTOP. This is needed in both nested and real session
	// modes: in nested mode xdpw would otherwise stay connected to the
	// host compositor and screen sharing would fail.
	//
	// Skip silently when the unit is not installed (some distros ship
	// xdg-desktop-portal-gtk only). Capture stderr on real failures so
	// the audit log shows *why* — the previous code just logged the
	// exit code, which was useless for debugging.
	for _, svc := range []string{"xdg-desktop-portal-wlr", "xdg-desktop-portal"} {
		// Pre-check unit presence — `systemctl is-enabled` returns 0 for
		// enabled, 1 for disabled, but exit 4 ("unit not found") clearly
		// signals the unit doesn't exist on this system.
		checkCmd := exec.Command("systemctl", "--user", "show", "-p", "LoadState", "--value", svc)
		if out, err := checkCmd.Output(); err == nil {
			state := strings.TrimSpace(string(out))
			if state == "not-found" || state == "" {
				log.Printf("[PORTAL] skipping %s restart: unit not installed", svc)
				continue
			}
		}

		restartCmd := exec.Command("systemctl", "--user", "restart", svc)
		var stderr strings.Builder
		restartCmd.Stderr = &stderr
		if err := restartCmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			log.Printf("[PORTAL] could not restart %s: %s", svc, msg)
		} else {
			log.Printf("[PORTAL] %s restarted with new environment", svc)
		}
	}
}

// startPanelAndRestoreSession starts the panel, restores the previous
// session and falls back to a terminal if the panel never shows up.
func (s *server) startPanelAndRestoreSession() {
	// Helper: sleep but bail out early on shutdown.
	wait := func(d time.Duration) bool {
		select {
		case <-s.shutdown:
			return false
		case <-time.After(d):
			return true
		}
	}
	if !wait(2 * time.Second) { // Give XWayland more time to initialize
		return
	}
	// startPanel reads compositor state: run it on the main thread.
	if err := s.enqueueAction(s.startPanel); err != nil {
		log.Printf("[PANEL] failed to schedule the initial panel start: %v", err)
	}

	// Restore previous session after panel is ready
	if !wait(3 * time.Second) {
		return
	}
	select {
	case s.mainThreadActions <- func() { s.restoreSession() }:
		s.triggerWakeup()
	case <-s.shutdown:
		return
	}

	// Then the applications of the session, knowing what it restored.
	if !wait(time.Second) {
		return
	}
	if !s.nestedMode {
		s.startAutostart()
	}

	// Fallback: if panel doesn't appear after 10s, launch a terminal
	if !wait(6 * time.Second) {
		return
	}
	_ = s.enqueueAction(func() {
		if s.panelXway == nil || !s.panelXway.mapped {
			log.Println("WARNING: Panel not detected after 10s, launching fallback terminal")
			s.launchTerminal()
		}
	})
}

// shutdownOnSignal stops the event loop when a signal arrives.
func (s *server) shutdownOnSignal(sigChan chan os.Signal) {
	<-sigChan
	log.Println("\nShutting down...")
	s.shuttingDown.Store(true)
	select {
	case <-s.shutdown:
	default:
		close(s.shutdown)
	}
	s.requestEndSession(false) // finishRun stops the panel
}

// endSession saves the session and ends the event loop. Main thread (the
// session is read from the views).
func (s *server) endSession(restart bool) {
	if restart {
		s.wantRestart.Store(true)
	}
	s.shuttingDown.Store(true)
	s.saveSessionState()
	s.display.Terminate()
}

// requestEndSession has the main thread end the session. If it does not
// answer, the event loop is ended without saving the session.
func (s *server) requestEndSession(restart bool) {
	if s.enqueueAction(func() { s.endSession(restart) }) != nil {
		if restart {
			s.wantRestart.Store(true)
		}
		s.shuttingDown.Store(true)
		s.display.Terminate()
	}
}

// setupPortalConfig creates an XDG portal configuration so that
// xdg-desktop-portal-wlr handles ScreenCast/Screenshot (needed for WebRTC
// camera/screen sharing in Firefox/Chrome) and GTK handles the rest.
func (s *server) setupPortalConfig() {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "xdg-desktop-portal")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		log.Printf("Warning: could not create portal config dir: %v\n", err)
		return
	}

	// Use lowercase "tyde" to match XDG_CURRENT_DESKTOP after case-folding
	configPath := filepath.Join(configDir, "tyde-portals.conf")

	// Our native portal handles Screenshot and Settings.
	// FileChooser is delegated to GTK — zenity (GTK4) cannot display a
	// file dialog inside a wlroots compositor (1x1 unmapped window bug).
	// ScreenCast (PipeWire screen sharing) delegates to wlr backend.
	content := `[preferred]
default=gtk
org.freedesktop.impl.portal.Screenshot=tyde;wlr
org.freedesktop.impl.portal.ScreenCast=wlr
org.freedesktop.impl.portal.FileChooser=gtk
org.freedesktop.impl.portal.Settings=tyde;gtk
`
	if err := atomicWriteFile(configPath, []byte(content)); err != nil {
		log.Printf("Warning: could not write portal config: %v\n", err)
		return
	}
	log.Printf("Portal config written to %s\n", configPath)

	s.setupScreencastChooser()
}

// setupScreencastChooser makes xdg-desktop-portal-wlr ask which screen or
// window to share with tyde_chooser. xdpw reads the configuration named after
// the desktop before its generic one, so a user's own config is left alone.
func (s *server) setupScreencastChooser() {
	chooser := findTydeBinary("tyde_chooser")
	if chooser == "" {
		log.Println("[PORTAL] tyde_chooser not found: screencasts use the portal's default chooser")
		return
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	configPath := filepath.Join(base, "xdg-desktop-portal-wlr", "Tyde")
	if old, err := os.ReadFile(configPath); err == nil && !strings.HasPrefix(string(old), screencastConfigMarker) {
		log.Printf("[PORTAL] keeping the user's %s", configPath)
		return
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		log.Printf("Warning: could not create %s: %v", filepath.Dir(configPath), err)
		return
	}

	content := screencastConfigMarker + `: remove this line to keep your own settings.
[screencast]
chooser_type=dmenu
chooser_cmd=` + chooser + "\n"
	if err := atomicWriteFile(configPath, []byte(content)); err != nil {
		log.Printf("Warning: could not write %s: %v", configPath, err)
	}
}
