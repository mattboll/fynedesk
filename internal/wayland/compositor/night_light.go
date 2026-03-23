package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_gamma_control_v1.h>

// Get the gamma LUT size supported by this output.
static size_t output_gamma_size(struct wlr_output *output) {
	return wlr_output_get_gamma_size(output);
}

// Apply a gamma LUT to the output.
static bool output_set_gamma(struct wlr_output *output,
		size_t size, const uint16_t *r, const uint16_t *g, const uint16_t *b) {
	struct wlr_output_state state;
	wlr_output_state_init(&state);
	wlr_output_state_set_gamma_lut(&state, size, r, g, b);
	bool ok = wlr_output_commit_state(output, &state);
	wlr_output_state_finish(&state);
	return ok;
}
*/
import "C"

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"unsafe"
)

// nightLightState tracks the current night light configuration.
type nightLightState struct {
	enabled     bool
	temperature int // Kelvin (2700-6500)
}

// applyNightLight applies gamma correction to all outputs based on the
// current night light settings.
func (s *server) applyNightLight() {
	for _, out := range s.outputs {
		s.applyNightLightToOutput(out)
	}
}

// applyNightLightToOutput applies the night light gamma to a single output.
func (s *server) applyNightLightToOutput(out *outputState) {
	cOutput := outputPtr(out.output)
	gammaSize := int(C.output_gamma_size(cOutput))
	if gammaSize == 0 {
		log.Printf("[nightlight] output %s: gamma LUT not supported\n", out.output.Name())
		return
	}

	r := make([]C.uint16_t, gammaSize)
	g := make([]C.uint16_t, gammaSize)
	b := make([]C.uint16_t, gammaSize)

	if s.nightLight.enabled {
		rF, gF, bF := colorTempToRGB(s.nightLight.temperature)
		for i := 0; i < gammaSize; i++ {
			val := float64(i) / float64(gammaSize-1) * 65535.0
			r[i] = C.uint16_t(val * rF)
			g[i] = C.uint16_t(val * gF)
			b[i] = C.uint16_t(val * bF)
		}
	} else {
		// Reset to identity (linear) gamma
		for i := 0; i < gammaSize; i++ {
			val := C.uint16_t(float64(i) / float64(gammaSize-1) * 65535.0)
			r[i] = val
			g[i] = val
			b[i] = val
		}
	}

	ok := C.output_set_gamma(cOutput, C.size_t(gammaSize),
		(*C.uint16_t)(unsafe.Pointer(&r[0])),
		(*C.uint16_t)(unsafe.Pointer(&g[0])),
		(*C.uint16_t)(unsafe.Pointer(&b[0])))
	if !bool(ok) {
		log.Printf("[nightlight] failed to set gamma on output %s\n", out.output.Name())
	}
}

// toggleNightLight flips the night light on/off, applies gamma, and persists
// the setting via Fyne preferences so the panel stays in sync.
func (s *server) toggleNightLight() {
	s.nightLight.enabled = !s.nightLight.enabled
	s.applyNightLight()
	if s.nightLight.enabled {
		log.Printf("[nightlight] enabled (%dK)\n", s.nightLight.temperature)
	} else {
		log.Println("[nightlight] disabled")
	}
	s.persistNightLight()
}

// persistNightLight writes the current night light state to Fyne preferences JSON
// so the panel sidebar checkbox stays in sync and the setting survives restarts.
func (s *server) persistNightLight() {
	home := os.Getenv("HOME")
	prefsPath := filepath.Join(home, ".config", "fyne", "com.fyshos.fynedesk", "preferences.json")

	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return
	}
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return
	}

	prefs["nightlightenabled"] = s.nightLight.enabled
	prefs["nightlighttemperature"] = float64(s.nightLight.temperature)

	out, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return
	}
	atomicWriteFile(prefsPath, out)
}

// colorTempToRGB converts a color temperature in Kelvin to RGB multipliers (0.0-1.0).
// Based on Tanner Helland's algorithm (used by redshift/gammastep).
// Valid range: 1000K - 10000K.
func colorTempToRGB(kelvin int) (r, g, b float64) {
	temp := float64(kelvin) / 100.0

	// Red
	if temp <= 66 {
		r = 1.0
	} else {
		r = 329.698727446 * math.Pow(temp-60, -0.1332047592) / 255.0
	}

	// Green
	if temp <= 66 {
		g = (99.4708025861*math.Log(temp) - 161.1195681661) / 255.0
	} else {
		g = 288.1221695283 * math.Pow(temp-60, -0.0755148492) / 255.0
	}

	// Blue
	if temp >= 66 {
		b = 1.0
	} else if temp <= 19 {
		b = 0.0
	} else {
		b = (138.5177312231*math.Log(temp-10) - 305.0447927307) / 255.0
	}

	// Clamp
	r = math.Max(0, math.Min(1, r))
	g = math.Max(0, math.Min(1, g))
	b = math.Max(0, math.Min(1, b))
	return
}
