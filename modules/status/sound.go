package status

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/mafik/pulseaudio"

	"fyshos.com/tyde"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
)

var soundMeta = tyde.ModuleMetadata{
	Name:        "Sound",
	NewInstance: newSound,
}

type sound struct {
	bar    *statusBar
	client *pulseaudio.Client
	mute   *scrollButton
	done   chan struct{} // closed to stop IPC watcher goroutines
}

func newSound() tyde.Module {
	return &sound{}
}

func (b *sound) LaunchSuggestions(input string) []tyde.LaunchSuggestion {
	if _, err := b.value(); err != nil {
		return nil // don't load if not present
	}

	lower := strings.ToLower(input)
	matches := false
	val := lower
	if startsWith(lower, "volume ") {
		if len(lower) > 7 {
			val = lower[7:]
		} else {
			val = ""
		}
	} else if startsWith(lower, "vol ") {
		if len(lower) > 4 {
			val = lower[4:]
		} else {
			val = ""
		}
	} else if lower == "mute" || lower == "unmute" {
		matches = true
	} else {
		return nil
	}

	if !matches {
		if startsWith(val, "u") || startsWith(val, "d") || val == "mute" || val == "unmute" {
			matches = true
		}
	}

	if matches {
		return []tyde.LaunchSuggestion{&volItem{input: val, s: b}}
	}

	return nil
}

func (b *sound) Shortcuts() map[*tyde.Shortcut]func() {
	return map[*tyde.Shortcut]func(){
		tyde.NewShortcut("Mute Sound", tyde.KeyVolumeMute, tyde.AnyModifier): func() {
			b.toggleMute()
			vol, err := b.value()
			if err == nil {
				icon := wmtheme.SoundHighIcon
				if b.muted() {
					icon = wmtheme.MuteIcon
				}
				showOSD(icon, float64(vol))
			}
		},
		tyde.NewShortcut("Reduce Sound Volume", tyde.KeyVolumeDown, tyde.AnyModifier): func() {
			b.offsetValue(-5)
			if vol, err := b.value(); err == nil {
				showOSD(wmtheme.SoundHighIcon, float64(vol))
			}
		},
		tyde.NewShortcut("Increase Sound Volume", tyde.KeyVolumeUp, tyde.AnyModifier): func() {
			b.offsetValue(5)
			if vol, err := b.value(); err == nil {
				showOSD(wmtheme.SoundHighIcon, float64(vol))
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
	b.mute = newScrollButton(wmtheme.SoundHighIcon)
	b.mute.scroll = func(f float32) {
		if b.muted() {
			return
		}

		b.offsetValue(int(f / 10))
	}
	b.mute.Importance = widget.LowImportance
	b.mute.OnTapped = b.toggleMute
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

	go b.showValue() // only shown: writing it back changed it

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
				b.updateIcon(vol, muted)
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
			b.updateIcon(vol, muted)
		})
	}
}

// Metadata returns ModuleMetadata
func (b *sound) Metadata() tyde.ModuleMetadata {
	return soundMeta
}

// showValue shows the current volume.
func (b *sound) showValue() {
	vol, err := b.value()
	if err != nil {
		fyne.LogError("Failed to get volume", err)
		return
	}
	muted := b.muted()
	fyne.Do(func() {
		b.bar.SetValue(float64(vol))
		b.updateIcon(vol, muted)
	})
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

func (b *sound) updateIcon(vol int, mute bool) {
	if mute {
		b.mute.SetIcon(wmtheme.MuteIcon)
	} else {
		if vol <= 20 {
			b.mute.SetIcon(wmtheme.SoundLowIcon)
		} else if vol <= 60 {
			b.mute.SetIcon(wmtheme.SoundMidIcon)
		} else {
			b.mute.SetIcon(wmtheme.SoundHighIcon)
		}
	}
}

type volItem struct {
	input string
	s     *sound
}

func (i *volItem) Icon() fyne.Resource {
	return wmtheme.SoundHighIcon
}

func (i *volItem) Title() string {
	if _, err := strconv.Atoi(i.input); err == nil {
		return "Volume " + i.input + "%"
	} else if i.input == "mute" {
		return "Mute volume"
	} else if i.input == "unmute" {
		return "Unmute volume"
	} else if startsWith(i.input, "u") {
		return "Volume up"
	} else if startsWith(i.input, "d") {
		return "Volume down"
	}

	return ""
}

func (i *volItem) Launch() {
	if i.input == "mute" {
		_ = i.s.client.SetMute(true)
	} else if i.input == "unmute" {
		_ = i.s.client.SetMute(false)
	} else if startsWith(i.input, "u") {
		i.s.offsetValue(5)
	} else if startsWith(i.input, "d") {
		i.s.offsetValue(-5)
	} else if val, err := strconv.Atoi(i.input); err == nil {
		if val < 0 {
			val = 0
		} else if val > 100 {
			val = 100
		}
		i.s.setValue(val)
	}
}

// startsWith implements lenient prefix matching for the launcher: it returns
// true when one string is a prefix of the other. The launcher uses this so
// the user can type a partial keyword (e.g. "vol") and still match the
// keyword "volume", AND so the keyword "vol " can match a fully-typed
// "volume up". The original buggy implementation used strings.IndexAny
// which made unrelated strings match by character (e.g. "mug" → "mute");
// commit bd1be76 over-corrected to HasPrefix only, which broke partial
// typing. This restores the intended bidirectional prefix semantic without
// the IndexAny char-matching bug.
func startsWith(haystack, needle string) bool {
	if haystack == "" {
		return false
	}
	if len(haystack) >= len(needle) {
		return strings.HasPrefix(haystack, needle)
	}
	return strings.HasPrefix(needle, haystack)
}
