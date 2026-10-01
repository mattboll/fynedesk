package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde/locale"
	"fyshos.com/tyde/wlipc"
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
	cp.entry.SetPlaceHolder(locale.T("cmd.prompt"))

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
		label := locale.Tf("common.labelValue", act.category, act.name)
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
		{name: locale.T("cmd.closeWindow"), category: locale.T("cmd.catWindow"), action: wlipc.ActionCloseWindow},
		{name: locale.T("cmd.maxRestore"), category: locale.T("cmd.catWindow"), action: wlipc.ActionMaximize},
		{name: locale.T("cmd.minimize"), category: locale.T("cmd.catWindow"), action: wlipc.ActionMinimize},
		{name: locale.T("cmd.fullscreen"), category: locale.T("cmd.catWindow"), action: wlipc.ActionToggleFullscreen},
		{name: locale.T("cmd.snapLeft"), category: locale.T("cmd.catWindow"), action: wlipc.ActionSnapLeft},
		{name: locale.T("cmd.snapRight"), category: locale.T("cmd.catWindow"), action: wlipc.ActionSnapRight},
		{name: locale.T("cmd.switchNext"), category: locale.T("cmd.catWindow"), action: wlipc.ActionSwitchAppNext},
		{name: locale.T("cmd.switchPrev"), category: locale.T("cmd.catWindow"), action: wlipc.ActionSwitchAppPrev},
		{name: locale.T("cmd.overview"), category: locale.T("cmd.catWindow"), action: wlipc.ActionWindowOverview},

		// Apps
		{name: locale.T("cmd.launcher"), category: locale.T("cmd.catApps"), action: wlipc.ActionShowLauncher},
		{name: locale.T("cmd.terminal"), category: locale.T("cmd.catApps"), action: wlipc.ActionOpenTerminal},
		{name: locale.T("cmd.dropdown"), category: locale.T("cmd.catApps"), action: wlipc.ActionToggleDropdown},
		{name: locale.T("cmd.emoji"), category: locale.T("cmd.catTools"), action: wlipc.ActionShowEmojiPicker},
		{name: locale.T("cmd.clipboard"), category: locale.T("cmd.catTools"), action: wlipc.ActionShowClipboard},
		{name: locale.T("cmd.sidebar"), category: locale.T("cmd.catTools"), action: wlipc.ActionToggleSidebar},

		// Screenshot
		{name: locale.T("cmd.screenshotFull"), category: locale.T("cmd.catScreenshot"), action: wlipc.ActionScreenshotFull},
		{name: locale.T("cmd.screenshotAll"), category: locale.T("cmd.catScreenshot"), action: wlipc.ActionScreenshotAll},
		{name: locale.T("cmd.screenshotRegion"), category: locale.T("cmd.catScreenshot"), action: wlipc.ActionScreenshotRegion},
		{name: locale.T("cmd.screenshotWindow"), category: locale.T("cmd.catScreenshot"), action: wlipc.ActionScreenshotWindow},

		// Desktop
		{name: locale.T("cmd.prevDesktop"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionPrevDesktop},
		{name: locale.T("cmd.nextDesktop"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionNextDesktop},
		{name: locale.T("cmd.desktop1"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionSwitchDesk1},
		{name: locale.T("cmd.desktop2"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionSwitchDesk2},
		{name: locale.T("cmd.desktop3"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionSwitchDesk3},
		{name: locale.T("cmd.desktop4"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionSwitchDesk4},
		{name: locale.T("cmd.moveDesktop1"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionMoveToDesk1},
		{name: locale.T("cmd.moveDesktop2"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionMoveToDesk2},
		{name: locale.T("cmd.moveDesktop3"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionMoveToDesk3},
		{name: locale.T("cmd.moveDesktop4"), category: locale.T("cmd.catDesktop"), action: wlipc.ActionMoveToDesk4},

		// Tiling
		{name: locale.T("cmd.toggleTiling"), category: locale.T("cmd.catLayout"), action: wlipc.ActionToggleTiling},
		{name: locale.T("cmd.swapMaster"), category: locale.T("cmd.catLayout"), action: wlipc.ActionSwapMaster},
		{name: locale.T("cmd.toggleFloat"), category: locale.T("cmd.catLayout"), action: wlipc.ActionToggleFloat},

		// System
		{name: locale.T("cmd.lockScreen"), category: locale.T("cmd.catSystem"), action: wlipc.ActionLockScreen},
		{name: locale.T("cmd.volUp"), category: locale.T("cmd.catSystem"), action: wlipc.ActionVolumeUp},
		{name: locale.T("cmd.volDown"), category: locale.T("cmd.catSystem"), action: wlipc.ActionVolumeDown},
		{name: locale.T("cmd.volMute"), category: locale.T("cmd.catSystem"), action: wlipc.ActionVolumeMute},
		{name: locale.T("cmd.brightUp"), category: locale.T("cmd.catSystem"), action: wlipc.ActionBrightnessUp},
		{name: locale.T("cmd.brightDown"), category: locale.T("cmd.catSystem"), action: wlipc.ActionBrightnessDown},
	}
}
