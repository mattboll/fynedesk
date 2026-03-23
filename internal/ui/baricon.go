package ui

import (
	"image/color"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"fyshos.com/fynedesk/internal/icon"
	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"fyshos.com/fynedesk"
)

// appWindow describes a type of icon that refers to an open window rather than an app.
// The findApp function can be used to attempt looking up the application from it's window.
type appWindow struct {
	win fynedesk.Window
	bar *bar
}

// findApp will try to return an application data associated with a window.
// This may fail for many reasons, usually related too bad window metadata, and will then return nil.
func (a *appWindow) findApp() appie.AppData {
	if a.win == nil {
		return nil
	}

	return icon.FindAppFromWinInfo(a.win, a.bar.desk.IconProvider())
}

type barIconRenderer struct {
	objects    []fyne.CanvasObject
	dot        *canvas.Circle
	urgentLine *canvas.Rectangle
	badgeBg    *canvas.Circle
	badgeText  *canvas.Text

	image *barIcon
}

func (bi *barIconRenderer) MinSize() fyne.Size {
	size := theme.IconInlineSize()
	return fyne.NewSize(size, size)
}

func (bi *barIconRenderer) Layout(size fyne.Size) {
	for _, obj := range bi.objects {
		if obj == bi.dot || obj == bi.urgentLine || obj == bi.badgeBg || obj == bi.badgeText {
			continue // positioned separately
		}
		obj.Resize(size)
	}
	if bi.dot != nil {
		dotSize := float32(5)
		bi.dot.Resize(fyne.NewSize(dotSize, dotSize))
		bi.dot.Move(fyne.NewPos((size.Width-dotSize)/2, size.Height-dotSize-1))
	}
	if bi.urgentLine != nil {
		lineH := float32(3)
		lineW := size.Width * 0.6
		bi.urgentLine.Resize(fyne.NewSize(lineW, lineH))
		bi.urgentLine.Move(fyne.NewPos((size.Width-lineW)/2, size.Height-lineH))
	}
	if bi.badgeBg != nil {
		badgeSize := float32(14)
		bi.badgeBg.Resize(fyne.NewSize(badgeSize, badgeSize))
		bi.badgeBg.Move(fyne.NewPos(size.Width-badgeSize+2, -2))
		if bi.badgeText != nil {
			bi.badgeText.Resize(fyne.NewSize(badgeSize, badgeSize))
			bi.badgeText.Move(fyne.NewPos(size.Width-badgeSize+2, -2))
		}
	}
}

func (bi *barIconRenderer) Objects() []fyne.CanvasObject {
	return bi.objects
}

func (bi *barIconRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (bi *barIconRenderer) Refresh() {
	bi.objects = nil

	if bi.image.launching {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
		bg.CornerRadius = 4
		bg.FillColor = color.NRGBA{R: 100, G: 180, B: 255, A: 80}
		bi.objects = append(bi.objects, bg)
	} else if bi.image.pressed {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNamePressed))
		bg.CornerRadius = 4
		bi.objects = append(bi.objects, bg)
	} else if bi.image.hovered {
		bg := canvas.NewRectangle(theme.Color(theme.ColorNameHover))
		bg.CornerRadius = 4
		bi.objects = append(bi.objects, bg)
	}

	if bi.image.resource != nil {
		raster := canvas.NewImageFromResource(bi.image.resource)
		raster.FillMode = canvas.ImageFillContain
		if bi.image.dragging {
			raster.Translucency = 0.5
		}
		bi.objects = append(bi.objects, raster)
	}
	bi.Layout(bi.image.Size())

	// Translucent when all windows in the group are iconic
	if bi.image.windowData != nil {
		allIconic := bi.image.windowData.win.Iconic()
		if allIconic {
			for _, gw := range bi.image.groupWindows {
				if !gw.win.Iconic() {
					allIconic = false
					break
				}
			}
		}
		if allIconic {
			for _, obj := range bi.objects {
				if img, ok := obj.(*canvas.Image); ok {
					img.Translucency = 0.67
				}
			}
		}
	}

	bi.dot = nil
	if bi.image.isRunning && bi.image.windowData == nil {
		bi.dot = canvas.NewCircle(theme.Color(theme.ColorNamePrimary))
		bi.objects = append(bi.objects, bi.dot)
		bi.Layout(bi.image.Size())
	}

	// Urgent indicator (pulsing amber underline when window requests attention)
	bi.urgentLine = nil
	if bi.image.windowData != nil {
		anyUrgent := bi.image.windowData.win.Urgent()
		if !anyUrgent {
			for _, gw := range bi.image.groupWindows {
				if gw.win.Urgent() {
					anyUrgent = true
					break
				}
			}
		}
		if anyUrgent {
			alpha := uint8(255)
			if !bi.image.urgentBright {
				alpha = 100
			}
			bi.urgentLine = canvas.NewRectangle(color.NRGBA{R: 255, G: 152, B: 0, A: alpha})
			bi.urgentLine.CornerRadius = 1
			bi.objects = append(bi.objects, bi.urgentLine)
			bi.Layout(bi.image.Size())

			// Start pulse animation if not already running
			if bi.image.urgentPulse == nil {
				bi.image.urgentPulse = time.NewTicker(600 * time.Millisecond)
				go func() {
					for range bi.image.urgentPulse.C {
						fyne.Do(func() {
							bi.image.urgentBright = !bi.image.urgentBright
							bi.image.Refresh()
						})
					}
				}()
			}
		} else if bi.image.urgentPulse != nil {
			bi.image.urgentPulse.Stop()
			bi.image.urgentPulse = nil
			bi.image.urgentBright = false
		}
	}

	// Badge for grouped windows
	bi.badgeBg = nil
	bi.badgeText = nil
	if len(bi.image.groupWindows) > 0 {
		count := 1 + len(bi.image.groupWindows)
		bi.badgeBg = canvas.NewCircle(theme.Color(theme.ColorNamePrimary))
		bi.badgeText = canvas.NewText(strconv.Itoa(count), color.White)
		bi.badgeText.TextSize = 9
		bi.badgeText.Alignment = fyne.TextAlignCenter
		bi.objects = append(bi.objects, bi.badgeBg, bi.badgeText)
		bi.Layout(bi.image.Size())
	}

	canvas.Refresh(bi.image)
}

