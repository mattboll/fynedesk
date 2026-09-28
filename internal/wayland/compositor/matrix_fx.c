#include <stdlib.h>
#include <string.h>
#include <drm_fourcc.h>
#include <wlr/render/allocator.h>
#include <wlr/render/drm_format_set.h>
#include <wlr/render/gles2.h>
#include <wlr/render/swapchain.h>
#include <wlr/render/wlr_renderer.h>

#include "gl_util.h"
#include "matrix_fx.h"

struct matrix_fx {
    struct wlr_renderer *renderer;
    struct wlr_swapchain *chain;
    struct wlr_scene_buffer *picture;
    int w, h;
    uint8_t *atlas; // kept until it is uploaded
    int glyph_w, glyph_h, nglyphs;
    GLuint atlas_tex, cols_tex;
};

static const char *matrix_vs =
    "attribute vec2 pos;\n"
    "void main() { gl_Position = vec4(pos, 0.0, 1.0); }\n";

// gl_FragCoord.y counts rows from the top of the buffer, like the textures
// of wlroots. The colours are premultiplied.
// gl_FragCoord.y counts rows from the top of the buffer, like the textures
// of wlroots. The colours are premultiplied. The columns come in a texture,
// one texel wide each (see matrix_fx_draw): the randomness is drawn on the
// CPU, where it does not depend on the precision of the GPU.
static const char *matrix_fs =
    "#ifdef GL_FRAGMENT_PRECISION_HIGH\n"
    "precision highp float;\n"
    "#else\n"
    "precision mediump float;\n"
    "#endif\n"
    "uniform sampler2D atlas;\n"
    "uniform sampler2D cols;\n"
    "uniform float ncols;\n"
    "uniform vec2 size;\n"
    "uniform vec2 glyph;\n"
    "uniform float nglyphs;\n"
    "uniform vec2 band;\n"
    "uniform float dir;\n"
    "uniform float fade;\n"
    "float byte(float v) { return floor(v * 255.0 + 0.5); }\n"
    "void main() {\n"
    "  float x = floor(gl_FragCoord.x), y = floor(gl_FragCoord.y);\n"
    "  bool swept = dir > 0.0 ? x < band.x : x >= band.y;\n"
    "  if (swept) {\n"
    "    gl_FragColor = vec4(0.0, 8.0 / 255.0, 0.0, 1.0) * fade;\n"
    "    return;\n"
    "  }\n"
    "  gl_FragColor = vec4(0.0);\n"
    "  if (x < band.x || x >= band.y) return;\n"
    "  float col = floor((x - band.x) / glyph.x);\n"
    "  if (col >= ncols) return;\n"
    "  float cu = (col + 0.5) / ncols;\n"
    "  vec4 head = texture2D(cols, vec2(cu, 0.5 / 11.0));\n"
    "  float start = byte(head.r) * 256.0 + byte(head.g);\n"
    "  float len = byte(head.b);\n"
    "  float d = mod(y - start, size.y);\n"
    "  float g = floor(d / glyph.y);\n"
    "  if (g >= len) return;\n"
    "  float lx = x - band.x - col * glyph.x;\n"
    "  float ly = d - g * glyph.y;\n"
    "  float idx = byte(texture2D(cols, vec2(cu, (g + 1.5) / 11.0)).r);\n"
    "  vec2 uv = vec2((idx * glyph.x + lx + 0.5) / (nglyphs * glyph.x), (ly + 0.5) / glyph.y);\n"
    "  if (texture2D(atlas, uv).a < 0.5) return;\n"
    "  float green = g < 0.5 ? 1.0 : max(0.2, 0.8 - g * 0.125);\n"
    "  float alpha = g > len - 1.5 ? 0.5 : 1.0;\n"
    "  gl_FragColor = vec4(0.0, green, green / 8.0, 1.0) * alpha;\n"
    "}\n";

static GLuint matrix_prog;

void matrix_fx_reset_gl(void) {
    matrix_prog = 0; // it went with the old context
}

struct matrix_fx *matrix_fx_create(struct wlr_scene_tree *parent, struct wlr_output *output, int w, int h,
        const uint8_t *atlas, int glyph_w, int glyph_h, int nglyphs) {
    if (!output->allocator || !output->swapchain || !gl_available(output)) {
        return NULL;
    }
    struct wlr_drm_format_set formats = {0};
    gl_output_formats(output, &formats);
    const struct wlr_drm_format *format = wlr_drm_format_set_get(&formats, DRM_FORMAT_ARGB8888);
    struct matrix_fx *fx = calloc(1, sizeof(*fx));
    fx->renderer = output->renderer;
    fx->w = w;
    fx->h = h;
    fx->glyph_w = glyph_w;
    fx->glyph_h = glyph_h;
    fx->nglyphs = nglyphs;
    size_t atlas_size = (size_t)nglyphs * glyph_w * glyph_h;
    fx->atlas = malloc(atlas_size);
    if (fx->atlas) {
        memcpy(fx->atlas, atlas, atlas_size);
    }
    fx->chain = format ? wlr_swapchain_create(output->allocator, w, h, format) : NULL;
    wlr_drm_format_set_finish(&formats); // the swapchain keeps a copy
    fx->picture = wlr_scene_buffer_create(parent, NULL);
    if (!fx->atlas || !fx->chain || !fx->picture) {
        matrix_fx_destroy(fx);
        return NULL;
    }
    return fx;
}

