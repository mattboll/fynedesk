package status

import (
	"bufio"
	"bytes"
	"errors"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wm"
	"github.com/FyshOS/networks/pkg/netman"
	"github.com/godbus/dbus/v5"
)

var networkMeta = tyde.ModuleMetadata{
	Name:        "Network",
	NewInstance: NewNetwork,
}

const networkNameEthernet = "Ethernet"

// WifiPicker shows the NetworkManager Wi-Fi picker. It is set by the desktop
// user interface, which this package cannot import.
var WifiPicker func()

type network struct {
	name *widget.Label
	icon *widget.Button

	wasBlocked bool

	netMu sync.Mutex       // guards conn and net: built from the menu's goroutine
	conn  *dbus.Conn       // system bus for Wi-Fi browsing, opened lazily and reused
	net   *netman.Networks // iwd-backed network browser, built once on first use
	done  chan struct{}
}

func (n *network) Destroy() {
	if n.done != nil {
		close(n.done)
		n.done = nil
	}
	n.netMu.Lock()
	if n.conn != nil {
		_ = n.conn.Close()
		n.conn, n.net = nil, nil
	}
	n.netMu.Unlock()
}

func (n *network) wirelessName() (string, error) {
	iw, _ := exec.LookPath("iw")
	if iw == "" {
		iw, _ = exec.LookPath("/usr/sbin/iw")
	}
	if iw != "" {
		out, err := wm.ExecOutput(iw, "dev")
		if err != nil {
			log.Println("Error running iw", err)
			return "", err
		}
		// Parse 'iw dev' output for the first 'ssid <name>' line.
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 2 && fields[0] == "ssid" {
				return strings.Join(fields[1:], " "), nil
			}
		}
		return "", errors.New("no network connected")
	}

	// macOS fallback: 'airport -I' returns key/value lines including SSID.
	const airport = "/System/Library/PrivateFrameworks/Apple80211.framework/Resources/airport"
	out, err := wm.ExecOutput(airport, "-I")
	if err != nil {
		log.Println("Error getting network info from airport utility", err)
		return "", err
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if k, v, ok := strings.Cut(line, ": "); ok && strings.TrimSpace(k) == "SSID" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", errors.New("no network connected")
}

// airportDevice returns the macOS device name of the Wi-Fi hardware port,
// or "" if this machine has none (as on CI runners and Mac minis without Wi-Fi).
func airportDevice() string {
	out, err := wm.ExecOutput("networksetup", "-listallhardwareports")
	if err != nil {
		log.Println("Error running networksetup tool", err)
		return ""
	}

	return parseAirportDevice(string(out))
}

// parseAirportDevice picks the Wi-Fi device out of "networksetup -listallhardwareports" output.
func parseAirportDevice(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "Hardware Port: Wi-Fi") && !strings.HasPrefix(line, "Hardware Port: AirPort") {
			continue
		}
		if i+1 >= len(lines) {
			break
		}
		return strings.TrimSpace(strings.TrimPrefix(lines[i+1], "Device:"))
	}

	return ""
}

// wlanRfkill returns the rfkill id of the Wi-Fi radio and whether it is
// blocked (soft or hard), running rfkill (at path) with the default timeout.
func wlanRfkill(path string) (id string, blocked bool, err error) {
	out, err := wm.ExecOutput(path, "--noheadings", "--output", "ID,TYPE,SOFT,HARD")
	if err != nil {
		return "", false, err
	}
	return parseRfkillWlan(string(out))
}

// parseRfkillWlan reads "ID TYPE SOFT HARD" lines for the first wlan radio.
func parseRfkillWlan(out string) (id string, blocked bool, err error) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[1] == "wlan" {
			return f[0], f[2] == "blocked" || f[3] == "blocked", nil
		}
	}
	return "", false, errors.New("rfkill: no Wi-Fi radio")
}

func (n *network) isBlocked() (bool, error) {
	if ip, _ := exec.LookPath("rfkill"); ip != "" {
		_, blocked, err := wlanRfkill(ip)
		if err != nil {
			log.Println("Error running rfkill tool", err)
		}
		return blocked, err
	}
	if ip, _ := exec.LookPath("networksetup"); ip != "" {
		dev := airportDevice()
		if dev == "" { // no Wi-Fi hardware, so nothing to block
			return false, nil
		}

		out, err := wm.ExecOutput("networksetup", "-getairportpower", dev)
		if err != nil {
			log.Println("Error running networksetup tool", err)
			return false, err
		}
		// output looks like "Wi-Fi Power (en0): On"
		state := strings.TrimSpace(string(out))
		return strings.HasSuffix(state, ": Off"), nil
	}

	return false, nil
}

