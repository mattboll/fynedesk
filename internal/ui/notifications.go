package ui

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/icon"
	"fyshos.com/fynedesk/locale"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
	"fyshos.com/fynedesk/wm"
)

// isMatrixTheme returns true if the current background type is "matrix".
func isMatrixTheme() bool {
	return fyne.CurrentApp().Preferences().String("background_type") == "matrix"
}

// Matrix theme colors
var (
	matrixGreen       = color.NRGBA{R: 0x00, G: 0xCC, B: 0x66, A: 0xFF}
	matrixBrightGreen = color.NRGBA{R: 0xCC, G: 0xFF, B: 0xCC, A: 0xFF}
	matrixDarkBg      = color.NRGBA{R: 0x00, G: 0x08, B: 0x00, A: 0xE0}
	matrixBorder      = color.NRGBA{R: 0x00, G: 0x66, B: 0x33, A: 0xFF}
)

// --- Toast overlay (always splash window, top-right) ---

func initNotificationToasts() {
	wm.AddNotificationListener(func(n *wm.Notification) {
		if wm.DoNotDisturb() {
			return // DND: still in history, just suppress toast
		}
		fyne.Do(func() {
			showToast(n)
		})
	})
}

func showToast(n *wm.Notification) {
	matrix := isMatrixTheme()

	// App name — subtle secondary text above the title
	textItems := []fyne.CanvasObject{}
	if n.AppName != "" {
		appColor := wmtheme.ToastBody()
		if matrix {
			appColor = matrixGreen
		}
		appLabel := canvas.NewText(n.AppName, appColor)
		appLabel.TextSize = 10
		textItems = append(textItems, appLabel)
	}

	// Title — bright text for high contrast on dark background
	titleStr := truncateText(n.Title, 38)
	titleColor := wmtheme.ToastTitle()
	if matrix {
		titleColor = matrixBrightGreen
	}
	title := canvas.NewText(titleStr, titleColor)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.TextSize = 13
	textItems = append(textItems, title)

	// Body — soft secondary text, only if non-empty
	if n.Body != "" {
		bodyStr := truncateText(n.Body, 50)
		bodyColor := wmtheme.ToastBody()
		if matrix {
			bodyColor = matrixGreen
		}
		body := canvas.NewText(bodyStr, bodyColor)
		body.TextSize = 12
		textItems = append(textItems, body)
	}

	text := container.NewVBox(textItems...)

	// Try to load an icon for the notification
	var iconWidget fyne.CanvasObject
	if res := resolveNotificationIcon(n); res != nil {
		img := canvas.NewImageFromResource(res)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(24, 24))
		iconWidget = img
	}

	// Left spacer to clear accent glow area
	accentSpacer := canvas.NewRectangle(color.Transparent)
	accentSpacer.SetMinSize(fyne.NewSize(14, 0))
	var leftItems fyne.CanvasObject
	if iconWidget != nil {
		leftItems = container.NewHBox(accentSpacer, iconWidget)
	} else {
		leftItems = accentSpacer
	}
	inner := container.NewBorder(nil, nil, leftItems, nil, container.NewPadded(text))

	// Semi-transparent fill
	var windowFill *canvas.Rectangle
	if matrix {
		windowFill = canvas.NewRectangle(matrixDarkBg)
	} else {
		toastBg := wmtheme.ToastBackground()
		fr, fg, fb, _ := toastBg.RGBA()
		windowFill = canvas.NewRectangle(color.NRGBA{R: uint8(fr >> 8), G: uint8(fg >> 8), B: uint8(fb >> 8), A: 160})
	}

	// Rounded dark background
	var bg *canvas.Rectangle
	if matrix {
		bg = canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x0A, B: 0x00, A: 0xF0})
	} else {
		bg = canvas.NewRectangle(wmtheme.ToastBackground())
	}
	bg.CornerRadius = 8

	// Subtle border outline
	borderFrame := canvas.NewRectangle(color.Transparent)
	borderFrame.CornerRadius = 8
	borderFrame.StrokeWidth = 1
	if matrix {
		borderFrame.StrokeColor = matrixBorder
		borderFrame.StrokeWidth = 2
	} else {
		borderFrame.StrokeColor = wmtheme.ToastBorder()
	}

	// Glow accent bar on left edge
	accent := newGlowAccent()

	styled := container.NewStack(windowFill, bg, borderFrame, accent, inner)

	win := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	win.SetTitle("Toast " + SkipTaskbarHint + " " + NoFocusHint)
	win.SetPadded(false) // Remove Fyne padding — the toast renders its own background

	tappable := newTappableBox(styled, func() {
		win.Close()
		// Open the app the way GNOME does: fire the notification's default
		// action (e.g. Slack opening the right channel) then raise/launch it.
		if notificationHasAction(n, "default") {
			invokeNotificationAction(n, "default")
		}
		activateApp(n.AppName)
	})
	win.SetContent(tappable)

	toastW := float32(320)
	toastH := float32(76)
	toastSize := fyne.NewSize(toastW, toastH)
	screen := fynedesk.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()
	panelW := wmtheme.WidgetPanelWidth
	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		panelW = wmtheme.NarrowBarWidth
	}
	finalX := screenW - toastW - 10 - panelW
	finalY := float32(10)
	startY := -toastH // above the screen

	win.Resize(toastSize)
	wlipc.RequestOverlayPosition(win.Title(), finalX, startY, toastW, toastH)
	win.Show()

	// Slide-down animation (300ms ease-out-cubic) — skip if reduce motion
	if fynedesk.Instance().Settings().ReduceMotion() {
		wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, toastW, toastH)
	} else {
		go func() {
			dur := 300 * time.Millisecond
			start := time.Now()
			ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
			defer ticker.Stop()

			for range ticker.C {
				t := float64(time.Since(start)) / float64(dur)
				if t >= 1 {
					wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, toastW, toastH)
					break
				}
				y := startY + float32(easeOutCubic(t))*float32(finalY-startY)
				wlipc.RequestOverlayPosition(win.Title(), finalX, y, toastW, toastH)
			}
		}()
	}

	// Auto-dismiss with slide-up
	delay := 4 * time.Second
	if n.Timeout > 0 {
		d := time.Duration(n.Timeout) * time.Millisecond
		if d < delay {
			delay = d
		}
	}
	log.Printf("[TOAST] showing %q delay=%v\n", n.Title, delay)

	time.AfterFunc(delay, func() {
		if fynedesk.Instance().Settings().ReduceMotion() {
			fyne.Do(func() { win.Close() })
			return
		}
		// Slide-up animation (250ms ease-in-cubic)
		go func() {
			dur := 250 * time.Millisecond
			start := time.Now()
			ticker := time.NewTicker(16 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				t := float64(time.Since(start)) / float64(dur)
				if t >= 1 {
					fyne.Do(func() { win.Close() })
					return
				}
				y := finalY + float32(easeInCubic(t))*float32(startY-finalY)
				wlipc.RequestOverlayPosition(win.Title(), finalX, y, toastW, toastH)
			}
		}()
	})
}

