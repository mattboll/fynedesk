// Package compositor implements a Wayland compositor using wlroots bindings.
// The FyneDesk UI runs as a separate Fyne client application through XWayland.
package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#cgo LDFLAGS: -lGLESv2 -lEGL
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wlr/render/dmabuf.h>
#include <wayland-server-core.h>
#include <wlr/xwayland.h>
#include <wlr/types/wlr_seat.h>
#include <wlr/types/wlr_xdg_shell.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_output_layout.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/xcursor.h>
#include <wlr/render/gles2.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <wlr/types/wlr_data_device.h>
#include <wlr/types/wlr_primary_selection.h>
#include <drm_fourcc.h>
#include <EGL/egl.h>
#include <wlr/types/wlr_screencopy_v1.h>

// Screencopy manager pointer, shared with desktop.go for idle-frame handling.
// Non-static: accessed via extern in desktop.go's CGO preamble.
struct wlr_screencopy_manager_v1 *g_screencopy_mgr = NULL;

static void set_screencopy_mgr(struct wlr_screencopy_manager_v1 *mgr) {
	g_screencopy_mgr = mgr;
}

// --- Drag and drop support ---
// Auto-accept client drag requests so DnD works in XWayland apps (Firefox, etc.)

static struct wl_listener request_start_drag_listener;
static struct wl_listener start_drag_listener;
static struct wl_listener drag_destroy_listener;
static struct wlr_scene_tree *drag_icon_scene_tree = NULL;
static struct wlr_scene_tree *drag_icon_tree_node = NULL;

static void handle_request_start_drag(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_start_drag_event *event = data;
	struct wlr_seat *seat = event->drag->seat;

	// Try pointer grab serial validation first (most common case).
	if (wlr_seat_validate_pointer_grab_serial(seat, event->origin, event->serial)) {
		wlr_seat_start_pointer_drag(seat, event->drag, event->serial);
		return;
	}
	// Fallback: accept unconditionally. XWayland clients (Firefox, Chrome)
	// may produce serials that don't pass validation because XWayland
	// synthesises them independently of the compositor's serial tracker.
	// Without this fallback, Firefox tab tear-off and file drags fail silently.
	wlr_seat_start_pointer_drag(seat, event->drag, event->serial);
}

static void handle_drag_destroy(struct wl_listener *listener, void *data) {
	drag_icon_tree_node = NULL;
	wl_list_remove(&drag_destroy_listener.link);
}

static void handle_start_drag(struct wl_listener *listener, void *data) {
	struct wlr_drag *drag = data;
	if (drag->icon && drag_icon_scene_tree) {
		drag_icon_tree_node = wlr_scene_drag_icon_create(drag_icon_scene_tree, drag->icon);
	} else {
		drag_icon_tree_node = NULL;
	}
	// Listen for drag end to clear the icon pointer before wlroots frees it
	drag_destroy_listener.notify = handle_drag_destroy;
	wl_signal_add(&drag->events.destroy, &drag_destroy_listener);
}

static void setup_drag_handlers(struct wlr_seat *seat, struct wlr_scene_tree *icon_tree) {
	drag_icon_scene_tree = icon_tree;
	request_start_drag_listener.notify = handle_request_start_drag;
	wl_signal_add(&seat->events.request_start_drag, &request_start_drag_listener);
	start_drag_listener.notify = handle_start_drag;
	wl_signal_add(&seat->events.start_drag, &start_drag_listener);
}

static void update_drag_icon_position(int x, int y) {
	if (drag_icon_tree_node) {
		wlr_scene_node_set_position(&drag_icon_tree_node->node, x, y);
	}
}


// --- Clipboard/Selection support ---
// Approve client clipboard requests so copy/paste works between applications.

#include <unistd.h>
extern void goClipboardChanged(int fd);

static struct wlr_seat *selection_seat = NULL;
static struct wl_listener request_set_selection_listener;
static struct wl_listener request_set_primary_selection_listener;

static void handle_request_set_selection(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_set_selection_event *event = data;
	wlr_seat_set_selection(selection_seat, event->source, event->serial);

	// Capture clipboard text content for history
	if (!event->source) return;

	const char *preferred_mime = NULL;
	char **p;
	wl_array_for_each(p, &event->source->mime_types) {
		if (*p) {
			if (strcmp(*p, "text/plain;charset=utf-8") == 0) {
				preferred_mime = "text/plain;charset=utf-8";
				break;
			}
			if (!preferred_mime && (strcmp(*p, "text/plain") == 0 || strcmp(*p, "UTF8_STRING") == 0)) {
				preferred_mime = *p;
			}
		}
	}
	if (!preferred_mime) return;

	int fds[2];
	if (pipe(fds) != 0) return;

	wlr_data_source_send(event->source, preferred_mime, fds[1]);
	close(fds[1]);

	goClipboardChanged(fds[0]);
}

static void handle_request_set_primary_selection(struct wl_listener *listener, void *data) {
	struct wlr_seat_request_set_primary_selection_event *event = data;
	wlr_seat_set_primary_selection(selection_seat, event->source, event->serial);
}

