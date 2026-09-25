package status

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
)

// fakePower stands in for the machine's battery: tests must not depend on
// it, nor hibernate it.
type fakePower struct {
	level   float64
	plugged bool
}

func (f *fakePower) Get() (float64, error)    { return f.level, nil }
func (f *fakePower) PluggedIn() (bool, error) { return f.plugged, nil }

func newTestBattery(t *testing.T, power *fakePower) (*battery, fyne.Window, *[]float64) {
	var hibernated []float64
	b := &battery{battery: power, hibernate: func(val float64) { hibernated = append(hibernated, val) }}
	wid := b.StatusAreaWidget()
	t.Cleanup(b.Destroy)
	w := test.NewWindow(wid)
	w.Resize(fyne.NewSize(103, 44))
	return b, w, &hibernated
}

func TestBattery_Render(t *testing.T) {
	b, w, _ := newTestBattery(t, &fakePower{level: 1, plugged: true})

	b.setValue(1)
	test.AssertImageMatches(t, "battery_full.png", w.Canvas().Capture())

	b.setValue(0.5)
	test.AssertImageMatches(t, "battery_50.png", w.Canvas().Capture())

	b.setValue(0.25)
	test.AssertImageMatches(t, "battery_25.png", w.Canvas().Capture())
}

func TestBattery_Render_Unplugged(t *testing.T) {
	b, w, _ := newTestBattery(t, &fakePower{level: 0.5})

	b.setValue(0.5)
	test.AssertImageMatches(t, "battery_unplugged_50.png", w.Canvas().Capture())
}

func TestBattery_Render_LowWarning(t *testing.T) {
	b, w, hibernated := newTestBattery(t, &fakePower{level: 0.09})

	b.setValue(0.09)
	test.AssertImageMatches(t, "battery_low.png", w.Canvas().Capture())
	assert.Empty(t, *hibernated, "9% warns, it does not hibernate yet")
}

func TestBattery_CriticalHibernatesOnce(t *testing.T) {
	b, _, hibernated := newTestBattery(t, &fakePower{level: 0.3})

	b.setValue(0.04)
	b.setValue(0.03)
	assert.Equal(t, []float64{0.04}, *hibernated)

	// Plugged in, then unplugged again: it may hibernate again.
	b.battery = &fakePower{level: 0.03, plugged: true}
	b.setValue(0.03)
	b.battery = &fakePower{level: 0.03}
	b.setValue(0.03)
	assert.Len(t, *hibernated, 2)
}
