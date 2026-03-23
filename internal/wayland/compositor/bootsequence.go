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
	"math"
	"os"
	"time"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Boot sequence diagnostic messages and their reveal times
type bootLine struct {
	text  string
	delay time.Duration
}

var bootMessages = []bootLine{
	{"FYNEDESK WAYLAND COMPOSITOR v2.0", 0},
	{"", 100 * time.Millisecond},
	{"> Initializing display subsystem........ OK", 200 * time.Millisecond},
	{"> Loading wlroots render backend........ OK", 400 * time.Millisecond},
	{"> Configuring output pipeline........... OK", 600 * time.Millisecond},
	{"> Starting XWayland bridge.............. OK", 900 * time.Millisecond},
	{"> Loading keyboard layout............... OK", 1100 * time.Millisecond},
	{"> Building scene graph.................. OK", 1300 * time.Millisecond},
	{"> Loading wallpaper engine.............. OK", 1500 * time.Millisecond},
	{"> Connecting panel interface............", 1800 * time.Millisecond},
	{"> Panel interface....................... OK", 2200 * time.Millisecond},
	{"", 2500 * time.Millisecond},
	{"SYSTEM READY", 2800 * time.Millisecond},
}

const (
	bootTotalDuration = 3500 * time.Millisecond
	bootFadeStart     = 3000 * time.Millisecond
)

// Monospace font cache for boot sequence
var monoFontFace font.Face

func getMonoFontFace() font.Face {
	if monoFontFace != nil {
		return monoFontFace
	}

	fontPaths := []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
		"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
		"/usr/local/share/fonts/dejavu/DejaVuSansMono.ttf", // FreeBSD
		"/usr/local/share/fonts/Liberation/LiberationMono-Regular.ttf",
	}

	for _, p := range fontPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, err := opentype.Parse(data)
		if err != nil {
			continue
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    14,
			DPI:     96,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		monoFontFace = face
		return face
	}
	return nil
}

// fullLayoutBounds returns the bounding rectangle of all connected outputs
// in layout coordinates. Used to create overlays that span all monitors.
func (s *server) fullLayoutBounds() (minX, minY, totalW, totalH int) {
	if len(s.outputs) == 0 {
		return 0, 0, 0, 0
	}
	minX, minY = 1<<30, 1<<30
	maxX, maxY := -(1 << 30), -(1 << 30)
	for _, out := range s.outputs {
		geo := s.getOutputGeometry(out)
		if geo.x < minX {
			minX = geo.x
		}
		if geo.y < minY {
			minY = geo.y
		}
		if geo.x+geo.width > maxX {
			maxX = geo.x + geo.width
		}
		if geo.y+geo.height > maxY {
			maxY = geo.y + geo.height
		}
	}
	return minX, minY, maxX - minX, maxY - minY
}

// startBootSequence creates the boot animation overlay spanning all outputs.
func (s *server) startBootSequence() {
	if s.bootActive || s.reduceMotion {
		return
	}
	if len(s.outputs) == 0 {
		return
	}
	minX, minY, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return
	}

	// Opaque black initial frame
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	pix := img.Pix
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 0xFF
	}

	pixBuf := C.pixel_buffer_create(C.int(w), C.int(h))
	if pixBuf == nil {
		return
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&pix[0]), C.int(w), C.int(h))

	ovTree := (*C.struct_wlr_scene_tree)(s.overlayTree)
	sceneBuf := C.scene_buffer_create(ovTree, &pixBuf.base)
	C.scene_buffer_set_dest_size(sceneBuf, C.int(w), C.int(h))
	C.scene_node_set_position(&sceneBuf.node, C.int(minX), C.int(minY))

	s.bootBuf = unsafe.Pointer(sceneBuf)
	s.bootPixBuf = unsafe.Pointer(pixBuf)
	s.bootImg = img
	s.bootActive = true
	s.bootStart = time.Now()
}

