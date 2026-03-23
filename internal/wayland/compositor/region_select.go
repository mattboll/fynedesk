package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

// pixel_buffer is defined in bootsequence.go — redeclare helpers we need.
// CGO doesn't share static functions across files, so we re-declare minimal wrappers.
struct rs_pixel_buffer {
	struct wlr_buffer base;
	void *data;
	uint32_t format;
	size_t stride;
};
static void rs_pixel_buffer_destroy(struct wlr_buffer *wlr_buf) {
	struct rs_pixel_buffer *buf = (struct rs_pixel_buffer *)wlr_buf;
	free(buf->data);
	free(buf);
}
static bool rs_pixel_buffer_begin_data_ptr_access(struct wlr_buffer *wlr_buf,
		uint32_t flags, void **data, uint32_t *format, size_t *stride) {
	struct rs_pixel_buffer *buf = (struct rs_pixel_buffer *)wlr_buf;
	*data = buf->data; *format = buf->format; *stride = buf->stride;
	return true;
}
static void rs_pixel_buffer_end_data_ptr_access(struct wlr_buffer *wlr_buf) {}
static const struct wlr_buffer_impl rs_pixel_buffer_impl = {
	.destroy = rs_pixel_buffer_destroy,
	.begin_data_ptr_access = rs_pixel_buffer_begin_data_ptr_access,
	.end_data_ptr_access = rs_pixel_buffer_end_data_ptr_access,
};
static struct rs_pixel_buffer *rs_pixel_buffer_create(int w, int h) {
	struct rs_pixel_buffer *buf = calloc(1, sizeof(struct rs_pixel_buffer));
	if (!buf) return NULL;
	buf->format = DRM_FORMAT_ABGR8888;
	buf->stride = (size_t)w * 4;
	buf->data = calloc((size_t)h, buf->stride);
	if (!buf->data) { free(buf); return NULL; }
	wlr_buffer_init(&buf->base, &rs_pixel_buffer_impl, w, h);
	return buf;
}
static void rs_pixel_buffer_update(struct rs_pixel_buffer *buf, const void *pixels, int w, int h) {
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
static struct wlr_scene_buffer *rs_scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void rs_scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void rs_scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void rs_scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void rs_scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
*/
import "C"

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"
)

// startRegionSelect enters region selection mode: a semi-transparent overlay
// is drawn on all outputs and the user can click-drag to select a rectangle.
func (s *server) startRegionSelect() {
	if s.regionSelectActive {
		return
	}

	minX, minY, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return
	}

	// Create the overlay image (semi-transparent dark)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRegionOverlay(img, -1, -1, -1, -1) // No selection yet

	pixBuf := C.rs_pixel_buffer_create(C.int(w), C.int(h))
	if pixBuf == nil {
		return
	}
	C.rs_pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(w), C.int(h))

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	sceneBuf := C.rs_scene_buffer_create(ovTree, &pixBuf.base)
	C.rs_scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
	C.rs_scene_node_set_position(&sceneBuf.node, C.int(minX), C.int(minY))

	s.regionSelBuf = unsafe.Pointer(sceneBuf)
	s.regionSelPixBuf = unsafe.Pointer(pixBuf)
	s.regionSelectActive = true
	s.regionStartX = s.cursor.X()
	s.regionStartY = s.cursor.Y()
	s.regionEndX = s.regionStartX
	s.regionEndY = s.regionStartY

	log.Println("[SCREENSHOT] Region selection started — click and drag to select area, release to capture")
}

// updateRegionSelect redraws the selection overlay as the cursor moves.
func (s *server) updateRegionSelect() {
	if !s.regionSelectActive || s.regionSelPixBuf == nil {
		return
	}

	minX, minY, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return
	}

	// Convert cursor coords to overlay-relative coords
	sx := s.regionStartX - float64(minX)
	sy := s.regionStartY - float64(minY)
	ex := s.cursor.X() - float64(minX)
	ey := s.cursor.Y() - float64(minY)

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRegionOverlay(img, sx, sy, ex, ey)

	pixBuf := (*C.struct_rs_pixel_buffer)(s.regionSelPixBuf)
	C.rs_pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(w), C.int(h))
	sceneBuf := (*C.struct_wlr_scene_buffer)(s.regionSelBuf)
	C.rs_scene_buffer_set_buffer(sceneBuf, nil) // force damage
	C.rs_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
}

// finishRegionSelect captures the selected region and cleans up the overlay.
func (s *server) finishRegionSelect() {
	if !s.regionSelectActive {
		return
	}

	// Calculate selection rectangle in layout coordinates
	x1, y1 := s.regionStartX, s.regionStartY
	x2, y2 := s.cursor.X(), s.cursor.Y()
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	selW := int(x2 - x1)
	selH := int(y2 - y1)

	// Clean up overlay BEFORE capturing (so it's not in the screenshot)
	s.cancelRegionSelect()

	if selW < 5 || selH < 5 {
		log.Println("[SCREENSHOT] Region too small, cancelled")
		return
	}

	// Capture the region with grim
	region := fmt.Sprintf("%d,%d %dx%d", int(x1), int(y1), selW, selH)
	go s.captureRegion(region)
}

