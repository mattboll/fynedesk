#include <math.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <drm_fourcc.h>
#include <wlr/render/allocator.h>
#include <wlr/render/drm_format_set.h>
#include <wlr/render/gles2.h>
#include <wlr/render/swapchain.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/types/wlr_buffer.h>
#include <wlr/util/box.h>
#include <wlr/util/region.h>

#include "blur.h"
#include "gl_util.h"

// What lies behind is drawn at half the size, then halved BLUR_LEVELS times
// more and brought back up (dual Kawase blur): wide and cheap.
#define BLUR_LEVELS 2
#define BLUR_OFFSET 1.6f
// Room taken around the window, in layout pixels, so that what lies just
// outside it bleeds in as it would in a wider blur.
#define BLUR_MARGIN 40

struct blur {
    struct wlr_scene_tree *view;
    struct wlr_scene_buffer *picture; // the blur, first child of the view tree
    struct wl_listener view_destroy;
    bool alive;
    struct wlr_renderer *renderer;
    struct wlr_allocator *allocator;
    struct wlr_drm_format_set formats;
    struct wlr_swapchain *chain; // the pictures, of the window's size
    int out_w, out_h;
    GLuint tex[BLUR_LEVELS + 1], fbo[BLUR_LEVELS + 1];
    int tw[BLUR_LEVELS + 1], th[BLUR_LEVELS + 1];
    struct wlr_box capture;     // what was drawn behind, layout pixels
    uint64_t behind_sig, mask_sig;
    bool drawn;                 // tex[0] holds the blur of what lies behind
    // The window: what it drew since the blur was cut to its shape.
    struct wlr_surface *surface;
    struct wl_listener commit, surface_destroy;
    pixman_region32_t mask_damage; // surface pixels
    bool mask_full;
};

// --- signatures: what lies behind changed or not ---

static void sig_mix(uint64_t *sig, uint64_t v) {
    *sig ^= v + 0x9e3779b97f4a7c15ull + (*sig << 6) + (*sig >> 2);
}

static uint64_t fbits(float f) {
    union { float f; uint32_t u; } x = { .f = f };
    return x.u;
}

// --- textures of the scene nodes ---

static struct wlr_texture *node_texture(struct wlr_scene_buffer *sb) {
    if (sb->buffer) {
        struct wlr_client_buffer *cb = wlr_client_buffer_get(sb->buffer);
        if (cb && cb->texture) {
            return cb->texture;
        }
    }
    // A buffer of the compositor: the scene keeps the texture it made of it.
    return sb->WLR_PRIVATE.texture;
}

static bool node_box(struct wlr_scene_node *node, int x, int y, struct wlr_box *box) {
    if (node->type == WLR_SCENE_NODE_RECT) {
        struct wlr_scene_rect *rect = wlr_scene_rect_from_node(node);
        *box = (struct wlr_box){ x, y, rect->width, rect->height };
        return rect->color[3] > 0 && !wlr_box_empty(box);
    }
    struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(node);
    if (sb->opacity == 0) {
        return false;
    }
    int w = sb->dst_width, h = sb->dst_height;
    if (w <= 0 || h <= 0) {
        w = sb->WLR_PRIVATE.buffer_width;
        h = sb->WLR_PRIVATE.buffer_height;
        if (sb->transform & WL_OUTPUT_TRANSFORM_90) {
            int t = w; w = h; h = t;
        }
    }
    *box = (struct wlr_box){ x, y, w, h };
    return !wlr_box_empty(box);
}

// --- GL ---

static const char *blur_vs =
    "attribute vec2 pos;\n"
    "attribute vec2 texcoord;\n"
    "attribute vec2 maskcoord;\n"
    "varying vec2 uv;\n"
    "varying vec2 muv;\n"
    "void main() {\n"
    "  uv = texcoord;\n"
    "  muv = maskcoord;\n"
    "  gl_Position = vec4(pos, 0.0, 1.0);\n"
    "}\n";

