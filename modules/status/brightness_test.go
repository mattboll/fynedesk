package status

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBrightItem_Title(t *testing.T) {
	i := &brightItem{input: "50"}
	assert.Equal(t, "Brightness 50%", i.Title())

	i = &brightItem{input: "down"}
	assert.Equal(t, "Brightness down", i.Title())

	i = &brightItem{input: "up"}
	assert.Equal(t, "Brightness up", i.Title())

	i = &brightItem{input: ""}
	assert.Equal(t, "", i.Title())
}

func TestBrightItem_Icon(t *testing.T) {
	i := &brightItem{input: "50"}
	assert.NotNil(t, i.Icon())
}

func TestParseBrightnessctlMachine(t *testing.T) {
	v, err := parseBrightnessctlMachine("intel_backlight,backlight,12000,50%,24000\n")
	if err != nil || v != 0.5 {
		t.Errorf("got %v, %v", v, err)
	}
	if _, err := parseBrightnessctlMachine("garbage"); err == nil {
		t.Error("garbage parsed")
	}
}
