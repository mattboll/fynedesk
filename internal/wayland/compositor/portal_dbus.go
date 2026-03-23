package compositor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// portalDBus implements xdg-desktop-portal backend interfaces:
//   - org.freedesktop.impl.portal.Screenshot
//   - org.freedesktop.impl.portal.Settings (color-scheme)
//
// FileChooser is NOT implemented here — zenity (GTK4) cannot display a file
// dialog inside a wlroots compositor. xdg-desktop-portal-gtk handles it.
type portalDBus struct {
	srv  *server
	conn *dbus.Conn
}

func newPortalDBus(srv *server) *portalDBus {
	return &portalDBus{srv: srv}
}

// --- Screenshot Portal ---

// Screenshot implements org.freedesktop.impl.portal.Screenshot.Screenshot
// See: https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.impl.portal.Screenshot.html
func (p *portalDBus) Screenshot(handle, appID, parentWindow string,
	options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {

	log.Printf("[PORTAL] Screenshot request from %q (handle=%s)\n", appID, handle)

	interactive := false
	if v, ok := options["interactive"]; ok {
		if b, ok := v.Value().(bool); ok {
			interactive = b
		}
	}

	filename, err := p.captureScreenshot(interactive)
	if err != nil {
		log.Printf("[PORTAL] Screenshot failed: %v\n", err)
		return 2, nil, nil // 2 = cancelled/failed
	}

	uri := "file://" + filename
	results := map[string]dbus.Variant{
		"uri": dbus.MakeVariant(uri),
	}

	log.Printf("[PORTAL] Screenshot saved: %s\n", filename)
	return 0, results, nil // 0 = success
}

// PickColor implements org.freedesktop.impl.portal.Screenshot.PickColor
func (p *portalDBus) PickColor(handle, appID, parentWindow string,
	options map[string]dbus.Variant) (uint32, map[string]dbus.Variant, *dbus.Error) {

	log.Printf("[PORTAL] PickColor request from %q (not implemented)\n", appID)
	return 2, nil, nil // not implemented
}

// captureScreenshot takes a screenshot and returns the file path.
func (p *portalDBus) captureScreenshot(interactive bool) (string, error) {
	homeDir, _ := os.UserHomeDir()
	picturesDir := filepath.Join(homeDir, "Pictures")
	os.MkdirAll(picturesDir, 0755)

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(picturesDir, fmt.Sprintf("screenshot_%s.png", timestamp))

	if interactive {
		// Region selection with slurp
		slurpCmd := exec.Command("slurp")
		slurpCmd.Env = os.Environ()
		output, err := slurpCmd.Output()
		if err != nil {
			return "", fmt.Errorf("region selection failed: %w", err)
		}
		region := strings.TrimSpace(string(output))
		grimCmd := exec.Command("grim", "-g", region, filename)
		grimCmd.Env = os.Environ()
		if err := grimCmd.Run(); err != nil {
			return "", fmt.Errorf("grim capture failed: %w", err)
		}
	} else {
		// Full screen capture
		cmd := exec.Command("grim", filename)
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("grim capture failed: %w", err)
		}
	}

	return filename, nil
}

// --- Settings Portal (color-scheme) ---

// settingsPortal is a separate export target for org.freedesktop.impl.portal.Settings.
// We use a wrapper because godbus exports ALL public methods of an object on one
// interface. Separating this avoids Screenshot methods appearing on Settings.
type settingsPortal struct {
	p *portalDBus
}

// ReadAll implements org.freedesktop.impl.portal.Settings.ReadAll.
// Returns all settings for the requested namespaces.
func (sp *settingsPortal) ReadAll(namespaces []string) (map[string]map[string]dbus.Variant, *dbus.Error) {
	result := make(map[string]map[string]dbus.Variant)
	for _, ns := range namespaces {
		if ns == "org.freedesktop.appearance" || ns == "" {
			result["org.freedesktop.appearance"] = map[string]dbus.Variant{
				"color-scheme": dbus.MakeVariant(uint32(sp.p.srv.colorScheme)),
			}
		}
	}
	return result, nil
}

// Read implements org.freedesktop.impl.portal.Settings.Read.
// Returns a single setting value.
func (sp *settingsPortal) Read(namespace, key string) (dbus.Variant, *dbus.Error) {
	if namespace == "org.freedesktop.appearance" && key == "color-scheme" {
		return dbus.MakeVariant(uint32(sp.p.srv.colorScheme)), nil
	}
	return dbus.MakeVariant(uint32(0)),
		dbus.NewError("org.freedesktop.portal.Error.NotFound",
			[]interface{}{fmt.Sprintf("unknown setting %s.%s", namespace, key)})
}

// emitColorSchemeChanged emits the SettingChanged D-Bus signal for color-scheme.
func (p *portalDBus) emitColorSchemeChanged(scheme int) {
	if p.conn == nil {
		return
	}
	err := p.conn.Emit(
		"/org/freedesktop/portal/desktop",
		"org.freedesktop.impl.portal.Settings.SettingChanged",
		"org.freedesktop.appearance",
		"color-scheme",
		dbus.MakeVariant(uint32(scheme)),
	)
	if err != nil {
		log.Printf("[PORTAL] Failed to emit SettingChanged: %v\n", err)
	} else {
		labels := []string{"no preference", "dark", "light"}
		label := "unknown"
		if scheme >= 0 && scheme < len(labels) {
			label = labels[scheme]
		}
		log.Printf("[PORTAL] Emitted color-scheme changed: %s (%d)\n", label, scheme)
	}
}

// --- D-Bus Registration ---

// startPortalDBus registers the xdg-desktop-portal backend services.
func (s *server) startPortalDBus() {
	// Skip in nested mode — host session provides portals
	if os.Getenv("WLR_BACKENDS") != "" {
		log.Println("[PORTAL] Nested mode, skipping portal registration")
		return
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("[PORTAL] D-Bus: could not connect: %v\n", err)
		return
	}

	p := newPortalDBus(s)
	p.conn = conn
	s.portal = p

	// Export Screenshot portal
	err = conn.Export(p, "/org/freedesktop/portal/desktop",
		"org.freedesktop.impl.portal.Screenshot")
	if err != nil {
		log.Printf("[PORTAL] Could not export Screenshot: %v\n", err)
		return
	}

	// FileChooser is NOT exported here — zenity (GTK4) cannot display a
	// file dialog inside a wlroots compositor (1x1 unmapped window bug).
	// Instead, xdg-desktop-portal-gtk handles FileChooser as the fallback
	// backend, which works correctly since it manages its own GTK window.

	// Export Settings portal (color-scheme)
	sp := &settingsPortal{p: p}
	err = conn.Export(sp, "/org/freedesktop/portal/desktop",
		"org.freedesktop.impl.portal.Settings")
	if err != nil {
		log.Printf("[PORTAL] Could not export Settings: %v\n", err)
	}

	// Request the portal bus name
	reply, err := conn.RequestName("org.freedesktop.impl.portal.desktop.fynedesk",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("[PORTAL] Could not request portal name: %v\n", err)
		return
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("[PORTAL] Portal name already taken\n")
		return
	}

	log.Println("[PORTAL] xdg-desktop-portal backend registered (Screenshot, Settings)")
}