func easeOutCubic(t float64) float64 {
	return 1 - math.Pow(1-t, 3)
}

func easeInCubic(t float64) float64 {
	return t * t * t
}

func truncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen-1]) + "\u2026"
	}
	return s
}

// resolveNotificationIcon tries to find an icon resource for a notification.
// It first tries the IconName as an FDO icon theme lookup via the icon provider,
// then falls back to the AppName.
func resolveNotificationIcon(n *wm.Notification) fyne.Resource {
	desk := fynedesk.Instance()
	if desk == nil || desk.IconProvider() == nil {
		return nil
	}
	provider := desk.IconProvider()

	// Try icon name first (D-Bus appIcon parameter)
	if n.IconName != "" {
		apps := provider.FindAppsMatching(n.IconName)
		if len(apps) > 0 {
			if res := apps[0].Icon("", 32); res != nil {
				return res
			}
		}
	}

	// Fall back to app name lookup
	if n.AppName != "" {
		app := icon.FindAppByName(n.AppName, provider)
		if app != nil {
			if res := app.Icon("", 32); res != nil {
				return res
			}
		}
	}
	return nil
}

// glowAccent draws a vertical accent line on the left edge with a soft horizontal glow.
type glowAccent struct {
	widget.BaseWidget
	raster *canvas.Raster
}

func newGlowAccent() *glowAccent {
	g := &glowAccent{}
	g.raster = canvas.NewRaster(g.draw)
	g.raster.ScaleMode = canvas.ImageScalePixels
	g.ExtendBaseWidget(g)
	return g
}

func (g *glowAccent) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.raster)
}