static void setup_selection_handlers(struct wlr_seat *seat) {
	selection_seat = seat;
	request_set_selection_listener.notify = handle_request_set_selection;
	wl_signal_add(&seat->events.request_set_selection, &request_set_selection_listener);
	request_set_primary_selection_listener.notify = handle_request_set_primary_selection;
	wl_signal_add(&seat->events.request_set_primary_selection, &request_set_primary_selection_listener);
}

// --- Keyboard focus reset ---
// Clear keyboard focus so the next KeyboardNotifyEnter re-sends wl_keyboard.enter
// with a fresh keycodes array (wlroots skips re-enter on same surface).
static void keyboard_clear_focus(struct wlr_seat *seat) {
	wlr_seat_keyboard_clear_focus(seat);
}


// --- wlr_scene wrappers ---

static struct wlr_scene *create_scene(void) {
	return wlr_scene_create();
}

static struct wlr_scene_output *create_scene_output(struct wlr_scene *scene, struct wlr_output *output) {
	return wlr_scene_output_create(scene, output);
}

static void scene_output_set_position(struct wlr_scene_output *so, int x, int y) {
	wlr_scene_output_set_position(so, x, y);
}

// Saved EGL state from the last render pass, used for off-frame GL operations
// (thumbnail capture). Captured while the renderer's EGL context is active.
// Non-static: shared across CGO compilation units (desktop.go writes, switcher.go reads).
EGLDisplay g_egl_display = EGL_NO_DISPLAY;
EGLContext g_egl_context = EGL_NO_CONTEXT;

// Tree nodes
static struct wlr_scene_tree *scene_tree_create(struct wlr_scene_tree *parent) {
	return wlr_scene_tree_create(parent);
}

static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
	wlr_scene_node_set_enabled(node, enabled);
}

static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}

static void scene_node_raise_to_top(struct wlr_scene_node *node) {
	wlr_scene_node_raise_to_top(node);
}

static void scene_node_reparent(struct wlr_scene_node *node, struct wlr_scene_tree *new_parent) {
	wlr_scene_node_reparent(node, new_parent);
}

static void scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}

// Surface nodes
static struct wlr_scene_tree *scene_xdg_surface_create(struct wlr_scene_tree *parent, struct wlr_xdg_surface *surface) {
	return wlr_scene_xdg_surface_create(parent, surface);
}

static struct wlr_scene_tree *scene_subsurface_tree_create(struct wlr_scene_tree *parent, struct wlr_surface *surface) {
	return wlr_scene_subsurface_tree_create(parent, surface);
}

// Rect nodes
static struct wlr_scene_rect *scene_rect_create(struct wlr_scene_tree *parent, int w, int h, const float color[4]) {
	return wlr_scene_rect_create(parent, w, h, color);
}

static void scene_rect_set_size(struct wlr_scene_rect *rect, int w, int h) {
	wlr_scene_rect_set_size(rect, w, h);
}

static void scene_rect_set_color(struct wlr_scene_rect *rect, const float color[4]) {
	wlr_scene_rect_set_color(rect, color);
}

// Buffer nodes
static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}

static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}

static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}

static void scene_buffer_set_opacity(struct wlr_scene_buffer *buf, float opacity) {
	wlr_scene_buffer_set_opacity(buf, opacity);
}

// Get scene_output for an output
static struct wlr_scene_output *scene_get_scene_output(struct wlr_scene *scene, struct wlr_output *output) {
	return wlr_scene_get_scene_output(scene, output);
}

// Hit-test the scene graph: returns the wlr_surface under (lx,ly) with surface-local coords,
// and walks up the tree to find the view data pointer stored in scene_tree.node.data.
static int scene_view_at(struct wlr_scene *scene, double lx, double ly,
		struct wlr_surface **surface_out, double *sx, double *sy, void **data_out) {
	struct wlr_scene_node *node = wlr_scene_node_at(&scene->tree.node, lx, ly, sx, sy);
	if (node == NULL) {
		*surface_out = NULL;
		*data_out = NULL;
		return 0;
	}
	// Walk up the tree to find the view's data pointer
	struct wlr_scene_tree *tree = node->parent;
	while (tree != NULL && tree->node.data == NULL) {
		tree = tree->node.parent;
	}
	*data_out = tree ? tree->node.data : NULL;
	// Check if the hit node is a surface
	if (node->type != WLR_SCENE_NODE_BUFFER) {
		*surface_out = NULL;
		return 0;
	}
	struct wlr_scene_buffer *scene_buffer = wlr_scene_buffer_from_node(node);
	struct wlr_scene_surface *scene_surface = wlr_scene_surface_try_from_buffer(scene_buffer);
	if (!scene_surface) {
		*surface_out = NULL;
		return 0;
	}
	*surface_out = scene_surface->surface;
	return 1;
}

// --- Custom pixel_buffer for NRGBA image data ---
// Implements wlr_buffer_impl so we can feed Go-rendered images (titlebars, wallpaper)
// into the scene graph as wlr_scene_buffer nodes.

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

