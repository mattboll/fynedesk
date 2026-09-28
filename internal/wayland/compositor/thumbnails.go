package compositor

/*
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/render/gles2.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

#include "gl_util.h"
#include "pixel_buffer.h"

// Read raw pixel data from a surface's SHM buffer via memcpy.
// Only tries source buffer (SHM). DMA-BUF/GPU buffers are not supported.
// out_data must be at least w*h*4 bytes. Returns DRM format fourcc on success, 0 on failure.
static uint32_t surface_read_shm(struct wlr_surface *surface,
		void *out_data, int *out_w, int *out_h) {
	if (!surface || !surface->buffer) return 0;

	// Only try source buffer (the original SHM wl_buffer).
	// wlr_client_buffer.base does NOT support begin_data_ptr_access.
	struct wlr_buffer *buf = surface->buffer->source;
	if (!buf) return 0;

	int w = buf->width;
	int h = buf->height;
	if (w <= 0 || h <= 0 || w > 16384 || h > 16384) return 0;

	void *data = NULL;
	uint32_t fmt = 0;
	size_t stride = 0;
	if (!wlr_buffer_begin_data_ptr_access(buf,
			WLR_BUFFER_DATA_PTR_ACCESS_READ, &data, &fmt, &stride))
		return 0;

	if (!data || stride < (size_t)w * 4) {
		wlr_buffer_end_data_ptr_access(buf);
		return 0;
	}

	// Safe memcpy row by row (stride may differ from w*4)
	size_t row_bytes = (size_t)w * 4;
	for (int y = 0; y < h; y++) {
		memcpy((uint8_t*)out_data + y * row_bytes,
		       (uint8_t*)data + y * stride, row_bytes);
	}

	wlr_buffer_end_data_ptr_access(buf);
	*out_w = w;
	*out_h = h;
	return fmt;
}

// Lazily-compiled shader programs for rendering textures to an FBO: one for
// plain textures, one for external (OES) ones, which video and some dmabuf
// clients use. Used by blit_surface_scaled and read_single_surface.
static GLuint g_thumb_programs[2] = {0, 0};
static GLint g_thumb_locs[2] = {-1, -1};

static const char *thumb_vs_src =
	"attribute vec2 pos;\n"
	"varying vec2 uv;\n"
	"void main() {\n"
	"  uv = pos * 0.5 + 0.5;\n"
	"  gl_Position = vec4(pos, 0.0, 1.0);\n"
	"}\n";

static const char *thumb_fs_srcs[2] = {
	"precision mediump float;\n"
	"varying vec2 uv;\n"
	"uniform sampler2D tex;\n"
	"void main() {\n"
	"  gl_FragColor = texture2D(tex, uv);\n"
	"}\n",
	"#extension GL_OES_EGL_image_external : require\n"
	"precision mediump float;\n"
	"varying vec2 uv;\n"
	"uniform samplerExternalOES tex;\n"
	"void main() {\n"
	"  gl_FragColor = texture2D(tex, uv);\n"
	"}\n",
};

// thumb_program returns the program for a texture target, compiling it the
// first time, or 0 if the target is not supported.
static GLuint thumb_program(GLenum target, GLint *loc) {
	int kind;
	if (target == GL_TEXTURE_2D) {
		kind = 0;
	} else if (target == GL_TEXTURE_EXTERNAL_OES) {
		kind = 1;
	} else {
		return 0;
	}
	if (g_thumb_programs[kind] == 0) {
		static const char *const attribs[] = { "pos" };
		GLuint prog = gl_program(thumb_vs_src, thumb_fs_srcs[kind], attribs, 1);
		if (!prog) return 0;
		g_thumb_programs[kind] = prog;
		g_thumb_locs[kind] = glGetUniformLocation(prog, "tex");
	}
	*loc = g_thumb_locs[kind];
	return g_thumb_programs[kind];
}

// thumb_compile_shader makes sure the plain texture program exists.
static int thumb_compile_shader(void) {
	GLint loc;
	return thumb_program(GL_TEXTURE_2D, &loc) != 0;
}

// --- Thumbnail capture: persistent FBO, batch EGL, scaled rendering ---

// Persistent FBO for thumbnail capture (avoids create/destroy per window)
static GLuint g_thumb_fbo = 0;
static GLuint g_thumb_render_tex = 0;
static int g_thumb_fbo_w = 0;
static int g_thumb_fbo_h = 0;

// Ensure the persistent FBO matches the requested dimensions.
static int setup_thumb_fbo(int w, int h) {
	if (!thumb_compile_shader()) return 0;

	if (g_thumb_fbo == 0) {
		glGenTextures(1, &g_thumb_render_tex);
		glGenFramebuffers(1, &g_thumb_fbo);
		g_thumb_fbo_w = 0;
		g_thumb_fbo_h = 0;
	}

	if (g_thumb_fbo_w != w || g_thumb_fbo_h != h) {
		glBindTexture(GL_TEXTURE_2D, g_thumb_render_tex);
		glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA,
			GL_UNSIGNED_BYTE, NULL);
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
		glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);

		glBindFramebuffer(GL_FRAMEBUFFER, g_thumb_fbo);
		glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0,
			GL_TEXTURE_2D, g_thumb_render_tex, 0);

		if (glCheckFramebufferStatus(GL_FRAMEBUFFER) != GL_FRAMEBUFFER_COMPLETE)
			return 0;

		g_thumb_fbo_w = w;
		g_thumb_fbo_h = h;
	} else {
		glBindFramebuffer(GL_FRAMEBUFFER, g_thumb_fbo);
	}
	return 1;
}

// Forget the thumbnail GL objects: they belong to a GL context that is gone
// (renderer recreated after a GPU reset) and are rebuilt lazily.
static void reset_thumb_gl(void) {
	g_thumb_programs[0] = g_thumb_programs[1] = 0;
	g_thumb_locs[0] = g_thumb_locs[1] = -1;
	g_thumb_fbo = 0;
	g_thumb_render_tex = 0;
	g_thumb_fbo_w = 0;
	g_thumb_fbo_h = 0;
}

// Compute thumbnail dimensions preserving aspect ratio.
static void thumb_dimensions(int src_w, int src_h, int max_w, int max_h,
		int *out_w, int *out_h) {
	int tw = max_w, th = max_h;
	// Compare aspect ratios: src_w/src_h vs max_w/max_h
	// Use cross-multiplication to avoid float: src_w * max_h vs max_w * src_h
	if (src_w * max_h > max_w * src_h) {
		// Source is wider → fit to width
		th = src_h * max_w / src_w;
	} else {
		// Source is taller → fit to height
		tw = src_w * max_h / src_h;
	}
	if (tw < 1) tw = 1;
	if (th < 1) th = 1;
	*out_w = tw;
	*out_h = th;
}

// Blit a surface texture into the current FBO, scaling from window coords to thumb coords.
// Does NOT modify the source texture's parameters (safe for wlroots).
static void blit_surface_scaled(struct wlr_surface *surface,
		int sx, int sy, int win_w, int win_h, int thumb_w, int thumb_h) {
	if (!surface || !surface->buffer || !surface->buffer->texture)
		return;

	struct wlr_texture *tex = surface->buffer->texture;
	// The surface's size in window coordinates: the texture of a HiDPI
	// client is larger (scale 2: twice), and would be drawn cropped.
	int tw = surface->current.width;
	int th = surface->current.height;
	if (tw <= 0 || th <= 0) return;

	struct wlr_gles2_texture_attribs attribs;
	wlr_gles2_texture_get_attribs(tex, &attribs);
	GLint loc;
	GLuint prog = thumb_program(attribs.target, &loc);
	if (!prog) return;

	glUseProgram(prog);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(attribs.target, attribs.tex);
	glUniform1i(loc, 0);

	// Scale surface rectangle from window space to thumbnail space
	int vx = sx * thumb_w / win_w;
	int vw = (tw * thumb_w + win_w - 1) / win_w;  // ceil
	int vh = (th * thumb_h + win_h - 1) / win_h;  // ceil
	int vy = thumb_h - (sy * thumb_h / win_h) - vh;
	if (vw < 1) vw = 1;
	if (vh < 1) vh = 1;

	glEnable(GL_SCISSOR_TEST);
	glViewport(vx, vy, vw, vh);
	glScissor(vx, vy, vw, vh);

	GLfloat verts[] = { -1,-1, 1,-1, -1,1, 1,1 };
	glEnableVertexAttribArray(0);
	glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(0);
}

// Callback data for scaled surface iteration
struct composite_thumb_data {
	int win_w, win_h;     // original window dimensions
	int thumb_w, thumb_h; // target thumbnail dimensions
	int geo_x, geo_y;     // geometry offset to subtract from surface coords
};

// Iterator callback: blit each surface (main + subsurfaces) into the FBO
// with scaling and geometry offset adjustment.
static void composite_surface_iterator(struct wlr_surface *surface,
		int sx, int sy, void *data) {
	struct composite_thumb_data *cd = (struct composite_thumb_data *)data;
	blit_surface_scaled(surface, sx - cd->geo_x, sy - cd->geo_y,
		cd->win_w, cd->win_h, cd->thumb_w, cd->thumb_h);
}

// Check if pixel data has any non-zero content at sample points.
static int thumb_has_content(const uint8_t *px, int w, int h) {
	int offsets[5] = {
		((h/2) * w + (w/2)) * 4,
		((h/4) * w + (w/4)) * 4,
		((h/4) * w + (3*w/4)) * 4,
		((3*h/4) * w + (w/4)) * 4,
		((3*h/4) * w + (3*w/4)) * 4,
	};
	for (int i = 0; i < 5; i++) {
		int o = offsets[i];
		if (px[o] || px[o+1] || px[o+2] || px[o+3])
			return 1;
	}
	return 0;
}

// Capture an XDG surface at thumbnail resolution by compositing all subsurfaces.
// Handles Firefox-style clients that render in subsurfaces.
// Must be called between gl_begin/gl_end.
// out_data must be at least maxW*maxH*4 bytes. Returns 1 on success.
static int capture_xdg_thumb(struct wlr_xdg_surface *xdg_surface,
		int maxW, int maxH, void *out_data, int *out_w, int *out_h) {
	struct wlr_box geo = xdg_surface->geometry;
	int win_w = geo.width, win_h = geo.height;
	if (win_w <= 0 || win_h <= 0 || win_w > 16384 || win_h > 16384) return 0;

	int tw, th;
	thumb_dimensions(win_w, win_h, maxW, maxH, &tw, &th);
	if (!setup_thumb_fbo(tw, th)) return 0;

	glClearColor(0, 0, 0, 0);
	glClear(GL_COLOR_BUFFER_BIT);
	glDisable(GL_BLEND);

	struct composite_thumb_data cd = {
		.win_w = win_w, .win_h = win_h,
		.thumb_w = tw, .thumb_h = th,
		.geo_x = geo.x, .geo_y = geo.y
	};
	wlr_xdg_surface_for_each_surface(xdg_surface,
		composite_surface_iterator, &cd);

	glDisable(GL_SCISSOR_TEST);
	glViewport(0, 0, tw, th);
	glReadPixels(0, 0, tw, th, GL_RGBA, GL_UNSIGNED_BYTE, out_data);

	if (glGetError() != GL_NO_ERROR) return 0;
	if (!thumb_has_content((uint8_t*)out_data, tw, th)) return 0;

	*out_w = tw;
	*out_h = th;
	return 1;
}

// Capture a single wlr_surface at thumbnail resolution (for XWayland windows).
// Must be called between gl_begin/gl_end.
// out_data must be at least maxW*maxH*4 bytes. Returns 1 on success.
static int capture_wlr_thumb(struct wlr_surface *surface,
		int maxW, int maxH, void *out_data, int *out_w, int *out_h) {
	if (!surface || !surface->buffer || !surface->buffer->texture)
		return 0;
	struct wlr_texture *tex = surface->buffer->texture;
	int win_w = tex->width, win_h = tex->height;
	if (win_w <= 0 || win_h <= 0 || win_w > 16384 || win_h > 16384) return 0;

	struct wlr_gles2_texture_attribs attribs;
	wlr_gles2_texture_get_attribs(tex, &attribs);
	GLint loc;
	GLuint prog = thumb_program(attribs.target, &loc);
	if (!prog) return 0;

	int tw, th;
	thumb_dimensions(win_w, win_h, maxW, maxH, &tw, &th);
	if (!setup_thumb_fbo(tw, th)) return 0;

	glViewport(0, 0, tw, th);
	glDisable(GL_BLEND);
	glDisable(GL_SCISSOR_TEST);
	glClearColor(0, 0, 0, 0);
	glClear(GL_COLOR_BUFFER_BIT);
	glUseProgram(prog);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(attribs.target, attribs.tex);
	glUniform1i(loc, 0);

	GLfloat verts[] = { -1,-1, 1,-1, -1,1, 1,1 };
	glEnableVertexAttribArray(0);
	glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 0, verts);
	glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
	glDisableVertexAttribArray(0);

	glReadPixels(0, 0, tw, th, GL_RGBA, GL_UNSIGNED_BYTE, out_data);

	if (glGetError() != GL_NO_ERROR) return 0;
	if (!thumb_has_content((uint8_t*)out_data, tw, th)) return 0;

	*out_w = tw;
	*out_h = th;
	return 1;
}

*/
import "C"

