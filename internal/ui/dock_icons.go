package ui

import (
	"log"
	"maps"
	"time"

	"fyne.io/fyne/v2"

	"fyshos.com/tyde/wlipc"
)

// The compositor flies a minimized window to its icon in the dock: the panel
// tells it where each window's icon is, when the dock changes.

// scheduleIconReport reports the icons a moment after the dock changed, once
// for a burst of changes.
func (b *bar) scheduleIconReport() {
	if !wlipc.IsWaylandSession() || b.outputOffsetX != 0 || b.outputOffsetY != 0 {
		return // the primary dock only
	}
	if b.iconReport != nil {
		b.iconReport.Stop()
	}
	b.iconReport = time.AfterFunc(300*time.Millisecond, func() { fyne.Do(b.reportIcons) })
}

// reportIcons sends the position of each window's icon, if it changed.
func (b *bar) reportIcons() {
	icons := b.windowIcons()
	if maps.Equal(icons, b.lastIcons) {
		return
	}
	b.lastIcons = icons
	go func() {
		if err := wlipc.ReportDockIcons(icons); err != nil {
			log.Println("[dock] icons:", err)
		}
	}()
}

// windowIcons returns the centre of the icon of each window, by window ID:
// its own icon, or the launcher of its application.
func (b *bar) windowIcons() map[string]wlipc.DockIcon {
	shown := map[fyne.CanvasObject]bool{}
	for _, c := range b.children {
		shown[c] = true
	}
	center := func(icon *barIcon) wlipc.DockIcon {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(icon)
		size := icon.Size()
		return wlipc.DockIconAbsolute(pos.X+size.Width/2, pos.Y+size.Height/2)
	}
	launchers := map[string]*barIcon{}
	for _, icon := range b.icons {
		if icon.appData != nil && icon.windowData == nil && shown[icon] {
			launchers[icon.appData.Name()] = icon
		}
	}
	icons := map[string]wlipc.DockIcon{}
	for _, icon := range b.icons {
		for _, w := range icon.allWindows() {
			win, ok := w.win.(*ipcWindow)
			if !ok {
				continue
			}
			switch {
			case shown[icon]:
				icons[win.id] = center(icon)
			case w.findApp() != nil && launchers[w.findApp().Name()] != nil:
				icons[win.id] = center(launchers[w.findApp().Name()])
			}
		}
	}
	return icons
}
