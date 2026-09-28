package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"
*/
import "C"

import (
	"bytes"
	"image"
	"image/color"
	"log"
	"os"
	"slices"
	"time"
	"unsafe"

	"golang.org/x/image/draw"

	dynwp "fyshos.com/tyde/internal/wallpaper"
	"fyshos.com/tyde/wlipc"
)

// loadWallpaperForNewOutput loads the wallpaper image for a newly connected output.
// It checks for a per-monitor wallpaper override first, then falls back to global settings.
func (s *server) loadWallpaperForNewOutput(out *outputState) {
	outName := out.output.Name()
	bgType := s.backgroundType
	bgPath := ""

	// Check per-monitor override
	if mw, ok := s.monitorWallpapers[outName]; ok && mw.background != "" {
		bgPath = mw.background
		if mw.backgroundType != "" {
			bgType = mw.backgroundType
		}
		log.Printf("[WALLPAPER] Using per-monitor wallpaper for %s: type=%s path=%s\n", outName, bgType, bgPath)
	}

	// Animated wallpapers (matrix/starfield) — per-monitor or global
	if bgType == "matrix" || bgType == "starfield" {
		s.initAnimWallpaper(out, bgType)
		return
	}

	// If no per-monitor path, read global from prefs
	if bgPath == "" {
		prefs, _, err := s.readPrefs()
		if err != nil {
			log.Printf("[WALLPAPER] loadWallpaperForNewOutput: readPrefs error for %s, using solid color\n", outName)
			s.loadDefaultBackground(out)
			return
		}
		bgPath, _ = prefs["background"].(string)
		if bgPath == "" {
			log.Printf("[WALLPAPER] loadWallpaperForNewOutput: no background path for %s, using solid color\n", outName)
			s.loadDefaultBackground(out)
			return
		}
	}

	// Dynamic wallpaper: resolve path from directory based on time-of-day
	if bgType == "dynamic" {
		resolved, err := dynwp.ResolveDynamicWallpaper(bgPath)
		if err != nil {
			log.Printf("[WALLPAPER] dynamic resolve failed: %v, falling back to default\n", err)
			s.loadDefaultBackground(out)
			return
		}
		s.dynamicWallpaperSlot = dynwp.CurrentSlotName(time.Now().Hour())
		log.Printf("[WALLPAPER] dynamic: slot=%q path=%s\n", s.dynamicWallpaperSlot, resolved)
		bgPath = resolved
	}

	s.loadWallpaperFromPath(out, bgPath)
}

// loadWallpaperFromPath decodes and processes the wallpaper image in a
// background goroutine. The wallpaper is applied in two phases:
//  1. Immediate: decode + scale → apply wallpaper (typically <2s)
//  2. Deferred: compute blur → apply panel blur (3-8s on high-res)
//
// This makes wallpaper changes feel instantaneous.
func (s *server) loadWallpaperFromPath(out *outputState, bgPath string) {
	outName := out.output.Name()
	w, h := out.width, out.height
	isPrimary := s.isPrimaryOutput(s.getOutputGeometry(out))
	fill, bg := s.backgroundFill, wallpaperBackgroundColor(s.backgroundColor)
	autoAccent := s.autoAccentColor

	// onMain applies the result on the main thread, if the output is still
	// there at the size the image was made for (unplugged or resized
	// meanwhile, another load is on its way).
	onMain := func(apply func()) {
		_ = s.enqueueAction(func() {
			if !slices.Contains(s.outputs, out) || out.width != w || out.height != h {
				log.Printf("[WALLPAPER] %s gone or resized, wallpaper dropped\n", outName)
				return
			}
			apply()
		})
	}

	go func() {
		f, err := os.Open(bgPath)
		if err != nil {
			log.Printf("[WALLPAPER] open error for %s: %v, using solid color\n", outName, err)
			onMain(func() { s.loadDefaultBackground(out) })
			return
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			log.Printf("[WALLPAPER] decode error for %s: %v, using solid color\n", outName, err)
			onMain(func() { s.loadDefaultBackground(out) })
			return
		}

		// Phase 1: scale and apply wallpaper immediately (no blur yet)
		nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
		dynwp.Draw(nrgba, img, fill, bg, draw.ApproxBiLinear)

		log.Printf("[WALLPAPER] scaled %s %dx%d, applying immediately\n", outName, w, h)
		onMain(func() { s.applyWallpaper(out, nrgba) })

		// Extract accent color in background (primary output only)
		if isPrimary && autoAccent {
			go func() {
				accent := dynwp.ExtractAccentColor(img)
				hex := dynwp.ColorToHex(accent)
				log.Printf("[ACCENT] Extracted accent color: %s\n", hex)
				if err := wlipc.WriteAccentColor(hex); err != nil {
					log.Printf("[ACCENT] Failed to write accent color: %v\n", err)
				}
			}()
		}
	}()
}

