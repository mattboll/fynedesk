package ui

import (
	"image"
	"image/color"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/backgrounds"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/internal/wallpaper"
	"fyshos.com/fynedesk/wlipc"
)

type background struct {
	widget.BaseWidget

	wallpaper       *fyne.Container
	screenAreaItems []fyne.CanvasObject // cached ScreenAreaModule widgets

	animMu     sync.Mutex
	stopAnim   chan struct{}
	anim       wallpaper.AnimatedWallpaper
	animBuf    *image.NRGBA
	animRaster *canvas.Raster // persistent canvas.Raster for animation
}

func (b *background) CreateRenderer() fyne.WidgetRenderer {
	c := container.NewStack(b.loadModules()...)
	return widget.NewSimpleRenderer(c)
}

func (b *background) loadModules() []fyne.CanvasObject {
	objects := []fyne.CanvasObject{b.wallpaper}

	b.screenAreaItems = nil
	for _, m := range fynedesk.Instance().Modules() {
		if deskMod, ok := m.(fynedesk.ScreenAreaModule); ok {
			wid := deskMod.ScreenAreaWidget()
			if wid == nil {
				continue
			}
			b.screenAreaItems = append(b.screenAreaItems, wid)
			objects = append(objects, wid)
		}
	}

	return objects
}

// setScreenAreaVisible shows or hides screen area module widgets (e.g. desktop icons).
// Uses cached widget references from loadModules() so Hide()/Show() operates on
// the same instances that are in the renderer's object tree.
func (b *background) setScreenAreaVisible(visible bool) {
	for _, wid := range b.screenAreaItems {
		if visible {
			wid.Show()
		} else {
			wid.Hide()
		}
	}
}

func (b *background) stopAnimation() {
	b.animMu.Lock()
	defer b.animMu.Unlock()
	if b.stopAnim != nil {
		close(b.stopAnim)
		b.stopAnim = nil
	}
	b.anim = nil
}

// ensureAnimRaster returns the persistent canvas.Raster used for animations.
// The Raster's Generator callback returns the current animation frame buffer,
// which is updated in-place by the animation goroutine.
func (b *background) ensureAnimRaster() *canvas.Raster {
	if b.animRaster != nil {
		return b.animRaster
	}
	b.animRaster = canvas.NewRaster(func(w, h int) image.Image {
		b.animMu.Lock()
		buf := b.animBuf
		b.animMu.Unlock()
		if buf != nil {
			return buf
		}
		// Return a black image until the first frame is ready
		return image.NewNRGBA(image.Rect(0, 0, w, h))
	})
	b.animRaster.ScaleMode = canvas.ImageScaleFastest
	return b.animRaster
}

func (b *background) startAnimatedBackground(bgType string) {
	var anim wallpaper.AnimatedWallpaper
	switch bgType {
	case "matrix":
		anim = &wallpaper.MatrixAnim{}
	case "starfield":
		anim = &wallpaper.StarfieldAnim{}
	default:
		return
	}

	log.Printf("[WALLPAPER-PANEL] startAnimatedBackground(%s)\n", bgType)

	raster := b.ensureAnimRaster()
	b.wallpaper.Objects[0] = raster
	// Ensure the raster gets laid out with the container's size.
	// Direct Objects[] mutation skips layout; Refresh() re-runs it.
	b.wallpaper.Refresh()

	b.animMu.Lock()
	b.anim = anim
	b.animBuf = nil // force re-init
	stop := make(chan struct{})
	b.stopAnim = stop
	b.animMu.Unlock()

	// If ReduceMotion is enabled, render one static frame and stop
	if fynedesk.Instance().Settings().ReduceMotion() {
		return
	}

	go func() {
		ticker := time.NewTicker(125 * time.Millisecond) // ~8 FPS — low CPU, still fluid enough for rain effect
		defer ticker.Stop()
		var refreshPending atomic.Bool
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Skip this frame if the previous Refresh hasn't been
				// processed yet — avoids queuing up work when the main
				// thread is busy (menu/dialog opening).
				if refreshPending.Load() {
					continue
				}

				// Size() reads two float32 fields — safe from any goroutine
				// and a stale value is harmless for animation.
				sz := b.wallpaper.Size()
				w := int(sz.Width)
				h := int(sz.Height)
				if w <= 0 || h <= 0 {
					continue
				}

				// Heavy CPU work (Tick) runs in this background goroutine.
				// Render at 1/4 resolution for ~16x less pixel processing;
				// canvas.Raster upscales via ImageScaleFastest (GPU nearest-neighbor).
				animW := w / 4
				animH := h / 4
				if animW < 160 {
					animW = 160
				}
				if animH < 100 {
					animH = 100
				}
				b.animMu.Lock()
				if b.anim == nil {
					b.animMu.Unlock()
					continue
				}
				if b.animBuf == nil || b.animBuf.Rect.Dx() != animW || b.animBuf.Rect.Dy() != animH {
					b.animBuf = image.NewNRGBA(image.Rect(0, 0, animW, animH))
					for i := 3; i < len(b.animBuf.Pix); i += 4 {
						b.animBuf.Pix[i] = 0xFF
					}
					b.anim.Init(animW, animH, time.Now().UnixNano())
					log.Printf("[WALLPAPER-PANEL] anim init %dx%d (render %dx%d)\n", w, h, animW, animH)
				}
				b.anim.Tick(b.animBuf)
				b.animMu.Unlock()

				// Only canvas.Refresh needs the main thread.
				// Fire-and-forget: the goroutine continues computing
				// frames while the main thread processes the refresh.
				refreshPending.Store(true)
				fyne.Do(func() {
					raster.Refresh() // Refresh raster only, not the whole container
					refreshPending.Store(false)
				})
			}
		}
	}()
}

