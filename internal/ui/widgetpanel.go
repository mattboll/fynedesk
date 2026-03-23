package ui

import (
	"image/color"
	"log"
	"os/exec"
	"os/user"
	"strconv"
	"time"

	"github.com/disintegration/imaging"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

// Go date package does not follow changing timezones, so we will.
// startedOffset is the minutes from UTC in our starting timezone.
var startedOffset int

type widgetRenderer struct {
	panel *widgetPanel
	bg    *canvas.Rectangle

	objects []fyne.CanvasObject
}

func (w *widgetRenderer) MinSize() fyne.Size {
	return w.panel.MinSize()
}

func (w *widgetRenderer) Layout(size fyne.Size) {
	w.bg.Resize(size)
	// objects[1] is the Border container (top/bottom/center with scroll)
	w.objects[1].Resize(size)
}

func (w *widgetRenderer) Refresh() {
	w.bg.FillColor = wmtheme.WidgetPanelBackground()
	w.bg.Refresh()

	w.panel.account.SetText(w.panel.accountLabel())
	if w.panel.desk.Settings().NarrowWidgetPanel() {
		w.panel.clocks.Objects[0].Hide()
		w.panel.clocks.Objects[1].Show()
	} else {
		w.panel.clocks.Objects[0].Show()
		w.panel.clocks.Objects[1].Hide()
	}
	fg := theme.Color(theme.ColorNameForeground)
	w.panel.clock.Color = fg
	w.panel.clockSec.Color = fg
	w.panel.vClock.Color = fg
	canvas.Refresh(w.panel.clock)
	canvas.Refresh(w.panel.clockSec)
}

func (w *widgetRenderer) Objects() []fyne.CanvasObject {
	return w.objects
}

func (w *widgetRenderer) Destroy() {
}

type widgetPanel struct {
	widget.BaseWidget

	desk            fynedesk.Desktop
	about, settings fyne.Window

	account                 *widget.Button
	clock, clockSec, vClock *canvas.Text
	date                    *widget.Label
	rotated                 *canvas.Image
	modules, clocks         *fyne.Container
	notifications           fyne.CanvasObject

	calendarWin     fyne.Window // current calendar overlay (nil if closed)
	lastRotatedText string      // cached text to avoid redundant rotate
}

func (w *widgetPanel) clockTick() {
	// Buffered channel (size 1) prevents deadlock when fyne.Do blocks:
	// without the buffer, the AfterFunc goroutine blocks on send while
	// the consumer is stuck waiting for fyne.Do, causing the clock to freeze.
	wait := make(chan struct{}, 1)
	time.AfterFunc(time.Second, func() {
		wait <- struct{}{}
	})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panel] PANIC in clockTick goroutine: %v", r)
			}
		}()
		for range wait {
			fyne.Do(w.clockRefresh)

			time.AfterFunc(time.Second, func() {
				select {
				case wait <- struct{}{}:
				default:
					// Drop tick if previous one hasn't been consumed yet
				}
			})
		}
	}()
}

func (w *widgetPanel) clockRefresh() {
	if w.rotated == nil {
		return // not yet been drawn so don't worry
	}

	showSec := w.desk.Settings().ClockShowSeconds()
	w.clock.Text = w.formattedTime()
	if showSec {
		w.clockSec.Text = adjustedNow().Format(":05")
		w.clockSec.Show()
		w.vClock.Text = w.formattedTimeWithSeconds()
	} else {
		w.clockSec.Text = ""
		w.clockSec.Hide()
		w.vClock.Text = w.formattedTime()
	}
	canvas.Refresh(w.clock)
	canvas.Refresh(w.clockSec)
	if w.desk.Settings().NarrowWidgetPanel() {
		// Only re-render the rotated clock if text actually changed
		newText := w.vClock.Text
		if newText != w.lastRotatedText {
			w.lastRotatedText = newText
			go w.rotate(w.vClock)
		}
	}

	w.date.SetText(w.formattedDate())
	w.date.Refresh()
}

func (w *widgetPanel) formattedTime() string {
	if w.desk.Settings().ClockFormatting() == "12h" {
		return adjustedNow().Format("3:04pm")
	}
	return adjustedNow().Format("15:04")
}

