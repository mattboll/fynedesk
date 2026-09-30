package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"
	"fyshos.com/tyde/wm"
)

// Toast geometry, in Fyne units.
const (
	toastW        = float32(320)
	toastH        = float32(76)
	toastGap      = float32(8)
	toastMargin   = float32(10)
	maxToasts     = 3
	toastButtonsH = float32(34)
	toastDefault  = 4 * time.Second
	toastMax      = 30 * time.Second
)

// toast is a notification popup on screen.
type toast struct {
	n       *wm.Notification
	win     fyne.Window
	title   string // window title, unique: the compositor places overlays by title
	app     *canvas.Text
	heading *canvas.Text
	body    *canvas.Text
	icon    *canvas.Image

	h       float32 // height: taller with buttons
	x, y    float32 // current position
	anim    int     // generation of the running animation; a newer one stops it
	timer   *time.Timer
	closing bool
}

// toastStack shows the notification popups top-right, newest first. It is
// only used from the Fyne thread.
type toastStack struct {
	toasts []*toast
}

var toasts = &toastStack{}

func initNotificationToasts() {
	wm.AddNotificationListener(func(n *wm.Notification) {
		if n.Urgency == wm.UrgencyLow || wm.HoldPopup() {
			return // still in the history, no popup
		}
		fyne.Do(func() { toasts.show(n) })
	})
	wm.AddWithdrawListener(func(id uint32) {
		fyne.Do(func() {
			if t := toasts.find(id); t != nil {
				toasts.dismiss(t)
			}
		})
	})
}

func (s *toastStack) find(id uint32) *toast {
	for _, t := range s.toasts {
		if t.n.ID == id && !t.closing {
			return t
		}
	}
	return nil
}

// show pops a notification up, or updates the popup of the one it replaces.
func (s *toastStack) show(n *wm.Notification) {
	if t := s.find(n.ID); t != nil && n.Replaced {
		if len(notificationButtons(t.n)) == len(notificationButtons(n)) {
			t.update(n)
			s.startTimer(t)
			return
		}
		s.dismiss(t) // other buttons: another popup
	}

	t := newToast(n)
	s.toasts = append([]*toast{t}, s.toasts...)
	// Too many on screen: the oldest go, they stay in the history.
	shown := 0
	for _, old := range s.toasts {
		if old.closing {
			continue
		}
		if shown++; shown > maxToasts {
			s.dismiss(old)
		}
	}

	x, _ := s.slot(0)
	t.x, t.y = x, -t.h // slides down from above the screen
	t.win.Resize(fyne.NewSize(toastW, t.h))
	wlipc.RequestOverlayPosition(t.title, t.x, t.y, toastW, t.h)
	t.win.Show()
	s.layout()
	s.startTimer(t)
}

// slot returns the position of a toast below others that take height.
func (s *toastStack) slot(above float32) (float32, float32) {
	screen := tyde.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	panelW := wmtheme.WidgetPanelWidth
	if tyde.Instance().Settings().NarrowWidgetPanel() {
		panelW = wmtheme.NarrowBarWidth
	}
	return screenW - toastW - toastMargin - panelW, toastMargin + above
}

// layout moves every toast to its slot.
func (s *toastStack) layout() {
	above := float32(0)
	for _, t := range s.toasts {
		if t.closing {
			continue
		}
		x, y := s.slot(above)
		t.moveTo(x, y, 300*time.Millisecond, easeOutCubic, nil)
		above += t.h + toastGap
	}
}

func (s *toastStack) startTimer(t *toast) {
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.n.Urgency == wm.UrgencyCritical {
		return // stays until clicked or withdrawn
	}
	delay := toastDefault
	if d := time.Duration(t.n.Timeout) * time.Millisecond; d > 0 {
		delay = min(d, toastMax)
	}
	t.timer = time.AfterFunc(delay, func() {
		fyne.Do(func() { s.dismiss(t) })
	})
}

// dismiss slides a toast out to the right, then the others close the gap.
func (s *toastStack) dismiss(t *toast) {
	if t.closing {
		return
	}
	t.closing = true
	if t.timer != nil {
		t.timer.Stop()
	}
	t.moveTo(t.x+toastW+2*toastMargin, t.y, 250*time.Millisecond, easeInCubic, func() {
		t.win.Close()
		for i, o := range s.toasts {
			if o == t {
				s.toasts = append(s.toasts[:i], s.toasts[i+1:]...)
				break
			}
		}
	})
	s.layout()
}

