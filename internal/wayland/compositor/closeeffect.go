package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

struct pixel_buffer {
	struct wlr_buffer base;
	void *data;
	uint32_t format;
	size_t stride;
};
static void pixel_buffer_destroy(struct wlr_buffer *wlr_buf) {
	struct pixel_buffer *buf = (struct pixel_buffer *)wlr_buf;
	free(buf->data);
	free(buf);
}
static bool pixel_buffer_begin_data_ptr_access(struct wlr_buffer *wlr_buf,
		uint32_t flags, void **data, uint32_t *format, size_t *stride) {
	struct pixel_buffer *buf = (struct pixel_buffer *)wlr_buf;
	*data = buf->data; *format = buf->format; *stride = buf->stride;
	return true;
}
static void pixel_buffer_end_data_ptr_access(struct wlr_buffer *wlr_buf) {}
static const struct wlr_buffer_impl pixel_buffer_impl = {
	.destroy = pixel_buffer_destroy,
	.begin_data_ptr_access = pixel_buffer_begin_data_ptr_access,
	.end_data_ptr_access = pixel_buffer_end_data_ptr_access,
};
static struct pixel_buffer *pixel_buffer_create(int w, int h) {
	struct pixel_buffer *buf = calloc(1, sizeof(struct pixel_buffer));
	if (!buf) return NULL;
	buf->format = DRM_FORMAT_ABGR8888;
	buf->stride = (size_t)w * 4;
	buf->data = calloc((size_t)h, buf->stride);
	if (!buf->data) { free(buf); return NULL; }
	wlr_buffer_init(&buf->base, &pixel_buffer_impl, w, h);
	return buf;
}
static void pixel_buffer_update(struct pixel_buffer *buf, const void *pixels, int w, int h) {
	size_t new_stride = (size_t)w * 4;
	size_t new_size = new_stride * (size_t)h;
	if (buf->base.width != w || buf->base.height != h) {
		free(buf->data);
		buf->data = malloc(new_size);
		buf->stride = new_stride;
		buf->base.width = w;
		buf->base.height = h;
	}
	memcpy(buf->data, pixels, new_size);
}
static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
*/
import "C"

import (
	"image"
	"image/color"
	"math/rand"
	"time"
	"unsafe"

	"fyshos.com/fynedesk/internal/wallpaper"
)

const (
	closeAnimDuration       = 300 * time.Millisecond
	closeAnimDurationMatrix = 500 * time.Millisecond
)

// closeAnim holds state for a window close glitch animation.
type closeAnim struct {
	start     time.Time
	x, y      float64 // visual position on screen
	w, h      int     // display size (original window dimensions)
	duration  time.Duration
	srcImg    *image.NRGBA
	buf       unsafe.Pointer // *C.struct_wlr_scene_buffer
	pixBuf    unsafe.Pointer // *C.struct_pixel_buffer
	matrixCol *matrixCloseColumns // per-column rain state (matrix style only)
}

// startCloseAnimXdg creates a close animation for an XDG window.
func (s *server) startCloseAnimXdg(v *xdgView) {
	if s.reduceMotion || v.cachedThumb == nil || v.minimized {
		return
	}

	// Use cached dimensions — accessing xdgToplevel.Base().GetGeometry() during
	// OnDestroy can crash because the underlying wlr_surface may be partially freed.
	winW, winH := v.configuredW, v.configuredH
	if winW <= 0 || winH <= 0 {
		return
	}

	posX, posY := v.x, v.y
	if v.decorated {
		posY -= float64(titlebarHeight)
		winH += titlebarHeight
	}

	s.createCloseAnim(v.cachedThumb, posX, posY, winW, winH)
}

// startCloseAnimXway creates a close animation for an XWayland window.
func (s *server) startCloseAnimXway(v *xwayView) {
	if s.reduceMotion || v.cachedThumb == nil || v.minimized || v.isPanel || v.isOverlay || v.overrideRedirect {
		return
	}

	winW, winH := v.surface.Width(), v.surface.Height()
	if winW <= 0 || winH <= 0 {
		return
	}

	posX, posY := v.x, v.y
	if v.decorated {
		posY -= float64(titlebarHeight)
		winH += titlebarHeight
	}

	s.createCloseAnim(v.cachedThumb, posX, posY, winW, winH)
}