import (
	"bytes"
	"image"
	"log"
	"time"
	"unsafe"

	"golang.org/x/image/draw"
)

// captureViewThumbnails captures thumbnails for visible views.
// Called from renderOutput after frame_done. Uses a single EGL context
// activation for all captures, renders directly at thumbnail resolution (~100KB
// per window instead of ~8MB at full res), and reuses a persistent FBO.
// Returns true if a thumbnail shown in the switcher changed.
func (s *server) captureViewThumbnails() bool {
	hasPending := len(s.previewPendingIDs) > 0
	if s.locked.Load() || (!s.switcherActive && !hasPending) {
		return false
	}
	now := time.Now()
	if now.Sub(s.lastThumbCapture) < 100*time.Millisecond {
		return false
	}
	s.lastThumbCapture = now

	// Activate EGL once for the entire batch
	if !C.gl_begin() {
		if s.thumbCaptureFailLogged == 0 {
			log.Println("[THUMB] gl_begin() failed: EGL context not available")
			s.thumbCaptureFailLogged = 1
		}
		return false
	}
	if s.thumbCaptureFailLogged != 0 {
		log.Println("[THUMB] gl_begin() succeeded after previous failure")
		s.thumbCaptureFailLogged = 0
	}
	defer C.gl_end()

	// Scratch buffer for thumbnail pixels (maxW * maxH * 4 = ~107KB),
	// shared by the captures of this pass.
	pix := make([]byte, thumbMaxW*thumbMaxH*4)

	changed := false
	if s.switcherActive {
		// Capture windows shown in the active switcher; the switcher is
		// composed again only if one of them looks different.
		for _, w := range s.switcherWindows {
			switch v := w.(type) {
			case *xdgView:
				if thumb := s.captureXDGThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
					changed = changed || !sameImage(v.cachedThumb, thumb)
					v.cachedThumb = thumb
				}
			case *xwayView:
				if thumb := s.captureWlrThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
					changed = changed || !sameImage(v.cachedThumb, thumb)
					v.cachedThumb = thumb
				}
			}
		}
	}

	// Capture windows requested by taskbar hover previews
	if hasPending {
		pending := s.previewPendingIDs
		s.previewPendingIDs = nil
		for _, id := range pending {
			s.captureThumbByID(id, pix)
		}
	}

	// Log capture stats every 60 captures
	if s.thumbCaptureCount%60 == 1 {
		xdgWithThumb, xwayWithThumb := 0, 0
		for _, v := range s.xdgViews {
			if v.mapped && v.cachedThumb != nil {
				xdgWithThumb++
			}
		}
		for _, v := range s.xwayViews {
			if v.mapped && !v.isPanel && !v.isOverlay && v.cachedThumb != nil {
				xwayWithThumb++
			}
		}
		log.Printf("[THUMB] capture #%d: xdg=%d xway=%d with thumbs", s.thumbCaptureCount, xdgWithThumb, xwayWithThumb)
	}
	s.thumbCaptureCount++
	return changed
}

