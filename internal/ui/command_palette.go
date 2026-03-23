package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk/wlipc"
)

var cmdPalette *commandPalette

// ShowCommandPalette opens (or toggles) the command palette overlay.
func ShowCommandPalette() {
	if cmdPalette != nil {
		cmdPalette.close()
		return
	}

	cp := newCommandPalette()
	cp.win.SetOnClosed(func() {
		cmdPalette = nil
	})
	cmdPalette = cp
	cp.show()
}

type cmdEntry struct {
	widget.Entry
	palette *commandPalette
}

func (e *cmdEntry) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyEscape:
		e.palette.close()
	case fyne.KeyReturn:
		e.palette.runSelected()
	case fyne.KeyUp:
		e.palette.setActiveIndex(e.palette.activeIndex - 1)
	case fyne.KeyDown:
		e.palette.setActiveIndex(e.palette.activeIndex + 1)
	default:
		e.Entry.TypedKey(ev)
	}
}

type paletteAction struct {
	name     string
	category string
	action   string // wlipc action constant (sent via IPC)
}

type commandPalette struct {
	win         fyne.Window
	entry       *cmdEntry
	list        *fyne.Container
	scroll      *container.Scroll
	activeIndex int
	actions     []paletteAction
	filtered    []paletteAction
}

func newCommandPalette() *commandPalette {
	cp := &commandPalette{}

	cp.actions = buildPaletteActions()

	cp.entry = &cmdEntry{palette: cp}
	cp.entry.ExtendBaseWidget(cp.entry)
	cp.entry.SetPlaceHolder("> Type a command...")

	cp.list = container.NewVBox()
	cp.scroll = container.NewScroll(cp.list)

	// Initial unfiltered list
	cp.filtered = cp.actions
	cp.rebuildList()

	cp.entry.OnChanged = func(input string) {
		cp.filterActions(input)
	}

	title := "Command Palette " + SkipTaskbarHint
	if d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver); ok {
		cp.win = d.CreateSplashWindow()
		cp.win.SetPadded(true)
		cp.win.SetTitle(title)
	} else {
		cp.win = fyne.CurrentApp().NewWindow(title)
	}

	cp.win.SetContent(container.NewBorder(cp.entry, nil, nil, nil, cp.scroll))

	return cp
}

func (cp *commandPalette) show() {
	btnH := widget.NewButton("", nil).MinSize().Height
	ideal := fyne.NewSize(400,
		btnH*10+theme.Padding()*8+cp.entry.MinSize().Height)

	fyne.Do(func() {
		cp.win.Resize(ideal)
		cp.win.CenterOnScreen()
		cp.win.Show()
		cp.win.Canvas().Focus(cp.entry)

		go ensureFocused(cp.win, cp.entry)
	})
}

func (cp *commandPalette) close() {
	if cp.win != nil {
		cp.win.Close()
	}
}

func (cp *commandPalette) filterActions(input string) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		cp.filtered = cp.actions
	} else {
		cp.filtered = nil
		for _, a := range cp.actions {
			if fuzzyMatch(input, a.name) || fuzzyMatch(input, a.category) {
				cp.filtered = append(cp.filtered, a)
			}
		}
	}
	cp.rebuildList()
}

func (cp *commandPalette) rebuildList() {
	cp.list.Objects = nil
	cp.activeIndex = 0

	for i, a := range cp.filtered {
		act := a
		idx := i
		label := act.category + ": " + act.name
		btn := widget.NewButton(label, func() {
			cp.executeAction(act)
		})
		btn.Alignment = widget.ButtonAlignLeading
		if idx == 0 {
			btn.Importance = widget.HighImportance
		}
		cp.list.Add(btn)
	}
	cp.list.Refresh()
	cp.scroll.Offset = fyne.NewPos(0, 0)
	cp.scroll.Refresh()
}

func (cp *commandPalette) setActiveIndex(index int) {
	if index < 0 || index >= len(cp.list.Objects) {
		return
	}

	// Unhighlight old
	if cp.activeIndex < len(cp.list.Objects) {
		if old, ok := cp.list.Objects[cp.activeIndex].(*widget.Button); ok {
			old.Importance = widget.MediumImportance
			old.Refresh()
		}
	}

	// Highlight new
	if active, ok := cp.list.Objects[index].(*widget.Button); ok {
		active.Importance = widget.HighImportance
		active.Refresh()
	}

	cp.activeIndex = index

	// Auto-scroll
	if index < len(cp.list.Objects) {
		obj := cp.list.Objects[index]
		cp.scroll.Offset = fyne.NewPos(0,
			obj.Position().Y+obj.Size().Height/2-cp.scroll.Size().Height/2)
		cp.scroll.Refresh()
	}
}

func (cp *commandPalette) runSelected() {
	if cp.activeIndex >= len(cp.filtered) {
		return
	}
	cp.executeAction(cp.filtered[cp.activeIndex])
}

