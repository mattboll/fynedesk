package status

import (
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
)

var powerProfileMeta = fynedesk.ModuleMetadata{
	Name:        "Power Profile",
	NewInstance: newPowerProfile,
}

const (
	profilePowerSaver  = "power-saver"
	profileBalanced    = "balanced"
	profilePerformance = "performance"
)

type powerProfile struct {
	btn     *widget.Button
	label   *widget.Label
	current string
	done    atomic.Bool
}

func (p *powerProfile) Metadata() fynedesk.ModuleMetadata {
	return powerProfileMeta
}

func (p *powerProfile) Destroy() {
	p.done.Store(true)
}

func (p *powerProfile) StatusAreaWidget() fyne.CanvasObject {
	cur, err := getProfile()
	if err != nil {
		return nil // power-profiles-daemon not available
	}
	p.current = cur

	p.label = widget.NewLabel(profileDisplayName(p.current))
	p.btn = widget.NewButtonWithIcon("", p.iconForProfile(p.current), func() {
		p.cycleProfile()
	})
	p.btn.Importance = widget.LowImportance

	go p.watchProfile()

	return container.New(&handleNarrow{}, p.btn, p.label)
}

func (p *powerProfile) cycleProfile() {
	var next string
	switch p.current {
	case profilePerformance:
		next = profileBalanced
	case profileBalanced:
		next = profilePowerSaver
	default:
		next = profilePerformance
	}

	if err := setProfile(next); err != nil {
		fyne.LogError("Failed to set power profile", err)
		return
	}
	p.current = next
	fyne.Do(func() {
		p.btn.SetIcon(p.iconForProfile(next))
		p.label.SetText(profileDisplayName(next))
	})
}

func (p *powerProfile) watchProfile() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for !p.done.Load() {
		<-tick.C
		cur, err := getProfile()
		if err != nil {
			continue
		}
		if cur != p.current {
			p.current = cur
			fyne.Do(func() {
				p.btn.SetIcon(p.iconForProfile(cur))
				p.label.SetText(profileDisplayName(cur))
			})
		}
	}
}

func (p *powerProfile) iconForProfile(profile string) fyne.Resource {
	switch profile {
	case profilePerformance:
		return wmtheme.PowerIcon
	case profilePowerSaver:
		return wmtheme.BatteryIcon
	default:
		return theme.ComputerIcon()
	}
}

func profileDisplayName(profile string) string {
	switch profile {
	case profilePerformance:
		return "Performance"
	case profilePowerSaver:
		return "Power Saver"
	default:
		return "Balanced"
	}
}

func getProfile() (string, error) {
	out, err := exec.Command("powerprofilesctl", "get").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func setProfile(profile string) error {
	return exec.Command("powerprofilesctl", "set", profile).Run()
}

func newPowerProfile() fynedesk.Module {
	return &powerProfile{}
}