// sameImage reports whether two thumbnails have the same pixels.
func sameImage(a, b *image.NRGBA) bool {
	return a != nil && b != nil && a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix)
}

// previewFreshness is how old the thumbnail of a window can be when the bar
// shows it.
const previewFreshness = 500 * time.Millisecond

// captureForPreview captures the thumbnail of a window now, for the bar's
// preview, unless it did a moment ago. The preview used to get the thumbnail
// kept from whenever it was captured last, possibly long ago, or none: the
// capture was left to the next frame and the bar asked again 600 ms later.
// Main thread.
func (s *server) captureForPreview(id string) {
	if t, ok := s.previewTimes[id]; ok && time.Since(t) < previewFreshness {
		return
	}
	if !C.gl_begin() {
		return // the next frame captures it (schedulePreviewCapture)
	}
	pix := make([]byte, thumbMaxW*thumbMaxH*4)
	s.captureThumbByID(id, pix)
	C.gl_end()
	if s.previewTimes == nil || len(s.previewTimes) > 64 {
		s.previewTimes = map[string]time.Time{}
	}
	s.previewTimes[id] = time.Now()
}

// captureThumbByID captures a thumbnail for the window with the given ID.
// EGL context must already be active (called within captureViewThumbnails).
func (s *server) captureThumbByID(id string, pix []byte) {
	for _, v := range s.xdgViews {
		if v.id == id && v.mapped {
			if thumb := s.captureXDGThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
				v.cachedThumb = thumb
			}
			return
		}
	}
	for _, v := range s.xwayViews {
		if v.id == id && v.mapped {
			if thumb := s.captureWlrThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
				v.cachedThumb = thumb
			}
			return
		}
	}
}