func (g *glowAccent) draw(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w < 4 || h < 16 {
		return img
	}

	pad := 8 // vertical padding to clear rounded corners

	// Get accent color from theme
	accentCol := wmtheme.AccentGlow()
	r, gc, b, _ := accentCol.RGBA()
	ar, ag, ab := uint8(r>>8), uint8(gc>>8), uint8(b>>8)

	for y := pad; y < h-pad; y++ {
		// Core accent line (3px)
		for x := 0; x < 3 && x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: ar, G: ag, B: ab, A: 220})
		}
		// Soft glow fade (12px) — quadratic falloff
		for x := 3; x < 15 && x < w; x++ {
			frac := 1.0 - float64(x-3)/12.0
			alpha := uint8(40.0 * frac * frac)
			img.SetNRGBA(x, y, color.NRGBA{R: ar, G: ag, B: ab, A: alpha})
		}
	}

	return img
}

// tappableBox is a container that responds to tap events for click-to-dismiss.
type tappableBox struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	onTapped func()
}

func newTappableBox(content fyne.CanvasObject, onTapped func()) *tappableBox {
	t := &tappableBox{content: content, onTapped: onTapped}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tappableBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

func (t *tappableBox) Tapped(_ *fyne.PointEvent) {
	if t.onTapped != nil {
		t.onTapped()
	}
}

// --- Notification panel widget (bell + badge + list + clear all) ---

type notificationPanel struct {
	widget.BaseWidget

	mu           sync.Mutex
	expanded     bool
	overlay      fyne.Window
	overlayItems *fyne.Container // list container inside the narrow overlay (nil when closed)

	// Per-notification expand state (keyed by local notif ID), so a row keeps
	// its expanded/collapsed state across list rebuilds triggered by refresh.
	expandedIDs map[uint32]bool

	badge    *canvas.Circle
	bellBtn  *widget.Button
	header   fyne.CanvasObject
	listBox  *fyne.Container
	scroll   *container.Scroll
	clearBtn *widget.Button
	body     fyne.CanvasObject // border(nil, clearBtn, nil, nil, scroll)

	refreshTimer *time.Timer // debounce rapid history changes
}

func newNotificationPanel() *notificationPanel {
	p := &notificationPanel{expandedIDs: map[uint32]bool{}}
	p.ExtendBaseWidget(p)

	// Badge dot
	p.badge = canvas.NewCircle(wmtheme.BadgeColor())
	p.badge.Hide()

	// Bell button
	p.bellBtn = &widget.Button{
		Icon:       wmtheme.NotificationsIcon,
		Importance: widget.LowImportance,
		OnTapped:   p.onBellTapped,
	}

	p.header = container.New(&badgeLayout{}, p.bellBtn, p.badge)

	// List
	p.listBox = container.NewVBox()
	p.scroll = container.NewVScroll(p.listBox)
	p.scroll.SetMinSize(fyne.NewSize(0, 100))

	// Clear all
	p.clearBtn = widget.NewButton(locale.T("notif.clearAll"), func() {
		wm.ClearNotificationHistory()
	})
	p.clearBtn.Importance = widget.LowImportance

	p.body = container.NewBorder(nil, container.NewCenter(p.clearBtn), nil, nil, p.scroll)
	p.body.Hide()

	// Listen for history changes (debounced: coalesce rapid mutations into one refresh)
	wm.AddHistoryChangeListener(func() {
		p.mu.Lock()
		if p.refreshTimer != nil {
			p.refreshTimer.Stop()
		}
		p.refreshTimer = time.AfterFunc(50*time.Millisecond, func() {
			fyne.Do(p.refresh)
		})
		p.mu.Unlock()
	})

	// Listen for new notifications to show badge
	wm.AddNotificationListener(func(n *wm.Notification) {
		fyne.Do(func() {
			p.badge.Show()
			p.badge.Refresh()
		})
	})

	return p
}

func (p *notificationPanel) updateLocale() {
	p.clearBtn.SetText(locale.T("notif.clearAll"))
	p.refresh()
}

func (p *notificationPanel) refresh() {
	groups := wm.GroupedNotificationHistory()

	p.listBox.Objects = nil
	for _, g := range groups {
		p.listBox.Add(p.buildGroupRow(g))
	}

	if len(groups) == 0 {
		p.badge.Hide()
		p.clearBtn.Hide()
		if p.expanded {
			empty := widget.NewLabel(locale.T("notif.noNotifications"))
			empty.Alignment = fyne.TextAlignCenter
			p.listBox.Add(container.NewCenter(empty))
		}
	} else {
		p.clearBtn.Show()
	}

	p.listBox.Refresh()

	// Keep the narrow overlay's list in sync (expand toggles, removals) while
	// it stays open, so it reflects the same state as the inline list.
	if p.overlayItems != nil && len(groups) > 0 {
		p.overlayItems.Objects = nil
		for _, g := range groups {
			p.overlayItems.Add(p.buildGroupRow(g))
		}
		p.overlayItems.Refresh()
	}

	// If the narrow overlay is open and we cleared everything, close it.
	// Don't recreate the overlay from here — it causes IPC calls on the event
	// loop that can block if the compositor is busy. The user can reopen it.
	if p.overlay != nil && len(groups) == 0 {
		p.overlay.Close()
		p.overlay = nil
	}
}

// notificationLeftIcon returns a tappable app-icon widget that opens the
// notification's app when clicked, or nil if no icon could be resolved.
func (p *notificationPanel) notificationLeftIcon(n *wm.Notification) fyne.CanvasObject {
	res := resolveNotificationIcon(n)
	if res == nil {
		return nil
	}
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(24, 24))
	return newTappableBox(img, func() { p.openAndDismiss(n) })
}

