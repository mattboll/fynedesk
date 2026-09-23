package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"  // register decoders so renderWallpaper can read any wallpaper
	_ "image/jpeg" // ...
	_ "image/png"  // ...
	"io"
	"log"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/FyshOS/backgrounds"
	xdraw "golang.org/x/image/draw"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/wallpaper"
	"fyshos.com/tyde/wlipc"
)

type background struct {
	widget.BaseWidget

	wallpaper       *fyne.Container     // holds the current wallpaper so it can be rebuilt live
	screenAreaItems []fyne.CanvasObject // cached ScreenAreaModule widgets

	animMu     sync.Mutex
	stopAnim   chan struct{}
	anim       wallpaper.AnimatedWallpaper
	animBuf    *image.NRGBA
	animRaster *canvas.Raster // persistent canvas.Raster for animation
}

func (b *background) CreateRenderer() fyne.WidgetRenderer {
	b.wallpaper = container.NewStack(b.loadModules(backgroundType())...)
	b.startAnimatedBackground(backgroundType())
	return widget.NewSimpleRenderer(b.wallpaper)
}

// backgroundType returns the configured kind of background: "image" or the
// name of an animated wallpaper.
func backgroundType() string {
	return fyne.CurrentApp().Preferences().String("background_type")
}

func (b *background) loadModules(bgType string) []fyne.CanvasObject {
	objects := []fyne.CanvasObject{b.loadWallpaper(bgType)}

	// Add screen area modules (e.g. desktop files)
	b.screenAreaItems = nil
	for _, m := range tyde.Instance().Modules() {
		if deskMod, ok := m.(tyde.ScreenAreaModule); ok {
			if wid := deskMod.ScreenAreaWidget(); wid != nil {
				b.screenAreaItems = append(b.screenAreaItems, wid)
				objects = append(objects, wid)
			}
		}
	}

	return objects
}

