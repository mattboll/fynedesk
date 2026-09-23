package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Settings panel icons that Fyne and the Tyde theme lack, from Material Icons
// (Apache License 2.0) like the rest of the icon set. Themed, so they follow
// the foreground colour.
var (
	// dockIcon is Material "call_to_action": a screen with a bar along its bottom.
	dockIcon = theme.NewThemedResource(&fyne.StaticResource{
		StaticName: "dock.svg",
		StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
			`<path d="M21 3H3c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h18c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm0 16H3v-3h18v3z"/></svg>`),
	})

	// windowRulesIcon is Material "rule": checked and crossed lines.
	windowRulesIcon = theme.NewThemedResource(&fyne.StaticResource{
		StaticName: "rule.svg",
		StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
			`<path d="M16.54 11L13 7.46l1.41-1.41 2.12 2.12 4.24-4.24 1.41 1.41L16.54 11zM11 7H2v2h9V7zm10 ` +
			`6.41L19.59 12 17 14.59 14.41 12 13 13.41 15.59 16 13 18.59 14.41 20 17 17.41 19.59 20 21 ` +
			`18.59 18.41 16 21 13.41zM11 15H2v2h9v-2z"/></svg>`),
	})

	// tuneIcon is Material "tune": three sliders.
	tuneIcon = theme.NewThemedResource(&fyne.StaticResource{
		StaticName: "tune.svg",
		StaticContent: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
			`<path d="M3 17v2h6v-2H3zM3 5v2h10V5H3zm10 16v-2h8v-2h-8v-2h-2v6h2zM7 9v2H3v2h4v2h2V9H7zm14 ` +
			`4v-2H11v2h10zm-6-4h2V7h4V5h-4V3h-2v6z"/></svg>`),
	})
)
