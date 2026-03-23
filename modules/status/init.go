// Package status implements status area modules including battery, sound, brightness, network, and keyboard indicators.
package status

import "fyshos.com/fynedesk"

func init() {
	// system area (bottom of widget panel) - order is top to bottom
	fynedesk.RegisterModule(keyboardMeta)
	fynedesk.RegisterModule(networkMeta)
	fynedesk.RegisterModule(batteryMeta)
	fynedesk.RegisterModule(soundMeta)
	fynedesk.RegisterModule(brightnessMeta)
	fynedesk.RegisterModule(powerProfileMeta)
}
