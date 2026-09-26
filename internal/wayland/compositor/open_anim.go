package compositor

/*
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

// Images drawn in Go (see pixel_buffer.h).
#include "pixel_buffer.h"

static struct wlr_scene_buffer *oa_scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void oa_scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void oa_scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void oa_scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void oa_scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
static void oa_scene_tree_set_enabled(struct wlr_scene_tree *tree, bool enabled) {
	wlr_scene_node_set_enabled(&tree->node, enabled);
}
*/
import "C"

import (
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"time"
	"unsafe"
)

// openAnim holds state for the icon-to-window launch animation.
// Phase 1 (0-60%): icon slides from dock to window center at constant size.
// Phase 2 (60-100%): icon glitches/destructs at window center, then window appears.
type openAnim struct {
	sceneBuf unsafe.Pointer // *C.struct_wlr_scene_buffer
	pixBuf   unsafe.Pointer // *C.struct_pixel_buffer
	start    time.Time
	duration time.Duration
	// Start (dock center) and end (window center) positions
	fromX, fromY float64
	toX, toY     float64
	// Icon source image (clean, for glitch rendering)
	iconImg  *image.NRGBA
	iconSize int
	// Associated view ID (for re-enabling window)
	viewID string
}

const (
	openAnimDuration = 350 * time.Millisecond
	openAnimIconSize = 128 // Icon render size (stays constant, no scaling)
)

// startOpenAnimXdg creates the icon launch animation for an XDG view.
// The caller must NOT enable the view's sceneTree before calling this.
// If the animation cannot start, this function enables the tree itself.
func (s *server) startOpenAnimXdg(v *xdgView) {
	tree := (*C.struct_wlr_scene_tree)(v.sceneTree)

	appID := getXdgToplevelAppID(v.xdgToplevel)
	nodeEnabled := (*C.struct_wlr_scene_tree)(v.sceneTree).node.enabled
	log.Printf("[OPEN-ANIM] XDG: appID=%q decorated=%v maximized=%v id=%s sceneEnabled=%v", appID, v.decorated, v.maximized, v.id, nodeEnabled)
	if appID == "" {
		log.Printf("[OPEN-ANIM] XDG: SKIP (empty appID)")
		C.oa_scene_tree_set_enabled(tree, true)
		return
	}

	icon := s.loadOpenAnimIcon(appID)
	if icon == nil {
		log.Printf("[OPEN-ANIM] XDG: SKIP (no icon)")
		C.oa_scene_tree_set_enabled(tree, true)
		return
	}

	// Remove deco nodes so they don't flash when tree is re-enabled
	v.hideDecorations = true
	s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
	v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
	v.decoTitlebar = nil
	releasePixelBuffer(&v.decoTitlePix)
	removeCornerNodes(&v.decoCornerBL, &v.decoCornerBR, &v.decoCornerPL, &v.decoCornerPR)
	if v.decoIconBuf != nil {
		C.oa_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(v.decoIconBuf).node)
		v.decoIconBuf = nil
		releasePixelBuffer(&v.decoIconPix)
	}

	surf := v.xdgToplevel.Base().Surface()
	st := surf.Current()
	w, h := st.Width(), st.Height()
	if v.decorated {
		h += titlebarHeight
	}

	s.createOpenAnim(icon, v.id, v.x, v.y, w, h)
	// If createOpenAnim failed, re-enable the tree
	if s.openAnim == nil {
		C.oa_scene_tree_set_enabled(tree, true)
		v.hideDecorations = false
	}
}

// startOpenAnimXway creates the icon launch animation for an XWayland view.
// The caller must NOT enable the view's sceneTree before calling this.
// If the animation cannot start, this function enables the tree itself.
func (s *server) startOpenAnimXway(v *xwayView) {
	tree := (*C.struct_wlr_scene_tree)(v.sceneTree)

	class := getXwaylandSurfaceClass(v.surface)
	log.Printf("[OPEN-ANIM] XWay: class=%q decorated=%v maximized=%v id=%s", class, v.decorated, v.maximized, v.id)
	if class == "" {
		log.Printf("[OPEN-ANIM] XWay: SKIP (empty class)")
		C.oa_scene_tree_set_enabled(tree, true)
		return
	}

	icon := s.loadOpenAnimIcon(class)
	if icon == nil {
		C.oa_scene_tree_set_enabled(tree, true)
		return
	}

	// Remove deco nodes so they don't flash when tree is re-enabled
	v.hideDecorations = true
	s.removeDecoNodes(v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR, v.decoTitlebar)
	v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
	v.decoTitlebar = nil
	releasePixelBuffer(&v.decoTitlePix)
	removeCornerNodes(&v.decoCornerBL, &v.decoCornerBR, &v.decoCornerPL, &v.decoCornerPR)
	if v.decoIconBuf != nil {
		C.oa_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(v.decoIconBuf).node)
		v.decoIconBuf = nil
		releasePixelBuffer(&v.decoIconPix)
	}

	w, h := v.surface.Width(), v.surface.Height()
	if v.decorated {
		h += titlebarHeight
	}

	s.createOpenAnim(icon, v.id, v.x, v.y, w, h)
	// If createOpenAnim failed, re-enable the tree
	if s.openAnim == nil {
		C.oa_scene_tree_set_enabled(tree, true)
		v.hideDecorations = false
	}
}