// Nodes behind.
static const char *blur_fs_src = GL_FS_TEXTURE("sampler2D");
static const char *blur_fs_src_ext = GL_OES_EXTENSION GL_FS_TEXTURE("samplerExternalOES");
static const char *blur_fs_solid = GL_FS_SOLID;
static const char *blur_fs_down =
    "precision mediump float;\n"
    "varying vec2 uv;\n"
    "uniform sampler2D tex;\n"
    "uniform vec2 o;\n"
    "void main() {\n"
    "  vec4 s = texture2D(tex, uv) * 4.0;\n"
    "  s += texture2D(tex, uv - o);\n"
    "  s += texture2D(tex, uv + o);\n"
    "  s += texture2D(tex, uv + vec2(o.x, -o.y));\n"
    "  s += texture2D(tex, uv - vec2(o.x, -o.y));\n"
    "  gl_FragColor = s / 8.0;\n"
    "}\n";
static const char *blur_fs_up =
    "precision mediump float;\n"
    "varying vec2 uv;\n"
    "uniform sampler2D tex;\n"
    "uniform vec2 o;\n"
    "void main() {\n"
    "  vec4 s = texture2D(tex, uv + vec2(-2.0 * o.x, 0.0));\n"
    "  s += texture2D(tex, uv + vec2(-o.x, o.y)) * 2.0;\n"
    "  s += texture2D(tex, uv + vec2(0.0, 2.0 * o.y));\n"
    "  s += texture2D(tex, uv + vec2(o.x, o.y)) * 2.0;\n"
    "  s += texture2D(tex, uv + vec2(2.0 * o.x, 0.0));\n"
    "  s += texture2D(tex, uv + vec2(o.x, -o.y)) * 2.0;\n"
    "  s += texture2D(tex, uv + vec2(0.0, -2.0 * o.y));\n"
    "  s += texture2D(tex, uv + vec2(-o.x, -o.y)) * 2.0;\n"
    "  gl_FragColor = s / 12.0;\n"
    "}\n";
// The blur, where the window draws something: even a faint window (the
// dock) is frosted glass, and its shadow fades the blur out. It is partly
// desaturated, as frosted glass is: a bright wallpaper does not tint the
// whole panel.
#define CUT_FS(sampler) \
    "precision mediump float;\n" \
    "varying vec2 uv;\n" \
    "varying vec2 muv;\n" \
    "uniform sampler2D tex;\n" \
    "uniform " sampler " mask;\n" \
    "void main() {\n" \
    "  float m = smoothstep(0.01, 0.12, texture2D(mask, muv).a);\n" \
    "  vec3 c = texture2D(tex, uv).rgb;\n" \
    "  c = mix(c, vec3(dot(c, vec3(0.299, 0.587, 0.114))), 0.4);\n" \
    "  gl_FragColor = vec4(c, 1.0) * m;\n" \
    "}\n"
static const char *blur_fs_cut = CUT_FS("sampler2D");
static const char *blur_fs_cut_ext = GL_OES_EXTENSION CUT_FS("samplerExternalOES");

enum { PROG_SRC, PROG_SRC_EXT, PROG_SOLID, PROG_DOWN, PROG_UP, PROG_CUT, PROG_CUT_EXT, PROG_COUNT };
static GLuint blur_programs[PROG_COUNT];

void blur_reset_gl(void) {
    memset(blur_programs, 0, sizeof(blur_programs)); // they went with the old context
}

static GLuint blur_program(int kind) {
    if (!blur_programs[kind]) {
        const char *fs[PROG_COUNT] = { blur_fs_src, blur_fs_src_ext, blur_fs_solid,
            blur_fs_down, blur_fs_up, blur_fs_cut, blur_fs_cut_ext };
        static const char *const attribs[] = { "pos", "texcoord", "maskcoord" };
        blur_programs[kind] = gl_program(blur_vs, fs[kind], attribs, 3);
    }
    return blur_programs[kind];
}

// quad draws the rectangle (x, y, w, h) of a target of size (tw, th),
// sampling (u0, v0)-(u1, v1) of the texture and (m0, n0)-(m1, n1) of the mask.
static void quad(double x, double y, double w, double h, int tw, int th,
        double u0, double v0, double u1, double v1, double m0, double n0, double m1, double n1) {
    const double coords[2][4] = { { u0, v0, u1, v1 }, { m0, n0, m1, n1 } };
    gl_quad(x, y, w, h, tw, th, coords, 2);
}

