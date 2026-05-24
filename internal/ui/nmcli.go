package ui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyshos.com/fynedesk/wm"
)

// nmcliConnectTimeout is the deadline for nmcli connection commands. These
// genuinely take seconds (DHCP, auth) so they cannot share the short default
// timeout used for status queries.
const nmcliConnectTimeout = 30 * time.Second

// WifiNetwork represents a WiFi network detected by NetworkManager.
type WifiNetwork struct {
	SSID     string
	Signal   int    // 0-100
	Security string // e.g. "WPA2", "WPA3", "" for open
	Active   bool
}

// IsSecured returns true if the network requires a password.
func (w WifiNetwork) IsSecured() bool {
	return w.Security != ""
}

// scanWifiNetworks returns available WiFi networks via nmcli.
// Results are sorted: active network first, then by signal descending.
func scanWifiNetworks() ([]WifiNetwork, error) {
	out, err := wm.ExecOutput("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY,IN-USE", "device", "wifi", "list")
	if err != nil {
		return nil, fmt.Errorf("nmcli: %w", err)
	}

	seen := make(map[string]WifiNetwork)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := parseTerseLine(line)
		if len(fields) < 4 {
			continue
		}
		ssid := fields[0]
		if ssid == "" {
			continue // hidden network
		}
		signal, _ := strconv.Atoi(fields[1])
		security := fields[2]
		active := strings.TrimSpace(fields[3]) == "*"

		// Deduplicate by SSID, keep highest signal (or active)
		if existing, ok := seen[ssid]; ok {
			if active || (!existing.Active && signal > existing.Signal) {
				seen[ssid] = WifiNetwork{SSID: ssid, Signal: signal, Security: security, Active: active}
			}
		} else {
			seen[ssid] = WifiNetwork{SSID: ssid, Signal: signal, Security: security, Active: active}
		}
	}

	networks := make([]WifiNetwork, 0, len(seen))
	for _, n := range seen {
		networks = append(networks, n)
	}
	sort.Slice(networks, func(i, j int) bool {
		if networks[i].Active != networks[j].Active {
			return networks[i].Active
		}
		return networks[i].Signal > networks[j].Signal
	})
	return networks, nil
}

// rescanWifi triggers a WiFi rescan in the background.
func rescanWifi() {
	_ = wm.ExecRunCtx(5*time.Second, "nmcli", "device", "wifi", "rescan")
}

// hasSavedWifiProfile reports whether NetworkManager already has a saved
// connection profile matching the given SSID. The profile name is normally
// the SSID itself when the profile was created via this UI, but NM may also
// store profiles under different names — so we additionally match on the
// 802-11-wireless.ssid setting.
func hasSavedWifiProfile(ssid string) bool {
	out, err := wm.ExecOutput("nmcli", "-t", "-f", "NAME,TYPE", "connection", "show")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := parseTerseLine(line)
		if len(fields) < 2 || fields[1] != "802-11-wireless" {
			continue
		}
		if fields[0] == ssid {
			return true
		}
		// Profile name may differ from SSID — check the wireless ssid setting.
		nameOut, err := wm.ExecOutput("nmcli", "-t", "-g", "802-11-wireless.ssid",
			"connection", "show", fields[0])
		if err == nil && strings.TrimSpace(string(nameOut)) == ssid {
			return true
		}
	}
	return false
}

// connectSavedWifi activates an existing NetworkManager profile for the SSID
// without re-prompting for a password. Returns an error if no profile exists
// or activation fails (e.g. the saved key is now wrong).
func connectSavedWifi(ssid string) error {
	return runNmcli("connection", "up", ssid)
}

// connectWifi connects to the given SSID, optionally with a password.
// For secured networks, it creates a connection profile with the correct
// key-mgmt setting derived from the security field (e.g. "WPA2 WPA3").
func connectWifi(ssid, password, security string) error {
	if password == "" {
		// Open network — simple connect is fine.
		return runNmcli("device", "wifi", "connect", ssid)
	}

	dev := wifiDevice()
	if dev == "" {
		return fmt.Errorf("no wifi device found")
	}

	keyMgmt := "wpa-psk"
	if strings.Contains(security, "WPA3") {
		keyMgmt = "sae"
	}

	// Delete any stale profile for this SSID so we start fresh.
	_ = wm.ExecRun("nmcli", "connection", "delete", ssid)

	// Create a new connection profile with explicit key-mgmt.
	err := runNmcli("connection", "add",
		"type", "wifi",
		"ifname", dev,
		"con-name", ssid,
		"ssid", ssid,
		"wifi-sec.key-mgmt", keyMgmt,
		"wifi-sec.psk", password,
	)
	if err != nil {
		return err
	}

	// Activate the newly created profile.
	if err := runNmcli("connection", "up", ssid); err != nil {
		// Clean up on failure.
		_ = wm.ExecRun("nmcli", "connection", "delete", ssid)
		return err
	}
	return nil
}

// runNmcli executes an nmcli command and returns a user-friendly error.
// Connect operations legitimately take seconds (DHCP, auth) so this uses a
// longer timeout than the default ExecRun.
func runNmcli(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), nmcliConnectTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nmcli", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// disconnectWifi disconnects the WiFi device.
func disconnectWifi() error {
	dev := wifiDevice()
	if dev == "" {
		return fmt.Errorf("no wifi device found")
	}
	return wm.ExecRun("nmcli", "device", "disconnect", dev)
}

// wifiDevice returns the name of the WiFi network interface.
func wifiDevice() string {
	out, err := wm.ExecOutput("nmcli", "-t", "-f", "DEVICE,TYPE", "device", "status")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && parts[1] == "wifi" {
			return parts[0]
		}
	}
	return ""
}

// parseTerseLine splits an nmcli terse-mode line on ':' while
// respecting '\:' escapes (SSIDs can contain colons).
func parseTerseLine(line string) []string {
	var fields []string
	var current strings.Builder
	escaped := false
	for _, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ':' {
			fields = append(fields, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	fields = append(fields, current.String())
	return fields
}