func (b *background) updateBackground(path, bgType string) {
	log.Printf("[WALLPAPER-PANEL] updateBackground(path=%q, type=%q)\n", path, bgType)
	b.stopAnimation()

	// In Wayland mode, the compositor handles the wallpaper in
	// backgroundTree. The panel background stays transparent so that
	// when panelTree is raised above windowsTree, the windows beneath
	// remain visible through the panel's alpha channel.
	if wlipc.IsWaylandSession() {
		b.wallpaper.Objects[0] = canvas.NewRectangle(color.Transparent)
	} else {
		switch bgType {
		case "matrix", "starfield":
			b.startAnimatedBackground(bgType)
		default:
			_, err := os.Stat(path)
			if path == "" || err != nil {
				set := fyne.CurrentApp().Settings()
				src := backgrounds.Default()
				b.wallpaper.Objects[0] = src.Load(set.Theme(), set.ThemeVariant())
			} else {
				bg := canvas.NewImageFromFile(path)
				bg.ScaleMode = canvas.ImageScaleFastest
				b.wallpaper.Objects[0] = bg
			}
		}
	}
	// Use Container.Refresh() to re-run layout (sizes new child) and then
	// canvas-refresh. This is critical when Objects[0] was replaced, as the
	// new object needs to be sized by the Stack layout.
	b.wallpaper.Refresh()
	b.Refresh()
}

func backgroundPath() string {
	pathEnv := fynedesk.Instance().Settings().Background()
	if pathEnv == "" {
		return ""
	}

	if stat, err := os.Stat(pathEnv); os.IsNotExist(err) || !stat.Mode().IsRegular() {
		return ""
	}

	return pathEnv
}

func newBackground() *background {
	ret := &background{}

	bgType := fyne.CurrentApp().Preferences().String("background_type")
	log.Printf("[WALLPAPER-PANEL] newBackground type=%q\n", bgType)

	var bg fyne.CanvasObject

	// In Wayland mode, the compositor handles the wallpaper in
	// backgroundTree. The panel background is fully transparent so that
	// when panelTree is raised above windowsTree (hotspot reveal), the
	// windows beneath remain visible through the panel's alpha channel.
	if wlipc.IsWaylandSession() {
		bg = canvas.NewRectangle(color.Transparent)
	} else {
		switch bgType {
		case "matrix", "starfield":
			bg = ret.ensureAnimRaster()
		default:
			imagePath := backgroundPath()
			if imagePath != "" {
				img := canvas.NewImageFromFile(imagePath)
				img.ScaleMode = canvas.ImageScaleFastest
				bg = img
			} else {
				set := fyne.CurrentApp().Settings()
				b := backgrounds.Default()
				bg = b.Load(set.Theme(), set.ThemeVariant())
			}
		}
	}

	ret.wallpaper = container.NewStack(bg)
	ret.ExtendBaseWidget(ret)

	if !wlipc.IsWaylandSession() && (bgType == "matrix" || bgType == "starfield") {
		ret.startAnimatedBackground(bgType)
	}

	return ret
}
