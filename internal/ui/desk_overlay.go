package ui

import (
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/tyde"
)

// inputShaper is implemented by window managers that can update X11 input shapes
// on the root and frame windows to control which areas receive mouse events.
type inputShaper interface {
	SetOverlayActive(active bool, regions []image.Rectangle)
}

// backdrop is a full-screen widget that dismisses an overlay on tap or mouse-in
// (mouse-in on the backdrop means the cursor left the overlay content).
// Mouse-out dismiss only activates after the mouse has been inside the content at least once.
type backdrop struct {
	widget.BaseWidget
	onDismiss func()
	armed     bool // true after mouse has entered the content area
}

func (b *backdrop) CreateRenderer() fyne.WidgetRenderer {
	rad := theme.Size(theme.SizeNameModalBlurRadius)
	return widget.NewSimpleRenderer(canvas.NewBlur(rad))
}

func (b *backdrop) Tapped(*fyne.PointEvent) {
	if b.onDismiss != nil {
		b.onDismiss()
	}
}

func (b *backdrop) MouseIn(*deskDriver.MouseEvent) {
	// Only dismiss if the mouse was previously inside the content
	if b.armed && b.onDismiss != nil {
		b.onDismiss()
	}
}

func newBackdrop(onDismiss func()) *backdrop {
	b := &backdrop{onDismiss: onDismiss}
	b.ExtendBaseWidget(b)
	return b
}

// hoverCatch is a wrapper that absorbs hover events, preventing them from
// falling through to the backdrop when the cursor is between child widgets.
// It also arms the backdrop for mouse-out dismiss once the mouse enters.
type hoverCatch struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	backdrop *backdrop
}

func (h *hoverCatch) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewWithoutLayout(h.content))
}

func (h *hoverCatch) MouseIn(*deskDriver.MouseEvent) {
	if h.backdrop != nil {
		h.backdrop.armed = true
	}
}

func newHoverCatch(content fyne.CanvasObject, bg *backdrop) *hoverCatch {
	h := &hoverCatch{content: content, backdrop: bg}
	h.ExtendBaseWidget(h)
	return h
}

// ShowOverlayWithBackdrop shows an overlay with click-outside and mouse-out dismiss.
// catchSize defines the hover-sensitive area (use content size + room for submenus).
// Returns the combined object (backdrop + content) for use with HideOverlay.
func (l *desktop) ShowOverlayWithBackdrop(content fyne.CanvasObject, size fyne.Size, catchSize fyne.Size, pos fyne.Position, contentOffset fyne.Position) fyne.CanvasObject {
	return l.showOverlayWithBackdrop(content, size, catchSize, pos, nil, contentOffset)
}

func (l *desktop) showOverlayWithBackdrop(content fyne.CanvasObject, size fyne.Size, catchSize fyne.Size, pos fyne.Position, focus fyne.Focusable, contentOffset fyne.Position) fyne.CanvasObject {
	var combined fyne.CanvasObject
	dismiss := func() {
		l.HideOverlay(combined)
	}

	bg := newBackdrop(dismiss)
	catch := newHoverCatch(content, bg)
	catch.Resize(catchSize)
	catch.Move(pos)
	content.Move(contentOffset)
	content.Resize(size)
	combined = container.NewStack(bg, container.NewWithoutLayout(catch))

	// Size the combined to fill the full window
	winSize := l.primaryWin.win.Canvas().Size()
	l.showOverlay(combined, winSize, fyne.NewPos(0, 0), focus)
	return combined
}

// ShowModal centres content above a blurred, full-screen backdrop. Unlike the
// menu overlays it does not dismiss on background tap or mouse-out (the backdrop
// has no dismiss action), so the modal stays until the caller invokes the
// returned hide function — typically from a button inside the content.
func (l *desktop) ShowModal(content fyne.CanvasObject, size fyne.Size) func() {
	winSize := l.primaryWin.win.Canvas().Size()
	pos := fyne.NewPos((winSize.Width-size.Width)/2, (winSize.Height-size.Height)/2)

	bg := newBackdrop(nil) // nil dismiss => modal: no tap or mouse-out dismissal
	content.Move(pos)
	content.Resize(size)
	combined := container.NewStack(bg, container.NewWithoutLayout(content))

	l.showOverlay(combined, winSize, fyne.NewPos(0, 0), nil)
	return func() { l.HideOverlay(combined) }
}

