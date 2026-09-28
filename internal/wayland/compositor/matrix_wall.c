#include <stdlib.h>
#include <string.h>
#include <drm_fourcc.h>
#include <wlr/render/allocator.h>
#include <wlr/render/drm_format_set.h>
#include <wlr/render/gles2.h>
#include <wlr/render/swapchain.h>
#include <wlr/render/wlr_renderer.h>

#include "gl_util.h"
#include "matrix_wall.h"

struct matrix_wall {
    struct wlr_renderer *renderer;
    struct wlr_swapchain *chain; // the frames shown
    int w, h;
    uint8_t *atlas; // kept until it is uploaded
    int glyph_w, glyph_h, nglyphs;
    GLuint atlas_tex, cols_tex;
    // The frame being drawn reads the previous one: two textures, in turn.
    GLuint tex[2], fbo[2];
    int cur;
};

static const char *wall_vs =
    "attribute vec2 pos;\n"
    "attribute vec2 texcoord;\n"
    "varying vec2 uv;\n"
    "void main() {\n"
    "  uv = texcoord;\n"
    "  gl_Position = vec4(pos, 0.0, 1.0);\n"
    "}\n";

// A pixel is a glyph of a column, drawn again at full strength, or the
// previous frame faded as the CPU rain fades it (x*217>>8: rounding down,
// so that the trails go black). gl_FragCoord.y counts rows from the top.
static const char *wall_rain_fs =
    "#ifdef GL_FRAGMENT_PRECISION_HIGH\n"
    "precision highp float;\n"
    "#else\n"
    "precision mediump float;\n"
    "#endif\n"
    "varying vec2 uv;\n"
    "uniform sampler2D prev;\n"
    "uniform sampler2D atlas;\n"
    "uniform sampler2D cols;\n"
    "uniform float ncols;\n"
    "uniform vec2 glyph;\n"
    "uniform float nglyphs;\n"
    "float byte(float v) { return floor(v * 255.0 + 0.5); }\n"
    "void main() {\n"
    "  vec3 p = floor(texture2D(prev, uv).rgb * 255.0 + 0.5);\n"
    "  vec3 faded = floor(p * 217.0 / 256.0) / 255.0;\n"
    "  gl_FragColor = vec4(faded, 1.0);\n"
    "  float x = floor(gl_FragCoord.x), y = floor(gl_FragCoord.y);\n"
    "  float ci = floor(x / glyph.x);\n"
    "  if (ci >= ncols) return;\n"
    "  vec4 c = texture2D(cols, vec2((ci + 0.5) / ncols, 0.5));\n"
    "  float head = byte(c.r) * 256.0 + byte(c.g) - 32768.0;\n"
    "  float len = byte(c.b), first = byte(c.a);\n"
    "  float j = -floor((y - head) / glyph.y);\n"
    "  if (j < 0.0 || j >= len) return;\n"
    "  float ly = y - (head - j * glyph.y);\n"
    "  float idx = j < 0.5 ? first : mod(first + j * 3.0, nglyphs);\n"
    "  float lx = x - ci * glyph.x;\n"
    "  vec2 auv = vec2((idx * glyph.x + lx + 0.5) / (nglyphs * glyph.x), (ly + 0.5) / glyph.y);\n"
    "  if (texture2D(atlas, auv).a < 0.5) return;\n"
    "  if (j < 0.5) {\n"
    "    gl_FragColor = vec4(204.0 / 255.0, 1.0, 204.0 / 255.0, 1.0);\n"
    "  } else {\n"
    "    gl_FragColor = vec4(0.0, floor(204.0 - floor(j * 153.0 / len)) / 255.0, 0.0, 1.0);\n"
    "  }\n"
    "}\n";

static const char *wall_copy_fs =
    "precision mediump float;\n"
    "varying vec2 uv;\n"
    "uniform sampler2D tex;\n"
    "void main() { gl_FragColor = texture2D(tex, uv); }\n";

static GLuint rain_prog, copy_prog;

void matrix_wall_reset_gl(void) {
    rain_prog = copy_prog = 0; // they went with the old context
}

struct matrix_wall *matrix_wall_create(struct wlr_output *output, int w, int h,
        const uint8_t *atlas, int glyph_w, int glyph_h, int nglyphs) {
    if (!output->allocator || !output->swapchain || !gl_available(output) || w <= 0 || h <= 0) {
        return NULL;
    }
    struct wlr_drm_format_set formats = {0};
    gl_output_formats(output, &formats);
    const struct wlr_drm_format *format = wlr_drm_format_set_get(&formats, DRM_FORMAT_ARGB8888);
    struct matrix_wall *mw = calloc(1, sizeof(*mw));
    mw->renderer = output->renderer;
    mw->w = w;
    mw->h = h;
    mw->glyph_w = glyph_w;
    mw->glyph_h = glyph_h;
    mw->nglyphs = nglyphs;
    size_t atlas_size = (size_t)nglyphs * glyph_w * glyph_h;
    mw->atlas = malloc(atlas_size);
    if (mw->atlas) {
        memcpy(mw->atlas, atlas, atlas_size);
    }
    mw->chain = format ? wlr_swapchain_create(output->allocator, w, h, format) : NULL;
    wlr_drm_format_set_finish(&formats); // the swapchain keeps a copy
    if (!mw->atlas || !mw->chain) {
        matrix_wall_destroy(mw);
        return NULL;
    }
    return mw;
}

