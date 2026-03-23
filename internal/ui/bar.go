package ui

import (
	"image/color"
	"time"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmTheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wm"
)

// bar is the main widget housing app icons and taskbar area
type bar struct {
	widget.BaseWidget

	desk          fynedesk.Desktop    // The desktop instance we are holding icons for
	children      []fyne.CanvasObject // Icons that are laid out by the bar
	mouseInside   bool                // Is the mouse inside of the bar?
	mousePosition fyne.Position       // The current coordinates of the mouse cursor

	iconSize         float32
	iconScale        float32
	disableTaskbar   bool
	disableZoom      bool
	icons            []*barIcon
	separator        *canvas.Rectangle
	lastMouseRefresh time.Time // debounce mouse-driven refreshes

	// Output offset for multi-monitor support.
	// For the primary bar these are (0,0); for secondary bars they hold the
	// output's position and dimensions so tooltips/previews appear on the
	// correct monitor.
	outputOffsetX, outputOffsetY float32
	outputW, outputH             float32
}

// MouseIn alerts the widget that the mouse has entered
func (b *bar) MouseIn(event *deskDriver.MouseEvent) {
	b.mouseInside = true
	b.updateHoveredIcon(event.Position)
	if b.desk.Settings().LauncherDisableZoom() {
		return
	}
	b.mousePosition = event.Position
	b.Refresh()
}

// MouseOut alerts the widget that the mouse has left
func (b *bar) MouseOut() {
	b.mouseInside = false
	for _, icon := range b.icons {
		if icon.hovered {
			icon.hovered = false
			icon.Refresh()
		}
	}
	iconMouseOut()
	if b.desk.Settings().LauncherDisableZoom() {
		return
	}
	b.Refresh()
}

// MouseMoved alerts the widget that the mouse has changed position
func (b *bar) MouseMoved(event *deskDriver.MouseEvent) {
	fynedesk.Instance().DelayScreenSaver()
	b.mousePosition = event.Position

	// Check which icon is under the cursor for preview
	b.updateHoveredIcon(event.Position)

	if b.desk.Settings().LauncherDisableZoom() {
		return
	}
	// Debounce: refresh at most ~30 Hz to avoid burning CPU on every mouse event
	now := time.Now()
	if now.Sub(b.lastMouseRefresh) < 33*time.Millisecond {
		return
	}
	b.lastMouseRefresh = now
	b.Refresh()
}

// updateHoveredIcon checks which icon is under the cursor and updates hover state + preview.
func (b *bar) updateHoveredIcon(pos fyne.Position) {
	var found *barIcon
	for _, icon := range b.icons {
		iconPos := icon.Position()
		iconSize := icon.Size()
		if pos.X >= iconPos.X && pos.X <= iconPos.X+iconSize.Width &&
			pos.Y >= iconPos.Y && pos.Y <= iconPos.Y+iconSize.Height {
			found = icon
			break
		}
	}

	for _, icon := range b.icons {
		wasHovered := icon.hovered
		icon.hovered = (icon == found)
		if icon.hovered != wasHovered {
			icon.Refresh()
		}
	}

	oi := outputInfo{offsetX: b.outputOffsetX, offsetY: b.outputOffsetY, width: b.outputW, height: b.outputH}
	if found != nil && found.windowData != nil {
		iconMouseIn(found, oi)
		iconTooltipOut()
	} else if found != nil && found.windowData == nil && found.isRunning && found.appData != nil {
		// Pinned icon with a running window: find the matching taskbar icon
		// and show its preview instead of just a text tooltip.
		var matched *barIcon
		for _, ic := range b.icons {
			if ic.windowData == nil {
				continue
			}
			app := ic.windowData.findApp()
			if app != nil && app.Name() == found.appData.Name() {
				matched = ic
				break
			}
		}
		if matched != nil {
			iconMouseIn(matched, oi)
			iconTooltipOut()
		} else {
			iconMouseOut()
			iconTooltipIn(found, found.appData.Name(), oi)
		}
	} else if found != nil && found.windowData == nil {
		iconMouseOut()
		// Show text tooltip for pinned app icons and the search icon
		name := ""
		if found.appData != nil {
			name = found.appData.Name()
		} else if found.resource != nil {
			name = locale.T("menu.search")
		}
		if name != "" {
			iconTooltipIn(found, name, oi)
		}
	} else {
		iconMouseOut()
		iconTooltipOut()
	}
}

// append adds an object to the end of the widget
func (b *bar) append(object fyne.CanvasObject) {
	b.children = append(b.children, object)

	b.Refresh()
}