// buildGroupRow creates a UI row for a notification group.
// Single notifications render as one row; groups of 2+ show a collapsible header
// with the app name and count, expanding to the individual notifications.
func (p *notificationPanel) buildGroupRow(g *wm.NotificationGroup) fyne.CanvasObject {
	if len(g.Notifications) == 1 {
		return p.buildNotificationRow(g.Notifications[0])
	}

	latest := g.Notifications[0]
	gid := latest.ID // stable key for the group's expand state while it exists
	p.mu.Lock()
	expanded := p.expandedIDs[gid]
	p.mu.Unlock()

	label := g.AppName
	if label == "" {
		label = latest.Title
	}
	headerLabel := widget.NewLabel(fmt.Sprintf("%s (%d)", label, len(g.Notifications)))
	headerLabel.TextStyle = fyne.TextStyle{Bold: true}
	headerLabel.Truncation = fyne.TextTruncateEllipsis

	// Dismiss all in group
	removeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		for _, n := range g.Notifications {
			wm.RemoveNotification(n.ID)
		}
	})
	removeBtn.Importance = widget.LowImportance

	headerTap := newTappableBox(headerLabel, func() { p.toggleExpand(gid) })
	headerRow := container.NewBorder(nil, nil, p.notificationLeftIcon(latest), removeBtn, headerTap)

	if !expanded {
		ts := latest.Timestamp.Format("15:04")
		preview := widget.NewLabel(fmt.Sprintf("%s  %s", ts, latest.Title))
		preview.Truncation = fyne.TextTruncateEllipsis
		return container.NewVBox(headerRow, newTappableBox(preview, func() { p.toggleExpand(gid) }))
	}

	items := []fyne.CanvasObject{headerRow}
	for _, n := range g.Notifications {
		items = append(items, p.buildNotificationRow(n))
	}
	return container.NewVBox(items...)
}