// Schedule a frame on an output
static void schedule_output_frame(struct wlr_output *output) {
	wlr_output_schedule_frame(output);
}

// Helper to set seat on xwayland - this is missing from the Go bindings
void set_xwayland_seat(struct wlr_xwayland *xwl, struct wlr_seat *seat) {
	wlr_xwayland_set_seat(xwl, seat);
}

// Helper to restack xwayland surface above another (or to top if sibling is NULL)
void restack_xwayland_surface(struct wlr_xwayland_surface *surface, struct wlr_xwayland_surface *sibling, int mode) {
	wlr_xwayland_surface_restack(surface, sibling, mode);
}

// Helper to get XDG toplevel app_id (not exposed in Go bindings)
const char* get_xdg_toplevel_app_id(struct wlr_xdg_toplevel *toplevel) {
	return toplevel->app_id;
}

// Helper to get XWayland surface class (not exposed in Go bindings)
const char* get_xwayland_surface_class(struct wlr_xwayland_surface *surface) {
	return surface->class;
}

// Helper to check if XWayland surface is override-redirect (popups, menus, tooltips)
int is_xwayland_override_redirect(struct wlr_xwayland_surface *surface) {
	return surface->override_redirect ? 1 : 0;
}

// Helpers to get XWayland surface X11 position (not exposed in Go bindings)
int get_xwayland_surface_x(struct wlr_xwayland_surface *surface) {
	return surface->x;
}
int get_xwayland_surface_y(struct wlr_xwayland_surface *surface) {
	return surface->y;
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
*/
import "C"

import (
	_ "embed"
	"image/color"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"deedles.dev/wlr"
	"fyshos.com/fynedesk/wlipc"
)

//go:embed default_bg.png
var defaultBgPNG []byte

// outputPtr extracts the *C.struct_wlr_output from a wlr.Output value.
func outputPtr(o wlr.Output) *C.struct_wlr_output {
	return *(**C.struct_wlr_output)(unsafe.Pointer(&o))
}

// displayPtr extracts the *C.struct_wl_display from a wlr.Display value.
func displayPtr(d wlr.Display) *C.struct_wl_display {
	return *(**C.struct_wl_display)(unsafe.Pointer(&d))
}

// getOutputPhysSize returns the physical dimensions in mm from the wlr_output.
func getOutputPhysSize(out *outputState) (int, int) {
	p := outputPtr(out.output)
	return int(p.phys_width), int(p.phys_height)
}

// setXwaylandSeat associates the XWayland instance with a seat for input handling.
// This is critical for XWayland input to work properly.
// We use unsafe to access the internal C pointers since the Go bindings don't expose SetSeat.
func setXwaylandSeat(xwl wlr.Xwayland, seat wlr.Seat) {
	type xwaylandPtr struct {
		p *C.struct_wlr_xwayland
	}
	type seatPtr struct {
		p *C.struct_wlr_seat
	}

	xwlPtr := (*xwaylandPtr)(unsafe.Pointer(&xwl))
	seatP := (*seatPtr)(unsafe.Pointer(&seat))

	C.set_xwayland_seat(xwlPtr.p, seatP.p)
}

// restackXwaylandSurface puts an XWayland surface above others in the X11 stacking order.
// Mode 0 = Above, Mode 1 = Below
func restackXwaylandSurfaceAbove(surface wlr.XwaylandSurface) {
	type xwaySurfacePtr struct {
		p *C.struct_wlr_xwayland_surface
	}

	surfPtr := (*xwaySurfacePtr)(unsafe.Pointer(&surface))
	// Mode 0 = XCB_STACK_MODE_ABOVE, sibling = NULL means top of stack
	C.restack_xwayland_surface(surfPtr.p, nil, 0)
}

// getXdgToplevelAppID returns the app_id of an XDG toplevel (e.g. "org.mozilla.firefox").
func getXdgToplevelAppID(toplevel wlr.XDGToplevel) string {
	type toplevelPtr struct {
		p *C.struct_wlr_xdg_toplevel
	}
	tPtr := (*toplevelPtr)(unsafe.Pointer(&toplevel))
	cstr := C.get_xdg_toplevel_app_id(tPtr.p)
	if cstr == nil {
		return ""
	}
	return C.GoString(cstr)
}

// getXwaylandSurfaceClass returns the WM_CLASS of an XWayland surface.
func getXwaylandSurfaceClass(surface wlr.XwaylandSurface) string {
	type xwaySurfacePtr struct {
		p *C.struct_wlr_xwayland_surface
	}
	surfPtr := (*xwaySurfacePtr)(unsafe.Pointer(&surface))
	cstr := C.get_xwayland_surface_class(surfPtr.p)
	if cstr == nil {
		return ""
	}
	return C.GoString(cstr)
}

// isXwaylandOverrideRedirect checks if an XWayland surface is override-redirect
// (popup menus, tooltips, dropdowns — should bypass window management).
func isXwaylandOverrideRedirect(surface wlr.XwaylandSurface) bool {
	type xwaySurfacePtr struct {
		p *C.struct_wlr_xwayland_surface
	}
	surfPtr := (*xwaySurfacePtr)(unsafe.Pointer(&surface))
	return C.is_xwayland_override_redirect(surfPtr.p) != 0
}

// getXwaylandSurfacePos returns the X11 position of an XWayland surface.
func getXwaylandSurfacePos(surface wlr.XwaylandSurface) (int, int) {
	type xwaySurfacePtr struct {
		p *C.struct_wlr_xwayland_surface
	}
	surfPtr := (*xwaySurfacePtr)(unsafe.Pointer(&surface))
	return int(C.get_xwayland_surface_x(surfPtr.p)), int(C.get_xwayland_surface_y(surfPtr.p))
}

// --- Scene graph helpers ---

// surfaceFromCPtr constructs a wlr.Surface from a C pointer (reverse of surfacePtr).
func surfaceFromCPtr(p *C.struct_wlr_surface) wlr.Surface {
	var s wlr.Surface
	*(**C.struct_wlr_surface)(unsafe.Pointer(&s)) = p
	return s
}

// colorToFloat4 converts a color.RGBA to a [4]float32 suitable for wlr_scene_rect.
func colorToFloat4(c color.RGBA) [4]C.float {
	return [4]C.float{
		C.float(float32(c.R) / 255),
		C.float(float32(c.G) / 255),
		C.float(float32(c.B) / 255),
		C.float(float32(c.A) / 255),
	}
}

// xdgSurfacePtr extracts the *C.struct_wlr_xdg_surface from a wlr.XDGSurface Go value.
func xdgSurfacePtr(s wlr.XDGSurface) *C.struct_wlr_xdg_surface {
	return *(**C.struct_wlr_xdg_surface)(unsafe.Pointer(&s))
}

// surfacePtr extracts the *C.struct_wlr_surface from a wlr.Surface Go value.
func surfacePtr(s wlr.Surface) *C.struct_wlr_surface {
	return *(**C.struct_wlr_surface)(unsafe.Pointer(&s))
}

// xdgPopupPtr extracts the *C.struct_wlr_xdg_popup from a wlr.XDGPopup Go value.
func xdgPopupPtr(p wlr.XDGPopup) *C.struct_wlr_xdg_popup {
	return *(**C.struct_wlr_xdg_popup)(unsafe.Pointer(&p))
}

// setViewSceneEnabled toggles the enabled state of a view's scene tree.
func setViewSceneEnabled(sceneTree unsafe.Pointer, enabled bool) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	val := C.int(0)
	if enabled {
		val = 1
	}
	C.scene_node_set_enabled(&tree.node, val)
}

