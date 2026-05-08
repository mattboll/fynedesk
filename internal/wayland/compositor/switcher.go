package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#cgo LDFLAGS: -lGLESv2 -lEGL
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/render/gles2.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>
#include <EGL/egl.h>

// Defined in main.go's CGO preamble (non-static globals)
extern EGLDisplay g_egl_display;
extern EGLContext g_egl_context;

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
	*data = buf->data;
	*format = buf->format;
	*stride = buf->stride;
	return true;
}

static void pixel_buffer_end_data_ptr_access(struct wlr_buffer *wlr_buf) {
	// No-op
}

static const struct wlr_buffer_impl pixel_buffer_impl = {
	.destroy = pixel_buffer_destroy,
	.begin_data_ptr_access = pixel_buffer_begin_data_ptr_access,
	.end_data_ptr_access = pixel_buffer_end_data_ptr_access,
};

// Create a pixel buffer with ABGR8888 format (matches Go image.NRGBA byte order: R,G,B,A)
static struct pixel_buffer *pixel_buffer_create(int w, int h) {
	struct pixel_buffer *buf = calloc(1, sizeof(struct pixel_buffer));
	if (!buf) return NULL;
	buf->format = DRM_FORMAT_ABGR8888; // R,G,B,A byte order = NRGBA
	buf->stride = (size_t)w * 4;
	buf->data = calloc((size_t)h, buf->stride);
	if (!buf->data) {
		free(buf);
		return NULL;
	}
	wlr_buffer_init(&buf->base, &pixel_buffer_impl, w, h);
	return buf;
}

// Update pixel buffer data from Go NRGBA pixel slice. If size changed, reallocate.
static void pixel_buffer_update(struct pixel_buffer *buf, const void *pixels, int w, int h) {
	size_t new_stride = (size_t)w * 4;
	size_t new_size = new_stride * (size_t)h;
	// If dimensions changed, reallocate
	if (buf->base.width != w || buf->base.height != h) {
		free(buf->data);
		buf->data = malloc(new_size);
		buf->stride = new_stride;
		buf->base.width = w;
		buf->base.height = h;
	}
	memcpy(buf->data, pixels, new_size);
}

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

// Lazily-compiled shader program for rendering textures to an FBO.
// Used by blit_surface_texture to composite subsurfaces and read_single_surface
// for XWayland windows.
static GLuint g_thumb_program = 0;
static GLint g_thumb_loc_tex = -1;

static const char *thumb_vs_src =
	"attribute vec2 pos;\n"
	"varying vec2 uv;\n"
	"void main() {\n"
	"  uv = pos * 0.5 + 0.5;\n"
	"  gl_Position = vec4(pos, 0.0, 1.0);\n"
	"}\n";

static const char *thumb_fs_src =
	"precision mediump float;\n"
	"varying vec2 uv;\n"
	"uniform sampler2D tex;\n"
	"void main() {\n"
	"  gl_FragColor = texture2D(tex, uv);\n"
	"}\n";

static int thumb_compile_shader(void) {
	if (g_thumb_program != 0) return 1;

	GLuint vs = glCreateShader(GL_VERTEX_SHADER);
	glShaderSource(vs, 1, &thumb_vs_src, NULL);
	glCompileShader(vs);
	GLint ok = 0;
	glGetShaderiv(vs, GL_COMPILE_STATUS, &ok);
	if (!ok) { glDeleteShader(vs); return 0; }

	GLuint fs = glCreateShader(GL_FRAGMENT_SHADER);
	glShaderSource(fs, 1, &thumb_fs_src, NULL);
	glCompileShader(fs);
	glGetShaderiv(fs, GL_COMPILE_STATUS, &ok);
	if (!ok) { glDeleteShader(vs); glDeleteShader(fs); return 0; }

	g_thumb_program = glCreateProgram();
	glAttachShader(g_thumb_program, vs);
	glAttachShader(g_thumb_program, fs);
	glBindAttribLocation(g_thumb_program, 0, "pos");
	glLinkProgram(g_thumb_program);
	glGetProgramiv(g_thumb_program, GL_LINK_STATUS, &ok);
	glDeleteShader(vs);
	glDeleteShader(fs);
	if (!ok) { glDeleteProgram(g_thumb_program); g_thumb_program = 0; return 0; }

	g_thumb_loc_tex = glGetUniformLocation(g_thumb_program, "tex");
	return 1;
}