static void free_levels(struct blur *b) {
    for (int i = 0; i <= BLUR_LEVELS; i++) {
        if (b->fbo[i]) {
            glDeleteFramebuffers(1, &b->fbo[i]);
        }
        if (b->tex[i]) {
            glDeleteTextures(1, &b->tex[i]);
        }
        b->fbo[i] = b->tex[i] = 0;
        b->tw[i] = b->th[i] = 0;
    }
}

// ensure_levels makes the textures of the blur, the first of size (w, h),
// each next one half the size of the one before.
static bool ensure_levels(struct blur *b, int w, int h) {
    if (b->tw[0] == w && b->th[0] == h && b->fbo[0]) {
        return true;
    }
    free_levels(b);
    for (int i = 0; i <= BLUR_LEVELS; i++) {
        b->tw[i] = w;
        b->th[i] = h;
        glGenTextures(1, &b->tex[i]);
        glBindTexture(GL_TEXTURE_2D, b->tex[i]);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
        glBindTexture(GL_TEXTURE_2D, 0);
        glGenFramebuffers(1, &b->fbo[i]);
        glBindFramebuffer(GL_FRAMEBUFFER, b->fbo[i]);
        glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, b->tex[i], 0);
        GLenum status = glCheckFramebufferStatus(GL_FRAMEBUFFER);
        glBindFramebuffer(GL_FRAMEBUFFER, 0);
        if (status != GL_FRAMEBUFFER_COMPLETE) {
            free_levels(b);
            return false;
        }
        w = w > 1 ? w / 2 : 1;
        h = h > 1 ? h / 2 : 1;
    }
    return true;
}

// --- what lies behind ---

struct behind {
    struct blur *b;
    struct wlr_box capture; // layout pixels
    float scale;            // texture pixels per layout pixel
    bool draw;              // false: only the signature is computed
    bool stop;              // the view was reached: the rest is above it
    uint64_t sig;
};

static void draw_node(struct behind *w, struct wlr_scene_node *node, struct wlr_box *box) {
    int tw = w->b->tw[0], th = w->b->th[0];
    double x = (box->x - w->capture.x) * w->scale, y = (box->y - w->capture.y) * w->scale;
    double bw = box->width * w->scale, bh = box->height * w->scale;
    if (node->type == WLR_SCENE_NODE_RECT) {
        struct wlr_scene_rect *rect = wlr_scene_rect_from_node(node);
        GLuint prog = blur_program(PROG_SOLID);
        if (prog) {
            glUseProgram(prog);
            glUniform4f(glGetUniformLocation(prog, "color"), rect->color[0], rect->color[1], rect->color[2], rect->color[3]);
            quad(x, y, bw, bh, tw, th, 0, 0, 1, 1, 0, 0, 1, 1);
        }
        return;
    }
    struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(node);
    if (sb->WLR_PRIVATE.is_single_pixel_buffer) {
        GLuint prog = blur_program(PROG_SOLID);
        if (prog) {
            const uint32_t *c = sb->WLR_PRIVATE.single_pixel_buffer_color;
            float a = sb->opacity / (float)UINT32_MAX;
            glUseProgram(prog);
            glUniform4f(glGetUniformLocation(prog, "color"), c[0] * a, c[1] * a, c[2] * a, c[3] * a);
            quad(x, y, bw, bh, tw, th, 0, 0, 1, 1, 0, 0, 1, 1);
        }
        return;
    }
    struct wlr_texture *tex = node_texture(sb);
    if (!tex || sb->transform != WL_OUTPUT_TRANSFORM_NORMAL) {
        return; // a rotated buffer is left out: rare on a desktop
    }
    struct wlr_gles2_texture_attribs attribs;
    wlr_gles2_texture_get_attribs(tex, &attribs);
    GLuint prog = blur_program(attribs.target == GL_TEXTURE_EXTERNAL_OES ? PROG_SRC_EXT : PROG_SRC);
    if (!prog) {
        return;
    }
    struct wlr_fbox src = sb->src_box;
    if (wlr_fbox_empty(&src)) {
        src = (struct wlr_fbox){ 0, 0, tex->width, tex->height };
    }
    glUseProgram(prog);
    gl_bind_texture(attribs.target, attribs.tex, 0);
    glUniform1i(glGetUniformLocation(prog, "tex"), 0);
    glUniform1f(glGetUniformLocation(prog, "alpha"), sb->opacity);
    glUniform1f(glGetUniformLocation(prog, "opaque"), attribs.has_alpha ? 0.0f : 1.0f);
    quad(x, y, bw, bh, tw, th,
        src.x / tex->width, src.y / tex->height,
        (src.x + src.width) / tex->width, (src.y + src.height) / tex->height, 0, 0, 1, 1);
    glBindTexture(attribs.target, 0);
}