// appendSeparator adds a separator between the default icons and the taskbar
func (b *bar) appendSeparator() {
	b.separator = canvas.NewRectangle(theme.Color(theme.ColorNameForeground))
	b.append(b.separator)
}

// removeFromTaskbar removes an object from the taskbar area of the widget
func (b *bar) removeFromTaskbar(object fyne.CanvasObject) {
	for i, icon := range b.children {
		if icon != object {
			continue
		}

		b.children = append(b.children[:i], b.children[i+1:]...)
		break
	}

	b.Refresh()
}

func (b *bar) newAppIcon(data appie.AppData) *barIcon {
	iconRes := b.appIcon(data)
	icon := newBarIcon(iconRes, data, nil)

	icon.onTapped = func() {
		icon.launching = true
		icon.Refresh()
		// Clear launching state after 5s (window should appear by then)
		time.AfterFunc(5*time.Second, func() {
			fyne.Do(func() {
				icon.launching = false
				icon.Refresh()
			})
		})
		err := b.desk.RunApp(data)
		if err != nil {
			fyne.LogError("Failed to start app", err)
			icon.launching = false
			icon.Refresh()
			wm.SendNotification(wm.NewNotification(locale.T("launcher.failed"), locale.T("launcher.startFailed")+" "+data.Name()))
		}
	}

	return icon
}

func (b *bar) newTaskIcon(win *appWindow) *barIcon {
	iconRes := b.winIcon(win)
	return newBarIcon(iconRes, nil, win)
}

func (b *bar) createIcon(data appie.AppData, win fynedesk.Window) *barIcon {
	if data == nil && win == nil {
		return nil
	}

	var icon *barIcon
	if win == nil {
		icon = b.newAppIcon(data)
		icon.bar = b // allow drag reorder for pinned icons
	} else {
		icon = b.newTaskIcon(&appWindow{win: win, bar: b})
	}

	b.icons = append(b.icons, icon)
	return icon
}

// finishIconDrag determines the new position for a dragged pinned icon and saves the reordered list
func (b *bar) finishIconDrag(draggedIcon *barIcon) {
	defer b.Refresh()

	// Collect pinned icons (app icons without window data)
	var pinnedIcons []*barIcon
	for _, icon := range b.icons {
		if icon.appData != nil && icon.windowData == nil {
			pinnedIcons = append(pinnedIcons, icon)
		}
	}

	dragIdx := -1
	for i, icon := range pinnedIcons {
		if icon == draggedIcon {
			dragIdx = i
			break
		}
	}
	if dragIdx < 0 {
		return
	}

	// Determine target position based on dragged icon's pixel position
	narrow := b.desk.Settings().NarrowLeftLauncher()
	targetIdx := dragIdx
	dragPos := draggedIcon.Position()
	for i, icon := range pinnedIcons {
		if icon == draggedIcon {
			continue
		}
		iconPos := icon.Position()
		iconSize := icon.Size()
		var iconCenter, dragCenter float32
		if narrow {
			iconCenter = iconPos.Y + iconSize.Height/2
			dragCenter = dragPos.Y + draggedIcon.Size().Height/2
		} else {
			iconCenter = iconPos.X + iconSize.Width/2
			dragCenter = dragPos.X + draggedIcon.Size().Width/2
		}
		if dragIdx > i && dragCenter < iconCenter {
			targetIdx = i
			break
		}
		if dragIdx < i && dragCenter > iconCenter {
			targetIdx = i
		}
	}

	if targetIdx != dragIdx {
		icons := b.desk.Settings().LauncherIcons()
		if dragIdx < len(icons) && targetIdx < len(icons) {
			moved := icons[dragIdx]
			icons = append(icons[:dragIdx], icons[dragIdx+1:]...)
			newIcons := make([]string, 0, len(icons)+1)
			newIcons = append(newIcons, icons[:targetIdx]...)
			newIcons = append(newIcons, moved)
			newIcons = append(newIcons, icons[targetIdx:]...)
			b.desk.Settings().(*deskSettings).setLauncherIcons(newIcons)
		}
	}
}

