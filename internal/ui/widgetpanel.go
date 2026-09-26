package ui

import (
	"image/color"
	"os"
	"os/user"
	"strings"
	"sync/atomic"
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

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
)

// Go date package does not follow changing timezones, so we will.
// startedOffset is the minutes from UTC in our starting timezone.
var startedOffset int

// currentOffset is the latest minutes-from-UTC offset, refreshed off the render
// thread by the clock ticker (getOffset shells out to `date`, so it must not run
// on the render thread). adjustedNow reads this cached value.
var currentOffset atomic.Int64

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

	desk            tyde.Desktop
	about, settings fyne.Window
	settingsNav     *settingsNav // retained so a re-show can jump to a named panel

	account                 *widget.Button
	clock, clockSec, vClock *canvas.Text
	date                    *widget.Label
	rotated                 *canvas.Image
	modules, clocks         *fyne.Container
	agents                  *fyne.Container // the Coding Agents widget, when the module is on
	agentsWidget            *agentsWidget   // kept while the hub goes on
	narrow                  bool
	notifications           fyne.CanvasObject

	calendarWin     fyne.Window // current calendar overlay (nil if closed)
	lastRotatedText string      // cached text to avoid redundant rotate

	accountMenu         fyne.Window // the account menu window of a Wayland session, while open
	accountMenuClosedAt time.Time   // when it last closed, see accountMenuReopenDelay
}

// startClock drives the once-a-second clock refresh. It is a package var so
// tests can disable the background ticker, which would otherwise race the test
// goroutine.
var startClock = func(w *widgetPanel) {
	go func() {
		t := time.NewTicker(time.Second)
		for range t.C {
			refreshOffset() // re-read the timezone offset off the render thread
			fyne.Do(w.clockRefresh)
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
			go w.rotate(*w.vClock)
		}
	}

	w.date.SetText(w.formattedDate())
	w.date.Refresh()
}

// clockTime formats a time of day in the clock format of the settings.
func clockTime(t time.Time) string {
	format := "24h"
	if inst := tyde.Instance(); inst != nil && inst.Settings() != nil {
		format = inst.Settings().ClockFormatting()
	}
	return locale.Clock(t, format, false)
}

func (w *widgetPanel) formattedTime() string {
	return locale.Clock(adjustedNow(), w.desk.Settings().ClockFormatting(), false)
}

func (w *widgetPanel) formattedTimeWithSeconds() string {
	return locale.Clock(adjustedNow(), w.desk.Settings().ClockFormatting(), true)
}

func (w *widgetPanel) formattedDate() string {
	date := locale.DayMonth(adjustedNow())
	if w.desk.Settings().NarrowWidgetPanel() {
		date = strings.Replace(date, " ", "\n", 1)
	}
	return date
}

func (w *widgetPanel) createClock() {
	var style fyne.TextStyle
	style.Monospace = true
	// What Go's local time uses (fixed at start); getOffset reads the zone
	// of now.
	_, secs := time.Now().Zone()
	startedOffset = secs / 60
	currentOffset.Store(int64(getOffset()))

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

	startClock(w)
}

// rotate draws the clock text turned a quarter, for the narrow panel. It
// runs in a goroutine, on a copy of the text: the clock changes it on the
// Fyne thread meanwhile.
func (w *widgetPanel) rotate(clock canvas.Text) {
	c := software.NewTransparentCanvas()
	c.SetPadded(false)
	c.SetContent(&clock)

	out := imaging.Rotate270(c.Capture())
	ratio := clock.MinSize().Width / clock.MinSize().Height
	space := wmtheme.NarrowBarWidth - theme.Padding()*2
	fyne.Do(func() {
		w.rotated.Image = out
		w.rotated.SetMinSize(fyne.NewSize(space, space*ratio))
		w.rotated.Refresh()
	})
}

func (w *widgetPanel) CreateRenderer() fyne.WidgetRenderer {
	narrow := w.desk.Settings().NarrowWidgetPanel()
	accountLabel := w.accountLabel()
	var account *widget.Button
	w.account = widget.NewButtonWithIcon(accountLabel, accountIcon(), func() {
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
	)
	w.narrow = narrow
	if w.agents == nil {
		w.agents = container.NewVBox()
	}
	top.Add(w.agents)
	top.Add(w.notifications)

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

// accountIcon returns the user's avatar for the account button: their ~/.face
// image if they have set one (see the Account settings tab), otherwise the
// generic user icon. The file is read once here, not on every panel refresh.
func accountIcon() fyne.Resource {
	p := facePath()
	if p == "" {
		return wmtheme.UserIcon
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return wmtheme.UserIcon
	}
	return fyne.NewStaticResource("face", data)
}

// refreshAccountIcon updates the account button to the current ~/.face image,
// used when the user changes their picture in settings.
func (w *widgetPanel) refreshAccountIcon() {
	if w.account != nil {
		w.account.SetIcon(accountIcon())
	}
}

func (w *widgetPanel) reloadModules(mods []tyde.Module) {
	w.modules.Objects = nil
	w.loadModules(mods)
	w.modules.Refresh()
	w.agents.Refresh()
}

func (w *widgetPanel) loadModules(mods []tyde.Module) {
	if w.agents == nil {
		w.agents = container.NewVBox()
	}
	w.agents.Objects = nil
	for _, m := range mods {
		if am, ok := m.(*agentsModule); ok && am.hub != nil && !w.narrow {
			if w.agentsWidget == nil || w.agentsWidget.hub != am.hub {
				w.agentsWidget = newAgentsWidget(am.hub)
			}
			w.agents.Objects = []fyne.CanvasObject{w.agentsWidget}
		}
	}
	for _, m := range mods {
		if statusMod, ok := m.(tyde.StatusAreaModule); ok {
			wid := statusMod.StatusAreaWidget()
			if wid == nil {
				continue
			}

			w.modules.Objects = append(w.modules.Objects, wid)
		}
	}
}

func newWidgetPanel(rootDesk tyde.Desktop) *widgetPanel {
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

	calW := calendarPopupSize.Width
	calH := calendarPopupSize.Height
	win.Resize(fyne.NewSize(calW, calH))

	screen := tyde.Instance().Screens().Primary()
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
	newOffset := int(currentOffset.Load())
	return time.Now().Add(time.Minute * time.Duration(newOffset-startedOffset))
}

// refreshOffset re-reads the timezone offset (getOffset shells out to `date`, so
// this must run off the Fyne render thread) and caches it for adjustedNow.
func refreshOffset() {
	currentOffset.Store(int64(getOffset()))
}

// getOffset returns the offset from UTC of the system's time zone now, in
// minutes. Go reads the zone once at start (time.Local), so the zone is read
// again: TZ, else /etc/localtime.
func getOffset() int {
	now := time.Now()
	if loc := systemLocation(); loc != nil {
		now = now.In(loc)
	}
	_, secs := now.Zone()
	return secs / 60
}

// systemLocation reads the system's time zone as it is now, or nil.
func systemLocation() *time.Location {
	if tz, ok := os.LookupEnv("TZ"); ok {
		if loc, err := time.LoadLocation(strings.TrimPrefix(tz, ":")); err == nil {
			return loc
		}
		return nil
	}
	data, err := os.ReadFile("/etc/localtime")
	if err != nil {
		return nil
	}
	loc, err := time.LoadLocationFromTZData("Local", data)
	if err != nil {
		return nil
	}
	return loc
}