static void sig_node(struct behind *w, struct wlr_scene_node *node, struct wlr_box *box) {
    sig_mix(&w->sig, (uintptr_t)node);
    sig_mix(&w->sig, ((uint64_t)(uint32_t)box->x << 32) | (uint32_t)box->y);
    sig_mix(&w->sig, ((uint64_t)(uint32_t)box->width << 32) | (uint32_t)box->height);
    if (node->type == WLR_SCENE_NODE_RECT) {
        struct wlr_scene_rect *rect = wlr_scene_rect_from_node(node);
        for (int i = 0; i < 4; i++) {
            sig_mix(&w->sig, fbits(rect->color[i]));
        }
        return;
    }
    struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(node);
    sig_mix(&w->sig, fbits(sb->opacity));
    sig_mix(&w->sig, (uintptr_t)sb->buffer);
    sig_mix(&w->sig, (uintptr_t)node_texture(sb));
    struct wlr_scene_surface *ss = wlr_scene_surface_try_from_buffer(sb);
    if (ss) {
        sig_mix(&w->sig, ss->surface->current.seq); // a new frame of the application
    }
}

// walk goes through the scene in drawing order, up to the view.
static void walk(struct behind *w, struct wlr_scene_node *node, int x, int y) {
    if (w->stop) {
        return;
    }
    if (node == &w->b->view->node) {
        w->stop = true;
        return;
    }
    if (!node->enabled) {
        return;
    }
    x += node->x;
    y += node->y;
    if (node->type == WLR_SCENE_NODE_TREE) {
        struct wlr_scene_tree *tree = wlr_scene_tree_from_node(node);
        struct wlr_scene_node *child;
        wl_list_for_each(child, &tree->children, link) {
            walk(w, child, x, y);
        }
        return;
    }
    struct wlr_box box, clip;
    if (!node_box(node, x, y, &box) || !wlr_box_intersection(&clip, &box, &w->capture)) {
        return;
    }
    if (w->draw) {
        draw_node(w, node, &box);
    } else {
        sig_node(w, node, &box);
    }
}

// draw_behind draws what lies behind into tex[0], blurs it and leaves the
// blur in tex[0].
static bool draw_behind(struct blur *b, struct wlr_scene *scene, float scale) {
    int w = (int)ceil(b->capture.width * scale), h = (int)ceil(b->capture.height * scale);
    if (!ensure_levels(b, w > 0 ? w : 1, h > 0 ? h : 1)) {
        return false;
    }
    glBindFramebuffer(GL_FRAMEBUFFER, b->fbo[0]);
    glViewport(0, 0, b->tw[0], b->th[0]);
    glDisable(GL_SCISSOR_TEST);
    glClearColor(0, 0, 0, 1);
    glClear(GL_COLOR_BUFFER_BIT);
    glEnable(GL_BLEND);
    glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA); // premultiplied
    struct behind wk = { .b = b, .capture = b->capture, .scale = scale, .draw = true };
    walk(&wk, &scene->tree.node, 0, 0);
    glDisable(GL_BLEND);

    GLuint down = blur_program(PROG_DOWN), up = blur_program(PROG_UP);
    if (!down || !up) {
        return false;
    }
    for (int i = 1; i <= BLUR_LEVELS; i++) {
        glBindFramebuffer(GL_FRAMEBUFFER, b->fbo[i]);
        glViewport(0, 0, b->tw[i], b->th[i]);
        glUseProgram(down);
        gl_bind_texture(GL_TEXTURE_2D, b->tex[i - 1], 0);
        glUniform1i(glGetUniformLocation(down, "tex"), 0);
        glUniform2f(glGetUniformLocation(down, "o"), BLUR_OFFSET / b->tw[i - 1], BLUR_OFFSET / b->th[i - 1]);
        quad(0, 0, b->tw[i], b->th[i], b->tw[i], b->th[i], 0, 0, 1, 1, 0, 0, 1, 1);
    }
    for (int i = BLUR_LEVELS - 1; i >= 0; i--) {
        glBindFramebuffer(GL_FRAMEBUFFER, b->fbo[i]);
        glViewport(0, 0, b->tw[i], b->th[i]);
        glUseProgram(up);
        gl_bind_texture(GL_TEXTURE_2D, b->tex[i + 1], 0);
        glUniform1i(glGetUniformLocation(up, "tex"), 0);
        glUniform2f(glGetUniformLocation(up, "o"), BLUR_OFFSET / b->tw[i + 1], BLUR_OFFSET / b->th[i + 1]);
        quad(0, 0, b->tw[i], b->th[i], b->tw[i], b->th[i], 0, 0, 1, 1, 0, 0, 1, 1);
    }
    glBindTexture(GL_TEXTURE_2D, 0);
    glUseProgram(0);
    return true;
}