// hideTooltips hides all panel tooltip overlays (they show the app name
// and are distracting during the open animation).
func (s *server) hideTooltips() {
	for _, v := range s.xwayViews {
		if v.mapped && v.isOverlay && strings.Contains(v.surface.Title(), "Tooltip") {
			setViewSceneEnabled(v.sceneTree, false)
		}
	}
}

// createOpenAnim sets up the animated icon overlay.
func (s *server) createOpenAnim(icon *image.NRGBA, viewID string, winX, winY float64, winW, winH int) {
	// Only one animation runs at a time: finish the previous one so its
	// window is shown (merely cleaning it up would leave it hidden).
	s.finishOpenAnim()
	s.hideTooltips()

	sz := icon.Bounds().Dx()

	pixBuf := C.pixel_buffer_create(C.int(sz), C.int(sz))
	if pixBuf == nil {
		return
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&icon.Pix[0]), C.int(sz), C.int(sz))

	overlayTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	sceneBuf := C.oa_scene_buffer_create(overlayTree, &pixBuf.base)
	if sceneBuf == nil {
		C.pixel_buffer_release(pixBuf)
		return
	}

	fromX, fromY := s.dockCenter()
	toX := winX + float64(winW)/2
	toY := winY + float64(winH)/2

	s.openAnim = &openAnim{
		sceneBuf: unsafe.Pointer(sceneBuf),
		pixBuf:   unsafe.Pointer(pixBuf),
		start:    time.Now(),
		duration: openAnimDuration,
		fromX:    fromX,
		fromY:    fromY,
		toX:      toX,
		toY:      toY,
		iconImg:  icon,
		iconSize: sz,
		viewID:   viewID,
	}

	// Set initial position (centered on dock)
	C.oa_scene_node_set_position(&sceneBuf.node,
		C.int(fromX)-C.int(sz/2),
		C.int(fromY)-C.int(sz/2))
}

// tickOpenAnim advances the open animation. Returns true if still running.
func (s *server) tickOpenAnim() bool {
	oa := s.openAnim
	if oa == nil {
		return false
	}

	elapsed := time.Since(oa.start)
	if elapsed >= oa.duration {
		s.finishOpenAnim()
		return false
	}

	raw := float64(elapsed) / float64(oa.duration)
	sceneBuf := (*C.struct_wlr_scene_buffer)(oa.sceneBuf)
	pixBuf := (*C.struct_pixel_buffer)(oa.pixBuf)
	sz := oa.iconSize

	if raw < 0.60 {
		// Phase 1: slide from dock to window center (easeOutCubic)
		t := easeOutCubic(raw / 0.60)
		cx := oa.fromX + (oa.toX-oa.fromX)*t
		cy := oa.fromY + (oa.toY-oa.fromY)*t
		C.oa_scene_node_set_position(&sceneBuf.node,
			C.int(cx)-C.int(sz/2),
			C.int(cy)-C.int(sz/2))
	} else {
		// Phase 2: icon glitches/destructs at window center
		C.oa_scene_node_set_position(&sceneBuf.node,
			C.int(oa.toX)-C.int(sz/2),
			C.int(oa.toY)-C.int(sz/2))

		glitchT := (raw - 0.60) / 0.40
		glitched := applyOpenGlitch(oa.iconImg, glitchT)

		C.pixel_buffer_update(pixBuf,
			unsafe.Pointer(&glitched.Pix[0]),
			C.int(sz), C.int(sz))
		C.oa_scene_buffer_set_buffer(sceneBuf, nil)
		C.oa_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
	}

	return true
}