// ShowOverlay adds content to the desktop overlay layer, above all chrome and windows.
// The root window's input shape is expanded and frame input shapes are cleared
// so that Fyne receives mouse events for the overlay.
func (l *desktop) ShowOverlay(content fyne.CanvasObject, size fyne.Size, pos fyne.Position) {
	l.showOverlay(content, size, pos, nil)
}

func (l *desktop) showOverlay(content fyne.CanvasObject, size fyne.Size, pos fyne.Position, focus fyne.Focusable) {
	overlay := l.primaryWin.overlay
	win := l.primaryWin.win
	fyne.Do(func() {
		content.Resize(size)
		content.Move(pos)
		overlay.Add(content)
		overlay.Refresh()

		if l.overlayShapes == nil {
			l.overlayShapes = map[fyne.CanvasObject]image.Rectangle{}
		}
		l.overlayShapes[content] = l.overlayRegion(pos, size)
		l.applyOverlayShapes()

		if focus != nil {
			win.Canvas().Focus(focus)
		}
	})
}

// overlayRegion converts an overlay's canvas position and size into the screen-pixel
// rectangle it covers on the primary screen. Callers pass the overlay's resting
// position, so an overlay that animates into place still reports its final area.
func (l *desktop) overlayRegion(pos fyne.Position, size fyne.Size) image.Rectangle {
	screen := l.screens.Primary()
	if screen == nil {
		return image.Rectangle{}
	}

	scale := screen.CanvasScale()
	return image.Rect(
		screen.X+int(pos.X*scale),
		screen.Y+int(pos.Y*scale),
		screen.X+int((pos.X+size.Width)*scale),
		screen.Y+int((pos.Y+size.Height)*scale),
	)
}

// applyOverlayShapes pushes the union of the current overlay rectangles to the window
// manager, or clears the overlay state when no overlays remain.
func (l *desktop) applyOverlayShapes() {
	is, ok := l.wm.(inputShaper)
	if !ok {
		return
	}

	regions := make([]image.Rectangle, 0, len(l.overlayShapes)+1)
	for _, r := range l.overlayShapes {
		regions = append(regions, r)
	}

	// A Fyne canvas overlay (dialog or pop-up) is modal over the whole canvas, so
	// rather than measuring its content it claims the entire primary screen.
	if l.canvasOverlay {
		if screen := l.screens.Primary(); screen != nil {
			regions = append(regions, image.Rect(screen.X, screen.Y,
				screen.X+screen.Width, screen.Y+screen.Height))
		}
	}

	if len(regions) == 0 {
		is.SetOverlayActive(false, nil)
		return
	}
	is.SetOverlayActive(true, regions)
}

// HideOverlay removes content from the desktop overlay layer.
// When no overlays remain, input shapes are restored to normal.
func (l *desktop) HideOverlay(content fyne.CanvasObject) {
	overlay := l.primaryWin.overlay
	fyne.Do(func() {
		overlay.Remove(content)
		overlay.Refresh()

		delete(l.overlayShapes, content)
		l.applyOverlayShapes()
	})
}

// overlayLayer is the full-screen, visual-only layer that renders the widgets
// of any OverlayAreaModule above application windows. It can be rebuilt at
// runtime (rebuild) so that enabling or disabling such a module takes effect
// immediately. Wired to the primary screen only.
type overlayLayer struct {
	widget.BaseWidget
	desk    *desktop
	content *fyne.Container
}

func newOverlayLayer(d *desktop) *overlayLayer {
	o := &overlayLayer{desk: d, content: container.NewStack()}
	o.ExtendBaseWidget(o)
	o.rebuild()
	return o
}

func (o *overlayLayer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(o.content)
}

// rebuild repopulates the layer from the currently enabled overlay-area
// modules. Calling OverlayAreaWidget on a freshly created module instance is
// what starts it (e.g. a desktop pet's animation loop); previous instances
// were already torn down via Module.Destroy in clearModuleCache.
func (o *overlayLayer) rebuild() {
	var objs []fyne.CanvasObject
	for _, m := range o.desk.Modules() {
		if om, ok := m.(tyde.OverlayAreaModule); ok {
			if w := om.OverlayAreaWidget(); w != nil {
				objs = append(objs, w)
			}
		}
	}
	o.content.Objects = objs
	o.content.Refresh()
}
