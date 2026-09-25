// Package status implements status area modules including battery, sound, brightness, network, and keyboard indicators.
package status

import "fyshos.com/tyde"

func init() {
	// system area (bottom of widget panel) - order is top to bottom
	tyde.RegisterModule(agendaMeta)
	tyde.RegisterModule(calendarMeta)
	tyde.RegisterModule(keyboardMeta)
	tyde.RegisterModule(networkMeta)
	tyde.RegisterModule(phoneMeta)
	tyde.RegisterModule(batteryMeta)
	tyde.RegisterModule(soundMeta)
	tyde.RegisterModule(brightnessMeta)
	tyde.RegisterModule(powerProfileMeta)
}