func (b *bar) taskbarIconTapped(icon *barIcon) {
	if icon.windowData == nil {
		return
	}

	// Single window — original behavior
	if len(icon.groupWindows) == 0 {
		win := icon.windowData.win
		if win.Desktop() != fynedesk.Instance().Desktop() {
			b.desk.SetDesktop(win.Desktop())
			return
		}
		if !win.Iconic() && win.TopWindow() {
			win.Iconify()
			return
		}
		if win.Iconic() {
			win.Uniconify()
		}
		win.RaiseToTop()
		win.Focus()
		return
	}

	// Multiple windows — round-robin cycle on each click
	allWins := icon.allWindows()
	idx := icon.lastCycleIndex % len(allWins)
	target := allWins[idx]
	icon.lastCycleIndex = (idx + 1) % len(allWins)

	if target.win.Desktop() != fynedesk.Instance().Desktop() {
		b.desk.SetDesktop(target.win.Desktop())
	}
	if target.win.Iconic() {
		target.win.Uniconify()
	}
	target.win.RaiseToTop()
	target.win.Focus()
}

func (b *bar) WindowAdded(win fynedesk.Window) {
	if win.Properties().SkipTaskbar() || b.desk.Settings().LauncherDisableTaskbar() {
		return
	}

	// Try to group with an existing taskbar icon for the same app
	newWin := &appWindow{win: win, bar: b}
	newApp := newWin.findApp()
	if newApp != nil {
		for _, ic := range b.icons {
			if ic.windowData == nil {
				continue
			}
			existingApp := ic.windowData.findApp()
			if existingApp != nil && existingApp.Name() == newApp.Name() {
				ic.groupWindows = append(ic.groupWindows, newWin)
				ic.Refresh()
				b.updateRunningState()
				return
			}
		}
	}

	icon := b.createIcon(nil, win)
	if icon != nil {
		icon.onTapped = func() {
			b.taskbarIconTapped(icon)
		}
		b.append(icon)
	}
	b.updateRunningState()
}

func (b *bar) WindowMoved(_ fynedesk.Window) {}

func (b *bar) WindowOrderChanged() {}

func (b *bar) WindowRemoved(win fynedesk.Window) {
	if win.Properties().SkipTaskbar() || b.desk.Settings().LauncherDisableTaskbar() {
		return
	}
	for i, ic := range b.icons {
		if ic.windowData == nil {
			continue
		}

		// Check primary window
		if win == ic.windowData.win {
			if len(ic.groupWindows) > 0 {
				// Promote next grouped window to primary
				ic.windowData = ic.groupWindows[0]
				ic.resource = b.winIcon(ic.windowData)
				ic.groupWindows = ic.groupWindows[1:]
				ic.Refresh()
			} else {
				b.removeFromTaskbar(ic)
				b.icons = append(b.icons[:i], b.icons[i+1:]...)
			}
			b.updateRunningState()
			return
		}

		// Check grouped windows
		for j, gw := range ic.groupWindows {
			if win == gw.win {
				ic.groupWindows = append(ic.groupWindows[:j], ic.groupWindows[j+1:]...)
				ic.Refresh()
				b.updateRunningState()
				return
			}
		}
	}
	b.updateRunningState()
}

func (b *bar) WindowStateChanged(win fynedesk.Window) {
	for _, ic := range b.icons {
		if ic.windowData == nil {
			continue
		}
		if win == ic.windowData.win {
			ic.Refresh()
			return
		}
		for _, gw := range ic.groupWindows {
			if win == gw.win {
				ic.Refresh()
				return
			}
		}
	}
}

// updateRunningState checks launcher icons against open windows and sets isRunning.
func (b *bar) updateRunningState() {
	for _, icon := range b.icons {
		if icon.appData == nil || icon.windowData != nil {
			continue
		}
		running := false
		for _, other := range b.icons {
			if other.windowData == nil {
				continue
			}
			app := other.windowData.findApp()
			if app != nil && app.Name() == icon.appData.Name() {
				running = true
				break
			}
		}
		if icon.isRunning != running {
			icon.isRunning = running
			if running {
				icon.launching = false // app window appeared
			}
			icon.Refresh()
		}
	}
}

func (b *bar) updateTaskbar() {
	disableTaskbar := b.desk.Settings().LauncherDisableTaskbar()
	if disableTaskbar == b.disableTaskbar {
		return
	}
	b.disableTaskbar = disableTaskbar
	if disableTaskbar {
		return
	}
	b.appendSeparator()

	for _, win := range b.desk.WindowManager().Windows() {
		b.WindowAdded(win)
	}
}

func (b *bar) updateIconOrder() {
	var index = 0
	for i, obj := range b.children {
		if _, ok := obj.(*canvas.Rectangle); ok {
			index = i
			break
		}
	}
	var taskbarIcons []*barIcon
	if index != 0 {
		taskbarIcons = b.icons[index-1:]
	}

	b.icons = nil
	b.children = nil
	b.appendLauncherIcons()

	if b.desk.Settings().LauncherDisableTaskbar() {
		return
	}
	b.icons = append(b.icons, taskbarIcons...)
	for _, obj := range taskbarIcons {
		b.append(obj)
	}
}