// buildNotificationRow renders a single notification entry. Collapsed, it shows
// the truncated title; tapping the title expands it to show the full title and
// body (word-wrapped) plus action buttons. The app icon (left) opens the app.
func (p *notificationPanel) buildNotificationRow(n *wm.Notification) fyne.CanvasObject {
	id := n.ID
	p.mu.Lock()
	expanded := p.expandedIDs[id]
	p.mu.Unlock()

	ts := n.Timestamp.Format("15:04")
	prefix := ts
	if n.AppName != "" {
		prefix = fmt.Sprintf("%s  %s", ts, n.AppName)
	}
	title := widget.NewLabel(fmt.Sprintf("%s  %s", prefix, n.Title))
	title.TextStyle = fyne.TextStyle{Bold: true}
	if expanded {
		title.Wrapping = fyne.TextWrapWord
	} else {
		title.Truncation = fyne.TextTruncateEllipsis
	}

	removeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		wm.RemoveNotification(id)
	})
	removeBtn.Importance = widget.LowImportance

	titleTap := newTappableBox(title, func() { p.toggleExpand(id) })
	header := container.NewBorder(nil, nil, p.notificationLeftIcon(n), removeBtn, titleTap)

	if !expanded {
		return header
	}

	items := []fyne.CanvasObject{header}
	if n.Body != "" {
		body := widget.NewLabel(n.Body)
		body.Wrapping = fyne.TextWrapWord
		items = append(items, body)
	}

	// D-Bus action buttons (excluding the body-click "default" action), plus an
	// explicit Open button. Each dismisses the notification after firing.
	var buttons []fyne.CanvasObject
	for i := 0; i+1 < len(n.Actions) && len(buttons) < 2; i += 2 {
		key, actionLabel := n.Actions[i], n.Actions[i+1]
		if key == "default" || actionLabel == "" {
			continue
		}
		k := key
		btn := widget.NewButton(actionLabel, func() {
			invokeNotificationAction(n, k)
			wm.RemoveNotification(n.ID)
		})
		btn.Importance = widget.LowImportance
		buttons = append(buttons, btn)
	}
	openBtn := widget.NewButton(locale.T("notif.open"), func() { p.openAndDismiss(n) })
	openBtn.Importance = widget.LowImportance
	buttons = append(buttons, openBtn)
	items = append(items, container.NewHBox(buttons...))

	return container.NewVBox(items...)
}

// toggleExpand flips a row's expanded state and rebuilds the visible lists.
func (p *notificationPanel) toggleExpand(id uint32) {
	p.mu.Lock()
	p.expandedIDs[id] = !p.expandedIDs[id]
	p.mu.Unlock()
	p.refresh()
}

// openAndDismiss activates a notification the way GNOME does: it invokes the
// "default" action if the source app provided one (e.g. Slack navigating to the
// right channel), raises or launches the app, and removes it from history.
func (p *notificationPanel) openAndDismiss(n *wm.Notification) {
	if notificationHasAction(n, "default") {
		invokeNotificationAction(n, "default")
	}
	activateApp(n.AppName)
	wm.RemoveNotification(n.ID)
}

// notificationHasAction reports whether the notification carries the given action key.
func notificationHasAction(n *wm.Notification, key string) bool {
	for i := 0; i+1 < len(n.Actions); i += 2 {
		if n.Actions[i] == key {
			return true
		}
	}
	return false
}

// invokeNotificationAction triggers a notification action on whichever process
// owns the org.freedesktop.Notifications bus name. In Wayland mode the compositor
// owns it (n.DBusID != 0), so the request is routed there to emit ActionInvoked
// with a matching signal sender; otherwise (X11/local) it is emitted directly.
func invokeNotificationAction(n *wm.Notification, key string) {
	if n.DBusID != 0 {
		if err := wlipc.RequestNotificationAction(n.DBusID, key); err != nil {
			log.Printf("[NOTIF] action %q failed: %v", key, err)
		}
		return
	}
	wm.InvokeAction(n.ID, key)
}

func (p *notificationPanel) onBellTapped() {
	// Reset badge
	p.badge.Hide()
	p.badge.Refresh()

	if fynedesk.Instance().Settings().NarrowWidgetPanel() {
		p.toggleNarrowOverlay()
	} else {
		p.toggleWideExpand()
	}
}

func (p *notificationPanel) toggleWideExpand() {
	p.mu.Lock()
	p.expanded = !p.expanded
	expanded := p.expanded
	p.mu.Unlock()

	if expanded {
		p.body.Show()
	} else {
		p.body.Hide()
	}
	p.refresh()
	p.Refresh()
}

func (p *notificationPanel) toggleNarrowOverlay() {
	if p.overlay != nil {
		p.overlay.Close()
		p.overlay = nil
		p.overlayItems = nil
		return
	}

	p.showNarrowOverlay()
}

