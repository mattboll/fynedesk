package ui

import (
	"fyne.io/fyne"
	"fyne.io/fyne/canvas"
	"fyne.io/fynedesk"
	wmTheme "fyne.io/fynedesk/theme"
)

var (
	appBar *bar
)

func barCreateIcon(b *bar, taskbar bool, data fynedesk.AppData, win fynedesk.Window) *barIcon {
	if data == nil {
		return nil
	}
	iconRes := b.getIconResource(data, win)
	icon := newBarIcon(iconRes, data)
	if taskbar == false {
		icon.onTapped = func() {
			err := b.desk.RunApp(data)
			if err != nil {
				fyne.LogError("Failed to start app", err)
			}
		}
	} else {
		icon.taskbarWindow = win
	}
	b.icons = append(b.icons, icon)
	return icon
}

func taskbarIconTapped(win fynedesk.Window) {
	if !win.Iconic() && win.TopWindow() {
		win.Iconify()
		return
	}
	if win.Iconic() {
		win.Uniconify()
	}
	win.RaiseToTop()
	win.Focus()
}

func (b *bar) WindowAdded(win fynedesk.Window) {
	if win.SkipTaskbar() || b.desk.Settings().LauncherDisableTaskbar() {
		return
	}
	data := b.desk.IconProvider().FindAppFromWinInfo(win)
	if data == nil {
		return
	}
	icon := barCreateIcon(b, true, data, win)
	if icon != nil {
		icon.onTapped = func() {
			taskbarIconTapped(win)
		}
		appBar.append(icon)
	}
}

func (b *bar) WindowRemoved(win fynedesk.Window) {
	if win.SkipTaskbar() || b.desk.Settings().LauncherDisableTaskbar() {
		return
	}
	for i, icon := range b.icons {
		if icon.taskbarWindow == nil || win != icon.taskbarWindow {
			continue
		}
		if !win.Iconic() {
			appBar.removeFromTaskbar(icon)
			b.icons = append(b.icons[:i], b.icons[i+1:]...)
		}
		break
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
	appBar.appendSeparator()
	if b.desk.WindowManager() == nil {
		return
	}

	for _, win := range b.desk.WindowManager().Windows() {
		b.WindowAdded(win)
	}
}

func (b *bar) updateIconOrder() {
	var index = -1
	for i, obj := range b.children {
		if _, ok := obj.(*canvas.Rectangle); ok {
			index = i
			break
		}
	}
	var taskbarIcons []*barIcon
	if index != -1 {
		taskbarIcons = b.icons[index:]
	}

	b.icons = nil
	b.children = nil
	b.appendLauncherIcons()

	if b.desk.Settings().LauncherDisableTaskbar() {
		return
	}
	b.icons = append(b.icons, taskbarIcons...)
	for _, obj := range taskbarIcons {
		appBar.append(obj)
	}
}

func (b *bar) updateIcons() {
	for _, icon := range b.icons {
		icon.resource = b.getIconResource(icon.appData, icon.taskbarWindow)
		icon.Refresh()
	}
	b.Refresh()
}

func (b *bar) getIconResource(data fynedesk.AppData, win fynedesk.Window) fyne.Resource {
	iconRes := data.Icon(b.desk.Settings().IconTheme(), int((float32(b.iconSize)*b.iconScale)*b.desk.Root().Canvas().Scale()))
	if iconRes == nil || iconRes == wmTheme.BrokenImageIcon {
		if win != nil {
			iconRes = win.Icon()
			if iconRes == nil {
				iconRes = wmTheme.BrokenImageIcon
			}
		}
	}
	return iconRes
}

func (b *bar) appendLauncherIcons() {
	for _, name := range b.desk.Settings().LauncherIcons() {
		app := b.desk.IconProvider().FindAppFromName(name)
		if app == nil {
			continue
		}
		icon := barCreateIcon(appBar, false, app, nil)
		if icon != nil {
			appBar.append(icon)
		}
	}
	if !b.desk.Settings().LauncherDisableTaskbar() {
		appBar.appendSeparator()
	}
}

func newBar(desk fynedesk.Desktop) fyne.CanvasObject {
	appBar = newAppBar(desk)

	if desk.WindowManager() != nil {
		desk.WindowManager().AddStackListener(appBar)
	}
	appBar.appendLauncherIcons()

	return appBar
}