// setViewScenePosition sets the position of a view's scene tree.
func setViewScenePosition(sceneTree unsafe.Pointer, x, y int) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	C.scene_node_set_position(&tree.node, C.int(x), C.int(y))
}

// raiseViewSceneToTop raises a view's scene tree to the top of its parent.
func raiseViewSceneToTop(sceneTree unsafe.Pointer) {
	if sceneTree == nil {
		return
	}
	tree := (*C.struct_wlr_scene_tree)(sceneTree)
	C.scene_node_raise_to_top(&tree.node)
}

func init() {
	// Pin the main goroutine to a single OS thread. EGL requires all calls
	// (eglMakeCurrent, rendering) to happen on the same thread that created
	// the context. Without this, Go's scheduler may migrate the goroutine
	// to a different OS thread, causing EGL_BAD_ACCESS on DRM backends.
	runtime.LockOSThread()
}

// Run starts the Wayland compositor. It takes over the calling goroutine
// and does not return until the compositor shuts down.
func Run() {
	// Reduce GC frequency to avoid pauses on the main thread (locked to OS
	// thread for EGL). Default GOGC=100 can cause 10-50ms stalls; 200 halves
	// the GC rate at the cost of ~2x heap headroom.
	debug.SetGCPercent(200)

	// Handle signals for clean shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	srv := &server{
		currentDesk:       0,
		numDesks:          4, // Default to 4 virtual desktops
		lastInputTime:     time.Now(),
		wmModifier:        wlr.KeyboardModifierLogo, // Default: Super key
		nestedMode:        os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != "",
		mainThreadActions: make(chan func(), 64),
		shutdown:          make(chan struct{}),
	}
	clipServer = srv
	srv.initPowerDefaults()

	// Create Wayland display
	srv.display = wlr.CreateDisplay()

	// Create backend (auto-detects: nested wayland/x11 or DRM)
	srv.backend = wlr.AutocreateBackend(srv.display)
	if !srv.backend.Valid() {
		log.Println("Failed to create backend")
		os.Exit(1)
	}

	// Create renderer
	srv.renderer = wlr.AutocreateRenderer(srv.backend)
	srv.renderer.InitWLDisplay(srv.display)

	// Create allocator
	srv.allocator = wlr.AutocreateAllocator(srv.backend, srv.renderer)
	if !srv.allocator.Valid() {
		log.Println("Failed to create allocator")
		os.Exit(1)
	}

	// Create compositor
	srv.compositor = wlr.CreateCompositor(srv.display, 5, srv.renderer)

	// Create subcompositor for subsurfaces
	wlr.CreateSubcompositor(srv.display)

	// Create data device manager (required for clipboard/GTK apps)
	srv.dataDeviceMgr = wlr.CreateDataDeviceManager(srv.display)

	// Create primary selection manager (required for middle-click paste and XWayland clipboard bridge)
	wlr.CreatePrimarySelectionV1DeviceManager(srv.display)

	// Create output layout
	srv.outLayout = wlr.CreateOutputLayout()

	// Create scene graph (wlr_scene handles damage tracking + rendering)
	srv.scene = unsafe.Pointer(C.create_scene())
	scene := (*C.struct_wlr_scene)(srv.scene)
	sceneTree := &scene.tree

	// Create layer trees (render order: background < panel < windows < override < fullscreen < overlay < switcher)
	srv.backgroundTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.panelTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.windowsTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.overrideTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.fullscreenTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.overlayTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.switcherTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	srv.lockTree = unsafe.Pointer(C.scene_tree_create(sceneTree))
	// Switcher is hidden by default
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(srv.switcherTree).node, 0)
	// Fullscreen layer hidden by default
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(srv.fullscreenTree).node, 0)
	// Lock layer hidden by default (enabled when lock client connects)
	C.scene_node_set_enabled(&(*C.struct_wlr_scene_tree)(srv.lockTree).node, 0)

	// Create XDG output manager (required by grim for output geometry)
	wlr.CreateXDGOutputManagerV1(srv.display, srv.outLayout)

	// Create screencopy manager (allows grim to capture screenshots)
	srv.screencopyMgr = wlr.CreateScreencopyManagerV1(srv.display)
	// Share the C pointer with desktop.go for idle-frame screencopy handling
	mgrPtr := *(*unsafe.Pointer)(unsafe.Pointer(&srv.screencopyMgr))
	C.set_screencopy_mgr((*C.struct_wlr_screencopy_manager_v1)(mgrPtr))

	// Enable fractional scaling (wp_fractional_scale_v1 + wp_viewporter)
	srv.setupFractionalScaling()

	// Create session lock manager (ext-session-lock-v1 for swaylock, etc.)
	setupSessionLock(srv)

	// Create XDG activation manager (xdg-activation-v1 for focus stealing / urgency)
	setupXDGActivation(srv)

	// Create text input / input method managers (text-input-v3 + input-method-v2 for IME)
	setupTextInput(srv)

	// Create security context manager (wp_security_context_v1 for Flatpak sandboxing)
	setupSecurityContext(srv)

	// Handle new outputs (monitors)
	srv.listeners = append(srv.listeners, srv.backend.OnNewOutput(srv.handleNewOutput))

	// Create XDG shell for native Wayland windows
	srv.xdgShell = wlr.CreateXDGShell(srv.display, 3)
	srv.listeners = append(srv.listeners, srv.xdgShell.OnNewSurface(srv.handleNewXDGSurface))

	// Create XDG decoration manager
	srv.xdgDecoMgr = wlr.CreateXDGDecorationManagerV1(srv.display)
	srv.listeners = append(srv.listeners, srv.xdgDecoMgr.OnNewToplevelDecoration(func(mgr wlr.XDGDecorationManagerV1, deco wlr.XDGToplevelDecorationV1) {
		// Find the corresponding xdgView
		surface := deco.Toplevel().Base()
		var view *xdgView
		for _, v := range srv.xdgViews {
			if v.xdgToplevel.Base() == surface {
				view = v
				break
			}
		}

		// Respect the client's decoration preference:
		// - CSD apps (Chrome, Electron) request ClientSide → honor it
		// - SSD apps (Qt, SDL, mpv) request ServerSide → provide SSD
		// - No preference → default to SSD
		applyDecoMode := func(d wlr.XDGToplevelDecorationV1) {
			requested := d.RequestedMode()
			if requested == wlr.XDGToplevelDecorationV1ModeClientSide {
				d.SetMode(wlr.XDGToplevelDecorationV1ModeClientSide)
				if view != nil {
					view.decorated = false
				}
			} else {
				d.SetMode(wlr.XDGToplevelDecorationV1ModeServerSide)
				if view != nil {
					view.decorated = true
				}
			}
		}
		deco.OnRequestMode(func(d wlr.XDGToplevelDecorationV1) {
			applyDecoMode(d)
		})
		applyDecoMode(deco)
	}))

	// Create seat for input BEFORE XWayland (XWayland needs the seat)
	srv.seat = wlr.CreateSeat(srv.display, "seat0")
	srv.listeners = append(srv.listeners, srv.seat.OnRequestSetCursor(srv.handleSetCursorRequest))

	// Enable drag and drop: auto-accept client drag requests and render drag icons.
	// Without this, XWayland apps (Firefox, Chrome) silently fail all DnD operations.
	{
		type seatPtr struct{ p *C.struct_wlr_seat }
		seatP := (*seatPtr)(unsafe.Pointer(&srv.seat))
		C.setup_drag_handlers(seatP.p, (*C.struct_wlr_scene_tree)(srv.overlayTree))

		// Enable clipboard: approve selection requests so copy/paste works between apps.
		C.setup_selection_handlers(seatP.p)
	}

	// Create XWayland for X11 apps (including Fyne)
	srv.xwayland = wlr.CreateXwayland(srv.display, srv.compositor, false)
	if srv.xwayland.Valid() {
		srv.listeners = append(srv.listeners, srv.xwayland.OnNewSurface(srv.handleNewXwaylandSurface))
		// Set the seat on XWayland - critical for input to work!
		setXwaylandSeat(srv.xwayland, srv.seat)
		log.Println("XWayland initialized with seat")
	} else {
		log.Println("Warning: XWayland not available")
	}

	// Create cursor — set XCURSOR_SIZE + XCURSOR_THEME so XWayland clients
	// (via libXcursor) use the same cursor size as the compositor. Without this,
	// libXcursor computes a default from the X screen height (16*H/480) which
	// can be very different in multi-output layouts.
	os.Setenv("XCURSOR_SIZE", "24")
	os.Setenv("XCURSOR_THEME", "Adwaita")
	srv.cursor = wlr.CreateCursor()
	srv.cursor.AttachOutputLayout(srv.outLayout)
	srv.cursorMgr = wlr.CreateXCursorManager("Adwaita", 24)

	// Handle input devices
	srv.listeners = append(srv.listeners, srv.backend.OnNewInput(srv.handleNewInput))

	// Cursor events
	srv.listeners = append(srv.listeners, srv.cursor.OnMotion(srv.handleCursorMotion))
	srv.listeners = append(srv.listeners, srv.cursor.OnMotionAbsolute(srv.handleCursorMotionAbsolute))
	srv.listeners = append(srv.listeners, srv.cursor.OnButton(srv.handleCursorButton))
	srv.listeners = append(srv.listeners, srv.cursor.OnAxis(srv.handleCursorAxis))
	srv.listeners = append(srv.listeners, srv.cursor.OnFrame(srv.handleCursorFrame))

	// Trackpad gesture support (swipe for desktop switching, overview, etc.)
	srv.setupGestures()

	// Load user settings BEFORE starting the backend so that s.backgroundType,
	// keybindings, and other fields are ready when the first output arrives
	// (handleNewOutput fires synchronously during backend.Start).
	// Wallpaper image loading is a no-op here (no outputs yet) but will be
	// handled by loadWallpaperForNewOutput when the output appears.
	srv.loadSettings()
	srv.initHotCorners()
	srv.loadClipboardHistory()

	// Start backend
	if err := srv.backend.Start(); err != nil {
		log.Println("Failed to start backend:", err)
		os.Exit(1)
	}

	// Get socket name and print it
	socket, err := srv.display.AddSocketAuto()
	if err != nil {
		log.Println("Failed to create Wayland socket:", err)
		os.Exit(1)
	}
	log.Printf("FyneDesk Wayland compositor running on WAYLAND_DISPLAY=%s\n", socket)

	// Set XWayland cursor
	if srv.xwayland.Valid() {
		xdisplay := srv.xwayland.Server().DisplayName()
		log.Printf("XWayland display: %s\n", xdisplay)
		os.Setenv("DISPLAY", xdisplay)

		// Set cursor for XWayland
		srv.cursorMgr.Load(1.0)
		xcursor := srv.cursorMgr.GetXCursor("default", 1.0)
		if xcursor.ImageCount() > 0 {
			img := xcursor.Image(0)
			// Extract actual cursor image dimensions via unsafe
			type xcImgPtr struct{ p *C.struct_wlr_xcursor_image }
			imgP := (*xcImgPtr)(unsafe.Pointer(&img))
			log.Printf("[CURSOR] XCursor 'default' at scale=1.0: actual image=%dx%d, hotspot=(%d,%d)\n",
				imgP.p.width, imgP.p.height, imgP.p.hotspot_x, imgP.p.hotspot_y)
			srv.xwayland.SetCursor(img)
		}
	}

	log.Println("Keybindings: Alt+Escape=quit, Alt+Tab=cycle, F11=fullscreen, Alt+F4=close")
	log.Println("             Ctrl+Alt+Left/Right=switch desktop, Super+1-4=goto desktop")
	log.Println("             Super+T/Return=terminal, Super+`=dropdown terminal")
	log.Println("             PrintScreen=screenshot, Shift+PrintScreen=region screenshot")
	log.Println("             Super+L=lock screen, Ctrl+Alt+Backspace=emergency logout")

	// Set the socket in environment for child processes
	os.Setenv("WAYLAND_DISPLAY", socket)

	// Override XDG_CURRENT_DESKTOP:
	// - "FyneDesk" identifies our desktop for xdg-open fallback
	// - "wlroots" enables xdg-desktop-portal-wlr backend (ScreenCast, Screenshot)
	//   which Firefox/Chrome use for WebRTC camera/screen sharing via PipeWire
	os.Setenv("XDG_CURRENT_DESKTOP", "FyneDesk:wlroots")

	// Enable Firefox/Thunderbird to run as native Wayland clients instead of XWayland.
	// This gives better PipeWire integration for WebRTC (camera, mic, screen sharing).
	os.Setenv("MOZ_ENABLE_WAYLAND", "1")

	// Create XDG portal configuration so the portal daemon uses the wlr backend
	// for ScreenCast/Screenshot and GTK backend as fallback for everything else.
	srv.setupPortalConfig()

	// Start gnome-keyring-daemon so apps (Slack, Chrome, etc.) can persist credentials.
	// Only in real session mode — in nested mode the parent session provides it.
	if !srv.nestedMode {
		if _, err := exec.LookPath("gnome-keyring-daemon"); err == nil {
			keyringCmd := exec.Command("gnome-keyring-daemon", "--start", "--components=secrets,pkcs11")
			keyringCmd.Env = safeEnv()
			if out, err := keyringCmd.Output(); err == nil {
				// Parse output lines like "GNOME_KEYRING_CONTROL=/run/user/1000/keyring"
				for _, line := range strings.Split(string(out), "\n") {
					if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
						os.Setenv(parts[0], parts[1])
					}
				}
				log.Println("gnome-keyring-daemon started")
			} else {
				log.Printf("Warning: gnome-keyring-daemon failed: %v\n", err)
			}
		}
	}

	// Push our environment to D-Bus so portals (used by Snap/Flatpak apps) and
	// D-Bus-activated services use our DISPLAY/WAYLAND_DISPLAY when opening URLs etc.
	go func() {
		args := []string{"WAYLAND_DISPLAY", "DISPLAY", "XDG_CURRENT_DESKTOP", "MOZ_ENABLE_WAYLAND"}
		// Only add --systemd flag if systemd is present (not on FreeBSD, non-systemd Linux)
		if _, err := os.Stat("/run/systemd/system"); err == nil {
			args = append([]string{"--systemd"}, args...)
		}
		dbusCmd := exec.Command("dbus-update-activation-environment", args...)
		dbusCmd.Env = safeEnv()
		if err := dbusCmd.Run(); err != nil {
			log.Printf("Warning: dbus-update-activation-environment failed: %v\n", err)
		}
		log.Println("D-Bus activation environment updated (DISPLAY, WAYLAND_DISPLAY, XDG_CURRENT_DESKTOP, MOZ_ENABLE_WAYLAND)")

		// Restart portal daemons so they pick up the new WAYLAND_DISPLAY and
		// XDG_CURRENT_DESKTOP. This is needed in both nested and real session
		// modes: in nested mode xdpw would otherwise stay connected to the
		// host compositor and screen sharing would fail.
		//
		// Skip silently when the unit is not installed (some distros ship
		// xdg-desktop-portal-gtk only). Capture stderr on real failures so
		// the audit log shows *why* — the previous code just logged the
		// exit code, which was useless for debugging.
		for _, svc := range []string{"xdg-desktop-portal-wlr", "xdg-desktop-portal"} {
			// Pre-check unit presence — `systemctl is-enabled` returns 0 for
			// enabled, 1 for disabled, but exit 4 ("unit not found") clearly
			// signals the unit doesn't exist on this system.
			checkCmd := exec.Command("systemctl", "--user", "show", "-p", "LoadState", "--value", svc)
			if out, err := checkCmd.Output(); err == nil {
				state := strings.TrimSpace(string(out))
				if state == "not-found" || state == "" {
					log.Printf("[PORTAL] skipping %s restart: unit not installed", svc)
					continue
				}
			}

			restartCmd := exec.Command("systemctl", "--user", "restart", svc)
			var stderr strings.Builder
			restartCmd.Stderr = &stderr
			if err := restartCmd.Run(); err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				log.Printf("[PORTAL] could not restart %s: %s", svc, msg)
			} else {
				log.Printf("[PORTAL] %s restarted with new environment", svc)
			}
		}
	}()

	// Start the panel process after XWayland is ready
	go func() {
		// Helper: sleep but bail out early on shutdown.
		wait := func(d time.Duration) bool {
			select {
			case <-srv.shutdown:
				return false
			case <-time.After(d):
				return true
			}
		}
		if !wait(2 * time.Second) { // Give XWayland more time to initialize
			return
		}
		srv.startPanel()

		// Restore previous session after panel is ready
		if !wait(3 * time.Second) {
			return
		}
		select {
		case srv.mainThreadActions <- func() { srv.restoreSession() }:
			srv.triggerWakeup()
		case <-srv.shutdown:
			return
		}

		// Fallback: if panel doesn't appear after 10s, launch a terminal
		if !wait(7 * time.Second) {
			return
		}
		if srv.panelXway == nil || !srv.panelXway.mapped {
			log.Println("WARNING: Panel not detected after 10s, launching fallback terminal")
			srv.launchTerminal()
		}
	}()

	// Write initial keyboard layout state for the panel
	if len(srv.keyboardLayouts) > 0 {
		state := wlipc.KeyboardLayoutState{
			ActiveIndex: srv.activeLayoutIndex,
			Layouts:     srv.keyboardLayouts,
		}
		_ = wlipc.NotifyKeyboardLayoutState(state)
	}

	// Restore saved volume level
	srv.restoreVolume()

	// If a previous compositor instance died (crash, kill -9) while the screen
	// was locked, re-lock immediately. Defends against an attacker killing the
	// compositor to bypass the lock screen.
	if srv.wasPreviouslyLocked() {
		log.Println("[LOCK] Previous instance was locked — locking screen immediately on startup")
		srv.idleLocked = true
		go srv.lockScreen()
	}

	// In nested mode, start a private D-Bus session so that child processes
	// (panel, apps) use their own bus instead of the host session bus.
	// This must happen BEFORE registering any D-Bus services.
	srv.startPrivateDBus()

	// Start D-Bus services
	srv.startScreenSaverDBus()
	srv.startNotificationsDBus()
	srv.startPortalDBus()

	// Background IPC flusher: serialize + write windows state off the render thread
	srv.startIPCFlusher()

	// Watch for mode change requests from panel
	go srv.watchModeRequests()
	go srv.watchIdleTimeout()
	go srv.watchSuspendResume()

	// Watchdog: detect event loop stalls and attempt recovery
	go srv.watchdogRecovery()

	// Start UNIX socket IPC server (alongside file-based IPC)
	srv.startSocketIPC()

	// Handle clean shutdown
	go func() {
		<-sigChan
		log.Println("\nShutting down...")
		srv.shuttingDown.Store(true)
		select {
		case <-srv.shutdown:
		default:
			close(srv.shutdown)
		}
		srv.saveSessionState() // Save before terminating (reads are safe from goroutine)
		if srv.panelCmd != nil && srv.panelCmd.Process != nil {
			srv.panelCmd.Process.Kill()
		}
		srv.display.Terminate()
	}()

	// Run event loop
	srv.display.Run()

	// Write shutdown marker so the runner knows this is an intentional exit.
	// If cleanup code crashes (segfault in wlroots), the runner won't restart.
	if srv.shuttingDown.Load() {
		homeDir, _ := os.UserHomeDir()
		markerPath := filepath.Join(homeDir, ".cache", "fyne", "com.fyshos.fynedesk", "shutdown-marker")
		_ = os.MkdirAll(filepath.Dir(markerPath), 0700)
		if err := atomicWriteFile(markerPath, []byte("shutdown")); err != nil {
			log.Printf("Warning: could not write shutdown marker: %v", err)
		}
	}

	// Cleanup
	if srv.ipcServer != nil {
		srv.ipcServer.Close()
	}
	if srv.panelCmd != nil && srv.panelCmd.Process != nil {
		srv.panelCmd.Process.Kill()
	}
	srv.stopPrivateDBus()
	if srv.xwayland.Valid() {
		srv.xwayland.Destroy()
	}
	cleanupSessionLock() // Remove all lock listeners before display.Destroy() to avoid wlroots assertion
	srv.display.Destroy()

	// If we reached this point through a clean shutdown (logout/restart), the
	// user-initiated path: the lock-state marker is no longer relevant and
	// would otherwise re-lock the next session unnecessarily after gdm auth.
	// Crashes never reach here, so the marker survives those.
	srv.markUnlocked()

	log.Println("Compositor terminated")

	// If a "restart" IPC arrived (instead of a clean shutdown), exit with the
	// runner's restart sentinel so fynedesk_runner relaunches us. Doing this
	// after Destroy() — instead of os.Exit(5) from the IPC handler — means
	// wlroots/X resources are released cleanly.
	if srv.wantRestart.Load() {
		os.Exit(5)
	}
}