// setup makes the GL objects on the first frame, with the context current.
static bool setup(struct matrix_wall *mw) {
    if (!rain_prog || !copy_prog) {
        static const char *const attribs[] = { "pos", "texcoord" };
        rain_prog = gl_program(wall_vs, wall_rain_fs, attribs, 2);
        copy_prog = gl_program(wall_vs, wall_copy_fs, attribs, 2);
    }
    if (!mw->atlas_tex && mw->atlas) {
        glGenTextures(1, &mw->atlas_tex);
        glBindTexture(GL_TEXTURE_2D, mw->atlas_tex);
        glPixelStorei(GL_UNPACK_ALIGNMENT, 1);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_ALPHA, mw->nglyphs * mw->glyph_w, mw->glyph_h, 0,
            GL_ALPHA, GL_UNSIGNED_BYTE, mw->atlas);
        glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
        free(mw->atlas);
        mw->atlas = NULL;
    }
    if (!mw->cols_tex) {
        glGenTextures(1, &mw->cols_tex);
    }
    for (int i = 0; i < 2; i++) {
        if (mw->fbo[i]) {
            continue;
        }
        glGenTextures(1, &mw->tex[i]);
        glBindTexture(GL_TEXTURE_2D, mw->tex[i]);
        // Black, opaque, like the CPU rain's first frame.
        uint8_t *black = calloc((size_t)mw->w * mw->h, 4);
        if (black) {
            for (size_t k = 3; k < (size_t)mw->w * mw->h * 4; k += 4) {
                black[k] = 0xff;
            }
        }
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, mw->w, mw->h, 0, GL_RGBA, GL_UNSIGNED_BYTE, black);
        free(black);
        glGenFramebuffers(1, &mw->fbo[i]);
        glBindFramebuffer(GL_FRAMEBUFFER, mw->fbo[i]);
        glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, mw->tex[i], 0);
        if (glCheckFramebufferStatus(GL_FRAMEBUFFER) != GL_FRAMEBUFFER_COMPLETE) {
            return false;
        }
    }
    return rain_prog && copy_prog && mw->atlas_tex;
}

// bind_nearest binds tex to a unit, sampled texel for texel.
static void bind_nearest(GLuint tex, int unit) {
    glActiveTexture(GL_TEXTURE0 + unit);
    glBindTexture(GL_TEXTURE_2D, tex);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
}

struct wlr_buffer *matrix_wall_tick(struct matrix_wall *mw, const uint8_t *cols, int ncols) {
    struct wlr_buffer *out = wlr_swapchain_acquire(mw->chain);
    if (!out) {
        return NULL;
    }
    if (!gl_begin()) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    glActiveTexture(GL_TEXTURE0);
    bool ok = setup(mw) && ncols > 0;
    GLuint out_fbo = ok ? wlr_gles2_renderer_get_buffer_fbo(mw->renderer, out) : 0;
    ok = ok && out_fbo;
    if (ok) {
        glBindTexture(GL_TEXTURE_2D, mw->cols_tex);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, ncols, 1, 0, GL_RGBA, GL_UNSIGNED_BYTE, cols);
        glDisable(GL_BLEND);
        glDisable(GL_SCISSOR_TEST);
        const double whole[1][4] = { { 0, 0, 1, 1 } };

        // The rain, into the texture that was not drawn last time.
        int next = 1 - mw->cur;
        glBindFramebuffer(GL_FRAMEBUFFER, mw->fbo[next]);
        glViewport(0, 0, mw->w, mw->h);
        glUseProgram(rain_prog);
        bind_nearest(mw->tex[mw->cur], 0);
        bind_nearest(mw->atlas_tex, 1);
        bind_nearest(mw->cols_tex, 2);
        glUniform1i(glGetUniformLocation(rain_prog, "prev"), 0);
        glUniform1i(glGetUniformLocation(rain_prog, "atlas"), 1);
        glUniform1i(glGetUniformLocation(rain_prog, "cols"), 2);
        glUniform1f(glGetUniformLocation(rain_prog, "ncols"), (float)ncols);
        glUniform2f(glGetUniformLocation(rain_prog, "glyph"), (float)mw->glyph_w, (float)mw->glyph_h);
        glUniform1f(glGetUniformLocation(rain_prog, "nglyphs"), (float)mw->nglyphs);
        gl_quad(0, 0, mw->w, mw->h, mw->w, mw->h, whole, 1);
        mw->cur = next;

        // Then onto the frame shown.
        glBindFramebuffer(GL_FRAMEBUFFER, out_fbo);
        glViewport(0, 0, out->width, out->height);
        glUseProgram(copy_prog);
        bind_nearest(mw->tex[mw->cur], 0);
        glUniform1i(glGetUniformLocation(copy_prog, "tex"), 0);
        gl_quad(0, 0, out->width, out->height, out->width, out->height, whole, 1);

        for (int unit = 2; unit >= 0; unit--) {
            glActiveTexture(GL_TEXTURE0 + unit);
            glBindTexture(GL_TEXTURE_2D, 0);
        }
    }
    gl_end();
    if (!ok) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    return out;
}

void matrix_wall_destroy(struct matrix_wall *mw) {
    if (mw->atlas_tex || mw->cols_tex || mw->fbo[0] || mw->fbo[1]) {
        gl_begin();
        glDeleteTextures(1, &mw->atlas_tex);
        glDeleteTextures(1, &mw->cols_tex);
        glDeleteFramebuffers(2, mw->fbo);
        glDeleteTextures(2, mw->tex);
        gl_end();
    }
    if (mw->chain) {
        wlr_swapchain_destroy(mw->chain);
    }
    free(mw->atlas);
    free(mw);
}
