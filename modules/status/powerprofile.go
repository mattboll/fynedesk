package status

import (
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/godbus/dbus/v5"

	"fyshos.com/tyde"
	wmtheme "fyshos.com/tyde/theme"
)

var powerProfileMeta = tyde.ModuleMetadata{
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
	current string // Fyne thread
	daemon  *profileDaemon
	stop    func() // stops watching the daemon
}

func (p *powerProfile) Metadata() tyde.ModuleMetadata {
	return powerProfileMeta
}

func (p *powerProfile) Destroy() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

func (p *powerProfile) StatusAreaWidget() fyne.CanvasObject {
	d, err := findProfileDaemon()
	if err != nil {
		return nil // power-profiles-daemon not available
	}
	cur, err := d.get()
	if err != nil {
		return nil
	}
	p.daemon, p.current = d, cur

	p.label = widget.NewLabel(profileDisplayName(p.current))
	p.btn = widget.NewButtonWithIcon("", p.iconForProfile(p.current), func() {
		p.cycleProfile()
	})
	p.btn.Importance = widget.LowImportance

	// The daemon says when the profile changes (it used to be asked through
	// powerprofilesctl, a Python script, every 5 s).
	p.stop = d.watch(func(profile string) {
		fyne.Do(func() { p.show(profile) })
	})

	return container.New(&handleNarrow{}, p.btn, p.label)
}

// show shows a profile. Fyne thread.
func (p *powerProfile) show(profile string) {
	p.current = profile
	p.btn.SetIcon(p.iconForProfile(profile))
	p.label.SetText(profileDisplayName(profile))
}

// cycleProfile moves to the next profile. Fyne thread.
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

	if err := p.daemon.set(next); err != nil {
		fyne.LogError("Failed to set power profile", err)
		return
	}
	p.show(next)
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

// profileDaemon is power-profiles-daemon on the system bus.
type profileDaemon struct {
	conn *dbus.Conn
	name string // bus name, which is also the interface
	path dbus.ObjectPath
}

// profileBus is the bus of the daemon (replaced in tests).
var profileBus = dbus.SystemBus

// findProfileDaemon finds power-profiles-daemon under its current name, or
// the one it had before 0.20.
func findProfileDaemon() (*profileDaemon, error) {
	conn, err := profileBus()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, d := range []profileDaemon{
		{conn: conn, name: "org.freedesktop.UPower.PowerProfiles", path: "/org/freedesktop/UPower/PowerProfiles"},
		{conn: conn, name: "net.hadess.PowerProfiles", path: "/net/hadess/PowerProfiles"},
	} {
		if _, err := d.get(); err == nil {
			return &d, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (d *profileDaemon) obj() dbus.BusObject { return d.conn.Object(d.name, d.path) }

func (d *profileDaemon) get() (string, error) {
	v, err := d.obj().GetProperty(d.name + ".ActiveProfile")
	if err != nil {
		return "", err
	}
	profile, ok := v.Value().(string)
	if !ok {
		return "", fmt.Errorf("ActiveProfile is a %T", v.Value())
	}
	return profile, nil
}

func (d *profileDaemon) set(profile string) error {
	return d.obj().SetProperty(d.name+".ActiveProfile", dbus.MakeVariant(profile))
}

// watch calls changed with the new profile whenever it changes, until the
// returned function is called.
func (d *profileDaemon) watch(changed func(string)) (stop func()) {
	opts := []dbus.MatchOption{
		dbus.WithMatchObjectPath(d.path),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	}
	if err := d.conn.AddMatchSignal(opts...); err != nil {
		return func() {}
	}
	signals := make(chan *dbus.Signal, 8)
	d.conn.Signal(signals)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-signals:
				if sig == nil || sig.Path != d.path || len(sig.Body) < 2 {
					continue
				}
				if props, ok := sig.Body[1].(map[string]dbus.Variant); ok {
					if v, ok := props["ActiveProfile"]; ok {
						if profile, ok := v.Value().(string); ok {
							changed(profile)
						}
					}
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			d.conn.RemoveSignal(signals)
			_ = d.conn.RemoveMatchSignal(opts...)
			close(done)
		})
	}
}

func newPowerProfile() tyde.Module {
	return &powerProfile{}
}
