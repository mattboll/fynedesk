package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"log"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"
)

// previewItem holds data for a single window preview thumbnail.
type previewItem struct {
	img   image.Image
	title string
	winID string
}

// previewPopup manages a floating window preview tooltip for taskbar icons.
type previewPopup struct {
	mu         sync.Mutex
	win        fyne.Window
	timer      *time.Timer
	windowID   string // currently hovered window ID
	outputInfo outputInfo
	iconPos    fyne.Position // position of the hovered icon (for left-bar alignment)
	iconSize   fyne.Size     // size of the hovered icon
}

var preview = &previewPopup{}

// requestGroupPreview starts a timer to show previews for a group of windows.
func (p *previewPopup) requestGroupPreview(windowIDs []string) {
	if len(windowIDs) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	key := windowIDs[0]
	if p.windowID == key && p.win != nil {
		return // already showing this preview
	}

	p.cancelLocked()
	p.windowID = key

	p.timer = time.AfterFunc(200*time.Millisecond, func() {
		if len(windowIDs) == 1 {
			p.fetchAndShow(windowIDs[0])
		} else {
			p.fetchAndShowGroup(windowIDs)
		}
	})
}

// hide cancels any pending preview and hides the current one.
func (p *previewPopup) hide() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelLocked()
}

func (p *previewPopup) cancelLocked() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if p.win != nil {
		win := p.win
		p.win = nil
		fyne.Do(func() { win.Close() })
	}
	p.windowID = ""
}

// fetchAndShow requests a thumbnail from the compositor and displays it.
// Retries once after a short delay if the compositor hasn't captured yet.
func (p *previewPopup) fetchAndShow(windowID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PREVIEW] PANIC in fetchAndShow: %v", r)
		}
	}()
	log.Printf("[PREVIEW] fetchAndShow: windowID=%s", windowID)
	client := wlipc.DefaultClient()
	if client == nil {
		log.Println("[PREVIEW] fetchAndShow: no IPC client")
		return
	}

	req := struct {
		WindowID string `json:"window_id"`
	}{WindowID: windowID}

	resp, err := client.Request(wlipc.ReqWindowPreview, req)
	respName := "<nil>"
	respLen := 0
	if resp != nil {
		respName = resp.Name
		respLen = len(resp.Data)
	}
	log.Printf("[PREVIEW] fetchAndShow: got response for %s: err=%v name=%q dataLen=%d", windowID, err, respName, respLen)
	if err != nil || resp == nil || resp.Name == "error" {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else if resp != nil {
			errMsg = string(resp.Data)
		}
		log.Printf("[PREVIEW] first request failed for %s: %s", windowID, errMsg)
		// Compositor may not have captured yet — wait for next render frame and retry
		time.Sleep(600 * time.Millisecond)
		p.mu.Lock()
		if p.windowID != windowID {
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		resp, err = client.Request(wlipc.ReqWindowPreview, req)
		if err != nil || resp == nil || resp.Name == "error" {
			log.Printf("[PREVIEW] retry also failed for %s", windowID)
			return
		}
	}

	var data struct {
		WindowID string `json:"window_id"`
		Title    string `json:"title"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		PNG      string `json:"png"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		log.Printf("[PREVIEW] parse error: %v\n", err)
		return
	}

	// Decode base64 PNG
	pngData, err := base64.StdEncoding.DecodeString(data.PNG)
	if err != nil {
		log.Printf("[PREVIEW] base64 decode error: %v", err)
		return
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		log.Printf("[PREVIEW] png decode error: %v", err)
		return
	}

	p.mu.Lock()
	if p.windowID != windowID {
		p.mu.Unlock()
		log.Printf("[PREVIEW] hover moved away: p.windowID=%s windowID=%s", p.windowID, windowID)
		return // hover moved away while fetching
	}
	p.mu.Unlock()

	log.Printf("[PREVIEW] scheduling showWindow for %s (img %dx%d)", windowID, img.Bounds().Dx(), img.Bounds().Dy())
	fyne.Do(func() {
		p.showWindow(img, data.Title)
	})
}