// setupPortalConfig creates an XDG portal configuration so that
// xdg-desktop-portal-wlr handles ScreenCast/Screenshot (needed for WebRTC
// camera/screen sharing in Firefox/Chrome) and GTK handles the rest.
func (s *server) setupPortalConfig() {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "xdg-desktop-portal")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		log.Printf("Warning: could not create portal config dir: %v\n", err)
		return
	}

	// Use lowercase "fynedesk" to match XDG_CURRENT_DESKTOP after case-folding
	configPath := filepath.Join(configDir, "fynedesk-portals.conf")

	// Our native portal handles Screenshot and Settings.
	// FileChooser is delegated to GTK — zenity (GTK4) cannot display a
	// file dialog inside a wlroots compositor (1x1 unmapped window bug).
	// ScreenCast (PipeWire screen sharing) delegates to wlr backend.
	content := `[preferred]
default=gtk
org.freedesktop.impl.portal.Screenshot=fynedesk;wlr
org.freedesktop.impl.portal.ScreenCast=wlr
org.freedesktop.impl.portal.FileChooser=gtk
org.freedesktop.impl.portal.Settings=fynedesk;gtk
`
	if err := atomicWriteFile(configPath, []byte(content)); err != nil {
		log.Printf("Warning: could not write portal config: %v\n", err)
		return
	}
	log.Printf("Portal config written to %s\n", configPath)
}
