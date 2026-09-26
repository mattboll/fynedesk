package status

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/dryvers"

	"fyshos.com/tyde"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

// BrightnessModuleName is the name of the screen brightness module.
const BrightnessModuleName = "Brightness"

var brightnessMeta = tyde.ModuleMetadata{
	Name:        BrightnessModuleName,
	NewInstance: newBrightness,
}

// Brightness is a progress bar module to modify screen brightness
type brightness struct {
	bright brightnessDriver

	bar  *statusBar
	done chan struct{} // closed to stop IPC watcher goroutines

	backlightOnce sync.Once
	backlight     bool
}

// hasBacklight reports whether the brightness can be read and is set, asked
// once.
func (b *brightness) hasBacklight() bool {
	b.backlightOnce.Do(func() {
		val, err := b.bright.Get()
		b.backlight = err == nil && val > 0
	})
	return b.backlight
}

// brightnessDriver reads and sets the screen brightness, between 0 and 1.
type brightnessDriver interface {
	Get() (float64, error)
	Set(float64) error
}

func (b *brightness) Destroy() {
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
}

// offsetValue moves the brightness by diff percent and returns the new
// value (in [0, 1]).
func (b *brightness) offsetValue(diff int) float64 {
	floatVal, _ := b.bright.Get()
	if floatVal <= 0.01 { // don't start doing 6, 11 etc just because we were on 1 (min)
		floatVal = 0
	}
	value := int(math.Round(floatVal*100)) + diff

	return b.setValue(value)
}

// setValue sets the brightness in percent and returns it (in [0, 1]); the
// value set is shown, rather than asked for again.
func (b *brightness) setValue(value int) float64 {
	if value < 1 {
		value = 1
	} else if value > 100 {
		value = 100
	}

	newVal := float64(value) / 100
	_ = b.bright.Set(newVal)
	fyne.Do(func() {
		b.bar.SetValue(newVal)
	})
	return newVal
}

func (b *brightness) LaunchSuggestions(input string) []tyde.LaunchSuggestion {
	lower := strings.ToLower(input)
	matches := false
	val := lower
	if startsWith(lower, "brightness ") {
		matches = true
		if len(lower) > 11 {
			val = lower[11:]
		} else {
			val = ""
		}
	} else if startsWith(lower, "bright ") {
		matches = true
		if len(lower) > 7 {
			val = lower[7:]
		} else {
			val = ""
		}
	} else if startsWith(lower, "backlight ") {
		matches = true
		if len(lower) > 10 {
			val = lower[10:]
		} else {
			val = ""
		}
	}

	// Only now, and once: whether there is a backlight (it ran brightnessctl
	// twice at every key typed in the launcher).
	if !matches || !b.hasBacklight() {
		return nil
	}

	if _, err := strconv.Atoi(val); err != nil {
		if !startsWith(val, "d") && !startsWith(val, "u") {
			return nil
		}
	}

	return []tyde.LaunchSuggestion{&brightItem{input: val, b: b}}
}

func (b *brightness) Metadata() tyde.ModuleMetadata {
	return brightnessMeta
}

func (b *brightness) Shortcuts() map[*tyde.Shortcut]func() {
	return map[*tyde.Shortcut]func(){
		tyde.NewShortcut("Reduce Screen Brightness", tyde.KeyBrightnessDown, tyde.AnyModifier): func() {
			showOSD(wmtheme.BrightnessIcon, b.offsetValue(-5)*100)
		},
		tyde.NewShortcut("Increase Screen Brightness", tyde.KeyBrightnessUp, tyde.AnyModifier): func() {
			showOSD(wmtheme.BrightnessIcon, b.offsetValue(5)*100)
		},
	}
}

func (b *brightness) StatusAreaWidget() fyne.CanvasObject {
	if val, err := b.bright.Get(); err != nil || val == 0 {
		return nil
	}

	b.bar = newStatusBar()
	brightnessIcon := newScrollIcon(wmtheme.BrightnessIcon)
	brightnessIcon.scroll = func(f float32) {
		go b.offsetValue(int(f / 10))
	}

	prop := canvas.NewRectangle(color.Transparent)
	prop.SetMinSize(brightnessIcon.MinSize().Add(fyne.NewSize(theme.Padding()*4, 0)))
	icon := container.NewCenter(prop, brightnessIcon)

	less := &widget.Button{Icon: theme.ContentRemoveIcon(), Importance: widget.LowImportance, OnTapped: func() {
		go b.offsetValue(-5)
	}}

	more := &widget.Button{Icon: theme.ContentAddIcon(), Importance: widget.LowImportance, OnTapped: func() {
		go b.offsetValue(5)
	}}

	bright := container.NewBorder(nil, nil, less, more, b.bar)

	go func() { // shown, not written back
		if val, err := b.bright.Get(); err == nil {
			fyne.Do(func() { b.bar.SetValue(val) })
		}
	}()

	if wlipc.IsWaylandSession() {
		b.done = make(chan struct{})
		wlipc.WatchBrightnessEvent(func() {
			val, err := b.bright.Get()
			if err != nil {
				return
			}
			fyne.Do(func() {
				b.bar.SetValue(val)
			})
		}, b.done)
	}

	return container.New(&handleNarrow{}, icon, bright)
}

// newBrightness creates a new module that will show screen brightness in the status area
func newBrightness() tyde.Module {
	if wlipc.IsWaylandSession() {
		// Under Wayland, xbacklight doesn't work (no RandR backlight in XWayland)
		// and the compositor uses brightnessctl, so the panel must too.
		return &brightness{bright: &brightnessCtl{}}
	}
	return &brightness{bright: dryvers.NewBrightness()}
}

// brightnessCtl drives the backlight with brightnessctl.
type brightnessCtl struct{}

// Get reads the brightness in one call: "brightnessctl -m" prints
// device,class,current,percent,max.
func (brightnessCtl) Get() (float64, error) {
	out, err := wm.ExecOutput("brightnessctl", "-m")
	if err != nil {
		return 0, err
	}
	return parseBrightnessctlMachine(string(out))
}

// parseBrightnessctlMachine reads the first line of "brightnessctl -m".
func parseBrightnessctlMachine(out string) (float64, error) {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	f := strings.Split(line, ",")
	if len(f) < 5 {
		return 0, fmt.Errorf("unexpected brightnessctl output %q", line)
	}
	val, err := strconv.ParseFloat(f[2], 64)
	if err != nil {
		return 0, err
	}
	max, err := strconv.ParseFloat(f[4], 64)
	if err != nil || max <= 0 {
		return 0, errors.New("invalid maximum brightness")
	}
	return val / max, nil
}

func (brightnessCtl) Set(value float64) error {
	return wm.ExecRun("brightnessctl", "set", strconv.Itoa(int(value*100))+"%")
}

type brightItem struct {
	input string
	b     *brightness
}

func (i *brightItem) Icon() fyne.Resource {
	return wmtheme.BrightnessIcon
}

func (i *brightItem) Title() string {
	if _, err := strconv.Atoi(i.input); err == nil {
		return "Brightness " + i.input + "%"
	} else if startsWith(i.input, "d") {
		return "Brightness down"
	} else if startsWith(i.input, "u") {
		return "Brightness up"
	}

	return ""
}

func (i *brightItem) Launch() {
	if val, err := strconv.Atoi(i.input); err == nil {
		i.b.setValue(val)
	} else if startsWith(i.input, "d") {
		i.b.offsetValue(-5)
	} else if startsWith(i.input, "u") {
		i.b.offsetValue(5)
	}
}