// fetchAndShowGroup fetches thumbnails for multiple windows and shows them side by side.
func (p *previewPopup) fetchAndShowGroup(windowIDs []string) {
	client := wlipc.DefaultClient()
	if client == nil {
		return
	}

	// First pass: request all previews, schedule captures for missing ones
	var previews []previewItem
	var retryIDs []string
	for _, wid := range windowIDs {
		req := struct {
			WindowID string `json:"window_id"`
		}{WindowID: wid}

		resp, err := client.Request(wlipc.ReqWindowPreview, req)
		if err != nil || resp == nil || resp.Name == "error" {
			retryIDs = append(retryIDs, wid)
			continue
		}

		var data struct {
			WindowID string `json:"window_id"`
			Title    string `json:"title"`
			PNG      string `json:"png"`
		}
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			continue
		}

		pngData, err := base64.StdEncoding.DecodeString(data.PNG)
		if err != nil {
			continue
		}
		img, err := png.Decode(bytes.NewReader(pngData))
		if err != nil {
			continue
		}

		previews = append(previews, previewItem{img: img, title: data.Title, winID: wid})
	}

	// Retry failed windows after a delay (compositor may need a render frame to capture)
	if len(retryIDs) > 0 && len(previews) < len(windowIDs) {
		time.Sleep(600 * time.Millisecond)
		p.mu.Lock()
		stillActive := p.windowID == windowIDs[0]
		p.mu.Unlock()
		if stillActive {
			for _, wid := range retryIDs {
				req := struct {
					WindowID string `json:"window_id"`
				}{WindowID: wid}
				resp, err := client.Request(wlipc.ReqWindowPreview, req)
				if err != nil || resp == nil || resp.Name == "error" {
					continue
				}
				var data struct {
					WindowID string `json:"window_id"`
					Title    string `json:"title"`
					PNG      string `json:"png"`
				}
				if err := json.Unmarshal(resp.Data, &data); err != nil {
					continue
				}
				pngData, err := base64.StdEncoding.DecodeString(data.PNG)
				if err != nil {
					continue
				}
				img, err := png.Decode(bytes.NewReader(pngData))
				if err != nil {
					continue
				}
				previews = append(previews, previewItem{img: img, title: data.Title, winID: wid})
			}
		}
	}

	if len(previews) == 0 {
		return
	}

	p.mu.Lock()
	if p.windowID != windowIDs[0] {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	fyne.Do(func() {
		p.showGroupWindow(previews)
	})
}

// showGroupWindow creates a preview window showing multiple window thumbnails.
func (p *previewPopup) showGroupWindow(previews []previewItem) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.win != nil {
		p.win.Close()
	}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}

	win := d.CreateSplashWindow()
	win.SetTitle("Preview " + SkipTaskbarHint)
	win.SetPadded(true)

	items := make([]fyne.CanvasObject, 0, len(previews))
	for _, pv := range previews {
		raster := canvas.NewImageFromImage(pv.img)
		raster.FillMode = canvas.ImageFillContain
		raster.SetMinSize(fyne.NewSize(
			float32(pv.img.Bounds().Dx()),
			float32(pv.img.Bounds().Dy()),
		))

		titleLabel := widget.NewLabel(pv.title)
		titleLabel.Truncation = fyne.TextTruncateEllipsis
		titleLabel.Alignment = fyne.TextAlignCenter

		winID := pv.winID
		tapBox := newTappableBox(
			container.NewBorder(nil, titleLabel, nil, nil, raster),
			func() {
				wlipc.RequestWindowAction(winID, "focus")
				p.hide()
			},
		)
		items = append(items, tapBox)
	}

	content := container.NewGridWithColumns(len(items), items...)
	win.SetContent(content)

	totalW := float32(0)
	maxH := float32(0)
	for _, pv := range previews {
		w := float32(pv.img.Bounds().Dx()) + theme.Padding()*2
		h := float32(pv.img.Bounds().Dy())
		totalW += w
		if h > maxH {
			maxH = h
		}
	}
	previewH := maxH + 40 + theme.Padding()*5

	groupW := totalW + theme.Padding()*2
	win.Resize(fyne.NewSize(groupW, previewH))

	screenW, screenH, offsetX, offsetY := p.outputInfo.screenDimensions()

	var finalX, finalY, startX, startY float32
	leftBar := fynedesk.Instance().Settings().NarrowLeftLauncher()
	if leftBar {
		// Left bar: position to the right of the bar, vertically aligned with the icon
		finalX = offsetX + wmtheme.NarrowBarWidth + 4
		finalY = offsetY + p.iconPos.Y + (p.iconSize.Height-previewH)/2
		if finalY < offsetY {
			finalY = offsetY
		}
		if finalY+previewH > offsetY+screenH {
			finalY = offsetY + screenH - previewH
		}
		startX = finalX - 20
		startY = finalY
	} else {
		// Bottom bar: position centered above the taskbar
		panelW := float32(0)
		finalX = offsetX + (screenW-panelW-groupW)/2 + panelW
		finalY = offsetY + screenH - previewH - wmtheme.NarrowBarWidth - 10
		startX = finalX
		startY = finalY + 20
	}

	wlipc.RequestOverlayPositionAbsolute(win.Title(), startX, startY, groupW, previewH)
	win.Show()

	if !fynedesk.Instance().Settings().ReduceMotion() {
		go animateSlideAbsolute(win.Title(), startX, startY, finalX, finalY, groupW, previewH)
	} else {
		wlipc.RequestOverlayPositionAbsolute(win.Title(), finalX, finalY, groupW, previewH)
	}

	p.win = win
}