func (p *notificationPanel) showNarrowOverlay() {
	groups := wm.GroupedNotificationHistory()

	var content fyne.CanvasObject
	items := container.NewVBox()
	p.overlayItems = items
	if len(groups) == 0 {
		empty := widget.NewLabel(locale.T("notif.noNotifications"))
		empty.Alignment = fyne.TextAlignCenter
		content = container.NewCenter(empty)
	} else {
		for _, g := range groups {
			items.Add(p.buildGroupRow(g))
		}

		clearBtn := widget.NewButton(locale.T("notif.clearAll"), func() {
			// Close overlay first, before firing history listeners,
			// to avoid the debounced refresh trying to close it again.
			if p.overlay != nil {
				win := p.overlay
				p.overlay = nil
				p.overlayItems = nil
				win.Close()
			}
			wm.ClearNotificationHistory()
		})
		clearBtn.Importance = widget.LowImportance

		content = container.NewBorder(nil, container.NewCenter(clearBtn), nil, nil,
			container.NewVScroll(items))
	}

	headerLabel := widget.NewLabel(locale.T("notif.title"))
	headerLabel.TextStyle = fyne.TextStyle{Bold: true}
	header := container.NewHBox(headerLabel, layout.NewSpacer())

	panel := container.NewBorder(header, nil, nil, nil, content)

	win := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	win.SetTitle("Notifications " + SkipTaskbarHint)
	win.SetContent(panel)
	win.SetOnClosed(func() {
		p.overlay = nil
		p.overlayItems = nil
	})
	p.overlay = win

	screen := fynedesk.Instance().Screens().Primary()
	screenW := float32(screen.Width) / screen.CanvasScale()

	overlayW := float32(300)
	overlayH := float32(400)
	finalX := screenW - overlayW - 10 - wmtheme.NarrowBarWidth
	finalY := float32(10)
	startY := float32(-overlayH) // slide down from above

	win.Resize(fyne.NewSize(overlayW, overlayH))
	wlipc.RequestOverlayPosition(win.Title(), finalX, startY, overlayW, overlayH)
	win.Show()

	if !fynedesk.Instance().Settings().ReduceMotion() {
		go func() {
			dur := 250 * time.Millisecond
			start := time.Now()
			ticker := time.NewTicker(16 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				t := float64(time.Since(start)) / float64(dur)
				if t >= 1 {
					wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, overlayW, overlayH)
					return
				}
				y := startY + float32(easeOutCubic(t))*(finalY-startY)
				wlipc.RequestOverlayPosition(win.Title(), finalX, y, overlayW, overlayH)
			}
		}()
	} else {
		wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, overlayW, overlayH)
	}
}

func (p *notificationPanel) CreateRenderer() fyne.WidgetRenderer {
	content := container.NewVBox(p.header, p.body)
	return widget.NewSimpleRenderer(content)
}

// activateApp attempts to focus/raise a window belonging to the given application name.
// It searches all managed windows by class and title (case-insensitive). If a matching
// window is found, it is raised and focused. If the window is on a different virtual
// desktop, the desktop is switched first. If no window is found, it tries to launch
// the application via the icon provider.
func activateApp(appName string) {
	if appName == "" {
		return
	}
	desk := fynedesk.Instance()
	if desk == nil || desk.WindowManager() == nil {
		return
	}

	nameLower := strings.ToLower(appName)
	for _, w := range desk.WindowManager().Windows() {
		props := w.Properties()
		if props == nil {
			continue
		}

		// Match against window class entries
		matched := false
		for _, class := range props.Class() {
			if strings.EqualFold(class, appName) || strings.Contains(strings.ToLower(class), nameLower) {
				matched = true
				break
			}
		}
		// Fall back to title match
		if !matched && strings.Contains(strings.ToLower(props.Title()), nameLower) {
			matched = true
		}

		if matched {
			// Switch desktop if needed
			if w.Desktop() != desk.Desktop() && !w.Pinned() {
				desk.SetDesktop(w.Desktop())
			}
			if w.Iconic() {
				w.Uniconify()
			}
			w.RaiseToTop()
			w.Focus()
			return
		}
	}

	// No matching window found — try to launch the app
	if provider := desk.IconProvider(); provider != nil {
		apps := provider.FindAppsMatching(appName)
		if len(apps) > 0 {
			_ = desk.RunApp(apps[0])
		}
	}
}

// badgeLayout positions an 8px badge dot at the top-right corner of the first object.
type badgeLayout struct{}

func (b *badgeLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	objects[0].Resize(size)
	objects[0].Move(fyne.NewPos(0, 0))

	dotSize := float32(8)
	objects[1].Resize(fyne.NewSize(dotSize, dotSize))
	objects[1].Move(fyne.NewPos(size.Width-dotSize-2, 2))
}

func (b *badgeLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(theme.IconInlineSize()+theme.InnerPadding()*2, theme.IconInlineSize()+theme.InnerPadding()*2)
}