// --- Thumbnail capture: persistent FBO, batch EGL, scaled rendering ---

// Persistent FBO for thumbnail capture (avoids create/destroy per window)
static GLuint g_thumb_fbo = 0;
static GLuint g_thumb_render_tex = 0;
static int g_thumb_fbo_w = 0;
static int g_thumb_fbo_h = 0;

// Activate EGL context for a batch of thumbnail captures.
// Call once before capturing multiple windows, then end_thumb_capture after.
static int begin_thumb_capture(void) {
	if (g_egl_display == EGL_NO_DISPLAY || g_egl_context == EGL_NO_CONTEXT)
		return 0;
	return eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE,
		g_egl_context) ? 1 : 0;
}

static void end_thumb_capture(void) {
	glBindFramebuffer(GL_FRAMEBUFFER, 0);
	glUseProgram(0);
	eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
}

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
	int tw = tex->width;
	int th = tex->height;
	if (tw <= 0 || th <= 0) return;

	struct wlr_gles2_texture_attribs attribs;
	wlr_gles2_texture_get_attribs(tex, &attribs);
	if (attribs.target != GL_TEXTURE_2D) return;

	glUseProgram(g_thumb_program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, attribs.tex);
	glUniform1i(g_thumb_loc_tex, 0);

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
// Must be called between begin_thumb_capture/end_thumb_capture.
// out_data must be at least maxW*maxH*4 bytes. Returns 1 on success.
static int capture_xdg_thumb(struct wlr_xdg_surface *xdg_surface,
		int maxW, int maxH, void *out_data, int *out_w, int *out_h) {
	struct wlr_box geo = {0};
	wlr_xdg_surface_get_geometry(xdg_surface, &geo);
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
// Must be called between begin_thumb_capture/end_thumb_capture.
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
	if (attribs.target != GL_TEXTURE_2D) return 0;

	int tw, th;
	thumb_dimensions(win_w, win_h, maxW, maxH, &tw, &th);
	if (!setup_thumb_fbo(tw, th)) return 0;

	glViewport(0, 0, tw, th);
	glDisable(GL_BLEND);
	glDisable(GL_SCISSOR_TEST);
	glClearColor(0, 0, 0, 0);
	glClear(GL_COLOR_BUFFER_BIT);
	glUseProgram(g_thumb_program);
	glActiveTexture(GL_TEXTURE0);
	glBindTexture(GL_TEXTURE_2D, attribs.tex);
	glUniform1i(g_thumb_loc_tex, 0);

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
static void sw_scene_buffer_set_opacity(struct wlr_scene_buffer *buf, float opacity) {
    wlr_scene_buffer_set_opacity(buf, opacity);
}
static void sw_scene_node_destroy(struct wlr_scene_node *node) {
    wlr_scene_node_destroy(node);
}
static void sw_pixel_buffer_free(struct pixel_buffer *buf) {
    if (!buf) return;
    free(buf->data);
    free(buf);
}
static struct wlr_scene_rect *sw_scene_rect_create(struct wlr_scene_tree *parent, int w, int h, const float color[4]) {
    return wlr_scene_rect_create(parent, w, h, color);
}
static void sw_scene_rect_set_color(struct wlr_scene_rect *rect, const float color[4]) {
    wlr_scene_rect_set_color(rect, color);
}
static void sw_scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
*/
import "C"

import (
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/FyshOS/appie"

	"deedles.dev/wlr/xkb"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// captureViewThumbnails captures thumbnails for visible views.
// Called from renderOutput after frame_done. Uses a single EGL context
// activation for all captures, renders directly at thumbnail resolution (~100KB
// per window instead of ~8MB at full res), and reuses a persistent FBO.
// Returns true if thumbnails were actually captured (false if throttled).
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
	if C.begin_thumb_capture() == 0 {
		if s.thumbCaptureFailLogged == 0 {
			log.Println("[THUMB] begin_thumb_capture() failed: EGL context not available")
			s.thumbCaptureFailLogged = 1
		}
		return false
	}
	if s.thumbCaptureFailLogged != 0 {
		log.Println("[THUMB] begin_thumb_capture() succeeded after previous failure")
		s.thumbCaptureFailLogged = 0
	}
	defer C.end_thumb_capture()

	// Reusable buffer for thumbnail pixels (maxW * maxH * 4 = ~107KB)
	pix := make([]byte, thumbMaxW*thumbMaxH*4)

	if s.switcherActive {
		// Capture windows shown in the active switcher
		for _, w := range s.switcherWindows {
			switch v := w.(type) {
			case *xdgView:
				if thumb := s.captureXDGThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
					v.cachedThumb = thumb
				}
			case *xwayView:
				if thumb := s.captureWlrThumbDirect(v, pix, thumbMaxW, thumbMaxH); thumb != nil {
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

	// Log capture stats periodically (once per ~30 seconds)
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
	return true
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
// EGL context must already be active (from begin_thumb_capture).
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
// EGL context must already be active (from begin_thumb_capture).
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
	if C.begin_thumb_capture() == 0 {
		return
	}
	defer C.end_thumb_capture()
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
	if C.begin_thumb_capture() == 0 {
		return
	}
	defer C.end_thumb_capture()
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

// updateSwitcherScene renders the switcher overlay as a composite NRGBA image
// and updates the scene buffer in switcherTree. Call this when the switcher is
// activated, when the selection changes, or when switching is confirmed/cancelled.
func (s *server) updateSwitcherScene() {
	if !s.switcherActive || len(s.switcherWindows) == 0 {
		// Hide the switcher tree
		setViewSceneEnabled(s.switcherTree, false)
		return
	}

	// Render switcher on the active output (where cursor is)
	outGeo := s.getActiveOutputGeo()
	screenW := outGeo.width
	screenH := outGeo.height
	screenX := outGeo.x
	screenY := outGeo.y

	// --- Layout constants ---
	const (
		gap        = 16
		padOuter   = 24
		textHeight = 22
		iconSz     = switcherIconSize
		selectBdr  = 3
		maxCols    = 6
	)

	n := len(s.switcherWindows)
	cols := n
	if cols > maxCols {
		cols = maxCols
	}
	rows := (n + cols - 1) / cols

	cellW := thumbMaxW
	cellH := thumbMaxH + textHeight

	panelW := cols*cellW + (cols-1)*gap + padOuter*2
	panelH := rows*cellH + (rows-1)*gap + padOuter*2

	if panelW > screenW-40 {
		panelW = screenW - 40
	}
	if panelH > screenH-40 {
		panelH = screenH - 40
	}

	panelX := (screenW - panelW) / 2
	panelY := (screenH - panelH) / 2

	// Create full-screen NRGBA image for the composite
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))

	// --- Screen dimming ---
	dimColor := color.NRGBA{R: 0, G: 0, B: 0, A: 120}
	draw.Draw(img, img.Bounds(), image.NewUniform(dimColor), image.Point{}, draw.Over)

	// --- Panel background ---
	bgColor := color.NRGBA{R: 30, G: 30, B: 35, A: 210}
	bgRect := image.Rect(panelX, panelY, panelX+panelW, panelY+panelH)
	draw.Draw(img, bgRect, image.NewUniform(bgColor), image.Point{}, draw.Over)

	// --- Selection highlight and window entries ---
	hlColor := color.NRGBA{R: 90, G: 160, B: 255, A: 220}
	placeholderColor := color.NRGBA{R: 50, G: 50, B: 55, A: 200}

	face := getTitleFontFace()

	for i, w := range s.switcherWindows {
		row := i / cols
		col := i % cols

		itemsInRow := cols
		if row == rows-1 {
			itemsInRow = n - row*cols
		}
		rowOffsetX := 0
		if itemsInRow < cols {
			rowOffsetX = (cols - itemsInRow) * (cellW + gap) / 2
		}

		cx := panelX + padOuter + rowOffsetX + col*(cellW+gap)
		cy := panelY + padOuter + row*(cellH+gap)

		// --- Get captured thumbnail image ---
		var thumbImg *image.NRGBA
		if i < len(s.switcherThumbImgs) {
			thumbImg = s.switcherThumbImgs[i]
		}
		tw, th := thumbMaxW, thumbMaxH
		if thumbImg != nil {
			tw = thumbImg.Bounds().Dx()
			th = thumbImg.Bounds().Dy()
		}

		thumbX := cx + (cellW-tw)/2
		thumbY := cy + (thumbMaxH-th)/2

		// --- Selection highlight border ---
		if i == s.switcherIndex {
			bx0, by0 := thumbX-selectBdr, thumbY-selectBdr
			bx1, by1 := thumbX+tw+selectBdr, thumbY+th+selectBdr
			// top
			draw.Draw(img, image.Rect(bx0, by0, bx1, by0+selectBdr), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// bottom
			draw.Draw(img, image.Rect(bx0, by1-selectBdr, bx1, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// left
			draw.Draw(img, image.Rect(bx0, by0, bx0+selectBdr, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// right
			draw.Draw(img, image.Rect(bx1-selectBdr, by0, bx1, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
		}

		// --- Draw thumbnail or placeholder ---
		if thumbImg != nil {
			dstRect := image.Rect(thumbX, thumbY, thumbX+tw, thumbY+th)
			draw.Draw(img, dstRect, thumbImg, thumbImg.Bounds().Min, draw.Over)
		} else {
			draw.Draw(img, image.Rect(thumbX, thumbY, thumbX+tw, thumbY+th), image.NewUniform(placeholderColor), image.Point{}, draw.Over)
		}

		// --- App icon overlay (bottom-right of thumbnail) ---
		appID := s.switcherWindowAppID(w)
		iconImg := s.loadSwitcherIconImage(appID)
		if iconImg != nil {
			iconX := thumbX + tw - iconSz - 4
			iconY := thumbY + th - iconSz - 4
			iconRect := image.Rect(iconX, iconY, iconX+iconSz, iconY+iconSz)
			draw.Draw(img, iconRect, iconImg, iconImg.Bounds().Min, draw.Over)
		}

		// --- Title text below thumbnail ---
		if face != nil {
			title := s.switcherDisplayTitle(w)
			if title != "" {
				var txtColor color.NRGBA
				if i == s.switcherIndex {
					txtColor = color.NRGBA{R: 240, G: 240, B: 245, A: 255}
				} else {
					txtColor = color.NRGBA{R: 150, G: 150, B: 155, A: 255}
				}
				s.drawTextOnImage(img, title, cx, cy+thumbMaxH, cellW, textHeight, txtColor, face)
			}
		}
	}

	// --- Update or create scene buffer ---
	if s.switcherPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(s.switcherPixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
		if s.switcherBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_buffer_set_dest_size(sceneBuf, C.int(screenW), C.int(screenH))
			C.scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
		}
	} else if s.switcherTree != nil {
		pixBuf := C.pixel_buffer_create(C.int(screenW), C.int(screenH))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
			swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)
			sceneBuf := C.scene_buffer_create(swTree, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
			s.switcherBuf = unsafe.Pointer(sceneBuf)
			s.switcherPixBuf = unsafe.Pointer(pixBuf)
		}
	}

	// Enable the switcher tree
	setViewSceneEnabled(s.switcherTree, true)

	// Apply fade opacity if animation is active
	if s.switcherBuf != nil && (s.switcherFadeIn || s.switcherFadeOut) {
		opacity := s.switcherFadeOpacity()
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
		C.sw_scene_buffer_set_opacity(sceneBuf, C.float(opacity))
	}
}

const switcherFadeDuration = 150 * time.Millisecond

// switcherFadeOpacity returns the current opacity [0.0, 1.0] based on fade state.
func (s *server) switcherFadeOpacity() float32 {
	elapsed := time.Since(s.switcherFadeStart)
	t := float32(elapsed) / float32(switcherFadeDuration)
	if t > 1 {
		t = 1
	}
	if s.switcherFadeOut {
		return 1 - t
	}
	return t
}

// tickSwitcherFade advances the switcher fade animation. Called from the frame callback.
// Returns true if the animation is still active and needs more frames.
func (s *server) tickSwitcherFade() bool {
	if !s.switcherFadeIn && !s.switcherFadeOut {
		return false
	}

	elapsed := time.Since(s.switcherFadeStart)
	if elapsed >= switcherFadeDuration {
		if s.switcherFadeIn {
			// Fade-in complete: set full opacity
			s.switcherFadeIn = false
			if s.switcherBuf != nil {
				sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
				C.sw_scene_buffer_set_opacity(sceneBuf, 1.0)
			}
			return false
		}
		if s.switcherFadeOut {
			// Fade-out complete: finalize close
			s.switcherFadeOut = false
			s.finishSwitcherClose()
			return false
		}
	}

	// Update opacity
	if s.switcherBuf != nil {
		opacity := s.switcherFadeOpacity()
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
		C.sw_scene_buffer_set_opacity(sceneBuf, C.float(opacity))
	}
	return true
}

// drawTextOnImage draws truncated text centered in a cell on the composite image.
func (s *server) drawTextOnImage(img *image.NRGBA, title string, cx, cy, maxWidth, height int, textColor color.NRGBA, face font.Face) {
	d := &font.Drawer{Face: face}
	for len(title) > 0 {
		adv := d.MeasureString(title)
		if adv.Ceil() <= maxWidth {
			break
		}
		if len(title) > 3 {
			title = title[:len(title)-4] + "..."
		} else {
			title = title[:len(title)-1]
		}
	}
	if len(title) == 0 {
		return
	}

	textWidth := d.MeasureString(title).Ceil()
	if textWidth <= 0 {
		return
	}

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	textH := ascent + descent
	baselineY := cy + (height-textH)/2 + ascent

	textX := cx + (maxWidth-textWidth)/2

	d.Dst = img
	d.Src = image.NewUniform(textColor)
	d.Dot = fixed.P(textX, baselineY)
	d.DrawString(title)
}

// loadSwitcherIconImage loads a switcher icon as an NRGBA image (for compositing).
func (s *server) loadSwitcherIconImage(appID string) *image.NRGBA {
	if appID == "" {
		return nil
	}
	iconPath := appie.FdoLookupIconPath("", switcherIconSize, strings.ToLower(appID))
	if iconPath == "" {
		iconPath = appie.FdoLookupIconPath("", switcherIconSize, appID)
	}
	if iconPath == "" {
		return nil
	}

	f, err := os.Open(iconPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	decoded, _, err := image.Decode(f)
	if err != nil {
		return nil
	}

	scaled := image.NewNRGBA(image.Rect(0, 0, switcherIconSize, switcherIconSize))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
	return scaled
}

// --- Window Overview (Exposé / Mission Control) ---
// Uses per-window wlr_scene_buffers with GPU scaling for crisp thumbnails.
// Original windows are hidden during overview; individual buffers animate
// from real positions to grid positions (GNOME Shell style).

const overviewAnimDuration = 350 * time.Millisecond

// toggleOverview opens or closes the window overview.
func (s *server) toggleOverview() {
	if s.overviewActive {
		s.closeOverview()
		return
	}
	s.openOverview()
}

// openOverview enters overview mode with per-window scene buffers.
func (s *server) openOverview() {
	if s.switcherActive {
		return
	}

	type entry struct {
		view     interface{}
		focusSeq uint64
	}

	var entries []entry
	for _, v := range s.xdgViews {
		if v.mapped && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, entry{view: v, focusSeq: v.focusSeq})
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, entry{view: v, focusSeq: v.focusSeq})
		}
	}
	if len(entries) == 0 {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].focusSeq > entries[j].focusSeq
	})

	outGeo := s.getActiveOutputGeo()
	layout := s.computeOverviewLayout(outGeo, len(entries))
	s.overviewLayout = layout

	// Capture resolution: native window size capped at output dimensions.
	captMaxW := outGeo.width
	captMaxH := outGeo.height

	// Activate EGL for batch thumbnail capture
	eglOK := C.begin_thumb_capture() != 0
	var pix []byte
	if eglOK {
		pix = make([]byte, captMaxW*captMaxH*4)
	}

	swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)

	// Create dim overlay (starts transparent for animation)
	var dimColor [4]C.float // all zeros = transparent black
	dimRect := C.sw_scene_rect_create(swTree, C.int(outGeo.width), C.int(outGeo.height), &dimColor[0])
	if dimRect != nil {
		C.scene_node_set_position(&dimRect.node, C.int(outGeo.x), C.int(outGeo.y))
	}
	s.overviewDimRect = unsafe.Pointer(dimRect)

	// Create per-window entries
	n := len(entries)
	s.overviewEntries = make([]overviewEntry, n)

	for i, ent := range entries {
		row := i / layout.cols
		col := i % layout.cols
		itemsInRow := layout.cols
		if row == layout.rows-1 {
			itemsInRow = n - row*layout.cols
		}
		rowOffset := 0
		if itemsInRow < layout.cols {
			rowOffset = (layout.cols - itemsInRow) * (layout.cellW + layout.gap) / 2
		}
		gridX := outGeo.x + layout.margin + rowOffset + col*(layout.cellW+layout.gap)
		gridY := outGeo.y + layout.margin + row*(layout.cellH+layout.gap)

		realX, realY, realW, realH := s.getViewScreenRect(ent.view)

		// Capture high-res thumbnail
		var thumbImg *image.NRGBA
		if eglOK {
			switch v := ent.view.(type) {
			case *xdgView:
				thumbImg = s.captureXDGThumbDirect(v, pix, captMaxW, captMaxH)
				if thumbImg == nil {
					thumbImg = v.cachedThumb
				}
			case *xwayView:
				thumbImg = s.captureWlrThumbDirect(v, pix, captMaxW, captMaxH)
				if thumbImg == nil {
					thumbImg = v.cachedThumb
				}
			}
		} else {
			// Fallback to cached thumbnails
			switch v := ent.view.(type) {
			case *xdgView:
				thumbImg = v.cachedThumb
			case *xwayView:
				thumbImg = v.cachedThumb
			}
		}

		var sceneBuf unsafe.Pointer
		var pixBuf unsafe.Pointer

		if thumbImg != nil {
			tw := thumbImg.Bounds().Dx()
			th := thumbImg.Bounds().Dy()
			pb := C.pixel_buffer_create(C.int(tw), C.int(th))
			if pb != nil {
				C.pixel_buffer_update(pb, unsafe.Pointer(&thumbImg.Pix[0]), C.int(tw), C.int(th))
				sb := C.scene_buffer_create(swTree, &pb.base)
				if sb != nil {
					// Start at window's real position and size
					C.scene_node_set_position(&sb.node, C.int(realX), C.int(realY))
					C.scene_buffer_set_dest_size(sb, C.int(realW), C.int(realH))
					sceneBuf = unsafe.Pointer(sb)
				}
				pixBuf = unsafe.Pointer(pb)
			}
		}

		// Get and hide original window's scene tree
		var origTree unsafe.Pointer
		switch v := ent.view.(type) {
		case *xdgView:
			origTree = v.sceneTree
		case *xwayView:
			origTree = v.sceneTree
		}
		if origTree != nil {
			setViewSceneEnabled(origTree, false)
		}

		// Fit window into cell while preserving aspect ratio
		fitW, fitH := layout.cellW, layout.thumbH
		if realW > 0 && realH > 0 {
			winAspect := float64(realW) / float64(realH)
			cellAspect := float64(layout.cellW) / float64(layout.thumbH)
			if winAspect > cellAspect {
				// Window is wider than cell — fit by width
				fitH = int(float64(layout.cellW) / winAspect)
			} else {
				// Window is taller than cell — fit by height
				fitW = int(float64(layout.thumbH) * winAspect)
			}
		}
		// Center within cell
		fitX := gridX + (layout.cellW-fitW)/2
		fitY := gridY + (layout.thumbH-fitH)/2

		s.overviewEntries[i] = overviewEntry{
			view: ent.view, sceneBuf: sceneBuf, pixBuf: pixBuf,
			sceneTree: origTree,
			realX: realX, realY: realY, realW: realW, realH: realH,
			gridX: fitX, gridY: fitY, gridW: fitW, gridH: fitH,
		}
	}

	if eglOK {
		C.end_thumb_capture()
	}

	s.overviewActive = true
	setViewSceneEnabled(s.switcherTree, true)

	if s.reduceMotion {
		s.applyOverviewPositions(1.0)
		s.createOverviewTitles()
	} else {
		s.overviewAnimActive = true
		s.overviewAnimStart = time.Now()
		s.overviewAnimClosing = false
		// Render first frame at progress=0 (windows at real positions)
		s.applyOverviewPositions(0.0)
	}
}

// applyOverviewPositions updates per-window scene buffer positions and sizes
// based on the animation progress (0.0 = real positions, 1.0 = grid positions).
// This is extremely lightweight: just setting int positions on scene nodes.
func (s *server) applyOverviewPositions(progress float64) {
	// Update dim overlay alpha
	if s.overviewDimRect != nil {
		dimAlpha := C.float(0.63 * progress)
		var color [4]C.float
		color[3] = dimAlpha
		C.sw_scene_rect_set_color((*C.struct_wlr_scene_rect)(s.overviewDimRect), &color[0])
	}

	// Update each window thumbnail position and display size
	for _, e := range s.overviewEntries {
		if e.sceneBuf == nil {
			continue
		}
		sb := (*C.struct_wlr_scene_buffer)(e.sceneBuf)

		cx := e.realX + int(float64(e.gridX-e.realX)*progress)
		cy := e.realY + int(float64(e.gridY-e.realY)*progress)
		cw := e.realW + int(float64(e.gridW-e.realW)*progress)
		ch := e.realH + int(float64(e.gridH-e.realH)*progress)
		if cw < 1 {
			cw = 1
		}
		if ch < 1 {
			ch = 1
		}

		C.scene_node_set_position(&sb.node, C.int(cx), C.int(cy))
		C.scene_buffer_set_dest_size(sb, C.int(cw), C.int(ch))
	}
}

// createOverviewTitles renders title labels below each window thumbnail.
// Called once when the overview animation completes (or immediately if reduce motion).
func (s *server) createOverviewTitles() {
	face := getTitleFontFace()
	if face == nil {
		return
	}
	swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)
	txtColor := color.NRGBA{R: 220, G: 220, B: 230, A: 255}

	for i := range s.overviewEntries {
		e := &s.overviewEntries[i]
		title := s.switcherWindowTitle(e.view)
		if title == "" {
			continue
		}

		layout := s.overviewLayout
		imgW := e.gridW
		imgH := layout.titleH
		if imgW < 1 || imgH < 1 {
			continue
		}

		img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
		s.drawTextOnImage(img, title, 0, 0, imgW, imgH, txtColor, face)

		pb := C.pixel_buffer_create(C.int(imgW), C.int(imgH))
		if pb == nil {
			continue
		}
		C.pixel_buffer_update(pb, unsafe.Pointer(&img.Pix[0]), C.int(imgW), C.int(imgH))

		sb := C.scene_buffer_create(swTree, &pb.base)
		if sb == nil {
			C.sw_pixel_buffer_free(pb)
			continue
		}

		C.scene_node_set_position(&sb.node, C.int(e.gridX), C.int(e.gridY+e.gridH))

		e.titleBuf = unsafe.Pointer(sb)
		e.titlePix = unsafe.Pointer(pb)
	}
}

// closeOverview exits overview mode (with animation if enabled).
func (s *server) closeOverview() {
	if !s.overviewActive {
		return
	}
	if s.overviewAnimClosing {
		return
	}

	// Destroy title labels before close animation (they don't animate)
	for i := range s.overviewEntries {
		e := &s.overviewEntries[i]
		if e.titleBuf != nil {
			C.sw_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.titleBuf).node)
			e.titleBuf = nil
		}
		if e.titlePix != nil {
			C.sw_pixel_buffer_free((*C.struct_pixel_buffer)(e.titlePix))
			e.titlePix = nil
		}
	}

	if s.reduceMotion || s.overviewAnimActive {
		s.finishCloseOverview()
		return
	}

	s.overviewAnimActive = true
	s.overviewAnimStart = time.Now()
	s.overviewAnimClosing = true
}