// collectSwitcherThumbs collects cached thumbnails for the current switcher windows.
func (s *server) collectSwitcherThumbs() []*image.NRGBA {
	result := make([]*image.NRGBA, len(s.switcherWindows))
	for i, w := range s.switcherWindows {
		switch v := w.(type) {
		case *xdgView:
			result[i] = v.cachedThumb
		case *xwayView:
			result[i] = v.cachedThumb
		}
	}
	return result
}

// captureXDGThumbDirect captures an XDG view directly at thumbnail resolution.
// EGL context must already be active (from gl_begin).
// pix is a reusable scratch buffer of at least maxW*maxH*4 bytes.
func (s *server) captureXDGThumbDirect(v *xdgView, pix []byte, maxW, maxH int) *image.NRGBA {
	xdgSurf := xdgSurfacePtr(v.xdgToplevel.Base())
	var outW, outH C.int
	if C.capture_xdg_thumb(xdgSurf, C.int(maxW), C.int(maxH),
		unsafe.Pointer(&pix[0]), &outW, &outH) != 0 {
		return s.thumbFromPixels(pix, int(outW), int(outH), v.cachedThumb)
	}

	// SHM fallback (main surface only, needs full-res read + scale)
	surf := surfacePtr(v.xdgToplevel.Base().Surface())
	return s.shmFallbackThumb(surf, maxW, maxH)
}