// captureRegion runs grim with the specified geometry string.
func (s *server) captureRegion(region string) {
	homeDir, _ := os.UserHomeDir()
	picturesDir := filepath.Join(homeDir, "Pictures")
	os.MkdirAll(picturesDir, 0755)
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := filepath.Join(picturesDir, fmt.Sprintf("screenshot_%s.png", timestamp))

	grimCmd := exec.Command("grim", "-g", region, filename)
	grimCmd.Env = os.Environ()
	if err := grimCmd.Start(); err != nil {
		log.Printf("[SCREENSHOT] grim region capture failed to start: %v", err)
		return
	}
	go func() {
		if err := grimCmd.Wait(); err != nil {
			log.Printf("[SCREENSHOT] grim region capture failed: %v", err)
			return
		}
		log.Printf("[SCREENSHOT] Region saved to %s", filename)
		s.enqueueAction(func() { s.notifyScreenshot(filename) })
	}()
}

// cancelRegionSelect cleans up the region selection overlay without capturing.
func (s *server) cancelRegionSelect() {
	s.regionSelectActive = false
	if s.regionSelBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.regionSelBuf)
		C.rs_scene_node_destroy(&sceneBuf.node)
		s.regionSelBuf = nil
	}
	if s.regionSelPixBuf != nil {
		pixBuf := (*C.struct_rs_pixel_buffer)(s.regionSelPixBuf)
		C.rs_pixel_buffer_destroy(&pixBuf.base)
		s.regionSelPixBuf = nil
	}
}

// fillRegionOverlay renders the selection overlay: dark semi-transparent
// background with a clear "hole" for the selected rectangle and a bright border.
func fillRegionOverlay(img *image.NRGBA, sx, sy, ex, ey float64) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	pix := img.Pix

	// Fill with semi-transparent dark
	dimColor := color.NRGBA{R: 0, G: 0, B: 0, A: 0x80}
	for y := 0; y < h; y++ {
		off := y * img.Stride
		for x := 0; x < w; x++ {
			pix[off] = dimColor.R
			pix[off+1] = dimColor.G
			pix[off+2] = dimColor.B
			pix[off+3] = dimColor.A
			off += 4
		}
	}

	// If there's a selection, cut out the rectangle (make it transparent)
	if sx >= 0 && sy >= 0 && ex >= 0 && ey >= 0 {
		x1, x2 := int(sx), int(ex)
		y1, y2 := int(sy), int(ey)
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		// Clamp to image bounds
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > w {
			x2 = w
		}
		if y2 > h {
			y2 = h
		}

		// Clear the selection area (fully transparent — shows content below)
		for y := y1; y < y2; y++ {
			off := y*img.Stride + x1*4
			for x := x1; x < x2; x++ {
				pix[off] = 0
				pix[off+1] = 0
				pix[off+2] = 0
				pix[off+3] = 0
				off += 4
			}
		}

		// Draw a 2px white border around the selection
		borderColor := color.NRGBA{R: 255, G: 255, B: 255, A: 230}
		for bw := 0; bw < 2; bw++ {
			// Top edge
			if y1-1-bw >= 0 {
				y := y1 - 1 - bw
				off := y*img.Stride + x1*4
				for x := x1; x < x2; x++ {
					pix[off] = borderColor.R
					pix[off+1] = borderColor.G
					pix[off+2] = borderColor.B
					pix[off+3] = borderColor.A
					off += 4
				}
			}
			// Bottom edge
			if y2+bw < h {
				y := y2 + bw
				off := y*img.Stride + x1*4
				for x := x1; x < x2; x++ {
					pix[off] = borderColor.R
					pix[off+1] = borderColor.G
					pix[off+2] = borderColor.B
					pix[off+3] = borderColor.A
					off += 4
				}
			}
			// Left edge
			if x1-1-bw >= 0 {
				x := x1 - 1 - bw
				for y := y1; y < y2; y++ {
					off := y*img.Stride + x*4
					pix[off] = borderColor.R
					pix[off+1] = borderColor.G
					pix[off+2] = borderColor.B
					pix[off+3] = borderColor.A
				}
			}
			// Right edge
			if x2+bw < w {
				x := x2 + bw
				for y := y1; y < y2; y++ {
					off := y*img.Stride + x*4
					pix[off] = borderColor.R
					pix[off+1] = borderColor.G
					pix[off+2] = borderColor.B
					pix[off+3] = borderColor.A
				}
			}
		}

		// Draw dimensions label
		selW := x2 - x1
		selH := y2 - y1
		if selW > 50 && selH > 20 {
			label := fmt.Sprintf("%dx%d", selW, selH)
			_ = label // TODO: could render text, but keeping it simple for now
		}
	}
}