// createCloseAnim sets up the scene buffer and adds to the active animations list.
func (s *server) createCloseAnim(thumb *image.NRGBA, x, y float64, displayW, displayH int) {
	if s.overlayTree == nil {
		return
	}

	b := thumb.Bounds()
	tw, th := b.Dx(), b.Dy()
	if tw <= 0 || th <= 0 {
		return
	}

	isMatrix := s.backgroundType == "matrix"

	// For matrix effect, upscale thumbnail to display resolution so
	// glyphs render at their native 7x14 size instead of being stretched.
	var srcImg *image.NRGBA
	var bufW, bufH int
	if isMatrix && displayW > tw && displayH > th {
		srcImg = upscaleNRGBA(thumb, displayW, displayH)
		bufW, bufH = displayW, displayH
	} else {
		srcImg = image.NewNRGBA(image.Rect(0, 0, tw, th))
		copy(srcImg.Pix, thumb.Pix)
		bufW, bufH = tw, th
	}

	pixBuf := C.pixel_buffer_create(C.int(bufW), C.int(bufH))
	if pixBuf == nil {
		return
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&srcImg.Pix[0]), C.int(bufW), C.int(bufH))

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	sceneBuf := C.scene_buffer_create(ovTree, &pixBuf.base)
	if sceneBuf == nil {
		// scene_buffer_create takes ownership of the buffer on success only;
		// on failure we still own pixBuf and must release it.
		C.pixel_buffer_destroy(&pixBuf.base)
		return
	}
	C.scene_buffer_set_dest_size(sceneBuf, C.int(displayW), C.int(displayH))
	C.scene_node_set_position(&sceneBuf.node, C.int(int(x)), C.int(int(y)))

	dur := closeAnimDuration
	if isMatrix {
		dur = closeAnimDurationMatrix
	}
	anim := &closeAnim{
		start:    time.Now(),
		duration: dur,
		x:        x,
		y:        y,
		w:        displayW,
		h:        displayH,
		srcImg:   srcImg,
		buf:      unsafe.Pointer(sceneBuf),
		pixBuf:   unsafe.Pointer(pixBuf),
	}
	if isMatrix {
		anim.matrixCol = initMatrixCloseColumns(bufW, bufH)
	}

	s.closeAnims = append(s.closeAnims, anim)
}

// tickCloseAnims advances all active close animations.
// Returns true if any are still running.
func (s *server) tickCloseAnims() bool {
	anyActive := false
	remaining := s.closeAnims[:0]

	for _, a := range s.closeAnims {
		elapsed := time.Since(a.start)
		if elapsed >= a.duration {
			// Animation complete — clean up
			destroyCloseAnim(a)
			continue
		}

		progress := float64(elapsed) / float64(a.duration)
		var glitched *image.NRGBA
		if s.backgroundType == "matrix" {
			glitched = applyMatrixGlitch(a.srcImg, progress, a.matrixCol)
		} else {
			glitched = applyGlitch(a.srcImg, progress)
		}

		tw, th := glitched.Bounds().Dx(), glitched.Bounds().Dy()
		pixBuf := (*C.struct_pixel_buffer)(a.pixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&glitched.Pix[0]), C.int(tw), C.int(th))
		sceneBuf := (*C.struct_wlr_scene_buffer)(a.buf)
		C.scene_buffer_set_buffer(sceneBuf, nil)
		C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)

		remaining = append(remaining, a)
		anyActive = true
	}

	s.closeAnims = remaining
	return anyActive
}

func destroyCloseAnim(a *closeAnim) {
	if a.buf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(a.buf)
		C.scene_node_destroy(&sceneBuf.node)
	}
	if a.pixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(a.pixBuf)
		C.pixel_buffer_destroy(&pixBuf.base)
	}
}