// showWindow creates and displays the preview splash window.
func (p *previewPopup) showWindow(img image.Image, title string) {
	log.Printf("[PREVIEW] showWindow called: title=%q imgSize=%dx%d", title, img.Bounds().Dx(), img.Bounds().Dy())
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.win != nil {
		p.win.Close()
	}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}

	win := d.CreateSplashWindow()
	win.SetTitle("Preview " + SkipTaskbarHint)
	win.SetPadded(true)

	raster := canvas.NewImageFromImage(img)
	raster.FillMode = canvas.ImageFillContain
	raster.SetMinSize(fyne.NewSize(
		float32(img.Bounds().Dx()),
		float32(img.Bounds().Dy()),
	))

	titleLabel := widget.NewLabel(title)
	titleLabel.Truncation = fyne.TextTruncateEllipsis
	titleLabel.Alignment = fyne.TextAlignCenter

	content := container.NewBorder(nil, titleLabel, nil, nil, raster)
	win.SetContent(content)

	previewW := float32(img.Bounds().Dx()) + theme.Padding()*4
	previewH := float32(img.Bounds().Dy()) + titleLabel.MinSize().Height + theme.Padding()*5
	win.Resize(fyne.NewSize(previewW, previewH))

	screenW, screenH, offsetX, offsetY := p.outputInfo.screenDimensions()

	var finalX, finalY, startX, startY float32
	leftBar := fynedesk.Instance().Settings().NarrowLeftLauncher()
	if leftBar {
		// Left bar: position to the right of the bar, vertically aligned with the icon
		finalX = offsetX + wmtheme.NarrowBarWidth + 4
		finalY = offsetY + p.iconPos.Y + (p.iconSize.Height-previewH)/2
		// Clamp to screen bounds
		if finalY < offsetY {
			finalY = offsetY
		}
		if finalY+previewH > offsetY+screenH {
			finalY = offsetY + screenH - previewH
		}
		startX = finalX - 20 // slide in from left
		startY = finalY
	} else {
		// Bottom bar: position centered above the taskbar
		panelW := float32(0)
		finalX = offsetX + (screenW-panelW-previewW)/2 + panelW
		finalY = offsetY + screenH - previewH - wmtheme.NarrowBarWidth - 10
		startX = finalX
		startY = finalY + 20 // slide up from below
	}

	wlipc.RequestOverlayPositionAbsolute(win.Title(), startX, startY, previewW, previewH)
	win.Show()

	if !fynedesk.Instance().Settings().ReduceMotion() {
		go animateSlideAbsolute(win.Title(), startX, startY, finalX, finalY, previewW, previewH)
	} else {
		wlipc.RequestOverlayPositionAbsolute(win.Title(), finalX, finalY, previewW, previewH)
	}

	p.win = win
}