bool matrix_fx_draw(struct matrix_fx *fx, float band_start, float band_end, int dir, float fade,
        const uint8_t *cols, int ncols) {
    struct wlr_buffer *out = wlr_swapchain_acquire(fx->chain);
    if (!out) {
        return false;
    }
    if (!gl_begin()) {
        wlr_buffer_unlock(out);
        return false;
    }
    glActiveTexture(GL_TEXTURE0);
    if (!matrix_prog) {
        static const char *const attribs[] = { "pos" };
        matrix_prog = gl_program(matrix_vs, matrix_fs, attribs, 1);
    }
    if (!fx->atlas_tex && fx->atlas) {
        glGenTextures(1, &fx->atlas_tex);
        glBindTexture(GL_TEXTURE_2D, fx->atlas_tex);
        glPixelStorei(GL_UNPACK_ALIGNMENT, 1);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_ALPHA, fx->nglyphs * fx->glyph_w, fx->glyph_h, 0,
            GL_ALPHA, GL_UNSIGNED_BYTE, fx->atlas);
        glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
        free(fx->atlas);
        fx->atlas = NULL;
    }
    if (!fx->cols_tex) {
        glGenTextures(1, &fx->cols_tex);
    }
    if (ncols > 0) {
        glBindTexture(GL_TEXTURE_2D, fx->cols_tex);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, ncols, MATRIX_FX_COL_ROWS, 0, GL_RGBA, GL_UNSIGNED_BYTE, cols);
    }
    GLuint fbo = wlr_gles2_renderer_get_buffer_fbo(fx->renderer, out);
    bool ok = matrix_prog && fbo && fx->atlas_tex && fx->cols_tex;
    if (ok) {
        glBindFramebuffer(GL_FRAMEBUFFER, fbo);
        glViewport(0, 0, out->width, out->height);
        glDisable(GL_BLEND);
        glDisable(GL_SCISSOR_TEST);
        glUseProgram(matrix_prog);
        // Nearest: a glyph is drawn pixel for pixel.
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(GL_TEXTURE_2D, fx->atlas_tex);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
        glUniform1i(glGetUniformLocation(matrix_prog, "atlas"), 0);
        glActiveTexture(GL_TEXTURE1);
        glBindTexture(GL_TEXTURE_2D, fx->cols_tex);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
        glUniform1i(glGetUniformLocation(matrix_prog, "cols"), 1);
        glUniform1f(glGetUniformLocation(matrix_prog, "ncols"), (float)(ncols > 0 ? ncols : 0));
        glUniform2f(glGetUniformLocation(matrix_prog, "size"), (float)out->width, (float)out->height);
        glUniform2f(glGetUniformLocation(matrix_prog, "glyph"), (float)fx->glyph_w, (float)fx->glyph_h);
        glUniform1f(glGetUniformLocation(matrix_prog, "nglyphs"), (float)fx->nglyphs);
        glUniform2f(glGetUniformLocation(matrix_prog, "band"), band_start, band_end);
        glUniform1f(glGetUniformLocation(matrix_prog, "dir"), (float)dir);
        glUniform1f(glGetUniformLocation(matrix_prog, "fade"), fade);
        gl_quad(0, 0, out->width, out->height, out->width, out->height, NULL, 0);
        glBindTexture(GL_TEXTURE_2D, 0);
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(GL_TEXTURE_2D, 0);
    }
    gl_end();
    if (!ok) {
        wlr_buffer_unlock(out);
        return false;
    }
    wlr_scene_buffer_set_buffer(fx->picture, out);
    wlr_buffer_unlock(out); // the scene buffer holds it now
    wlr_scene_buffer_set_dest_size(fx->picture, fx->w, fx->h);
    return true;
}

struct wlr_scene_node *matrix_fx_node(struct matrix_fx *fx) {
    return &fx->picture->node;
}

void matrix_fx_destroy(struct matrix_fx *fx) {
    if (fx->picture) {
        wlr_scene_node_destroy(&fx->picture->node);
    }
    if (fx->atlas_tex || fx->cols_tex) {
        gl_begin();
        glDeleteTextures(1, &fx->atlas_tex);
        glDeleteTextures(1, &fx->cols_tex);
        gl_end();
    }
    if (fx->chain) {
        wlr_swapchain_destroy(fx->chain);
    }
    free(fx->atlas);
    free(fx);
}