// captureWlrThumbDirect captures an XWayland view directly at thumbnail resolution.
// EGL context must already be active (from gl_begin).
// pix is a reusable scratch buffer of at least maxW*maxH*4 bytes.
func (s *server) captureWlrThumbDirect(v *xwayView, pix []byte, maxW, maxH int) *image.NRGBA {
	surf := surfacePtr(v.surface.Surface())
	var outW, outH C.int
	if C.capture_wlr_thumb(surf, C.int(maxW), C.int(maxH),
		unsafe.Pointer(&pix[0]), &outW, &outH) != 0 {
		return s.thumbFromPixels(pix, int(outW), int(outH), v.cachedThumb)
	}

	// SHM fallback
	return s.shmFallbackThumb(surf, maxW, maxH)
}

// ensureThumbXdg captures a thumbnail for an XDG view on demand if cachedThumb is nil.
// Called before close/unmap so the close animation has a snapshot to work with.
func (s *server) ensureThumbXdg(v *xdgView) {
	if v.cachedThumb != nil || !v.mapped {
		return
	}
	if !C.gl_begin() {
		return
	}
	defer C.gl_end()
	pix := make([]byte, thumbMaxW*thumbMaxH*4)
	if thumb := s.captureXDGThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
		v.cachedThumb = thumb
	}
}

// ensureThumbXway captures a thumbnail for an XWayland view on demand if cachedThumb is nil.
// Called before close/unmap so the close animation has a snapshot to work with.
func (s *server) ensureThumbXway(v *xwayView) {
	if v.cachedThumb != nil || !v.mapped {
		return
	}
	if !C.gl_begin() {
		return
	}
	defer C.gl_end()
	pix := make([]byte, thumbMaxW*thumbMaxH*4)
	if thumb := s.captureWlrThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
		v.cachedThumb = thumb
	}
}