// finishCloseOverview performs the actual cleanup after overview closes.
func (s *server) finishCloseOverview() {
	// Re-enable original window scene trees first (visible under switcherTree)
	for _, e := range s.overviewEntries {
		if e.sceneTree != nil {
			setViewSceneEnabled(e.sceneTree, true)
		}
	}

	// Destroy per-window scene buffers and pixel buffers
	for _, e := range s.overviewEntries {
		if e.sceneBuf != nil {
			C.sw_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.sceneBuf).node)
		}
		if e.pixBuf != nil {
			C.sw_pixel_buffer_free((*C.struct_pixel_buffer)(e.pixBuf))
		}
		if e.titleBuf != nil {
			C.sw_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.titleBuf).node)
		}
		if e.titlePix != nil {
			C.sw_pixel_buffer_free((*C.struct_pixel_buffer)(e.titlePix))
		}
	}

	// Destroy dim overlay
	if s.overviewDimRect != nil {
		C.sw_scene_node_destroy(&(*C.struct_wlr_scene_rect)(s.overviewDimRect).node)
		s.overviewDimRect = nil
	}

	s.overviewActive = false
	s.overviewAnimActive = false
	s.overviewAnimClosing = false
	s.overviewEntries = nil
	setViewSceneEnabled(s.switcherTree, false)
}