func (n *network) isEthernetConnected() (bool, error) {
	if ip, _ := exec.LookPath("ip"); ip != "" {
		out, err := wm.ExecOutput(ip, "link")
		if err != nil {
			log.Println("Error running ip tool", err)
			return false, err
		}
		// Count interfaces that are UP, not LOOPBACK, and not wireless (wl*).
		count := 0
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, ",UP,") {
				continue
			}
			if strings.Contains(line, "LOOPBACK") {
				continue
			}
			if strings.Contains(line, ": wl") {
				continue
			}
			count++
		}
		if count == 0 {
			return false, nil
		}
	} else if scutil, _ := exec.LookPath("scutil"); scutil != "" {
		out, err := wm.ExecOutput(scutil, "--nwi")
		if err != nil {
			log.Println("Error running scutil tool", err)
			return false, err
		}
		if !bytes.Contains(out, []byte("address")) {
			return false, nil
		}
	} else {
		out, err := wm.ExecOutput("ifconfig")
		if err != nil {
			log.Println("Error running ifconfig tool", err)
			return false, err
		}
		re, err := regexp.Compile(`^[^\t:]+:([^\n]|\n\t)*status: active`)
		if err != nil {
			log.Println("Error compiling regular expression", err)
			return false, err
		}
		m := re.FindSubmatch(out)
		if len(m) < 1 {
			return false, nil
		}
		// IPv4
		if strings.Contains(string(m[0]), "broadcast") {
			return true, nil
		}
		// IPv6, non-link-local only
		if found, _ := regexp.MatchString(`\s+inet6\s+[[:xdigit:]:]+\s+prefixlen\s+`, string(m[0])); found {
			return true, nil
		}
	}
	return true, nil
}

func (n *network) networkName() string {
	name, _ := n.wirelessName()
	if name != "" {
		return name
	}

	ether, _ := n.isEthernetConnected()
	if ether {
		return networkNameEthernet
	}
	return ""
}