func (bi *barIconRenderer) Destroy() {
}

// barIcon widget is a basic image component that load's its resource to match the theme.
type barIcon struct {
	widget.BaseWidget

	onTapped       func()        // The function that will be called when the icon is clicked
	resource       fyne.Resource // The image data of the image that the icon uses
	appData        appie.AppData // The application data corresponding to this icon.(if it is a launcher)
	windowData     *appWindow    // The window data associated with this icon (if it is a task window)
	groupWindows   []*appWindow  // Additional windows grouped under this icon (same app)
	lastCycleIndex int           // Index into allWindows() for round-robin cycling
	hovered        bool
	pressed        bool
	isRunning      bool // Whether a matching window is open for this launcher icon

	bar       *bar // parent bar (for drag reorder of pinned icons)
	dragging  bool
	dragStart fyne.Position
	dragAccum fyne.Delta // accumulated drag distance before threshold
	launching bool       // true while app is launching (shows pulse feedback)

	urgentPulse  *time.Ticker // pulse animation for urgent state
	urgentBright bool         // toggles between bright/dim for pulse
}

// allWindows returns all windows represented by this icon (primary + grouped).
func (bi *barIcon) allWindows() []*appWindow {
	if bi.windowData == nil {
		return nil
	}
	result := []*appWindow{bi.windowData}
	return append(result, bi.groupWindows...)
}

// Tapped means barIcon has been clicked
func (bi *barIcon) Tapped(*fyne.PointEvent) {
	if bi.dragging {
		return
	}
	bi.pressed = true
	bi.Refresh()
	action := bi.onTapped
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			bi.pressed = false
			bi.Refresh()
		})
		action()
	}()
}

const dragThreshold = 8 // pixels before drag activates

// Dragged is called when the icon is dragged (for pinned icon reorder)
func (bi *barIcon) Dragged(event *fyne.DragEvent) {
	if bi.appData == nil || bi.bar == nil {
		return
	}
	if !bi.dragging {
		bi.dragAccum.DX += event.Dragged.DX
		bi.dragAccum.DY += event.Dragged.DY
		dist := bi.dragAccum.DX*bi.dragAccum.DX + bi.dragAccum.DY*bi.dragAccum.DY
		if dist < dragThreshold*dragThreshold {
			return // below threshold, don't start drag yet
		}
		bi.dragging = true
		bi.dragStart = bi.Position()
		bi.Refresh() // show drag visual feedback
	}
	bi.Move(fyne.NewPos(
		bi.dragStart.X+event.Position.X-bi.Size().Width/2,
		bi.dragStart.Y+event.Position.Y-bi.Size().Height/2,
	))
}