// draw_cut draws the blur, cut to the shape of the window, into a buffer of
// the chain.
static struct wlr_buffer *draw_cut(struct blur *b, struct wlr_texture *mask, struct wlr_box *region) {
    struct wlr_buffer *out = wlr_swapchain_acquire(b->chain);
    if (!out) {
        return NULL;
    }
    struct wlr_gles2_texture_attribs attribs;
    wlr_gles2_texture_get_attribs(mask, &attribs);
    GLuint prog = blur_program(attribs.target == GL_TEXTURE_EXTERNAL_OES ? PROG_CUT_EXT : PROG_CUT);
    GLuint fbo = wlr_gles2_renderer_get_buffer_fbo(b->renderer, out);
    if (!prog || !fbo) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    glBindFramebuffer(GL_FRAMEBUFFER, fbo);
    glViewport(0, 0, out->width, out->height);
    glDisable(GL_SCISSOR_TEST);
    glDisable(GL_BLEND);
    glUseProgram(prog);
    gl_bind_texture(GL_TEXTURE_2D, b->tex[0], 0);
    gl_bind_texture(attribs.target, attribs.tex, 1);
    glUniform1i(glGetUniformLocation(prog, "tex"), 0);
    glUniform1i(glGetUniformLocation(prog, "mask"), 1);
    double cw = b->capture.width, ch = b->capture.height;
    double u0 = (region->x - b->capture.x) / cw, v0 = (region->y - b->capture.y) / ch;
    double u1 = u0 + region->width / cw, v1 = v0 + region->height / ch;
    quad(0, 0, out->width, out->height, out->width, out->height, u0, v0, u1, v1, 0, 0, 1, 1);
    glBindTexture(attribs.target, 0);
    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_2D, 0);
    glUseProgram(0);
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    return out;
}

// --- the blur ---

static bool blur_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
    return false;
}

static void blur_surface_commit(struct wl_listener *listener, void *data) {
    struct blur *b = wl_container_of(listener, b, commit);
    pixman_region32_t damage;
    pixman_region32_init(&damage);
    wlr_surface_get_effective_damage(b->surface, &damage);
    pixman_region32_union(&b->mask_damage, &b->mask_damage, &damage);
    pixman_region32_fini(&damage);
}

static void unhook_surface(struct blur *b) {
    if (b->surface) {
        wl_list_remove(&b->commit.link);
        wl_list_remove(&b->surface_destroy.link);
        b->surface = NULL;
    }
}

static void blur_surface_destroyed(struct wl_listener *listener, void *data) {
    struct blur *b = wl_container_of(listener, b, surface_destroy);
    unhook_surface(b);
}

// hook_surface follows what the window draws, to tell the scene which part
// of the blur changes with it.
static void hook_surface(struct blur *b, struct wlr_surface *surface) {
    if (b->surface == surface) {
        return;
    }
    unhook_surface(b);
    b->surface = surface;
    b->commit.notify = blur_surface_commit;
    wl_signal_add(&surface->events.commit, &b->commit);
    b->surface_destroy.notify = blur_surface_destroyed;
    wl_signal_add(&surface->events.destroy, &b->surface_destroy);
    b->mask_full = true;
}

static void blur_view_destroyed(struct wl_listener *listener, void *data) {
    struct blur *b = wl_container_of(listener, b, view_destroy);
    b->alive = false;
    b->picture = NULL; // it goes with the tree
    wl_list_remove(&b->view_destroy.link);
    wl_list_init(&b->view_destroy.link);
}