// applyGlitch renders the source image with progressive glitch effects.
// progress goes from 0 (clean) to 1 (fully corrupted and faded).
func applyGlitch(src *image.NRGBA, progress float64) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))

	// 1. RGB channel shift: R shifted right, B shifted left
	shift := int(progress * 5)
	for y := 0; y < h; y++ {
		srcOff := y * src.Stride
		dstOff := y * dst.Stride
		for x := 0; x < w; x++ {
			rSrc := min(x+shift, w-1)
			bSrc := max(x-shift, 0)

			dst.Pix[dstOff+x*4] = src.Pix[srcOff+rSrc*4]     // R from shifted
			dst.Pix[dstOff+x*4+1] = src.Pix[srcOff+x*4+1]    // G original
			dst.Pix[dstOff+x*4+2] = src.Pix[srcOff+bSrc*4+2] // B from shifted
			dst.Pix[dstOff+x*4+3] = src.Pix[srcOff+x*4+3]    // A original
		}
	}

	// 2. Scanlines: every other row darkened
	scanDarken := int(200 * progress) // 0-200 darkening
	for y := 0; y < h; y += 2 {
		off := y * dst.Stride
		for x := 0; x < w; x++ {
			idx := off + x*4
			dst.Pix[idx] = uint8(max(int(dst.Pix[idx])-scanDarken, 0))
			dst.Pix[idx+1] = uint8(max(int(dst.Pix[idx+1])-scanDarken, 0))
			dst.Pix[idx+2] = uint8(max(int(dst.Pix[idx+2])-scanDarken, 0))
		}
	}

	// 3. Pixelation: block average with increasing block size
	if progress > 0.25 {
		blockSize := int(2 + (progress-0.25)*20) // 2-17 pixel blocks
		pixelateInPlace(dst, blockSize)
	}

	// 4. Horizontal band displacement
	if progress > 0.4 {
		bandShift := int((progress - 0.4) * 30)
		applyBandShift(dst, bandShift)
	}

	// 5. Alpha fadeout (last 40%)
	if progress > 0.6 {
		fadeFrac := (progress - 0.6) / 0.4
		alpha := uint8(255 * (1 - fadeFrac))
		pix := dst.Pix
		for i := 3; i < len(pix); i += 4 {
			pix[i] = uint8(uint16(pix[i]) * uint16(alpha) / 255)
		}
	}

	return dst
}

// pixelateInPlace replaces each block with its average color.
func pixelateInPlace(img *image.NRGBA, blockSize int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := img.Pix
	stride := img.Stride

	for by := 0; by < h; by += blockSize {
		bh := min(blockSize, h-by)
		for bx := 0; bx < w; bx += blockSize {
			bw := min(blockSize, w-bx)

			// Compute average
			var r, g, bl, a int
			count := bw * bh
			for dy := 0; dy < bh; dy++ {
				off := (by+dy)*stride + bx*4
				for dx := 0; dx < bw; dx++ {
					r += int(pix[off])
					g += int(pix[off+1])
					bl += int(pix[off+2])
					a += int(pix[off+3])
					off += 4
				}
			}
			avg := color.NRGBA{
				R: uint8(r / count),
				G: uint8(g / count),
				B: uint8(bl / count),
				A: uint8(a / count),
			}

			// Fill block with average
			for dy := 0; dy < bh; dy++ {
				off := (by+dy)*stride + bx*4
				for dx := 0; dx < bw; dx++ {
					pix[off] = avg.R
					pix[off+1] = avg.G
					pix[off+2] = avg.B
					pix[off+3] = avg.A
					off += 4
				}
			}
		}
	}
}

// upscaleNRGBA scales an NRGBA image to (dstW, dstH) using nearest-neighbor.
func upscaleNRGBA(src *image.NRGBA, dstW, dstH int) *image.NRGBA {
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		sy := y * srcH / dstH
		srcOff := sy * src.Stride
		dstOff := y * dst.Stride
		for x := 0; x < dstW; x++ {
			sx := x * srcW / dstW
			si := srcOff + sx*4
			di := dstOff + x*4
			dst.Pix[di] = src.Pix[si]
			dst.Pix[di+1] = src.Pix[si+1]
			dst.Pix[di+2] = src.Pix[si+2]
			dst.Pix[di+3] = src.Pix[si+3]
		}
	}
	return dst
}

// matrixCloseColumns holds per-column rain state for the matrix close animation.
type matrixCloseColumns struct {
	speeds    []float64 // fall speed multiplier per column
	glyphSeed []int     // glyph index seed per column
}

const matrixTrailLen = 10 // number of trailing glyphs behind head

// initMatrixCloseColumns creates deterministic per-column rain parameters.
func initMatrixCloseColumns(w, h int) *matrixCloseColumns {
	numCols := w / wallpaper.MatrixGlyphW
	if numCols < 1 {
		numCols = 1
	}
	rng := rand.New(rand.NewSource(int64(w*h + 77)))
	cols := &matrixCloseColumns{
		speeds:    make([]float64, numCols),
		glyphSeed: make([]int, numCols),
	}
	for i := range cols.speeds {
		cols.speeds[i] = 0.6 + rng.Float64()*0.8 // 0.6→1.4
		cols.glyphSeed[i] = rng.Intn(wallpaper.MatrixNumGlyphs)
	}
	return cols
}