func (w *widgetPanel) formattedTimeWithSeconds() string {
	if w.desk.Settings().ClockFormatting() == "12h" {
		return adjustedNow().Format("3:04:05pm")
	}
	return adjustedNow().Format("15:04:05")
}

func (w *widgetPanel) formattedDate() string {
	format := "2 Jan"
	if w.desk.Settings().NarrowWidgetPanel() {
		format = "2\nJan"
	}

	return adjustedNow().Format(format)
}

func (w *widgetPanel) createClock() {
	var style fyne.TextStyle
	style.Monospace = true
	startedOffset = getOffset()

	fg := theme.Color(theme.ColorNameForeground)
	w.clock = &canvas.Text{
		Color:     fg,
		Text:      w.formattedTime(),
		Alignment: fyne.TextAlignCenter,
		TextStyle: style,
		TextSize:  3 * theme.TextSize(),
	}
	w.clockSec = &canvas.Text{
		Color:     fg,
		Text:      "",
		Alignment: fyne.TextAlignCenter,
		TextStyle: style,
		TextSize:  2 * theme.TextSize(),
	}
	if !w.desk.Settings().ClockShowSeconds() {
		w.clockSec.Hide()
	}
	w.vClock = &canvas.Text{
		Color:     fg,
		Text:      w.formattedTime(),
		Alignment: fyne.TextAlignCenter,
		TextStyle: style,
		TextSize:  wmtheme.NarrowBarWidth * 1.5,
	}
	w.date = &widget.Label{
		Text:      w.formattedDate(),
		Alignment: fyne.TextAlignCenter,
		TextStyle: style,
	}

	go w.clockTick()
}

func (w *widgetPanel) rotate(time *canvas.Text) {
	c := software.NewTransparentCanvas()
	c.SetPadded(false)
	c.SetContent(time)

	img := c.Capture()
	out := imaging.Rotate270(img)

	w.rotated.Image = out
	ratio := time.MinSize().Width / time.MinSize().Height
	space := wmtheme.NarrowBarWidth - theme.Padding()*2
	fyne.Do(func() {
		w.rotated.SetMinSize(fyne.NewSize(space, space*ratio))
		w.rotated.Refresh()
	})
}

func (w *widgetPanel) CreateRenderer() fyne.WidgetRenderer {
	narrow := w.desk.Settings().NarrowWidgetPanel()
	accountLabel := w.accountLabel()
	var account *widget.Button
	w.account = widget.NewButtonWithIcon(accountLabel, wmtheme.UserIcon, func() {
		w.showAccountMenu(account)
	})

	w.rotated = &canvas.Image{}
	clockRow := container.NewCenter(container.NewHBox(w.clock, container.NewVBox(layout.NewSpacer(), w.clockSec)))
	w.clocks = container.NewStack(clockRow, container.New(&vClockPad{}, w.rotated))
	if narrow {
		clockRow.Hide()
	} else {
		w.clocks.Objects[1].Hide()
	}
	w.clockRefresh()

	bg := canvas.NewRectangle(wmtheme.WidgetPanelBackground())

	clockContent := container.NewVBox(
		w.clocks,
		w.date,
	)
	clockTap := newTappableContainer(clockContent, func() {
		w.showCalendar()
	})

	top := container.NewVBox(
		canvas.NewRectangle(color.Transparent), // clear top edge for clocks
		clockTap,
		w.notifications,
	)

	w.modules = container.NewVBox()
	w.loadModules(w.desk.Modules())
	// Sidebar toggle button
	sidebarBtn := widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		ToggleSidebar()
	})
	sidebarBtn.Importance = widget.LowImportance
	var sidebarWidget fyne.CanvasObject
	if narrow {
		sidebarWidget = newHoverTooltip(sidebarBtn, locale.T("sidebar.toggle"))
	} else {
		sidebarWidget = sidebarBtn
	}

	var accountWidget fyne.CanvasObject
	if narrow {
		currentUser, _ := user.Current()
		tipText := "Account"
		if currentUser != nil {
			tipText = currentUser.Username
		}
		accountWidget = newHoverTooltip(w.account, tipText)
	} else {
		accountWidget = w.account
	}
	bottom := container.NewVBox(w.modules, sidebarWidget, accountWidget)

	content := container.NewBorder(top, bottom, nil, nil)

	return &widgetRenderer{
		panel:   w,
		bg:      bg,
		objects: []fyne.CanvasObject{bg, content},
	}
}