struct blur *blur_create(struct wlr_scene_tree *view, struct wlr_output *output) {
    struct wlr_renderer *renderer = output->renderer;
    if (!renderer || !output->allocator || !output->swapchain) {
        return NULL;
    }
    if (!gl_available(output)) {
        return NULL;
    }
    struct blur *b = calloc(1, sizeof(*b));
    b->renderer = renderer;
    b->allocator = output->allocator;
    gl_output_formats(output, &b->formats);
    b->picture = wlr_scene_buffer_create(view, NULL);
    if (!b->picture) {
        wlr_drm_format_set_finish(&b->formats);
        free(b);
        return NULL;
    }
    b->picture->point_accepts_input = blur_no_input;
    wlr_scene_node_lower_to_bottom(&b->picture->node);
    wlr_scene_node_set_enabled(&b->picture->node, false);
    b->view = view;
    b->alive = true;
    pixman_region32_init(&b->mask_damage);
    b->view_destroy.notify = blur_view_destroyed;
    wl_signal_add(&view->node.events.destroy, &b->view_destroy);
    return b;
}

// surface_node finds the buffer node of the surface in the tree.
static struct wlr_scene_buffer *surface_node(struct wlr_scene_node *node, struct wlr_surface *surface) {
    if (!node->enabled) {
        return NULL;
    }
    if (node->type == WLR_SCENE_NODE_BUFFER) {
        struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(node);
        struct wlr_scene_surface *ss = wlr_scene_surface_try_from_buffer(sb);
        return ss && ss->surface == surface ? sb : NULL;
    }
    if (node->type != WLR_SCENE_NODE_TREE) {
        return NULL;
    }
    struct wlr_scene_tree *tree = wlr_scene_tree_from_node(node);
    struct wlr_scene_node *child;
    wl_list_for_each(child, &tree->children, link) {
        struct wlr_scene_buffer *sb = surface_node(child, surface);
        if (sb) {
            return sb;
        }
    }
    return NULL;
}

static void blur_hide(struct blur *b) {
    if (b->picture && b->picture->node.enabled) {
        wlr_scene_node_set_enabled(&b->picture->node, false);
    }
    b->mask_full = true; // drawn whole when it shows again
}