// applyMatrixGlitch renders the Matrix-themed close effect.
// Rain columns fall from top to bottom. The head glyph is opaque, trailing
// glyphs fade to transparent over matrixTrailLen positions. Above the trail
// the image is fully transparent. Glyphs rotate as they descend.
func applyMatrixGlitch(src *image.NRGBA, progress float64, cols *matrixCloseColumns) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(dst.Pix, src.Pix)

	pix := dst.Pix
	stride := dst.Stride

	if cols == nil {
		return dst
	}

	numCols := len(cols.speeds)
	glyphH := wallpaper.MatrixGlyphH
	glyphW := wallpaper.MatrixGlyphW
	trailPx := matrixTrailLen * glyphH

	// Frame counter for glyph rotation (changes every ~50ms at 60fps)
	frame := int(progress * 60)

	for ci := 0; ci < numCols; ci++ {
		px := ci * glyphW
		if px+glyphW > w {
			continue
		}

		// Column head position: falls from 0 to h + trailPx
		totalDist := float64(h + trailPx)
		headY := int(progress * cols.speeds[ci] * totalDist)

		// Everything above the trail is transparent (dissolved)
		clearEnd := headY - trailPx
		if clearEnd > h {
			clearEnd = h
		}
		for y := 0; y < clearEnd; y++ {
			off := y*stride + px*4
			for x := 0; x < glyphW && px+x < w; x++ {
				pix[off+x*4] = 0
				pix[off+x*4+1] = 0
				pix[off+x*4+2] = 0
				pix[off+x*4+3] = 0
			}
		}

		// Draw trail: glyph j=1 is right behind head, j=matrixTrailLen is the tail.
		// j=1: nearly opaque, j=matrixTrailLen: fully transparent.
		for j := matrixTrailLen; j >= 1; j-- {
			gy := headY - j*glyphH
			if gy+glyphH <= 0 || gy >= h {
				continue
			}
			// Clear glyph cell to transparent
			clearGlyphCell(pix, stride, px, gy, glyphW, glyphH, w, h)

			// Alpha: 1st behind head = 255, last = 0
			alpha := uint8(255 * (matrixTrailLen - j) / matrixTrailLen)
			if alpha < 10 {
				continue // too faint, skip drawing
			}
			// Green intensity: bright near head, dim at tail
			g := uint8(int(alpha) * 200 / 255)
			if g < 20 {
				g = 20
			}
			// Rotate glyph: changes based on position + frame
			glyphI := (cols.glyphSeed[ci] + j*3 + frame/3) % wallpaper.MatrixNumGlyphs
			wallpaper.DrawMatrixGlyph(pix, stride, px, gy, w, h,
				glyphI, color.NRGBA{R: 0, G: g, B: 0, A: alpha})
		}

		// Draw head glyph (bright white-green, fully opaque)
		if headY+glyphH > 0 && headY < h {
			clearGlyphCell(pix, stride, px, headY, glyphW, glyphH, w, h)
			glyphI := (cols.glyphSeed[ci] + frame/2) % wallpaper.MatrixNumGlyphs
			wallpaper.DrawMatrixGlyph(pix, stride, px, headY, w, h,
				glyphI, color.NRGBA{R: 0xCC, G: 0xFF, B: 0xCC, A: 0xFF})
		}
	}

	return dst
}

// clearGlyphCell clears a glyph-sized rectangle to transparent.
func clearGlyphCell(pix []uint8, stride, px, py, glyphW, glyphH, imgW, imgH int) {
	for row := 0; row < glyphH; row++ {
		y := py + row
		if y < 0 || y >= imgH {
			continue
		}
		off := y*stride + px*4
		for x := 0; x < glyphW && px+x < imgW; x++ {
			pix[off+x*4] = 0
			pix[off+x*4+1] = 0
			pix[off+x*4+2] = 0
			pix[off+x*4+3] = 0
		}
	}
}

// applyBandShift displaces alternating horizontal bands.
func applyBandShift(img *image.NRGBA, maxShift int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	stride := img.Stride
	bandH := max(8, h/12)
	rowBuf := make([]byte, w*4)

	for bandStart := 0; bandStart < h; bandStart += bandH * 2 {
		// Compute shift for this band (alternating direction)
		shift := maxShift
		if (bandStart/bandH)%2 == 1 {
			shift = -maxShift
		}
		if shift == 0 {
			continue
		}

		bandEnd := min(bandStart+bandH, h)
		for y := bandStart; y < bandEnd; y++ {
			off := y * stride
			copy(rowBuf, img.Pix[off:off+w*4])
			for x := 0; x < w; x++ {
				srcX := ((x-shift)%w + w) % w
				dstIdx := off + x*4
				srcIdx := srcX * 4
				img.Pix[dstIdx] = rowBuf[srcIdx]
				img.Pix[dstIdx+1] = rowBuf[srcIdx+1]
				img.Pix[dstIdx+2] = rowBuf[srcIdx+2]
				img.Pix[dstIdx+3] = rowBuf[srcIdx+3]
			}
		}
	}
}
