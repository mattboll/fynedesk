package status

import (
	"errors"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

var brightnessMeta = fynedesk.ModuleMetadata{
	Name:        "Brightness",
	NewInstance: newBrightness,
}

type brightType int

const (
	noBacklight brightType = iota
	xbacklight
	brightnessctl
)

// Brightness is a progress bar module to modify screen brightness
type brightness struct {
	bar *statusBar

	mode brightType
	done chan struct{} // closed to stop IPC watcher goroutines
}

func (b *brightness) Destroy() {
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
}

func (b *brightness) value() (float64, error) {
	switch b.mode {
	case brightnessctl:
		out, err := wm.ExecOutput("brightnessctl", "get")
		if err != nil {
			fyne.LogError("Error running brightnessctl", err)
			return 0, err
		}
		maxOut, _ := wm.ExecOutput("brightnessctl", "max")
		val, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		if err != nil {
			fyne.LogError("Error parsing brightnessctl info", err)
			return 0, err
		}
		max, _ := strconv.ParseFloat(strings.TrimSpace(string(maxOut)), 64)
		return val / max, nil
	default:
		out, err := wm.ExecOutput("xbacklight")
		if err != nil {
			fyne.LogError("Error running xbacklight", err)
			return 0, err
		}

		if strings.TrimSpace(string(out)) == "" {
			return 0, errors.New("no back-lit screens found")
		}
		ret, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		if err != nil {
			fyne.LogError("Error parsing xbacklight info", err)
			return 0, err
		}
		return ret / 100, nil
	}
}

func (b *brightness) offsetValue(diff int) {
	floatVal, _ := b.value()
	if floatVal <= 0.01 { // don't start doing 6, 11 etc just because we were on 1 (min)
		floatVal = 0
	}
	value := int(floatVal*100) + diff

	b.setValue(value)
}

func (b *brightness) setValue(value int) {
	if value < 1 {
		value = 1
	} else if value > 100 {
		value = 100
	}

	switch b.mode {
	case brightnessctl:
		err := wm.ExecRun("brightnessctl", "set", strconv.Itoa(value)+"%")
		if err != nil {
			fyne.LogError("Error running brightnessctl", err)
			return
		}
	default:
		err := wm.ExecRun("xbacklight", "-set", strconv.Itoa(value))
		if err != nil {
			fyne.LogError("Error running xbacklight", err)
			return
		}
	}

	newVal, _ := b.value()
	fyne.Do(func() {
		b.bar.SetValue(newVal)
	})
}

func (b *brightness) LaunchSuggestions(input string) []fynedesk.LaunchSuggestion {
	if b.mode == noBacklight {
		return nil // don't load if not present
	}

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

	if matches {
		return []fynedesk.LaunchSuggestion{&brightItem{input: val, b: b}}
	}

	return nil
}

func (b *brightness) Metadata() fynedesk.ModuleMetadata {
	return brightnessMeta
}

func (b *brightness) Shortcuts() map[*fynedesk.Shortcut]func() {
	return map[*fynedesk.Shortcut]func(){
		fynedesk.NewShortcut("Increase Screen Brightness", fynedesk.KeyBrightnessDown, fynedesk.AnyModifier): func() {
			b.offsetValue(-5)
			if val, err := b.value(); err == nil {
				showOSD(wmtheme.BrightnessIcon, val*100)
			}
		},
		fynedesk.NewShortcut("Reduce Screen Brightness", fynedesk.KeyBrightnessUp, fynedesk.AnyModifier): func() {
			b.offsetValue(5)
			if val, err := b.value(); err == nil {
				showOSD(wmtheme.BrightnessIcon, val*100)
			}
		},
	}
}

func (b *brightness) StatusAreaWidget() fyne.CanvasObject {
	if b.mode == noBacklight {
		return nil
	}

	b.bar = newStatusBar()
	brightnessIcon := widget.NewIcon(wmtheme.BrightnessIcon)
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

	go b.offsetValue(0)

	if wlipc.IsWaylandSession() {
		b.done = make(chan struct{})
		wlipc.WatchBrightnessEvent(func() {
			val, err := b.value()
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
func newBrightness() fynedesk.Module {
	mode := noBacklight

	if wlipc.IsWaylandSession() {
		// Under Wayland, xbacklight doesn't work (no RandR backlight in XWayland).
		// The compositor uses brightnessctl, so the panel must too.
		// Use "brightnessctl get" (not bare "brightnessctl") as some versions
		// return non-zero with no subcommand even when a device exists.
		if wm.ExecRun("brightnessctl", "get") == nil {
			mode = brightnessctl
		}
	} else {
		mode = xbacklight
		err := wm.ExecRun("xbacklight")
		if err != nil {
			if wm.ExecRun("brightnessctl", "get") == nil {
				mode = brightnessctl
			} else {
				fyne.LogError("Could not launch xbacklight or brightnessctl", err)
			}
		}
	}

	return &brightness{mode: mode}
}

type brightItem struct {
	input string
	b     *brightness
}

func (i *brightItem) Icon() fyne.Resource {
	return wmtheme.BrightnessIcon
}

func (i *brightItem) Title() string {
	if startsWith(i.input, "down") {
		return "Brightness down"
	} else if _, err := strconv.Atoi(i.input); err == nil {
		return "Brightness " + i.input + "%"
	}

	return "Brightness up"
}

func (i *brightItem) Launch() {
	if startsWith(i.input, "down") {
		i.b.offsetValue(-5)
	} else if val, err := strconv.Atoi(i.input); err == nil {
		i.b.setValue(val)
	} else {
		i.b.offsetValue(5)
	}
}
