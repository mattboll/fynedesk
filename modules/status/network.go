package status

import (
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
)

var networkMeta = fynedesk.ModuleMetadata{
	Name:        "Network",
	NewInstance: NewNetwork,
}

const networkNameEthernet = "Ethernet"

type network struct {
	name *widget.Label
	icon *widget.Button
}

func (n *network) Destroy() {
}

func (n *network) wirelessName() (string, error) {
	net := ""
	iw, _ := exec.LookPath("iw")
	if iw == "" {
		iw, _ = exec.LookPath("/usr/sbin/iw")
	}
	if iw != "" {
		out, err := exec.Command("bash", []string{"-c", iw + " dev | grep ssid | cut -d ' ' -f2"}...).Output()
		if err != nil {
			log.Println("Error running iw", err)
			return "", err
		}
		net = strings.TrimSpace(string(out))
		if net == "" {
			return "", errors.New("no network connected")
		}
	} else {
		out, err := exec.Command("bash", []string{"-c", "/System/Library/PrivateFrameworks/Apple80211.framework/Resources/airport -I  | awk -F' SSID: '  '/ SSID: / {print $2}'"}...).Output()
		if err != nil {
			log.Println("Error getting network info from airport utility", err)
			return "", err
		}

		net = string(out)
	}
	return strings.TrimSpace(net), nil
}

func (n *network) isEthernetConnected() (bool, error) {
	if ip, _ := exec.LookPath("ip"); ip != "" {
		out, err := exec.Command("bash", []string{"-c", "ip link | grep \",UP,\" | grep -v LOOPBACK | grep -v \": wl\" | wc -l"}...).Output()
		if err != nil {
			log.Println("Error running ip tool", err)
			return false, err
		}
		if strings.TrimSpace(string(out)) == "0" {
			return false, nil
		}
	} else if scutil, _ := exec.LookPath("scutil"); scutil != "" {
		out, err := exec.Command("bash", []string{"-c", "scutil --nwi | grep address | wc -l"}...).Output()
		if err != nil {
			log.Println("Error running scutil tool", err)
			return false, err
		}
		if strings.TrimSpace(string(out)) == "0" {
			return false, nil
		}
	} else {
		out, err := exec.Command("ifconfig").Output()
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
	tick := time.NewTicker(time.Second * 10)
	go func() {
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
			<-tick.C
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
