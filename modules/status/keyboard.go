package status

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

var keyboardMeta = fynedesk.ModuleMetadata{
	Name:        "Keyboard Layout",
	NewInstance: NewKeyboardLayout,
}

type keyboardLayout struct {
	icon  *widget.Button
	label *widget.Label
	box   *fyne.Container

	layouts     []wlipc.KeyboardLayout
	activeIndex int
	done        chan struct{} // closed to stop IPC watcher goroutines
}

func (k *keyboardLayout) Destroy() {
	if k.done != nil {
		close(k.done)
	}
}

func (k *keyboardLayout) StatusAreaWidget() fyne.CanvasObject {
	// Only show if running under Wayland session
	if !wlipc.IsWaylandSession() {
		return nil
	}

	// Always create the widget — it will be shown/hidden dynamically
	k.label = widget.NewLabel("")
	k.icon = &widget.Button{Icon: wmtheme.KeyboardIcon, Importance: widget.LowImportance, OnTapped: k.cycleLayout}
	k.box = container.New(&handleNarrow{}, k.icon, k.label)

	// Read initial state if available
	state, _ := wlipc.GetKeyboardLayoutState()
	if state != nil && len(state.Layouts) > 1 {
		k.layouts = state.Layouts
		k.activeIndex = state.ActiveIndex
		if k.activeIndex < len(k.layouts) {
			k.label.SetText(k.layouts[k.activeIndex].ShortName())
		}
		k.box.Show()
	} else {
		k.box.Hide()
	}

	// Watch for layout state changes from compositor
	k.watchLayoutState()

	return k.box
}

func (k *keyboardLayout) cycleLayout() {
	if len(k.layouts) <= 1 {
		return
	}
	nextIndex := (k.activeIndex + 1) % len(k.layouts)
	_ = wlipc.RequestKeyboardLayout(nextIndex)
}

func (k *keyboardLayout) watchLayoutState() {
	k.done = make(chan struct{})
	wlipc.WatchKeyboardLayoutState(func(state *wlipc.KeyboardLayoutState) {
		k.layouts = state.Layouts
		k.activeIndex = state.ActiveIndex

		shortName := ""
		if k.activeIndex < len(k.layouts) {
			shortName = k.layouts[k.activeIndex].ShortName()
		}

		fyne.Do(func() {
			k.label.SetText(shortName)
			if len(k.layouts) > 1 {
				k.box.Show()
			} else {
				k.box.Hide()
			}
		})
	}, k.done)
}

func (k *keyboardLayout) Metadata() fynedesk.ModuleMetadata {
	return keyboardMeta
}

// NewKeyboardLayout creates a new module that will show keyboard layout info in the status area
func NewKeyboardLayout() fynedesk.Module {
	return &keyboardLayout{}
}