// startDynamicWallpaperTimer starts, once, a background goroutine that
// every minute has the main thread check whether the time-of-day slot of
// the dynamic wallpaper changed. Main thread.
func (s *server) startDynamicWallpaperTimer() {
	if s.dynamicWallpaperTimer {
		return
	}
	s.dynamicWallpaperTimer = true
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-s.shutdown:
				return
			case <-ticker.C:
			}
			_ = s.enqueueAction(s.checkDynamicWallpaper)
		}
	}()
}

// checkDynamicWallpaper reloads the dynamic wallpaper when its time-of-day
// slot changed. Main thread.
func (s *server) checkDynamicWallpaper() {
	if s.backgroundType != "dynamic" {
		return
	}
	slot := dynwp.CurrentSlotName(time.Now().Hour())
	if slot == s.dynamicWallpaperSlot {
		return
	}
	log.Printf("[WALLPAPER] Dynamic slot changed: %s → %s\n", s.dynamicWallpaperSlot, slot)
	for _, out := range s.outputs {
		s.loadWallpaperForNewOutput(out)
	}
}

// loadDefaultBackground loads the embedded default wallpaper for an output (fallback when no custom wallpaper)
func (s *server) loadDefaultBackground(out *outputState) {
	w := out.width
	h := out.height
	if w <= 0 || h <= 0 {
		return
	}

	// Try to decode the embedded default wallpaper PNG
	img, _, err := image.Decode(bytes.NewReader(defaultBgPNG))
	if err != nil {
		log.Printf("[WALLPAPER] Failed to decode embedded default wallpaper: %v, using solid color\n", err)
		// Fallback to solid dark color
		nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				nrgba.SetNRGBA(x, y, color.NRGBA{R: 0x1E, G: 0x1E, B: 0x1E, A: 0xFF})
			}
		}
		s.loadWallpaperForOutput(out, nrgba)
		return
	}

	// loadWallpaperForOutput handles scaling to output dimensions
	s.loadWallpaperForOutput(out, img)
}

// applyWallpaper applies a pre-processed wallpaper image to the scene tree.
// MUST be called on the main thread. The heavy work (decode, scale)
// was already done in a background goroutine.
func (s *server) applyWallpaper(out *outputState, nrgba *image.NRGBA) {
	w := out.width
	h := out.height
	if w <= 0 || h <= 0 {
		return
	}

	if out.wallpaperPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(out.wallpaperPixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&nrgba.Pix[0]), C.int(w), C.int(h))
		if out.wallpaperBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(out.wallpaperBuf)
			C.wlr_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.wlr_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
			C.wlr_scene_node_set_position(&sceneBuf.node, C.int(out.layoutX), C.int(out.layoutY))
		}
		log.Printf("[WALLPAPER] Updated wallpaper for %s at (%d,%d) %dx%d\n",
			out.output.Name(), out.layoutX, out.layoutY, w, h)
	} else if s.backgroundTree != nil {
		pixBuf := C.pixel_buffer_create(C.int(w), C.int(h))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&nrgba.Pix[0]), C.int(w), C.int(h))
			bgTree := (*C.struct_wlr_scene_tree)(s.backgroundTree)
			sceneBuf := C.wlr_scene_buffer_create(bgTree, &pixBuf.base)
			C.wlr_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
			C.wlr_scene_node_set_position(&sceneBuf.node, C.int(out.layoutX), C.int(out.layoutY))
			out.wallpaperBuf = unsafe.Pointer(sceneBuf)
			out.wallpaperPixBuf = unsafe.Pointer(pixBuf)
			log.Printf("[WALLPAPER] Created wallpaper for %s at (%d,%d) %dx%d (bgTree=%v)\n",
				out.output.Name(), out.layoutX, out.layoutY, w, h, s.backgroundTree != nil)
		} else {
			log.Printf("[WALLPAPER] pixel_buffer_create FAILED for %s\n", out.output.Name())
		}
	} else {
		log.Printf("[WALLPAPER] loadWallpaperForOutput: skipped %s (no backgroundTree)\n", out.output.Name())
	}
}

// loadWallpaperForOutput is the synchronous path used by callers that already
// have a decoded image (e.g. default background, boot sequence). Scales + blurs
// on the main thread. For user-triggered wallpaper changes, prefer the async
// loadWallpaperFromPath which does the heavy work in a goroutine.
func (s *server) loadWallpaperForOutput(out *outputState, img image.Image) {
	w := out.width
	h := out.height
	if w <= 0 || h <= 0 {
		log.Printf("[WALLPAPER] loadWallpaperForOutput: skipped %s (invalid size %dx%d)\n", out.output.Name(), w, h)
		return
	}

	// Scale image to output dimensions
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(nrgba, nrgba.Bounds(), img, img.Bounds(), draw.Src, nil)

	s.applyWallpaper(out, nrgba)
}