// applyOpenGlitch renders a destructuring effect on the icon.
// progress: 0 = clean icon, 1 = fully destructed/transparent.
func applyOpenGlitch(src *image.NRGBA, progress float64) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(dst.Pix, src.Pix)

	// 1. RGB channel split (increases with progress)
	shift := int(progress * 8)
	if shift > 0 {
		for y := 0; y < h; y++ {
			srcOff := y * src.Stride
			dstOff := y * dst.Stride
			for x := 0; x < w; x++ {
				rSrc := min(x+shift, w-1)
				bSrc := max(x-shift, 0)
				dst.Pix[dstOff+x*4] = src.Pix[srcOff+rSrc*4]
				dst.Pix[dstOff+x*4+1] = src.Pix[srcOff+x*4+1]
				dst.Pix[dstOff+x*4+2] = src.Pix[srcOff+bSrc*4+2]
				dst.Pix[dstOff+x*4+3] = src.Pix[srcOff+x*4+3]
			}
		}
	}

	// 2. Scanlines (darken every other row)
	scanDarken := int(255 * progress)
	for y := 0; y < h; y += 2 {
		off := y * dst.Stride
		for x := 0; x < w; x++ {
			idx := off + x*4
			dst.Pix[idx] = uint8(max(int(dst.Pix[idx])-scanDarken, 0))
			dst.Pix[idx+1] = uint8(max(int(dst.Pix[idx+1])-scanDarken, 0))
			dst.Pix[idx+2] = uint8(max(int(dst.Pix[idx+2])-scanDarken, 0))
		}
	}

	// 3. Pixelation (block size grows)
	if progress > 0.2 {
		blockSize := int(2 + (progress-0.2)*20)
		pixelateInPlace(dst, blockSize)
	}

	// 4. Horizontal band displacement
	if progress > 0.3 {
		bandShift := int((progress - 0.3) * 25)
		applyBandShift(dst, bandShift)
	}

	// 5. Alpha fadeout (entire duration, accelerating)
	alpha := uint8(255 * (1 - progress*progress))
	pix := dst.Pix
	for i := 3; i < len(pix); i += 4 {
		pix[i] = uint8(uint16(pix[i]) * uint16(alpha) / 255)
	}

	return dst
}

// finishOpenAnim completes the animation: shows the window, destroys the icon.
func (s *server) finishOpenAnim() {
	oa := s.openAnim
	if oa == nil {
		return
	}

	// Re-enable the window scene tree and restore decorations
	for _, v := range s.xdgViews {
		if v.id == oa.viewID {
			tree := (*C.struct_wlr_scene_tree)(v.sceneTree)
			C.oa_scene_tree_set_enabled(tree, true)
			v.hideDecorations = false
			// The animation removed every decoration node (borders included):
			// reconcile recreates the full set, not just the titlebar.
			s.reconcileXdgDecorations(v)
			break
		}
	}
	for _, v := range s.xwayViews {
		if v.id == oa.viewID {
			tree := (*C.struct_wlr_scene_tree)(v.sceneTree)
			C.oa_scene_tree_set_enabled(tree, true)
			v.hideDecorations = false
			s.reconcileXwayDecorations(v)
			break
		}
	}

	s.cleanupOpenAnim()
}

// cleanupOpenAnim destroys the icon scene buffer.
func (s *server) cleanupOpenAnim() {
	oa := s.openAnim
	if oa == nil {
		return
	}
	if oa.sceneBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(oa.sceneBuf)
		C.oa_scene_node_destroy(&sceneBuf.node)
	}
	releasePixelBuffer(&oa.pixBuf)
	s.openAnim = nil
}

// dockCenter returns the center position of the dock bar on screen.
func (s *server) dockCenter() (float64, float64) {
	out := s.primaryOutput()
	if out == nil {
		return 0, 0
	}
	w := float64(out.width)
	h := float64(out.height)
	ox := float64(out.layoutX)
	oy := float64(out.layoutY)

	if s.barPosition == "bottom" {
		return ox + w/2, oy + h - float64(barWidth)/2
	}
	return ox + float64(barWidth)/2, oy + h/2
}

// loadOpenAnimIcon returns an app icon as NRGBA at openAnimIconSize (cached,
// shared), or a generic one.
func (s *server) loadOpenAnimIcon(appID string) *image.NRGBA {
	names := []string{strings.ToLower(appID), appID}
	if parts := strings.Split(appID, "."); len(parts) > 1 {
		names = append(names, strings.ToLower(parts[len(parts)-1]))
	}
	if icon := s.scaledAppIcon(openAnimIconSize, names...); icon != nil {
		return icon
	}
	return s.genericOpenAnimIcon()
}

// genericOpenAnimIcon returns a generic rounded square icon as fallback.
func (s *server) genericOpenAnimIcon() *image.NRGBA {
	sz := openAnimIconSize
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	bg := color.NRGBA{R: 0x40, G: 0x50, B: 0x70, A: 0xff}
	radius := float64(sz) / 5

	for y := 0; y < sz; y++ {
		for x := 0; x < sz; x++ {
			if inRoundedRect(x, y, sz, sz, radius) {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	return img
}

// inRoundedRect checks if (x,y) is inside a rounded rectangle of dimensions w*h.
func inRoundedRect(x, y, w, h int, r float64) bool {
	fx, fy := float64(x), float64(y)
	fw, fh := float64(w), float64(h)

	if fx >= r && fx <= fw-r {
		return true
	}
	if fy >= r && fy <= fh-r {
		return true
	}

	corners := [][2]float64{
		{r, r},
		{fw - r, r},
		{r, fh - r},
		{fw - r, fh - r},
	}
	for _, c := range corners {
		dx := fx - c[0]
		dy := fy - c[1]
		if math.Sqrt(dx*dx+dy*dy) <= r {
			return true
		}
	}
	return false
}