// animateSlideAbsolute smoothly moves an overlay window from (startX,startY) to (finalX,finalY) over 200ms.
func animateSlideAbsolute(title string, startX, startY, finalX, finalY, w, h float32) {
	dur := 200 * time.Millisecond
	start := time.Now()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		t := float64(time.Since(start)) / float64(dur)
		if t >= 1 {
			wlipc.RequestOverlayPositionAbsolute(title, finalX, finalY, w, h)
			return
		}
		ease := float32(easeOutCubic(t))
		x := startX + ease*(finalX-startX)
		y := startY + ease*(finalY-startY)
		wlipc.RequestOverlayPositionAbsolute(title, x, y, w, h)
	}
}

// tooltipPopup manages a lightweight text-only tooltip for icons without window previews.
type tooltipPopup struct {
	mu      sync.Mutex
	win     fyne.Window
	timer   *time.Timer
	dismiss *time.Timer // auto-dismiss safety net
	label   string      // currently shown label
	oi      outputInfo
}

var tooltip = &tooltipPopup{}

// requestTooltip starts a short timer then shows a tooltip with the given text near the icon.
func (t *tooltipPopup) requestTooltip(text string, iconPos fyne.Position, iconSize fyne.Size, oi outputInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.label == text && t.win != nil {
		return // already showing
	}
	t.cancelLocked()
	t.label = text
	t.oi = oi

	t.timer = time.AfterFunc(400*time.Millisecond, func() {
		fyne.Do(func() {
			t.showTooltip(text, iconPos, iconSize)
		})
	})
}

func (t *tooltipPopup) hide() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancelLocked()
}

func (t *tooltipPopup) cancelLocked() {
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.dismiss != nil {
		t.dismiss.Stop()
		t.dismiss = nil
	}
	if t.win != nil {
		win := t.win
		t.win = nil
		fyne.Do(func() { win.Close() })
	}
	t.label = ""
}

func (t *tooltipPopup) showTooltip(text string, iconPos fyne.Position, iconSize fyne.Size) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.label != text {
		return // hover moved away
	}
	if t.win != nil {
		t.win.Close()
	}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}

	win := d.CreateSplashWindow()
	win.SetTitle("Tooltip " + SkipTaskbarHint)
	win.SetPadded(true)

	label := widget.NewLabel(text)
	label.Alignment = fyne.TextAlignCenter
	win.SetContent(label)

	tipW := label.MinSize().Width + theme.Padding()*4
	tipH := label.MinSize().Height + theme.Padding()*4

	// Position the tooltip next to the icon.
	// For left bar: to the right of the icon.
	// For bottom bar: above the icon.
	screenW, screenH, offsetX, offsetY := t.oi.screenDimensions()
	_ = screenW

	var finalX, finalY float32
	if fynedesk.Instance().Settings().NarrowLeftLauncher() {
		// Left vertical bar: show tooltip to the right of the icon
		finalX = offsetX + iconPos.X + iconSize.Width + 4
		finalY = offsetY + iconPos.Y + (iconSize.Height-tipH)/2
	} else {
		// Bottom bar: show tooltip above the icon
		finalX = offsetX + iconPos.X + (iconSize.Width-tipW)/2
		finalY = offsetY + screenH - iconSize.Height - tipH - 10
	}
	if finalY < offsetY {
		finalY = offsetY
	}

	win.Resize(fyne.NewSize(tipW, tipH))
	wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, tipW, tipH)
	win.Show()

	t.win = win

	// Auto-dismiss after 3 seconds as a safety net in case MouseOut is missed.
	t.dismiss = time.AfterFunc(3*time.Second, func() {
		t.hide()
	})
}

// hoverTooltip wraps a canvas object and shows a text tooltip on hover.
// Use it for icon-only buttons or compact widgets that need an accessible label.
type hoverTooltip struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	tipText  string
	tipDelay time.Duration
	timer    *time.Timer
	tipWin   fyne.Window
}

func newHoverTooltip(content fyne.CanvasObject, text string) *hoverTooltip {
	h := &hoverTooltip{content: content, tipText: text, tipDelay: 400 * time.Millisecond}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverTooltip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(h.content)
}