func (w *widgetPanel) MinSize() fyne.Size {
	if w.desk.Settings().NarrowWidgetPanel() {
		return fyne.NewSize(wmtheme.NarrowBarWidth, 200)
	}
	return fyne.NewSize(wmtheme.WidgetPanelWidth, 200)
}

func (w *widgetPanel) accountLabel() string {
	if w.desk.Settings().NarrowWidgetPanel() {
		return ""
	}
	currentUser, err := user.Current()
	if err != nil {
		fyne.LogError("Unable to look up user", err)
		return "Account"
	}
	displayName := currentUser.Username
	return displayName
}

func (w *widgetPanel) reloadModules(mods []fynedesk.Module) {
	w.modules.Objects = nil
	w.loadModules(mods)
	w.modules.Refresh()
}

func (w *widgetPanel) loadModules(mods []fynedesk.Module) {
	for _, m := range mods {
		if statusMod, ok := m.(fynedesk.StatusAreaModule); ok {
			wid := statusMod.StatusAreaWidget()
			if wid == nil {
				continue
			}

			w.modules.Objects = append(w.modules.Objects, wid)
		}
	}
}

func newWidgetPanel(rootDesk fynedesk.Desktop) *widgetPanel {
	w := &widgetPanel{desk: rootDesk}
	w.ExtendBaseWidget(w)
	w.notifications = newNotificationPanel()
	initNotificationToasts()
	w.createClock()

	return w
}

func (w *widgetPanel) showCalendar() {
	// Toggle: close existing calendar if open
	if w.calendarWin != nil {
		w.calendarWin.Close()
		w.calendarWin = nil
		return
	}

	cal := calendarPopup(adjustedNow())

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}
	win := d.CreateSplashWindow()
	win.SetTitle("Calendar " + SkipTaskbarHint)
	win.SetContent(cal)
	win.SetOnClosed(func() { w.calendarWin = nil })

	calW := float32(340)
	calH := float32(290)
	win.Resize(fyne.NewSize(calW, calH))

	screen := fynedesk.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	panelW := float32(0)
	if w.desk.Settings().NarrowLeftLauncher() {
		panelW = wmtheme.NarrowBarWidth
	}
	widgetW := wmtheme.WidgetPanelWidth
	if w.desk.Settings().NarrowWidgetPanel() {
		widgetW = wmtheme.NarrowBarWidth
	}
	// Position to the left of the widget panel, near the top
	finalX := screenW - widgetW - calW - 10
	if finalX < panelW {
		finalX = panelW + 10
	}
	finalY := float32(10)

	wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, calW, calH)
	win.Show()

	w.calendarWin = win
}

// tappableContainer wraps a container to make it respond to taps.
type tappableContainer struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappableContainer(content fyne.CanvasObject, onTap func()) *tappableContainer {
	t := &tappableContainer{content: content, onTap: onTap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableContainer) Tapped(_ *fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tappableContainer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

type vClockPad struct {
	minCache fyne.Size
}

func (u *vClockPad) Layout(objects []fyne.CanvasObject, _ fyne.Size) {
	objects[0].Resize(objects[0].MinSize())
	objects[0].Move(fyne.NewPos(5, 0))
}

func (u *vClockPad) MinSize(objects []fyne.CanvasObject) fyne.Size {
	clockMin := objects[0].MinSize()
	u.minCache = u.minCache.Max(clockMin)
	return u.minCache.Subtract(fyne.NewSize(0, theme.Padding()))
}

func adjustedNow() time.Time {
	newOffset := getOffset()
	return time.Now().Add(time.Minute * time.Duration(newOffset-startedOffset))
}

func getOffset() int {
	ret, err := exec.Command("date", "+%z").Output()
	if err != nil {
		fyne.LogError("Failed to load date offset", err)
		return 0
	}

	if len(ret) <= 2 {
		fyne.LogError("Invalid offset format "+string(ret), err)
	}

	hourStr := string(ret[0 : len(ret)-3])
	minStr := string(ret[len(ret)-3:])

	hours, _ := strconv.ParseInt(hourStr, 10, 64)
	mins, _ := strconv.ParseInt(minStr, 10, 0)
	return int(hours)*60 + int(mins)
}
