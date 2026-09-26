//go:build !openbsd && !freebsd && !netbsd
// +build !openbsd,!freebsd,!netbsd

package status

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"

	"github.com/mafik/pulseaudio"
)

// Destroy tidies up resources. Closing the client also closes its Updates()
// channel, which is what unblocks watchVolume's `for range updates` loop.
func (b *sound) Destroy() {
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
	if b.client == nil {
		return
	}
	b.client.Close()
	b.client = nil
}

func (b *sound) muted() bool {
	m, _ := b.client.Mute()
	return m
}

func (b *sound) value() (int, error) {
	volume, err := b.client.Volume()
	if err != nil {
		return 0, err
	}

	// Rounded: 0.29999 is 30 %, and writing back a truncated value lowered
	// the volume by 1 % each time.
	return int(math.Round(float64(volume) * 100)), nil
}

// pulseAddress returns the PulseAudio (or pipewire-pulse) socket of the
// session, which the client library would otherwise always look for under
// /run/user/<uid> whatever the runtime directory is.
func pulseAddress() []string {
	if server := os.Getenv("PULSE_SERVER"); strings.HasPrefix(server, "unix:") {
		return []string{strings.TrimPrefix(server, "unix:")}
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return []string{filepath.Join(dir, "pulse", "native")}
	}
	return nil // the library default
}

func (b *sound) setup() error {
	client, err := pulseaudio.NewClient(pulseAddress()...)
	if err != nil {
		return err
	}
	b.client = client
	return nil
}

func (b *sound) setValue(vol int) {
	if err := b.client.SetVolume(float32(vol) / 100); err != nil {
		fyne.LogError("Failed to set volume", err)
		return
	}

	muted := b.muted()
	fyne.Do(func() {
		b.updateIcon(vol, muted)
		b.bar.SetValue(float64(vol))
	})
}

func (b *sound) toggleMute() {
	toggle, err := b.client.ToggleMute()
	if err != nil {
		fyne.LogError("toggleMute() failed", err)
		return
	}

	val, _ := b.value()
	fyne.Do(func() { b.updateIcon(val, toggle) })
}
