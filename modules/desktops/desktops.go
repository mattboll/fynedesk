package desktops

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	"fyshos.com/tyde/wlipc"
)

func deskCount() int {
	if tyde.Instance() != nil {
		return tyde.Instance().Settings().DesktopCount()
	}
	return 4
}

var desksMeta = tyde.ModuleMetadata{
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

func (d *desktops) Metadata() tyde.ModuleMetadata {
	return desksMeta
}

func (d *desktops) Shortcuts() map[*tyde.Shortcut]func() {
	mapping := make(map[*tyde.Shortcut]func(), deskCount()+2)

	// These shortcuts are only active in X11 mode.
	// In Wayland mode, the compositor handles keybindings directly.
	for i := range deskCount() {
		id := strconv.Itoa(i + 1)
		deskID := i
		mapping[&tyde.Shortcut{Name: "Switch to Desktop " + id, KeyName: fyne.KeyName(id), Modifier: tyde.UserModifier}] = func() {
			d.setDesktop(deskID)
		}
		mapping[&tyde.Shortcut{Name: "Move Window to Desktop " + id, KeyName: fyne.KeyName(id), Modifier: tyde.UserModifier | fyne.KeyModifierShift}] = func() {
			top := tyde.Instance().WindowManager().TopWindow()
			if top != nil {
				top.SetDesktop(deskID)
			}
		}
	}

	current := func() int { return tyde.Instance().Desktop() }

	mapping[&tyde.Shortcut{Name: "Switch to Previous Desktop", KeyName: fyne.KeyUp, Modifier: tyde.UserModifier}] = func() {
		if current() > 0 {
			d.setDesktop(current() - 1)
		}
	}
	mapping[&tyde.Shortcut{Name: "Switch to Next Desktop", KeyName: fyne.KeyDown, Modifier: tyde.UserModifier}] = func() {
		if current() < deskCount()-1 {
			d.setDesktop(current() + 1)
		}
	}
	mapping[&tyde.Shortcut{Name: "Move Window to Previous Desktop", KeyName: fyne.KeyUp, Modifier: tyde.UserModifier | fyne.KeyModifierShift}] = func() {
		if current() == 0 {
			return
		}
		top := tyde.Instance().WindowManager().TopWindow()
		if top != nil {
			top.SetDesktop(current() - 1)
		}
	}
	mapping[&tyde.Shortcut{Name: "Move Window to Next Desktop", KeyName: fyne.KeyDown, Modifier: tyde.UserModifier | fyne.KeyModifierShift}] = func() {
		if current() == deskCount()-1 {
			return
		}
		top := tyde.Instance().WindowManager().TopWindow()
		if top != nil {
			top.SetDesktop(current() + 1)
		}
	}
	return mapping
}

func (d *desktops) StatusAreaWidget() fyne.CanvasObject {
	pager := container.NewStack(d.gui.buttons, d.gui.wins, d.gui.labels)
	// The desktop overview is drawn by the X11 compositor.
	if wlipc.IsWaylandSession() {
		return pager
	}

	reveal := widget.NewButtonWithIcon("", theme.GridIcon(), func() {
		tyde.Instance().ShowDesktopOverview(deskCount())
	})
	reveal.Importance = widget.LowImportance
	return container.NewVBox(reveal, pager)
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
	tyde.Instance().SetDesktop(id)
	d.gui.refresh()
}

// newDesktops creates a new module that will manage virtual desktops and display a pager widget.
func newDesktops() tyde.Module {
	d := &desktops{}
	d.gui = newPager(d)
	return d
}