func (h *hoverTooltip) MouseIn(_ *deskDriver.MouseEvent) {
	if h.tipText == "" {
		return
	}
	h.timer = time.AfterFunc(h.tipDelay, func() {
		fyne.Do(func() {
			h.showTip()
		})
	})
}

func (h *hoverTooltip) MouseMoved(_ *deskDriver.MouseEvent) {}

func (h *hoverTooltip) MouseOut() {
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	if h.tipWin != nil {
		h.tipWin.Close()
		h.tipWin = nil
	}
}

func (h *hoverTooltip) showTip() {
	if h.tipWin != nil {
		h.tipWin.Close()
	}

	d, ok := fyne.CurrentApp().Driver().(deskDriver.Driver)
	if !ok {
		return
	}

	win := d.CreateSplashWindow()
	win.SetTitle("Tooltip " + SkipTaskbarHint)
	win.SetPadded(true)

	label := widget.NewLabel(h.tipText)
	label.Alignment = fyne.TextAlignCenter
	win.SetContent(label)

	tipW := label.MinSize().Width + theme.Padding()*4
	tipH := label.MinSize().Height + theme.Padding()*4

	// Position to the left of the widget panel
	absPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(h.content)
	finalX := absPos.X - tipW - 4
	finalY := absPos.Y + (h.content.Size().Height-tipH)/2
	if finalX < 0 {
		finalX = absPos.X + h.content.Size().Width + 4
	}
	if finalY < 0 {
		finalY = 0
	}

	win.Resize(fyne.NewSize(tipW, tipH))
	wlipc.RequestOverlayPosition(win.Title(), finalX, finalY, tipW, tipH)
	win.Show()

	h.tipWin = win

	// Auto-dismiss after 3 seconds as a safety net
	time.AfterFunc(3*time.Second, func() {
		fyne.Do(func() {
			if h.tipWin == win {
				h.tipWin = nil
				win.Close()
			}
		})
	})
}

// outputInfo holds the position and dimensions of the output a bar is on.
// For the primary output, offsetX/Y are 0 and width/height are 0 (uses Screens().Primary()).
type outputInfo struct {
	offsetX, offsetY float32
	width, height    float32
}

// screenDimensions returns the screen dimensions and offset for overlay positioning.
// If oi has valid dimensions, it uses those; otherwise falls back to the primary screen.
func (oi outputInfo) screenDimensions() (screenW, screenH, offsetX, offsetY float32) {
	if oi.width > 0 && oi.height > 0 {
		return oi.width, oi.height, oi.offsetX, oi.offsetY
	}
	screen := fynedesk.Instance().Screens().Primary()
	scale := screen.CanvasScale()
	return float32(screen.Width) / scale,
		float32(screen.Height) / scale,
		float32(screen.X) / scale,
		float32(screen.Y) / scale
}

// iconMouseIn is called when mouse enters a taskbar icon.
func iconMouseIn(bi *barIcon, oi outputInfo) {
	if bi.windowData == nil || bi.windowData.win == nil {
		log.Printf("[PREVIEW] iconMouseIn: windowData nil or win nil")
		return
	}

	// Collect all window IDs for grouped icons
	var windowIDs []string
	for _, aw := range bi.allWindows() {
		if iw, ok := aw.win.(*ipcWindow); ok && iw.id != "" {
			windowIDs = append(windowIDs, iw.id)
		}
	}
	if len(windowIDs) == 0 {
		log.Printf("[PREVIEW] iconMouseIn: no window IDs found")
		return
	}
	log.Printf("[PREVIEW] iconMouseIn: windowIDs=%v", windowIDs)

	preview.outputInfo = oi
	preview.iconPos = bi.Position()
	preview.iconSize = bi.Size()
	preview.requestGroupPreview(windowIDs)
}

// iconMouseOut is called when mouse leaves a taskbar icon.
func iconMouseOut() {
	preview.hide()
	tooltip.hide()
}

// iconTooltipIn is called when mouse enters a non-window icon (pinned app or search).
func iconTooltipIn(bi *barIcon, name string, oi outputInfo) {
	tooltip.requestTooltip(name, bi.Position(), bi.Size(), oi)
}

// iconTooltipOut hides the tooltip.
func iconTooltipOut() {
	tooltip.hide()
}
