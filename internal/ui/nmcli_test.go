package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTerseLine(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{"simple", "MyWifi:85:WPA2:*", []string{"MyWifi", "85", "WPA2", "*"}},
		{"open network", "FreeWifi:42::", []string{"FreeWifi", "42", "", ""}},
		{"escaped colon in SSID", `My\:Wifi:70:WPA2:`, []string{"My:Wifi", "70", "WPA2", ""}},
		{"empty SSID", ":30:WPA2:", []string{"", "30", "WPA2", ""}},
		{"single field", "OnlySSID", []string{"OnlySSID"}},
		{"empty line", "", []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTerseLine(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}