// tickBootSequence renders the next frame of the boot animation.
// Returns true if the animation is still running.
func (s *server) tickBootSequence() bool {
	if !s.bootActive {
		return false
	}

	elapsed := time.Since(s.bootStart)
	if elapsed >= bootTotalDuration {
		s.endBootSequence()
		return false
	}

	if len(s.outputs) == 0 {
		s.endBootSequence()
		return false
	}

	face := getMonoFontFace()
	if face == nil {
		face = getTitleFontFace()
	}
	if face == nil {
		s.endBootSequence()
		return false
	}

	// Recalculate layout bounds each frame: a second output may have
	// connected after startBootSequence(). If the layout grew, recreate
	// the image buffer and scene node at the new size/position.
	minX, minY, newW, newH := s.fullLayoutBounds()
	img := s.bootImg
	sceneBuf := (*C.struct_wlr_scene_buffer)(s.bootBuf)
	if newW > img.Bounds().Dx() || newH > img.Bounds().Dy() {
		img = image.NewNRGBA(image.Rect(0, 0, newW, newH))
		s.bootImg = img

		// Recreate pixel buffer at new size
		if s.bootPixBuf != nil {
			pixBuf := (*C.struct_pixel_buffer)(s.bootPixBuf)
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(newW), C.int(newH))
		}
		C.scene_buffer_set_dest_size(sceneBuf, C.int(newW), C.int(newH))
	}
	// Always update position in case layout shifted
	C.scene_node_set_position(&sceneBuf.node, C.int(minX), C.int(minY))

	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	pix := img.Pix
	stride := img.Stride

	// Clear to black
	for i := 0; i < len(pix); i += 4 {
		pix[i] = 0
		pix[i+1] = 0
		pix[i+2] = 0
		pix[i+3] = 0xFF
	}

	textColor := color.NRGBA{R: 0, G: 210, B: 100, A: 255}
	titleColor := color.NRGBA{R: 0, G: 255, B: 150, A: 255}
	readyColor := color.NRGBA{R: 80, G: 255, B: 180, A: 255}

	lineHeight := 22
	startY := h/4 + lineHeight // baseline of first line
	startX := w / 6

	// Draw visible diagnostic lines
	lineIdx := 0
	for _, line := range bootMessages {
		if elapsed < line.delay {
			break
		}
		if line.text == "" {
			lineIdx++
			continue
		}

		y := startY + lineIdx*lineHeight
		if y > h-80 {
			break
		}

		c := textColor
		if line.text == bootMessages[0].text {
			c = titleColor
		} else if line.text == "SYSTEM READY" {
			c = readyColor
		}

		drawBootText(img, face, line.text, startX, y, c)
		lineIdx++
	}

	// Scan line: bright horizontal band sweeping downward (~1.2s per cycle)
	scanPeriod := 1200.0 // ms per sweep
	scanFrac := math.Mod(float64(elapsed.Milliseconds()), scanPeriod) / scanPeriod
	scanY := int(float64(h) * scanFrac)
	scanHeight := 2
	for dy := 0; dy < scanHeight; dy++ {
		y := scanY + dy
		if y >= h {
			break
		}
		off := y * stride
		for x := 0; x < w; x++ {
			pix[off] = uint8(min(int(pix[off])+25, 255))
			pix[off+1] = uint8(min(int(pix[off+1])+35, 255))
			pix[off+2] = uint8(min(int(pix[off+2])+15, 255))
			off += 4
		}
	}

	// Progress bar at bottom
	barY := h - 60
	barH := 3
	barX := w / 6
	barMaxW := w * 2 / 3
	progress := float64(elapsed) / float64(bootFadeStart)
	if progress > 1 {
		progress = 1
	}
	barW := int(float64(barMaxW) * progress)
	barColor := color.NRGBA{R: 0, G: 200, B: 100, A: 200}

	for dy := 0; dy < barH; dy++ {
		y := barY + dy
		if y >= h {
			break
		}
		off := y * stride
		for x := barX; x < barX+barW && x < w; x++ {
			idx := off + x*4
			pix[idx] = barColor.R
			pix[idx+1] = barColor.G
			pix[idx+2] = barColor.B
			pix[idx+3] = barColor.A
		}
	}

	// Fade out during last 500ms
	if elapsed >= bootFadeStart {
		fadeProgress := float64(elapsed-bootFadeStart) / float64(bootTotalDuration-bootFadeStart)
		alpha := uint8(255.0 * (1.0 - fadeProgress))
		for i := 3; i < len(pix); i += 4 {
			pix[i] = uint8(uint16(pix[i]) * uint16(alpha) / 255)
		}
	}

	// Update scene buffer
	pixBuf := (*C.struct_pixel_buffer)(s.bootPixBuf)
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&pix[0]), C.int(w), C.int(h))
	sceneBuf = (*C.struct_wlr_scene_buffer)(s.bootBuf)
	C.scene_buffer_set_buffer(sceneBuf, nil) // force damage
	C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)

	return true
}

// endBootSequence cleans up the boot animation overlay.
func (s *server) endBootSequence() {
	s.bootActive = false
	if s.bootBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.bootBuf)
		C.scene_node_destroy(&sceneBuf.node)
		s.bootBuf = nil
	}
	if s.bootPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(s.bootPixBuf)
		C.pixel_buffer_destroy(&pixBuf.base)
		s.bootPixBuf = nil
	}
	s.bootImg = nil
}

// drawBootText renders a single line of text on the boot overlay.
func drawBootText(img *image.NRGBA, face font.Face, text string, x, y int, c color.NRGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}
