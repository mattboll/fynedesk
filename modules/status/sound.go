package status

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mafik/pulseaudio"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

var soundMeta = fynedesk.ModuleMetadata{
	Name:        "Sound",
	NewInstance: newSound,
}

type sound struct {
	bar    *statusBar
	client *pulseaudio.Client
	mute   *widget.Button
	done   chan struct{} // closed to stop IPC watcher goroutines
}

func newSound() fynedesk.Module {
	return &sound{}
}

func (b *sound) LaunchSuggestions(input string) []fynedesk.LaunchSuggestion {
	if _, err := b.value(); err != nil {
		return nil // don't load if not present
	}

	lower := strings.ToLower(input)
	matches := false
	val := lower
	if startsWith(lower, "volume ") {
		matches = true
		if len(lower) > 7 {
			val = lower[7:]
		} else {
			val = ""
		}
	} else if startsWith(lower, "vol ") {
		matches = true
		if len(lower) > 4 {
			val = lower[4:]
		} else {
			val = ""
		}
	} else if startsWith(lower, "mute") || startsWith(lower, "unmute") {
		matches = true
	}

	if matches {
		return []fynedesk.LaunchSuggestion{&volItem{input: val, s: b}}
	}

	return nil
}

func (b *sound) Shortcuts() map[*fynedesk.Shortcut]func() {
	return map[*fynedesk.Shortcut]func(){
		fynedesk.NewShortcut("Mute Sound", fynedesk.KeyVolumeMute, fynedesk.AnyModifier): func() {
			b.toggleMute()
			vol, err := b.value()
			if err == nil {
				icon := wmtheme.SoundIcon
				if b.muted() {
					icon = wmtheme.MuteIcon
				}
				showOSD(icon, float64(vol))
			}
		},
		fynedesk.NewShortcut("Reduce Sound Volume", fynedesk.KeyVolumeDown, fynedesk.AnyModifier): func() {
			b.offsetValue(-5)
			if vol, err := b.value(); err == nil {
				showOSD(wmtheme.SoundIcon, float64(vol))
			}
		},
		fynedesk.NewShortcut("Increase Sound Volume", fynedesk.KeyVolumeUp, fynedesk.AnyModifier): func() {
			b.offsetValue(5)
			if vol, err := b.value(); err == nil {
				showOSD(wmtheme.SoundIcon, float64(vol))
			}
		},
	}
}

// StatusAreaWidget builds the widget
func (b *sound) StatusAreaWidget() fyne.CanvasObject {
	if err := b.setup(); err != nil {
		fyne.LogError("Unable to start sound module", err)
		return nil
	}

	b.bar = newStatusBar()
	b.bar.Max = 100
	b.mute = &widget.Button{Icon: wmtheme.SoundIcon, Importance: widget.LowImportance, OnTapped: b.toggleMute}
	if b.muted() {
		b.mute.SetIcon(wmtheme.MuteIcon)
	}

	less := &widget.Button{Icon: theme.ContentRemoveIcon(), Importance: widget.LowImportance, OnTapped: func() {
		b.offsetValue(-5)
	}}

	more := &widget.Button{Icon: theme.ContentAddIcon(), Importance: widget.LowImportance, OnTapped: func() {
		b.offsetValue(5)
	}}

	sound := container.NewBorder(nil, nil, less, more, b.bar)

	go b.offsetValue(0)

	// Poll volume changes periodically to catch external changes
	// (e.g. volume keys handled by host compositor, wpctl, etc.)
	go b.watchVolume()

	// Also watch IPC volume events from compositor (more reliable than PulseAudio
	// protocol when compositor uses wpctl to change PipeWire volume)
	if wlipc.IsWaylandSession() {
		b.done = make(chan struct{})
		wlipc.WatchVolumeEvent(func() {
			vol, err := b.value()
			if err != nil {
				return
			}
			muted := b.muted()
			fyne.Do(func() {
				b.bar.SetValue(float64(vol))
				if muted {
					b.mute.SetIcon(wmtheme.MuteIcon)
				} else {
					b.mute.SetIcon(wmtheme.SoundIcon)
				}
			})
		}, b.done)
	}

	return container.New(&handleNarrow{}, b.mute, sound)
}

func (b *sound) watchVolume() {
	updates, err := b.client.Updates()
	if err != nil {
		fyne.LogError("Failed to subscribe to PulseAudio updates", err)
		return
	}

	for range updates {
		vol, err := b.value()
		if err != nil {
			continue
		}
		muted := b.muted()
		fyne.Do(func() {
			b.bar.SetValue(float64(vol))
			if muted {
				b.mute.SetIcon(wmtheme.MuteIcon)
			} else {
				b.mute.SetIcon(wmtheme.SoundIcon)
			}
		})
	}
}

// Metadata returns ModuleMetadata
func (b *sound) Metadata() fynedesk.ModuleMetadata {
	return soundMeta
}

func (b *sound) offsetValue(diff int) {
	currVal, err := b.value()
	if err != nil {
		fyne.LogError("Failed to get volume", err)
		return
	}
	value := currVal + diff

	if value < 0 {
		value = 0
	} else if value > 100 {
		value = 100
	}

	b.setValue(value)
}

type volItem struct {
	input string
	s     *sound
}

func (i *volItem) Icon() fyne.Resource {
	return wmtheme.SoundIcon
}

func (i *volItem) Title() string {
	if startsWith(i.input, "up") {
		return "Volume up"
	} else if startsWith(i.input, "down") {
		return "Volume down"
	} else if _, err := strconv.Atoi(i.input); err == nil {
		return "Volume " + i.input + "%"
	}

	if i.s.muted() {
		return "Unmute volume"
	}
	return "Mute volume"
}

func (i *volItem) Launch() {
	if startsWith(i.input, "up") {
		i.s.offsetValue(5)
	} else if startsWith(i.input, "down") {
		i.s.offsetValue(-5)
	} else if val, err := strconv.Atoi(i.input); err == nil {
		if val < 0 {
			val = 0
		} else if val > 100 {
			val = 100
		}
		i.s.setValue(val)
	} else {
		i.s.toggleMute()
	}
}

func startsWith(haystack, needle string) bool {
	return strings.HasPrefix(haystack, needle)
}
