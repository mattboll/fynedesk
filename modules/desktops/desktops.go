package desktops

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/wlipc"
)

func deskCount() int {
	if fynedesk.Instance() != nil {
		return fynedesk.Instance().Settings().DesktopCount()
	}
	return 4
}

var desksMeta = fynedesk.ModuleMetadata{
	Name:        "Virtual Desktops",
	NewInstance: newDesktops,
}

type desktops struct {
	gui *pager
}

func (d *desktops) DesktopChangeNotify(_ int) {
	d.gui.refresh()
}

func (d *desktops) Destroy() {
}

func (d *desktops) Metadata() fynedesk.ModuleMetadata {
	return desksMeta
}

func (d *desktops) Shortcuts() map[*fynedesk.Shortcut]func() {
	mapping := make(map[*fynedesk.Shortcut]func(), deskCount()+2)

	// These shortcuts are only active in X11 mode.
	// In Wayland mode, the compositor handles keybindings directly.
	for i := range deskCount() {
		id := strconv.Itoa(i + 1)
		deskID := i
		mapping[&fynedesk.Shortcut{Name: "Switch to Desktop " + id, KeyName: fyne.KeyName(id), Modifier: fynedesk.UserModifier}] = func() {
			d.setDesktop(deskID)
		}
		mapping[&fynedesk.Shortcut{Name: "Move Window to Desktop " + id, KeyName: fyne.KeyName(id), Modifier: fynedesk.UserModifier | fyne.KeyModifierShift}] = func() {
			top := fynedesk.Instance().WindowManager().TopWindow()
			if top != nil {
				top.SetDesktop(deskID)
			}
		}
	}

	current := func() int { return fynedesk.Instance().Desktop() }

	mapping[&fynedesk.Shortcut{Name: "Switch to Previous Desktop", KeyName: fyne.KeyUp, Modifier: fynedesk.UserModifier}] = func() {
		if current() > 0 {
			d.setDesktop(current() - 1)
		}
	}
	mapping[&fynedesk.Shortcut{Name: "Switch to Next Desktop", KeyName: fyne.KeyDown, Modifier: fynedesk.UserModifier}] = func() {
		if current() < deskCount()-1 {
			d.setDesktop(current() + 1)
		}
	}
	mapping[&fynedesk.Shortcut{Name: "Move Window to Previous Desktop", KeyName: fyne.KeyUp, Modifier: fynedesk.UserModifier | fyne.KeyModifierShift}] = func() {
		if current() == 0 {
			return
		}
		top := fynedesk.Instance().WindowManager().TopWindow()
		if top != nil {
			top.SetDesktop(current() - 1)
		}
	}
	mapping[&fynedesk.Shortcut{Name: "Move Window to Next Desktop", KeyName: fyne.KeyDown, Modifier: fynedesk.UserModifier | fyne.KeyModifierShift}] = func() {
		if current() == deskCount()-1 {
			return
		}
		top := fynedesk.Instance().WindowManager().TopWindow()
		if top != nil {
			top.SetDesktop(current() + 1)
		}
	}
	return mapping
}

func (d *desktops) StatusAreaWidget() fyne.CanvasObject {
	return container.NewStack(d.gui.buttons, d.gui.wins, d.gui.labels)
}

func (d *desktops) setDesktop(id int) {
	// Use IPC for Wayland compositor
	if wlipc.IsWaylandSession() {
		if err := wlipc.RequestDesktopSwitch(id); err != nil {
			fyne.LogError("Failed to request desktop switch", err)
		}
		return // Compositor will update state, we'll get notified via watcher
	}

	// X11 mode: direct call
	fynedesk.Instance().SetDesktop(id)
}

// newDesktops creates a new module that will manage virtual desktops and display a pager widget.
func newDesktops() fynedesk.Module {
	d := &desktops{}
	d.gui = newPager(d)
	return d
}