// thumbFromPixels creates an NRGBA image from GL readback data (already at thumbnail size).
// Forces alpha=255 for opaque thumbnails. Reuses existing image if dimensions match.
func (s *server) thumbFromPixels(pix []byte, w, h int, existing *image.NRGBA) *image.NRGBA {
	n := w * h * 4
	img := existing
	if img == nil || len(img.Pix) != n {
		img = &image.NRGBA{Pix: make([]byte, n), Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	} else {
		img.Rect = image.Rect(0, 0, w, h)
		img.Stride = w * 4
	}
	copy(img.Pix, pix[:n])
	for i := 3; i < n; i += 4 {
		img.Pix[i] = 255
	}
	return img
}

// shmFallbackThumb reads via SHM and scales in Go (slow path, rarely used).
func (s *server) shmFallbackThumb(surf *C.struct_wlr_surface, maxW, maxH int) *image.NRGBA {
	if surf == nil || surf.buffer == nil {
		return nil
	}
	bufW := int(surf.buffer.base.width)
	bufH := int(surf.buffer.base.height)
	if bufW <= 0 || bufH <= 0 || bufW > 16384 || bufH > 16384 {
		return nil
	}
	var outW, outH C.int
	raw := make([]byte, bufW*bufH*4)
	drmFmt := C.surface_read_shm(surf, unsafe.Pointer(&raw[0]), &outW, &outH)
	if drmFmt == 0 {
		return nil
	}
	srcW, srcH := int(outW), int(outH)
	nrgba := make([]byte, srcW*srcH*4)
	convertRawToNRGBA(raw, nrgba, srcW, srcH, uint32(drmFmt))
	for i := 3; i < len(nrgba); i += 4 {
		nrgba[i] = 255
	}
	srcImg := &image.NRGBA{Pix: nrgba, Stride: srcW * 4, Rect: image.Rect(0, 0, srcW, srcH)}

	// Scale preserving aspect ratio
	tw, th := maxW, maxH
	ratio := float64(srcW) / float64(srcH)
	if ratio > float64(maxW)/float64(maxH) {
		th = int(float64(maxW) / ratio)
	} else {
		tw = int(float64(maxH) * ratio)
	}
	if tw <= 0 {
		tw = 1
	}
	if th <= 0 {
		th = 1
	}
	thumbImg := image.NewNRGBA(image.Rect(0, 0, tw, th))
	draw.BiLinear.Scale(thumbImg, thumbImg.Bounds(), srcImg, srcImg.Bounds(), draw.Src, nil)
	return thumbImg
}

// convertRawToNRGBA converts raw pixel data from a DRM format to Go NRGBA byte order (R,G,B,A).
func convertRawToNRGBA(src, dst []byte, w, h int, drmFmt uint32) {
	n := w * h * 4
	switch drmFmt {
	case uint32(C.DRM_FORMAT_ABGR8888):
		// Already R,G,B,A — just copy
		copy(dst[:n], src[:n])
	case uint32(C.DRM_FORMAT_XBGR8888):
		// R,G,B,X → R,G,B,255
		for i := 0; i < n; i += 4 {
			dst[i] = src[i]
			dst[i+1] = src[i+1]
			dst[i+2] = src[i+2]
			dst[i+3] = 255
		}
	case uint32(C.DRM_FORMAT_ARGB8888):
		// LE memory: B,G,R,A → swap R↔B
		for i := 0; i < n; i += 4 {
			dst[i] = src[i+2]   // R
			dst[i+1] = src[i+1] // G
			dst[i+2] = src[i]   // B
			dst[i+3] = src[i+3] // A
		}
	case uint32(C.DRM_FORMAT_XRGB8888):
		// LE memory: B,G,R,X → swap R↔B, alpha=255
		for i := 0; i < n; i += 4 {
			dst[i] = src[i+2]   // R
			dst[i+1] = src[i+1] // G
			dst[i+2] = src[i]   // B
			dst[i+3] = 255
		}
	default:
		// Unknown format — gray placeholder
		for i := 0; i < n; i += 4 {
			dst[i] = 80
			dst[i+1] = 80
			dst[i+2] = 80
			dst[i+3] = 255
		}
	}
}

// resetThumbGL drops the thumbnail GL objects (see reset_thumb_gl).
func resetThumbGL() {
	C.reset_thumb_gl()
}