// overviewSelectAt checks if (px, py) hits a window thumbnail in the overview grid.
// Returns the index or -1.
func (s *server) overviewSelectAt(px, py int) int {
	layout := s.overviewLayout
	for i, e := range s.overviewEntries {
		if px >= e.gridX && px < e.gridX+e.gridW &&
			py >= e.gridY && py < e.gridY+e.gridH+layout.titleH {
			return i
		}
	}
	return -1
}

// overviewClick handles a mouse click during overview mode.
func (s *server) overviewClick(px, py int) {
	if s.overviewAnimActive {
		return
	}

	idx := s.overviewSelectAt(px, py)
	if idx < 0 || idx >= len(s.overviewEntries) {
		s.closeOverview()
		return
	}

	w := s.overviewEntries[idx].view
	s.finishCloseOverview()

	switch v := w.(type) {
	case *xdgView:
		if v.mapped {
			s.focusXdgView(v)
		}
	case *xwayView:
		if v.mapped {
			s.focusXwayView(v)
		}
	}
}

// handleOverviewKey handles keypresses while overview is active.
func (s *server) handleOverviewKey(sym xkb.KeySym) {
	switch sym {
	case xkb.KeySymEscape:
		s.closeOverview()
	case xkb.KeySymw:
		s.closeOverview()
	}
}

