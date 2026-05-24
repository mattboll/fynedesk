package status

import (
	"bufio"
	"bytes"
	"errors"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/ui"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wm"
)

var networkMeta = fynedesk.ModuleMetadata{
	Name:        "Network",
	NewInstance: NewNetwork,
}

const networkNameEthernet = "Ethernet"

type network struct {
	name *widget.Label
	icon *widget.Button
	done chan struct{}
}

func (n *network) Destroy() {
	if n.done != nil {
		close(n.done)
		n.done = nil
	}
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
	tick := time.NewTicker(time.Second * 10)
	go func() {
		defer tick.Stop()
		for {
			val := n.networkName()
			if val != n.name.Text {
				fyne.Do(func() {
					n.name.SetText(val)

					if val == "" {
						n.icon.SetIcon(wmtheme.WifiOffIcon)
					} else if val == networkNameEthernet {
						n.icon.SetIcon(wmtheme.EthernetIcon)
					} else {
						n.icon.SetIcon(wmtheme.WifiIcon)
					}
				})
			}
			select {
			case <-n.done:
				return
			case <-tick.C:
			}
		}
	}()
}

func (n *network) StatusAreaWidget() fyne.CanvasObject {
	if _, err := n.wirelessName(); err != nil {
		if _, err = n.isEthernetConnected(); err != nil {
			return nil
		}
	}

	n.name = widget.NewLabel("")
	n.icon = &widget.Button{Icon: wmtheme.WifiOffIcon, Importance: widget.LowImportance, OnTapped: n.showSettings}
	n.tick()

	return container.New(&handleNarrow{}, n.icon, n.name)
}

func (n *network) Metadata() fynedesk.ModuleMetadata {
	return networkMeta
}

func (n *network) showSettings() {
	ui.ShowWifiPicker()
}

// NewNetwork creates a new module that will show network information in the status area
func NewNetwork() fynedesk.Module {
	return &network{}
}