func (b *bar) updateIcons() {
	for _, icon := range b.icons {
		if icon.windowData != nil {
			icon.resource = b.winIcon(icon.windowData)
		} else {
			icon.resource = b.appIcon(icon.appData)
		}
		icon.Refresh()
	}
	b.Refresh()
}

func (b *bar) appIcon(data appie.AppData) fyne.Resource {
	icon := data.Icon(b.desk.Settings().IconTheme(), int((float32(b.iconSize)*b.iconScale)*b.desk.Screens().Primary().CanvasScale()))
	if icon == nil {
		return wmTheme.BrokenImageIcon
	}
	return icon
}

func (b *bar) winIcon(win *appWindow) fyne.Resource {
	app := win.findApp()
	if app != nil {
		icon := b.appIcon(app)
		if icon != nil && icon != wmTheme.BrokenImageIcon {
			return icon
		}
	}

	iconRes := win.win.Properties().Icon()
	if iconRes == nil {
		return wmTheme.BrokenImageIcon
	}

	return iconRes
}

func (b *bar) appendLauncherIcons() {
	search := newBarIcon(theme.SearchIcon(), nil, nil)
	search.onTapped = ShowAppLauncher
	b.append(search)
	for _, name := range b.desk.Settings().LauncherIcons() {
		app := b.desk.IconProvider().FindAppFromName(name)
		if app == nil {
			continue
		}
		icon := b.createIcon(app, nil)
		if icon != nil {
			b.append(icon)
		}
	}
	if !b.desk.Settings().LauncherDisableTaskbar() {
		b.appendSeparator()
	}
}

// CreateRenderer creates the renderer that will be responsible for painting the widget
func (b *bar) CreateRenderer() fyne.WidgetRenderer {
	bg := b.makeBackground()
	return &barRenderer{objects: b.children, background: bg, layout: newBarLayout(b), appBar: b}
}

// makeBackground creates the dock background: a solid rect for the left bar,
// or a semi-transparent rounded-rect pill for the bottom dock.
func (b *bar) makeBackground() fyne.CanvasObject {
	if fynedesk.Instance().Settings().BarPosition() == "left" {
		return canvas.NewRectangle(wmTheme.WidgetPanelBackground())
	}
	bg := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x28})
	bg.CornerRadius = 14
	return bg
}

// newBar creates a new application launcher and taskbar
func newBar(desk fynedesk.Desktop) *bar {
	bar := &bar{desk: desk}
	bar.ExtendBaseWidget(bar)
	bar.iconSize = float32(desk.Settings().LauncherIconSize())
	bar.iconScale = float32(desk.Settings().LauncherZoomScale())
	bar.disableTaskbar = desk.Settings().LauncherDisableTaskbar()

	if wm := desk.WindowManager(); wm != nil {
		wm.AddStackListener(bar)
	}
	bar.appendLauncherIcons()

	return bar
}

// barRenderer provides the renderer functions for the bar Widget
type barRenderer struct {
	layout barLayout

	appBar     *bar
	background fyne.CanvasObject
	objects    []fyne.CanvasObject
}

// MinSize returns the layout's Min Size
func (b *barRenderer) MinSize() fyne.Size {
	return b.layout.MinSize(b.objects)
}

// Layout recalculates the widget
func (b *barRenderer) Layout(size fyne.Size) {
	b.layout.setPointerInside(b.appBar.mouseInside)
	b.layout.setPointerPosition(b.appBar.mousePosition)
	b.layout.Layout(b.Objects(), size)
}

// BackgroundColor returns the background color of the widget
func (b *barRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

// Objects returns the objects associated with the widget
func (b *barRenderer) Objects() []fyne.CanvasObject {
	return append([]fyne.CanvasObject{b.background}, b.objects...)
}

// Refresh will recalculate the widget and repaint it
func (b *barRenderer) Refresh() {
	b.background = b.appBar.makeBackground()
	if b.appBar.separator != nil {
		b.appBar.separator.FillColor = theme.Color(theme.ColorNameForeground)
	}
	b.objects = b.appBar.children
	b.Layout(b.appBar.Size())

	canvas.Refresh(b.appBar.separator)
}

// Destroy tidies up resources
func (b *barRenderer) Destroy() {
}