func (n *network) tick() {
	n.done = make(chan struct{})
	done := n.done
	tick := time.NewTicker(time.Second * 10)
	go func() {
		defer tick.Stop()
		for {
			n.refreshContent()
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()
}

// refreshContent queries the network state and updates the status widget.
// Run on a background goroutine, and it will then refresh on fyne.Do.
func (n *network) refreshContent() {
	val := n.networkName()
	blocked, _ := n.isBlocked()
	shown := val
	if val == networkNameEthernet {
		shown = locale.T("network.ethernet")
	}

	fyne.Do(func() {
		if shown != n.name.Text || blocked != n.wasBlocked {
			n.wasBlocked = blocked
			n.name.SetText(shown)

			if blocked {
				n.icon.SetIcon(wmtheme.AirplaneIcon)
			} else if val == "" {
				n.icon.SetIcon(wmtheme.WifiOffIcon)
			} else if val == networkNameEthernet {
				n.icon.SetIcon(wmtheme.EthernetIcon)
			} else {
				n.icon.SetIcon(wmtheme.WifiIcon)
			}
		}
	})
}

func (n *network) StatusAreaWidget() fyne.CanvasObject {
	blocked := false
	if _, err := n.wirelessName(); err != nil {
		if blocked, err = n.isBlocked(); blocked || err != nil {
		}
		if _, err = n.isEthernetConnected(); err != nil && !blocked {
			return nil
		}
	}

	n.name = widget.NewLabel("")
	n.icon = &widget.Button{Icon: wmtheme.WifiOffIcon, Importance: widget.LowImportance, OnTapped: n.showMenu}
	if blocked {
		n.icon.Icon = wmtheme.AirplaneIcon
	}
	n.tick()

	return container.New(&handleNarrow{}, n.icon, n.name)
}

func (n *network) Metadata() tyde.ModuleMetadata {
	return networkMeta
}

func (n *network) setFlightMode(block bool) error {
	if ip, _ := exec.LookPath("rfkill"); ip != "" {
		id, _, err := wlanRfkill(ip)
		if err != nil {
			log.Println("Error running rfkill tool", err)
			return err
		}

		if id != "" {
			mode := "block"
			if !block {
				mode = "unblock"
			}
			// No shell: the id and mode are arguments of their own.
			cmd := exec.Command("pkexec", ip, mode, id)
			err = cmd.Start()
			if err != nil {
				log.Println("Error running rfkill tool", err)
				return err
			}

			go func() {
				cmd.Wait()
				n.refreshContent()
			}()
		}

		return nil
	}

	if ip, _ := exec.LookPath("networksetup"); ip != "" {
		dev := airportDevice()
		if dev == "" { // no Wi-Fi hardware, so nothing to toggle
			return nil
		}

		mode := "off"
		if !block {
			mode = "on"
		}
		err := wm.ExecRun("networksetup", "-setairportpower", dev, mode)
		if err != nil {
			log.Println("Error running networksetup tool", err)
			return err
		}

		n.refreshContent()
	}

	return nil
}

// showMenu pops up the network menu beneath the status icon: the Wi-Fi networks
// iwd currently knows about (from netman), followed by an Airplane Mode toggle.
func (n *network) showMenu() {
	// NetworkManager systems get the full Wi-Fi picker.
	if nmcli, _ := exec.LookPath("nmcli"); nmcli != "" && WifiPicker != nil {
		WifiPicker()
		return
	}

	// Avoid hanging with network calls.
	go func() {
		blocked, _ := n.isBlocked()

		var items []*fyne.MenuItem
		// The radio is off in airplane mode, so there are no networks to list.
		if !blocked {
			if nm := n.networks(); nm != nil {
				// Menu(nil) returns iwd's currently-known networks without blocking;
				// kick a background scan so the next open reflects any changes.
				items = append(items, nm.Menu(nil).Items...)
				go nm.Scan()
			}
		}
		if len(items) > 0 {
			items = append(items, fyne.NewMenuItemSeparator())
		}

		air := fyne.NewMenuItem(locale.T("network.airplaneMode"), n.toggleFlightMode)
		air.Checked = blocked
		items = append(items, air)

		fyne.Do(func() {
			pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(n.icon)
			tyde.Instance().ShowMenuAt(fyne.NewMenu("", items...), pos)
		})
	}()
}

// networks lazily gets a network manager from our networks repo package that will generate our menu.
func (n *network) networks() *netman.Networks {
	n.netMu.Lock()
	defer n.netMu.Unlock()
	if n.net != nil {
		return n.net
	}

	win := tyde.Instance().Root()
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Println("network menu: system bus unavailable:", err)
		return nil
	}

	// handlePass prompts for a network passphrase, blocking until the user submits
	// or cancels; it is called from netman's iwd agent callback. Cancel returns "".
	handlePass := func(name string) string {
		result := make(chan string, 1)
		entry := widget.NewPasswordEntry()
		d := dialog.NewForm(locale.Tf("wifi.connectTo", name), locale.T("wifi.connect"), locale.T("wifi.cancel"),
			[]*widget.FormItem{widget.NewFormItem(locale.T("wifi.password"), entry)},
			func(ok bool) {
				if ok {
					result <- entry.Text
				} else {
					result <- ""
				}
			}, win)
		fyne.Do(func() {
			entry.OnSubmitted = func(_ string) {
				d.Submit()
			}
			d.Resize(fyne.NewSize(320, d.MinSize().Height))
			d.Show()
		})

		return <-result
	}

	nm, err := netman.New(conn, handlePass, func(err error) {
		fyne.Do(func() { dialog.ShowError(err, win) }) // called from netman's goroutines
	})
	if err != nil {
		_ = conn.Close()
		log.Println("network menu: iwd unavailable:", err)
		return nil
	}
	n.conn, n.net = conn, nm
	return nm
}

func (n *network) toggleFlightMode() {
	// Avoid slow netowrk calls on graphical thread.
	go func() {
		blocked, err := n.isBlocked()
		if err != nil {
			fyne.LogError("blocking not supported", err)
			return
		}
		err = n.setFlightMode(!blocked)
		if err != nil {
			fyne.LogError("setting flight mode", err)
		}
	}()
}

// NewNetwork creates a new module that will show network information in the status area
func NewNetwork() tyde.Module {
	return &network{}
}