// initAnimWallpaper initializes an animated wallpaper for an output.
// Renders at half resolution internally and lets the scene graph upscale,
// reducing pixel processing by 4x with minimal visual difference.
func (s *server) initAnimWallpaper(out *outputState, animType string) {
	w := out.width
	h := out.height
	if w <= 0 || h <= 0 {
		return
	}

	// Render at half resolution for ~4x lower CPU cost.
	animW := w / 2
	animH := h / 2

	var anim animatedWallpaper
	seed := time.Now().UnixNano() + int64(out.layoutX*1000+out.layoutY)
	switch animType {
	case "matrix":
		anim = &matrixAnim{}
	case "starfield":
		anim = &starfieldAnim{}
	default:
		return
	}

	anim.Init(animW, animH, seed)

	// Create NRGBA buffer (black initial) at animation resolution
	nrgba := image.NewNRGBA(image.Rect(0, 0, animW, animH))
	for i := 3; i < len(nrgba.Pix); i += 4 {
		nrgba.Pix[i] = 0xFF // Set alpha to opaque
	}

	out.animWallpaper = anim
	out.animBuf = nrgba
	out.lastAnimTick = time.Time{}

	// Create pixel buffer at animation resolution, upscale to full output via dest_size
	if out.wallpaperPixBuf == nil && s.backgroundTree != nil {
		pixBuf := C.pixel_buffer_create(C.int(animW), C.int(animH))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&nrgba.Pix[0]), C.int(animW), C.int(animH))
			bgTree := (*C.struct_wlr_scene_tree)(s.backgroundTree)
			sceneBuf := C.wlr_scene_buffer_create(bgTree, &pixBuf.base)
			C.wlr_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
			C.wlr_scene_node_set_position(&sceneBuf.node, C.int(out.layoutX), C.int(out.layoutY))
			out.wallpaperBuf = unsafe.Pointer(sceneBuf)
			out.wallpaperPixBuf = unsafe.Pointer(pixBuf)
		}
	} else if out.wallpaperPixBuf != nil {
		// Existing buffer from static wallpaper — update dimensions
		pixBuf := (*C.struct_pixel_buffer)(out.wallpaperPixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&nrgba.Pix[0]), C.int(animW), C.int(animH))
		if out.wallpaperBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(out.wallpaperBuf)
			C.wlr_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
		}
	}

	out.wallOnCPU = false
	s.startMatrixWall(out)

	log.Printf("[WALLPAPER] Initialized %s animation for %s (%dx%d, render %dx%d)\n",
		animType, out.output.Name(), w, h, animW, animH)
}

// clearAnimWallpaper removes animation state from an output.
func (s *server) clearAnimWallpaper(out *outputState) {
	s.stopMatrixWall(out)
	out.animWallpaper = nil
	out.animBuf = nil
	out.lastAnimTick = time.Time{}
}

// updateAnimatedWallpaper advances the animation by one tick and pushes pixels.
// Returns true if pixels were updated (for frame scheduling).
func (s *server) updateAnimatedWallpaper(out *outputState) bool {
	if out.animWallpaper == nil || out.animBuf == nil {
		return false
	}

	now := time.Now()
	if now.Sub(out.lastAnimTick) < 42*time.Millisecond {
		return false // Throttle to ~24fps
	}
	out.lastAnimTick = now

	if s.tickMatrixWall(out) {
		return true
	}
	out.animWallpaper.Tick(out.animBuf)

	// Push updated pixels to the scene buffer (animation buffer may be smaller than output)
	animW := out.animBuf.Rect.Dx()
	animH := out.animBuf.Rect.Dy()
	if out.wallpaperPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(out.wallpaperPixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&out.animBuf.Pix[0]), C.int(animW), C.int(animH))
		if out.wallpaperBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(out.wallpaperBuf)
			// Setting the same buffer drops the scene's texture of it and
			// damages it whole: the new pixels are shown.
			C.wlr_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
		}
	}
	return true
}

// isOutputOccludedByFullscreen checks if a fullscreen window covers this specific output.
func (s *server) isOutputOccludedByFullscreen(out *outputState) bool {
	ox, oy, ow, oh := out.layoutX, out.layoutY, out.width, out.height
	for _, v := range s.xdgViews {
		if v.fullscreen && v.mapped &&
			int(v.x) >= ox && int(v.x) < ox+ow &&
			int(v.y) >= oy && int(v.y) < oy+oh {
			return true
		}
	}
	for _, v := range s.xwayViews {
		if v.fullscreen && v.mapped &&
			int(v.x) >= ox && int(v.x) < ox+ow &&
			int(v.y) >= oy && int(v.y) < oy+oh {
			return true
		}
	}
	return false
}