bool blur_update(struct blur *b, struct wlr_scene *scene, struct wlr_surface *surface, float scale) {
    if (!b->alive || !surface) {
        return false;
    }
    struct wlr_texture *mask = wlr_surface_get_texture(surface);
    struct wlr_scene_buffer *node = surface_node(&b->view->node, surface);
    int vx, vy, lx, ly;
    if (!mask || !node || !wlr_scene_node_coords(&b->view->node, &vx, &vy) ||
            !wlr_scene_node_coords(&node->node, &lx, &ly)) {
        blur_hide(b);
        return false;
    }
    struct wlr_gles2_texture_attribs attribs;
    wlr_gles2_texture_get_attribs(mask, &attribs);
    if (!attribs.has_alpha) {
        blur_hide(b); // an opaque window hides what lies behind anyway
        return false;
    }
    struct wlr_box region = { lx, ly, surface->current.width, surface->current.height };
    if (wlr_box_empty(&region)) {
        blur_hide(b);
        return false;
    }

    // What lies behind, with room around it, within the scene's outputs.
    struct wlr_box capture = {
        region.x - BLUR_MARGIN, region.y - BLUR_MARGIN,
        region.width + 2 * BLUR_MARGIN, region.height + 2 * BLUR_MARGIN,
    };
    struct wlr_box layout = {0};
    struct wlr_scene_output *so;
    bool any = false;
    wl_list_for_each(so, &scene->outputs, link) {
        struct wlr_box ob = { so->x, so->y, so->output->width, so->output->height };
        if (so->output->scale > 0) {
            ob.width = (int)ceil(ob.width / so->output->scale);
            ob.height = (int)ceil(ob.height / so->output->scale);
        }
        if (!any) {
            layout = ob;
            any = true;
            continue;
        }
        int x2 = fmax(layout.x + layout.width, ob.x + ob.width);
        int y2 = fmax(layout.y + layout.height, ob.y + ob.height);
        layout.x = fmin(layout.x, ob.x);
        layout.y = fmin(layout.y, ob.y);
        layout.width = x2 - layout.x;
        layout.height = y2 - layout.y;
    }
    if (any && !wlr_box_intersection(&capture, &capture, &layout)) {
        blur_hide(b);
        return false;
    }

    // Half the size: it is blurred anyway.
    float s = scale / 2;
    struct behind sig = { .b = b, .capture = capture, .scale = s, .sig = 1469598103934665603ull };
    walk(&sig, &scene->tree.node, 0, 0);
    sig_mix(&sig.sig, ((uint64_t)(uint32_t)capture.x << 32) | (uint32_t)capture.y);
    sig_mix(&sig.sig, ((uint64_t)(uint32_t)capture.width << 32) | (uint32_t)capture.height);
    sig_mix(&sig.sig, fbits(scale));
    hook_surface(b, surface);
    uint64_t mask_sig = 1469598103934665603ull;
    sig_mix(&mask_sig, ((uint64_t)(uint32_t)region.x << 32) | (uint32_t)region.y);
    sig_mix(&mask_sig, ((uint64_t)(uint32_t)region.width << 32) | (uint32_t)region.height);
    sig_mix(&mask_sig, fbits(scale));
    bool behind_changed = !b->drawn || sig.sig != b->behind_sig;
    bool moved = mask_sig != b->mask_sig || b->mask_full;
    if (!behind_changed && !moved && !pixman_region32_not_empty(&b->mask_damage)) {
        return false;
    }

    int ow = (int)ceil(region.width * scale), oh = (int)ceil(region.height * scale);
    if (!b->chain || ow != b->out_w || oh != b->out_h) {
        if (b->chain) {
            wlr_swapchain_destroy(b->chain);
        }
        const struct wlr_drm_format *format = wlr_drm_format_set_get(&b->formats, DRM_FORMAT_ARGB8888);
        b->chain = format ? wlr_swapchain_create(b->allocator, ow, oh, format) : NULL;
        b->out_w = ow;
        b->out_h = oh;
        if (!b->chain) {
            blur_hide(b);
            return false;
        }
    }

    if (!gl_begin()) {
        return false;
    }
    bool ok = true;
    if (behind_changed) {
        b->capture = capture;
        ok = draw_behind(b, scene, s);
        b->drawn = ok;
    }
    struct wlr_buffer *out = ok ? draw_cut(b, mask, &region) : NULL;
    gl_end();
    if (!out) {
        blur_hide(b);
        return false;
    }
    b->behind_sig = sig.sig;
    b->mask_sig = mask_sig;
    // Only what the window drew anew changes, when it keeps its place over
    // the same things: the scene draws that part of the screen again only.
    pixman_region32_t damage;
    pixman_region32_init_rect(&damage, 0, 0, ow, oh);
    if (!behind_changed && !moved) {
        wlr_region_scale_xy(&damage, &b->mask_damage, (float)ow / region.width, (float)oh / region.height);
        pixman_region32_intersect_rect(&damage, &damage, 0, 0, ow, oh);
    }
    wlr_scene_buffer_set_buffer_with_damage(b->picture, out, &damage);
    pixman_region32_fini(&damage);
    pixman_region32_clear(&b->mask_damage);
    b->mask_full = false;
    wlr_buffer_unlock(out); // the scene buffer holds it now
    wlr_scene_buffer_set_dest_size(b->picture, region.width, region.height);
    wlr_scene_node_set_position(&b->picture->node, lx - vx, ly - vy);
    wlr_scene_node_lower_to_bottom(&b->picture->node);
    wlr_scene_node_set_enabled(&b->picture->node, true);
    return true;
}

bool blur_supported(void) {
    return g_egl_display != EGL_NO_DISPLAY;
}

bool blur_alive(struct blur *b) {
    return b->alive;
}

void blur_destroy(struct blur *b) {
    unhook_surface(b);
    pixman_region32_fini(&b->mask_damage);
    if (b->alive) {
        wl_list_remove(&b->view_destroy.link);
        if (b->picture) {
            wlr_scene_node_destroy(&b->picture->node);
        }
    }
    if (b->fbo[0] || b->tex[0]) {
        gl_begin();
        free_levels(b);
        gl_end();
    }
    if (b->chain) {
        wlr_swapchain_destroy(b->chain);
    }
    wlr_drm_format_set_finish(&b->formats);
    free(b);
}