func (cp *commandPalette) executeAction(act paletteAction) {
	cp.close()

	client := wlipc.DefaultClient()
	if client == nil {
		return
	}

	// Actions that need special handling
	switch act.action {
	case wlipc.ActionShowLauncher:
		ShowAppLauncher()
		return
	case wlipc.ActionShowEmojiPicker:
		ShowEmojiPicker(0, 0)
		return
	case wlipc.ActionShowClipboard:
		ShowClipboardManager()
		return
	case wlipc.ActionToggleSidebar:
		ToggleSidebar()
		return
	}

	// Send generic action to compositor via socket
	req := struct {
		Action string `json:"action"`
	}{Action: act.action}
	client.SendRequest("compositor-action", req)
}

// fuzzyMatch checks if all characters in pattern appear in order in str.
func fuzzyMatch(pattern, str string) bool {
	str = strings.ToLower(str)
	pi := 0
	for i := 0; i < len(str) && pi < len(pattern); i++ {
		if str[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

func buildPaletteActions() []paletteAction {
	return []paletteAction{
		// Window actions
		{name: "Close Window", category: "Window", action: wlipc.ActionCloseWindow},
		{name: "Maximize / Restore", category: "Window", action: wlipc.ActionMaximize},
		{name: "Minimize", category: "Window", action: wlipc.ActionMinimize},
		{name: "Toggle Fullscreen", category: "Window", action: wlipc.ActionToggleFullscreen},
		{name: "Snap Left", category: "Window", action: wlipc.ActionSnapLeft},
		{name: "Snap Right", category: "Window", action: wlipc.ActionSnapRight},
		{name: "Switch App Next", category: "Window", action: wlipc.ActionSwitchAppNext},
		{name: "Switch App Previous", category: "Window", action: wlipc.ActionSwitchAppPrev},
		{name: "Window Overview", category: "Window", action: wlipc.ActionWindowOverview},

		// Apps
		{name: "App Launcher", category: "Apps", action: wlipc.ActionShowLauncher},
		{name: "Open Terminal", category: "Apps", action: wlipc.ActionOpenTerminal},
		{name: "Dropdown Terminal", category: "Apps", action: wlipc.ActionToggleDropdown},
		{name: "Emoji Picker", category: "Tools", action: wlipc.ActionShowEmojiPicker},
		{name: "Clipboard Manager", category: "Tools", action: wlipc.ActionShowClipboard},
		{name: "Toggle Sidebar", category: "Tools", action: wlipc.ActionToggleSidebar},

		// Screenshot
		{name: "Screenshot Full", category: "Screenshot", action: wlipc.ActionScreenshotFull},
		{name: "Screenshot Region", category: "Screenshot", action: wlipc.ActionScreenshotRegion},
		{name: "Screenshot Window", category: "Screenshot", action: wlipc.ActionScreenshotWindow},

		// Desktop
		{name: "Previous Desktop", category: "Desktop", action: wlipc.ActionPrevDesktop},
		{name: "Next Desktop", category: "Desktop", action: wlipc.ActionNextDesktop},
		{name: "Desktop 1", category: "Desktop", action: wlipc.ActionSwitchDesk1},
		{name: "Desktop 2", category: "Desktop", action: wlipc.ActionSwitchDesk2},
		{name: "Desktop 3", category: "Desktop", action: wlipc.ActionSwitchDesk3},
		{name: "Desktop 4", category: "Desktop", action: wlipc.ActionSwitchDesk4},
		{name: "Move Window to Desktop 1", category: "Desktop", action: wlipc.ActionMoveToDesk1},
		{name: "Move Window to Desktop 2", category: "Desktop", action: wlipc.ActionMoveToDesk2},
		{name: "Move Window to Desktop 3", category: "Desktop", action: wlipc.ActionMoveToDesk3},
		{name: "Move Window to Desktop 4", category: "Desktop", action: wlipc.ActionMoveToDesk4},

		// Tiling
		{name: "Toggle Tiling", category: "Layout", action: wlipc.ActionToggleTiling},
		{name: "Swap Master", category: "Layout", action: wlipc.ActionSwapMaster},
		{name: "Toggle Float", category: "Layout", action: wlipc.ActionToggleFloat},

		// System
		{name: "Lock Screen", category: "System", action: wlipc.ActionLockScreen},
		{name: "Volume Up", category: "System", action: wlipc.ActionVolumeUp},
		{name: "Volume Down", category: "System", action: wlipc.ActionVolumeDown},
		{name: "Volume Mute", category: "System", action: wlipc.ActionVolumeMute},
		{name: "Brightness Up", category: "System", action: wlipc.ActionBrightnessUp},
		{name: "Brightness Down", category: "System", action: wlipc.ActionBrightnessDown},
	}
}