// updateBackground rebuilds the background content - the wallpaper and the
// screen area module overlays.
func (b *background) updateBackground(_, bgType string) {
	b.stopAnimation()
	if b.wallpaper != nil {
		b.wallpaper.Objects = b.loadModules(bgType)
		b.wallpaper.Refresh()
		b.startAnimatedBackground(bgType)
	}
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

// loadWallpaper returns the wallpaper object for the configured background.
func (b *background) loadWallpaper(bgType string) fyne.CanvasObject {
	// In Wayland mode, the compositor handles the wallpaper in
	// backgroundTree. The panel background stays transparent so that
	// when panelTree is raised above windowsTree, the windows beneath
	// remain visible through the panel's alpha channel.
	if wlipc.IsWaylandSession() {
		return canvas.NewRectangle(color.Transparent)
	}

	switch bgType {
	case "matrix", "starfield":
		return b.ensureAnimRaster()
	}
	return loadWallpaper()
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

	if wlipc.IsWaylandSession() {
		return // the compositor draws the wallpaper
	}
	raster := b.ensureAnimRaster()

	b.animMu.Lock()
	b.anim = anim
	b.animBuf = nil // force re-init
	stop := make(chan struct{})
	b.stopAnim = stop
	b.animMu.Unlock()

	// If ReduceMotion is enabled, render one static frame and stop
	if tyde.Instance().Settings().ReduceMotion() {
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

func loadWallpaper() fyne.CanvasObject {
	path := ""
	fill := ""
	colorHex := ""
	inst := tyde.Instance()
	if inst != nil {
		path = inst.Settings().Background()
		fill = inst.Settings().BackgroundFill()
		colorHex = inst.Settings().BackgroundColor()
	}

	if path != "" {
		if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() {
			img := canvas.NewImageFromFile(path)
			img.ScaleMode = canvas.ImageScaleFastest
			img.FillMode = backgroundFillMode(fill)

			bg := canvas.NewRectangle(ParseHexColor(colorHex))
			return container.NewStack(bg, img)
		}
	}

	set := fyne.CurrentApp().Settings()
	src := backgrounds.Default()
	return src.Load(set.Theme(), set.ThemeVariant())
}

// renderWallpaper rasterises the current wallpaper into an opaque RGBA image of
// the given pixel size, honouring the configured background colour and fill
// mode. It mirrors loadWallpaper's settings so a synthesised desktop face (used
// by the cube transition and the desktop overview before a desktop has been
// captured live).
func renderWallpaper(w, h int) *image.RGBA {
	if w <= 0 || h <= 0 {
		return nil
	}
	inst := tyde.Instance()
	if inst == nil {
		return nil
	}

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := ParseHexColor(inst.Settings().BackgroundColor())
	draw.Draw(out, out.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	src, fill := wallpaperSource(inst.Settings())
	if src == nil {
		return out
	}

	xdraw.CatmullRom.Scale(out, wallpaperRect(src.Bounds(), w, h, fill),
		src, src.Bounds(), draw.Over, nil)
	return out
}

// wallpaperSource returns the decoded wallpaper, mirroring loadWallpaper.
func wallpaperSource(set tyde.DeskSettings) (image.Image, string) {
	if path := set.Background(); path != "" {
		if img := decodeWallpaper("file:"+path, func() (io.ReadCloser, error) {
			return os.Open(path)
		}); img != nil {
			return img, set.BackgroundFill()
		}
	}

	app := fyne.CurrentApp()
	if app == nil {
		return nil, ""
	}
	s := app.Settings()
	img, _ := backgrounds.Default().Load(s.Theme(), s.ThemeVariant()).(*canvas.Image)
	if img == nil || img.Resource == nil {
		return nil, ""
	}

	res := img.Resource
	return decodeWallpaper("res:"+res.Name(), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(res.Content())), nil
	}), "Fill"
}

// wallpaperCache holds the most recently decoded wallpaper.
var wallpaperCache struct {
	sync.Mutex
	key string
	img image.Image
}

// decodeWallpaper decodes the image from open(), caching the result under key.
// Returns nil if it cannot be read or decoded.
func decodeWallpaper(key string, open func() (io.ReadCloser, error)) image.Image {
	wallpaperCache.Lock()
	defer wallpaperCache.Unlock()
	if wallpaperCache.key == key && wallpaperCache.img != nil {
		return wallpaperCache.img
	}

	r, err := open()
	if err != nil {
		return nil
	}
	defer r.Close()
	img, _, err := image.Decode(r)
	if err != nil {
		return nil
	}

	wallpaperCache.key, wallpaperCache.img = key, img
	return img
}

// wallpaperRect returns the destination rectangle for the wallpaper within a
// w×h image for the given fill mode, matching backgroundFillMode: Fit scales to
// fit inside (letterboxed), Fill scales to cover (overflow clipped by Scale),
// and the default stretches to the full size.
func wallpaperRect(src image.Rectangle, w, h int, fill string) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw <= 0 || sh <= 0 {
		return image.Rect(0, 0, w, h)
	}

	var scale float64
	switch fill {
	case "Fit":
		scale = math.Min(float64(w)/float64(sw), float64(h)/float64(sh))
	case "Fill":
		scale = math.Max(float64(w)/float64(sw), float64(h)/float64(sh))
	default: // Stretch
		return image.Rect(0, 0, w, h)
	}

	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	ox, oy := (w-dw)/2, (h-dh)/2
	return image.Rect(ox, oy, ox+dw, oy+dh)
}

func newBackground() *background {
	ret := &background{}
	ret.ExtendBaseWidget(ret)
	return ret
}

// backgroundFillModes lists the user-facing fill options in display order.
var backgroundFillModes = []string{"Stretch", "Fit", "Fill"}

// backgroundFillMode maps a user-facing fill name to a canvas fill mode.
func backgroundFillMode(name string) canvas.ImageFill {
	switch name {
	case "Fit":
		return canvas.ImageFillContain
	case "Fill":
		return canvas.ImageFillCover
	default: // "Stretch"
		return canvas.ImageFillStretch
	}
}

// ParseHexColor turns a "#rrggbb" or "#rrggbbaa" string into a colour,
// falling back to opaque black for empty or malformed input.
func ParseHexColor(hex string) color.NRGBA {
	c := color.NRGBA{A: 0xff}
	if len(hex) == 0 || hex[0] != '#' {
		return c
	}

	switch len(hex) {
	case 7: // #rrggbb
		_, err := fmt.Sscanf(hex, "#%02x%02x%02x", &c.R, &c.G, &c.B)
		if err != nil {
			return color.NRGBA{A: 0xff}
		}
	case 9: // #rrggbbaa
		_, err := fmt.Sscanf(hex, "#%02x%02x%02x%02x", &c.R, &c.G, &c.B, &c.A)
		if err != nil {
			return color.NRGBA{A: 0xff}
		}
	}
	return c
}

// HexColor formats a colour as a "#rrggbbaa" string for storage.
func HexColor(c color.Color) string {
	r, g, b, a := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8))
}
