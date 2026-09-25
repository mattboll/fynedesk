package status

import (
	"image/color"
	"log"
	"os/exec"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wm"
	"github.com/FyshOS/dryvers"
)

// BatteryModuleName is the name of the battery status module.
const BatteryModuleName = "Battery"

var batteryMeta = tyde.ModuleMetadata{
	Name:        BatteryModuleName,
	NewInstance: newBattery,
}

const criticalBatteryThreshold = 0.05 // 5%

// powerSource tells the battery level and whether the charger is plugged
// in (dryvers.Battery, or a stand-in for tests).
type powerSource interface {
	Get() (float64, error)
	PluggedIn() (bool, error)
}

type battery struct {
	battery powerSource
	// hibernate is called when the battery is about to run out; it must not
	// block.
	hibernate func(val float64)

	bar  *statusBar
	done chan struct{}
	icon *widget.Icon
	fill *canvas.Rectangle

	hibernateTriggered bool // avoid repeated hibernate attempts
}

func (b *battery) batteryTick() {
	tick := time.NewTicker(time.Second * 10)
	done := b.done // Destroy clears the field; the goroutine keeps its channel
	go func() {
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				val, _ := b.battery.Get()
				fyne.Do(func() {
					b.setValue(val)
				})
			}
		}
	}()
}

func (b *battery) Destroy() {
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
}

func (b *battery) Metadata() tyde.ModuleMetadata {
	return batteryMeta
}

func (b *battery) StatusAreaWidget() fyne.CanvasObject {
	if _, err := b.battery.Get(); err != nil {
		return nil
	}

	b.bar = newStatusBar()
	b.bar.SemanticColor = batteryColor
	b.icon = widget.NewIcon(wmtheme.BatteryIcon)
	b.fill = canvas.NewRectangle(theme.Color(theme.ColorNameForeground))
	prop := canvas.NewRectangle(color.Transparent)
	prop.SetMinSize(b.icon.MinSize().Add(fyne.NewSize(theme.Padding()*4, 0)))
	icon := container.NewStack(container.NewCenter(prop, b.icon), container.NewWithoutLayout(b.fill))

	// Set first value then tick
	val, _ := b.battery.Get()
	b.setValue(val)
	b.done = make(chan struct{})
	b.batteryTick() // it starts its own goroutine
	return container.New(&handleNarrow{}, icon, b.bar)
}

func (b *battery) positionFill(val float64) {
	max := float32(12)
	down := max - max*float32(val)
	b.fill.Move(fyne.NewPos(14, 13+down))
	b.fill.Resize(fyne.NewSize(8, 13-down))
}

func (b *battery) setValue(val float64) {
	b.bar.SetValue(val)
	b.positionFill(val)
	if on, err := b.battery.PluggedIn(); on || err != nil {
		b.icon.SetResource(wmtheme.PowerIcon)
		b.fill.Hide()
		b.hibernateTriggered = false // reset when plugged in
	} else if val < 0.1 {
		b.icon.SetResource(theme.NewErrorThemedResource(wmtheme.BatteryIcon))
		b.fill.FillColor = batteryColor(val)
		b.fill.Refresh()
		b.fill.Show()

		// Critical battery: hibernate to prevent data loss
		if val > 0 && val < criticalBatteryThreshold && !b.hibernateTriggered {
			b.hibernateTriggered = true
			b.hibernate(val)
		}
	} else {
		b.icon.SetResource(wmtheme.BatteryIcon)
		b.fill.FillColor = batteryColor(val)
		b.fill.Refresh()
		b.fill.Show()
	}
}

// triggerHibernate warns the user and puts the system into hibernate/suspend.
func (b *battery) triggerHibernate(val float64) {
	pct := strconv.Itoa(int(val * 100))
	n := wm.NewNotification("Critical Battery",
		"Battery at "+pct+"% — hibernating now to prevent data loss.")
	wm.SendNotification(n)

	// Wait a moment for the notification to display
	time.Sleep(2 * time.Second)

	// Re-check the AC state right before exec — the user may have plugged
	// in during the 2s notification window.
	if on, err := b.battery.PluggedIn(); err == nil && on {
		log.Printf("[battery] AC connected during hibernate countdown, aborting")
		b.hibernateTriggered = false
		return
	}

	// Try hibernate first, fall back to suspend
	if err := exec.Command("systemctl", "hibernate").Run(); err != nil {
		log.Printf("[battery] hibernate failed: %v, trying suspend", err)
		if err := exec.Command("systemctl", "suspend").Run(); err != nil {
			log.Printf("[battery] suspend also failed: %v", err)
		}
	}
}

// newBattery creates a new module that will show battery level in the status area
func newBattery() tyde.Module {
	b := &battery{battery: dryvers.NewBattery()}
	b.hibernate = func(val float64) { go b.triggerHibernate(val) }
	return b
}

type handleNarrow struct{}

func (h *handleNarrow) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Resize(fyne.NewSize(size.Height, size.Height))
	objects[1].Resize(fyne.NewSize(size.Width-size.Height-theme.Padding(), size.Height))
	objects[1].Move(fyne.NewPos(size.Height+theme.Padding(), 0))

	if tyde.Instance() != nil && tyde.Instance().Settings().NarrowWidgetPanel() {
		objects[1].Hide()
	} else {
		objects[1].Show()
	}
}

func (h *handleNarrow) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(36, 36)
}
