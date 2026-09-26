// GL helpers of the effects drawn with the renderer's GLES2 context: the
// blur (blur.c), the wobbly windows (wobble.c) and the window thumbnails
// (switcher.go).
#ifndef TYDE_GL_UTIL_H
#define TYDE_GL_UTIL_H

#include <stdbool.h>
#include <EGL/egl.h>
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>
#include <wlr/render/drm_format_set.h>
#include <wlr/types/wlr_output.h>

// Defined in main.go: the renderer's EGL display and context.
extern EGLDisplay g_egl_display;
extern EGLContext g_egl_context;

// gl_available reports whether the renderer is GLES2 with a known EGL
// display (with pixman, the effects are left out).
bool gl_available(struct wlr_output *output);

// gl_begin makes the renderer's EGL context current, gl_end flushes and
// releases it. wlroots switches contexts itself: make the textures of
// buffers before gl_begin.
bool gl_begin(void);
void gl_end(void);

// gl_program compiles and links a program, binding attribute i to
// attribs[i]. It returns 0 if it fails.
GLuint gl_program(const char *vs, const char *fs, const char *const *attribs, int nattribs);

// gl_bind_texture binds tex to a texture unit, sampled linearly and clamped
// to its edges.
void gl_bind_texture(GLenum target, GLuint tex, int unit);

// gl_quad draws the rectangle (x, y, w, h) of a target of size (tw, th), row
// 0 at the top like the textures of wlroots. Attribute 0 is the position;
// attribute i+1 samples the rectangle coords[i] = {u0, v0, u1, v1}.
void gl_quad(double x, double y, double w, double h, int tw, int th, const double coords[][4], int ncoords);

// gl_output_formats fills formats with ARGB8888 in the layouts of the
// output's buffers, which the GPU renders to. Finish it after use.
void gl_output_formats(struct wlr_output *output, struct wlr_drm_format_set *formats);

// Fragment shaders shared by the effects.
#define GL_OES_EXTENSION "#extension GL_OES_EGL_image_external : require\n"
// A premultiplied texture faded by alpha; opaque ignores the alpha channel
// of those that have none (XRGB).
#define GL_FS_TEXTURE(sampler) \
    "precision mediump float;\n" \
    "varying vec2 uv;\n" \
    "uniform " sampler " tex;\n" \
    "uniform float alpha;\n" \
    "uniform float opaque;\n" \
    "void main() {\n" \
    "  vec4 c = texture2D(tex, uv);\n" \
    "  c.a = max(c.a, opaque);\n" \
    "  gl_FragColor = c * alpha;\n" \
    "}\n"
#define GL_FS_SOLID \
    "precision mediump float;\n" \
    "uniform vec4 color;\n" \
    "void main() { gl_FragColor = color; }\n"

#endif