func newToast(n *wm.Notification) *toast {
	t := &toast{n: n, title: fmt.Sprintf("Toast %d %s %s", n.ID, SkipTaskbarHint, NoFocusHint)}
	matrix := isMatrixTheme()

	appColor, titleColor, bodyColor := wmtheme.ToastBody(), wmtheme.ToastTitle(), wmtheme.ToastBody()
	if matrix {
		appColor, titleColor, bodyColor = matrixGreen, matrixBrightGreen, matrixGreen
	}
	t.app = canvas.NewText("", appColor)
	t.app.TextSize = 10
	t.heading = canvas.NewText("", titleColor)
	t.heading.TextStyle = fyne.TextStyle{Bold: true}
	t.heading.TextSize = 13
	t.body = canvas.NewText("", bodyColor)
	t.body.TextSize = 12
	t.icon = &canvas.Image{FillMode: canvas.ImageFillContain}
	t.icon.SetMinSize(fyne.NewSize(24, 24))

	// Left spacer to clear the accent glow
	accentSpacer := canvas.NewRectangle(color.Transparent)
	accentSpacer.SetMinSize(fyne.NewSize(14, 0))
	text := container.NewVBox(t.app, t.heading, t.body)
	inner := container.NewBorder(nil, nil, container.NewHBox(accentSpacer, t.icon), nil, container.NewPadded(text))

	var windowFill, bg *canvas.Rectangle
	if matrix {
		windowFill = canvas.NewRectangle(matrixDarkBg)
		bg = canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x0A, B: 0x00, A: 0xF0})
	} else {
		fr, fg, fb, _ := wmtheme.ToastBackground().RGBA()
		windowFill = canvas.NewRectangle(color.NRGBA{R: uint8(fr >> 8), G: uint8(fg >> 8), B: uint8(fb >> 8), A: 160})
		if glassOn() {
			windowFill.FillColor = color.Transparent // the round glass is all there is
		}
		bg = canvas.NewRectangle(wmtheme.ToastBackground())
	}
	bg.CornerRadius = 8

	borderFrame := canvas.NewRectangle(color.Transparent)
	borderFrame.CornerRadius = 8
	borderFrame.StrokeWidth = 1
	borderFrame.StrokeColor = wmtheme.ToastBorder()
	if matrix {
		borderFrame.StrokeColor = matrixBorder
		borderFrame.StrokeWidth = 2
	}

	t.h = toastH
	if buttons := notificationButtons(n); len(buttons) > 0 {
		t.h += toastButtonsH
		inner = container.NewBorder(nil, toastButtons(t, buttons), nil, nil, inner)
	}
	styled := container.NewStack(windowFill, bg, borderFrame, newGlowAccent(), inner)

	t.win = fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	t.win.SetTransparent(glassOn())
	t.win.SetTitle(t.title)
	t.win.SetPadded(false) // the toast draws its own background
	t.win.SetContent(newTappableBox(styled, func() {
		toasts.dismiss(t)
		activateNotification(t.n)
	}))
	t.update(n)
	return t
}

// update shows the content of n, which may replace the notification shown.
func (t *toast) update(n *wm.Notification) {
	t.n = n
	t.app.Text = n.AppName
	t.app.Hidden = n.AppName == ""
	t.heading.Text = truncateText(n.Title, 38)
	t.body.Text = truncateText(n.Body, 50)
	t.body.Hidden = n.Body == ""
	t.icon.Resource = resolveNotificationIcon(n)
	t.icon.Hidden = t.icon.Resource == nil
	for _, o := range []fyne.CanvasObject{t.app, t.heading, t.body, t.icon} {
		o.Refresh()
	}
}

// moveTo animates the toast to (x, y), then calls done if not nil. Reduce
// motion makes it jump.
func (t *toast) moveTo(x, y float32, dur time.Duration, ease func(float64) float64, done func()) {
	t.anim++
	gen := t.anim
	fromX, fromY := t.x, t.y
	t.x, t.y = x, y
	if tyde.Instance().Settings().ReduceMotion() || (fromX == x && fromY == y) {
		wlipc.RequestOverlayPosition(t.title, x, y, toastW, t.h)
		if done != nil {
			done()
		}
		return
	}

	go func() {
		start := time.Now()
		ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
		defer ticker.Stop()
		for range ticker.C {
			stale := false
			fyne.DoAndWait(func() { stale = t.anim != gen })
			if stale {
				return
			}
			p := float64(time.Since(start)) / float64(dur)
			if p >= 1 {
				wlipc.RequestOverlayPosition(t.title, x, y, toastW, t.h)
				if done != nil {
					fyne.Do(done)
				}
				return
			}
			e := float32(ease(p))
			wlipc.RequestOverlayPosition(t.title, fromX+e*(x-fromX), fromY+e*(y-fromY), toastW, t.h)
		}
	}()
}

// activateNotification opens what a notification is about: its own action
// if it has one, otherwise the application's default action and window.
func activateNotification(n *wm.Notification) {
	if n.OnActivate != nil {
		n.OnActivate()
		return
	}
	// The way GNOME does it: fire the default action (e.g. Slack opening the
	// right channel) then raise or launch the application.
	if notificationHasAction(n, "default") {
		invokeNotificationAction(n, "default")
	}
	activateApp(n.AppName)
}

// toastButtons lays out the answers a notification offers; each dismisses it.
func toastButtons(t *toast, buttons []wm.NotificationButton) fyne.CanvasObject {
	row := container.NewHBox(layout.NewSpacer())
	for _, b := range buttons {
		tap := b.OnTap
		btn := widget.NewButton(truncateText(b.Label, 13), func() {
			toasts.dismiss(t)
			tap()
		})
		btn.Importance = widget.LowImportance
		row.Add(btn)
	}
	return container.NewPadded(row)
}