// tickOverviewAnim advances the overview animation. Returns true if still running.
func (s *server) tickOverviewAnim() bool {
	if !s.overviewAnimActive {
		return false
	}

	elapsed := time.Since(s.overviewAnimStart)
	progress := float64(elapsed) / float64(overviewAnimDuration)
	if progress > 1.0 {
		progress = 1.0
	}

	easedProgress := dampedSpring(progress)

	if s.overviewAnimClosing {
		s.applyOverviewPositions(1.0 - easedProgress)
	} else {
		s.applyOverviewPositions(easedProgress)
	}

	if progress >= 1.0 {
		if s.overviewAnimClosing {
			s.finishCloseOverview()
		} else {
			s.overviewAnimActive = false
			s.applyOverviewPositions(1.0)
			s.createOverviewTitles()
		}
		return false
	}
	return true
}

// computeOverviewLayout calculates the grid layout for the overview.
func (s *server) computeOverviewLayout(outGeo outputGeometry, n int) overviewLayoutData {
	const (
		margin = 40
		gap    = 16
		titleH = 24
	)
	cols, rows := overviewGridSize(n, outGeo.width, outGeo.height)
	usableW := outGeo.width - margin*2 - gap*(cols-1)
	usableH := outGeo.height - margin*2 - gap*(rows-1)
	cellW := usableW / cols
	cellH := usableH / rows
	thumbH := cellH - titleH
	if thumbH < 60 {
		thumbH = 60
	}
	return overviewLayoutData{
		screenW: outGeo.width, screenH: outGeo.height,
		screenX: outGeo.x, screenY: outGeo.y,
		cols: cols, rows: rows,
		cellW: cellW, cellH: cellH,
		thumbH: thumbH,
		margin: margin, gap: gap, titleH: titleH,
	}
}

// getViewScreenRect returns a window's approximate screen position and size.
func (s *server) getViewScreenRect(w interface{}) (x, y, width, height int) {
	switch v := w.(type) {
	case *xdgView:
		vw, vh := v.configuredW, v.configuredH
		if vw <= 0 || vh <= 0 {
			vw, vh = 800, 600
		}
		return int(v.x), int(v.y), vw, vh
	case *xwayView:
		return int(v.x), int(v.y), v.surface.Width(), v.surface.Height()
	}
	return 0, 0, 800, 600
}

// overviewGridSize calculates optimal columns and rows for n windows.
func overviewGridSize(n, screenW, screenH int) (cols, rows int) {
	if n <= 0 {
		return 1, 1
	}

	aspect := float64(screenW) / float64(screenH)
	bestCols := 1
	bestScore := math.MaxFloat64

	for c := 1; c <= n && c <= 8; c++ {
		r := (n + c - 1) / c
		gridAspect := float64(c) / float64(r)
		score := math.Abs(gridAspect - aspect)
		if score < bestScore {
			bestScore = score
			bestCols = c
		}
	}

	cols = bestCols
	rows = (n + cols - 1) / cols
	return
}
