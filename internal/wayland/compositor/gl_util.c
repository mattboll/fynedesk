#include <drm_fourcc.h>
#include <wlr/render/gles2.h>
#include <wlr/render/swapchain.h>

#include "gl_util.h"

bool gl_available(struct wlr_output *output) {
    return output->renderer && wlr_renderer_is_gles2(output->renderer) && g_egl_display != EGL_NO_DISPLAY;
}

bool gl_begin(void) {
    if (g_egl_display == EGL_NO_DISPLAY || g_egl_context == EGL_NO_CONTEXT) {
        return false;
    }
    return eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, g_egl_context);
}

void gl_end(void) {
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    glUseProgram(0);
    glFlush();
    eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
}

static GLuint compile(GLenum type, const char *src) {
    GLuint shader = glCreateShader(type);
    glShaderSource(shader, 1, &src, NULL);
    glCompileShader(shader);
    GLint ok = 0;
    glGetShaderiv(shader, GL_COMPILE_STATUS, &ok);
    if (!ok) {
        glDeleteShader(shader);
        return 0;
    }
    return shader;
}

GLuint gl_program(const char *vs_src, const char *fs_src, const char *const *attribs, int nattribs) {
    GLuint vs = compile(GL_VERTEX_SHADER, vs_src);
    GLuint fs = compile(GL_FRAGMENT_SHADER, fs_src);
    GLuint prog = 0;
    if (vs && fs) {
        prog = glCreateProgram();
        glAttachShader(prog, vs);
        glAttachShader(prog, fs);
        for (int i = 0; i < nattribs; i++) {
            glBindAttribLocation(prog, i, attribs[i]);
        }
        glLinkProgram(prog);
        GLint ok = 0;
        glGetProgramiv(prog, GL_LINK_STATUS, &ok);
        if (!ok) {
            glDeleteProgram(prog);
            prog = 0;
        }
    }
    if (vs) {
        glDeleteShader(vs);
    }
    if (fs) {
        glDeleteShader(fs);
    }
    return prog;
}

void gl_bind_texture(GLenum target, GLuint tex, int unit) {
    glActiveTexture(GL_TEXTURE0 + unit);
    glBindTexture(target, tex);
    glTexParameteri(target, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(target, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(target, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(target, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
}

#define GL_QUAD_MAX_COORDS 2

void gl_quad(double x, double y, double w, double h, int tw, int th, const double coords[][4], int ncoords) {
    if (ncoords > GL_QUAD_MAX_COORDS) {
        ncoords = GL_QUAD_MAX_COORDS;
    }
    GLfloat x0 = 2 * x / tw - 1, x1 = 2 * (x + w) / tw - 1;
    GLfloat y0 = 2 * y / th - 1, y1 = 2 * (y + h) / th - 1;
    // The two triangles, by corner: 0 = (x0, y0), 1 = (x1, y0), 2 = (x0, y1), 3 = (x1, y1).
    static const int corners[6] = { 0, 1, 2, 1, 3, 2 };
    const int stride = 2 + 2 * ncoords;
    GLfloat v[6 * (2 + 2 * GL_QUAD_MAX_COORDS)];
    for (int k = 0; k < 6; k++) {
        int c = corners[k];
        GLfloat *p = v + k * stride;
        p[0] = (c & 1) ? x1 : x0;
        p[1] = (c & 2) ? y1 : y0;
        for (int i = 0; i < ncoords; i++) {
            p[2 + 2 * i] = (c & 1) ? coords[i][2] : coords[i][0];
            p[3 + 2 * i] = (c & 2) ? coords[i][3] : coords[i][1];
        }
    }
    glBindBuffer(GL_ARRAY_BUFFER, 0);
    for (int i = 0; i <= ncoords; i++) {
        glVertexAttribPointer(i, 2, GL_FLOAT, GL_FALSE, stride * sizeof(GLfloat), v + 2 * i);
        glEnableVertexAttribArray(i);
    }
    glDrawArrays(GL_TRIANGLES, 0, 6);
    for (int i = 0; i <= ncoords; i++) {
        glDisableVertexAttribArray(i);
    }
}

void gl_output_formats(struct wlr_output *output, struct wlr_drm_format_set *formats) {
    const struct wlr_drm_format *primary = &output->swapchain->format;
    for (size_t i = 0; i < primary->len; i++) {
        wlr_drm_format_set_add(formats, DRM_FORMAT_ARGB8888, primary->modifiers[i]);
    }
    if (primary->len == 0) {
        wlr_drm_format_set_add(formats, DRM_FORMAT_ARGB8888, DRM_FORMAT_MOD_INVALID);
    }
}