// DragEnd is called when the drag is complete
func (bi *barIcon) DragEnd() {
	bi.dragAccum = fyne.Delta{}
	if !bi.dragging || bi.bar == nil {
		bi.dragging = false
		return
	}
	bi.dragging = false
	bi.bar.finishIconDrag(bi)
	bi.Refresh() // restore normal appearance
}

func addToBar(icon appie.AppData) {
	settings := fynedesk.Instance().Settings()
	icons := settings.LauncherIcons()
	icons = append(icons, icon.Name())

	settings.(*deskSettings).setLauncherIcons(icons)
}

func removeFromBar(icon appie.AppData) {
	settings := fynedesk.Instance().Settings()
	icons := settings.LauncherIcons()

	index := -1
	for i, defaultApp := range icons {
		if defaultApp == icon.Name() {
			index = i
			break
		}
	}
	if index >= 0 {
		icons = append(icons[:index], icons[index+1:]...)
	}
	settings.(*deskSettings).setLauncherIcons(icons)
}

// TappedSecondary means barIcon has been clicked by a secondary binding
func (bi *barIcon) TappedSecondary(ev *fyne.PointEvent) {
	app := bi.appData
	if app == nil && bi.windowData != nil {
		app = bi.windowData.findApp()
	}
	if app == nil || app.Name() == "" {
		return
	}

	var items []*fyne.MenuItem

	// Show individual windows for grouped icons
	if len(bi.groupWindows) > 0 {
		for _, aw := range bi.allWindows() {
			win := aw.win
			title := win.Properties().Title()
			if title == "" {
				title = app.Name()
			}
			items = append(items, fyne.NewMenuItem(title, func() {
				if win.Iconic() {
					win.Uniconify()
				}
				win.RaiseToTop()
				win.Focus()
			}))
		}
		items = append(items, fyne.NewMenuItemSeparator())
	}

	addRemove := fyne.NewMenuItem("Remove "+app.Name(), func() {
		if bi.windowData != nil {
			addToBar(app)
		} else {
			removeFromBar(app)
		}
	})

	if bi.windowData != nil {
		addRemove.Label = "Pin " + app.Name()
	}

	items = append(items, addRemove)
	editor := editorPath()
	if app.Source() != nil && editor != "" {
		items = append(items, fyne.NewMenuItem("Edit", func() {
			editApp(app, editor)
		}))
	}

	fynedesk.Instance().ShowMenuAt(fyne.NewMenu("", items...), ev.AbsolutePosition)
}

// CreateRenderer is a private method to fyne which links this widget to its renderer
func (bi *barIcon) CreateRenderer() fyne.WidgetRenderer {
	render := &barIconRenderer{image: bi}
	render.Refresh()

	return render
}

func cloneRepo(src *appie.AppSource, path string, done func()) (err error) {
	spin := widget.NewActivity()
	prop := canvas.NewRectangle(color.Transparent)
	prop.SetMinSize(fyne.NewSquareSize(56))

	w := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	w.SetContent(
		container.NewBorder(nil, widget.NewLabel("Downloading..."), nil, nil,
			container.NewStack(prop, spin)))
	spin.Start()
	w.Show()

	defer func() {
		w.Hide()
		spin.Stop()
	}()

	go func() {
		cmd := exec.Command("git", "clone", src.Repo, path)
		err = cmd.Run()
		if err == nil {
			return
		}

		fyne.Do(done)
	}()

	return err
}

func editApp(app appie.AppData, editor string) {
	root := sourceRoot()
	srcDir := filepath.Join(root, app.Name())

	open := func() {
		cmd := exec.Command(editor, srcDir)
		err := cmd.Start()

		if err != nil {
			fyne.LogError("Failed to start app editor: "+editor, err)
		}
	}

	if !exists(srcDir) {
		if !exists(root) {
			err := os.MkdirAll(root, 0755)
			if err != nil {
				fyne.LogError("Failed to make source root", err)
				return
			}
		}

		err := cloneRepo(app.Source(), srcDir, open)
		if err != nil {
			fyne.LogError("Error cloning the app source", err)
			return
		}
	}

	open()
}

func newBarIcon(res fyne.Resource, appData appie.AppData, winData *appWindow) *barIcon {
	barIcon := &barIcon{resource: res, appData: appData, windowData: winData}
	barIcon.ExtendBaseWidget(barIcon)

	return barIcon
}

func editorPath() string {
	fysion, err := exec.LookPath("fysion")
	if err == nil && fysion != "" {
		return fysion
	}

	apptrix, err := exec.LookPath("apptrix")
	if err == nil && apptrix != "" {
		return apptrix
	}

	return ""
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sourceRoot() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}

	return filepath.Join(u.HomeDir, "ApptrixApps")
}
