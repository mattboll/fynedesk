#include <math.h>
#include <stdlib.h>
#include <string.h>
#include <wayland-server-core.h>
#include <drm_fourcc.h>
#include <wlr/render/allocator.h>
#include <wlr/render/drm_format_set.h>
#include <wlr/render/gles2.h>
#include <wlr/render/pass.h>
#include <wlr/render/swapchain.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/types/wlr_buffer.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/util/box.h>
#include "gl_util.h"
#include "pixel_buffer.h"
#include "wobble.h"

// The buffers of the decorations, which the compositor keeps: the scene
// lets go of its own once it has made a texture of it.
struct wobble_pixels {
    struct wlr_scene_node *node;
    struct wlr_buffer *buffer;
};
#define WOBBLE_MAX_PIXELS 8

// Mesh drawn for each cell of the springs mesh, per side: the image bends
// smoothly between the springs.
#define WOBBLE_SUBDIV 3

// A node of the window, hidden while the wobbling picture stands in for it.
struct wobble_src {
    struct wlr_scene_node *node;
    struct wl_listener destroy;
    bool alive;
    bool is_rect;
    float opacity;  // buffer: opacity to restore
    float color[4]; // rect: colour to restore
    int x, y;       // position in the extent, this frame
    struct wlr_texture *texture; // of a decoration buffer, kept
    struct wlr_texture *frame_tex; // what this frame draws, from wobble_update
};

struct wobble {
    struct wlr_renderer *renderer;
    GLuint flat_tex, flat_fbo;        // the window as it is (plain GL)
    int flat_w, flat_h;
    struct wlr_swapchain *out_chain;  // the window bent
    struct wlr_scene_buffer *picture; // shows the bent window, above the view
    struct wobble_src *srcs;
    int nsrc, capsrc;
    struct wlr_scene_tree *view;
    struct wobble_pixels pixels[WOBBLE_MAX_PIXELS];
    int npixels;
    int *order; // the sources to draw this frame, in scene order
    int norder, caporder;
    double ox, oy, w, h; // extent of the window in its tree
    int ml, mt, mr, mb;  // room around it (left, top, right, bottom) for the bends
    float scale;         // buffer pixels per layout pixel
    int gx, gy;          // cells of the springs mesh
    GLfloat *verts;      // mesh drawn: x, y, u, v per vertex
};

// The picture lets the clicks through: the window is there, below it.
static bool wobble_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
    return false;
}

static void wobble_src_destroyed(struct wl_listener *listener, void *data) {
    struct wobble_src *src = wl_container_of(listener, src, destroy);
    src->alive = false;
    wl_list_remove(&src->destroy.link);
    wl_list_init(&src->destroy.link);
}

// node_buffer returns the buffer a buffer node shows: its own, or the
// decoration buffer the compositor keeps for it.
static struct wlr_buffer *node_buffer(struct wobble *wb, struct wlr_scene_buffer *sb) {
    if (sb->buffer) {
        return sb->buffer;
    }
    for (int i = 0; i < wb->npixels; i++) {
        if (wb->pixels[i].node == &sb->node) {
            return wb->pixels[i].buffer;
        }
    }
    return NULL;
}

// node_size returns the size of a buffer or rect node, 0 if it shows nothing.
static bool node_size(struct wobble *wb, struct wlr_scene_node *node, int *w, int *h) {
    if (node->type == WLR_SCENE_NODE_RECT) {
        struct wlr_scene_rect *rect = wlr_scene_rect_from_node(node);
        *w = rect->width;
        *h = rect->height;
        return rect->color[3] > 0 && *w > 0 && *h > 0;
    }
    struct wlr_scene_buffer *buf = wlr_scene_buffer_from_node(node);
    struct wlr_buffer *buffer = node_buffer(wb, buf);
    struct wlr_texture *kept = buf->WLR_PRIVATE.texture; // the scene's, when it let go of the buffer
    if ((!buffer && !kept) || buf->opacity == 0) {
        return false;
    }
    *w = buf->dst_width;
    *h = buf->dst_height;
    if (*w <= 0 || *h <= 0) {
        *w = buffer ? (int)buffer->width : (int)kept->width;
        *h = buffer ? (int)buffer->height : (int)kept->height;
        if (buf->transform & WL_OUTPUT_TRANSFORM_90) {
            int t = *w; *w = *h; *h = t;
        }
    }
    return *w > 0 && *h > 0;
}

static void extent_walk(struct wobble *wb, struct wlr_scene_node *node, int x, int y, double box[4], bool *any) {
    if (!node->enabled || node->data == WOBBLE_SKIP) {
        return;
    }
    x += node->x;
    y += node->y;
    if (node->type == WLR_SCENE_NODE_TREE) {
        struct wlr_scene_tree *tree = wlr_scene_tree_from_node(node);
        struct wlr_scene_node *child;
        wl_list_for_each(child, &tree->children, link) {
            extent_walk(wb, child, x, y, box, any);
        }
        return;
    }
    int w, h;
    if (!node_size(wb, node, &w, &h)) {
        return;
    }
    if (!*any) {
        box[0] = x; box[1] = y; box[2] = x + w; box[3] = y + h;
        *any = true;
        return;
    }
    box[0] = fmin(box[0], x); box[1] = fmin(box[1], y);
    box[2] = fmax(box[2], x + w); box[3] = fmax(box[3], y + h);
}

static void wobble_add_src(struct wobble *wb, struct wlr_scene_node *node, int x, int y) {
    if (wb->nsrc == wb->capsrc) {
        int cap = wb->capsrc ? wb->capsrc * 2 : 16;
        struct wobble_src *srcs = calloc(cap, sizeof(*srcs));
        // Listeners are linked by address: move them one by one.
        for (int i = 0; i < wb->nsrc; i++) {
            srcs[i] = wb->srcs[i];
            if (srcs[i].alive) {
                wl_list_remove(&wb->srcs[i].destroy.link);
                srcs[i].destroy.notify = wobble_src_destroyed;
                wl_signal_add(&srcs[i].node->events.destroy, &srcs[i].destroy);
            }
        }
        free(wb->srcs);
        wb->srcs = srcs;
        wb->capsrc = cap;
    }
    struct wobble_src *src = &wb->srcs[wb->nsrc++];
    src->node = node;
    src->alive = true;
    src->is_rect = node->type == WLR_SCENE_NODE_RECT;
    src->x = x;
    src->y = y;
    src->destroy.notify = wobble_src_destroyed;
    wl_signal_add(&node->events.destroy, &src->destroy);
    if (src->is_rect) {
        struct wlr_scene_rect *rect = wlr_scene_rect_from_node(node);
        for (int i = 0; i < 4; i++) {
            src->color[i] = rect->color[i];
        }
    } else {
        src->opacity = wlr_scene_buffer_from_node(node)->opacity;
    }
}

// collect_walk lists the nodes that draw the window this frame, in drawing
// order. The compositor may replace some while the window wobbles (the
// titlebar, on hover): the new ones are taken in, and hidden too.
static void collect_walk(struct wobble *wb, struct wlr_scene_node *node, int x, int y) {
    if (!node->enabled || node->data == WOBBLE_SKIP) {
        return;
    }
    x += node->x;
    y += node->y;
    if (node->type == WLR_SCENE_NODE_TREE) {
        struct wlr_scene_tree *tree = wlr_scene_tree_from_node(node);
        struct wlr_scene_node *child;
        wl_list_for_each(child, &tree->children, link) {
            collect_walk(wb, child, x, y);
        }
        return;
    }
    int index = -1;
    for (int i = 0; i < wb->nsrc; i++) {
        if (wb->srcs[i].alive && wb->srcs[i].node == node) {
            index = i;
            break;
        }
    }
    int w, h;
    if (index < 0) {
        if (!node_size(wb, node, &w, &h)) {
            return; // shows nothing
        }
        wobble_add_src(wb, node, 0, 0);
        index = wb->nsrc - 1;
    }
    wb->srcs[index].x = x - (int)wb->ox;
    wb->srcs[index].y = y - (int)wb->oy;
    if (wb->norder == wb->caporder) {
        wb->caporder = wb->caporder ? wb->caporder * 2 : 16;
        wb->order = realloc(wb->order, wb->caporder * sizeof(int));
    }
    wb->order[wb->norder++] = index;
}

static void collect_sources(struct wobble *wb) {
    wb->norder = 0;
    struct wlr_scene_node *child;
    wl_list_for_each(child, &wb->view->children, link) {
        collect_walk(wb, child, 0, 0);
    }
}

// hide_sources hides the window while the picture stands in for it. Its
// application gets the opacity of its surface back with each frame it
// draws (alpha modifier), so this is done again every frame.
static void hide_sources(struct wobble *wb) {
    const float clear[4] = {0, 0, 0, 0};
    for (int i = 0; i < wb->nsrc; i++) {
        struct wobble_src *src = &wb->srcs[i];
        if (!src->alive) {
            continue;
        }
        if (src->is_rect) {
            wlr_scene_rect_set_color(wlr_scene_rect_from_node(src->node), clear);
        } else {
            wlr_scene_buffer_set_opacity(wlr_scene_buffer_from_node(src->node), 0);
        }
    }
}

// --- GL: the bent picture ---

static const char *wobble_vs =
    "attribute vec2 pos;\n"
    "attribute vec2 texcoord;\n"
    "varying vec2 uv;\n"
    "void main() {\n"
    "  uv = texcoord;\n"
    "  gl_Position = vec4(pos, 0.0, 1.0);\n"
    "}\n";
static const char *wobble_fs = GL_FS_TEXTURE("sampler2D");
static const char *wobble_fs_ext = GL_OES_EXTENSION GL_FS_TEXTURE("samplerExternalOES");
static const char *wobble_fs_solid = GL_FS_SOLID;

static GLuint wobble_programs[3]; // sampler2D, samplerExternalOES, solid colour

void wobble_reset_gl(void) {
    memset(wobble_programs, 0, sizeof(wobble_programs)); // they went with the old context
}

static GLuint wobble_program(int kind) {
    if (!wobble_programs[kind]) {
        const char *fs[] = { wobble_fs, wobble_fs_ext, wobble_fs_solid };
        static const char *const attribs[] = { "pos", "texcoord" };
        wobble_programs[kind] = gl_program(wobble_vs, fs[kind], attribs, 2);
    }
    return wobble_programs[kind];
}

// source_texture returns the texture of a buffer node: the application's
// own, or one made from a decoration buffer. A decoration buffer is drawn
// again in place when it changes, and set again on its node: while the scene
// holds it, it is new, and the texture is made again.
static struct wlr_texture *source_texture(struct wobble *wb, struct wobble_src *src, struct wlr_scene_buffer *sb) {
    if (sb->buffer) {
        struct wlr_client_buffer *cb = wlr_client_buffer_get(sb->buffer);
        if (cb && cb->texture) {
            return cb->texture;
        }
    }
    if (src->texture && !sb->buffer) {
        return src->texture;
    }
    struct wlr_buffer *buffer = node_buffer(wb, sb);
    if (!buffer) {
        // A buffer of the compositor the scene let go of (the pieces of a
        // shadow): the scene's texture of it, which stays the scene's.
        return src->texture ? src->texture : sb->WLR_PRIVATE.texture;
    }
    if (src->texture) {
        wlr_texture_destroy(src->texture);
    }
    src->texture = wlr_texture_from_buffer(wb->renderer, buffer);
    return src->texture;
}

// draw_quad draws the rectangle (x, y, w, h) of a target of size (tw, th),
// sampling the texture rectangle (u0, v0)-(u1, v1).
static void draw_quad(GLuint prog, double x, double y, double w, double h, int tw, int th,
        double u0, double v0, double u1, double v1) {
    const double coords[1][4] = { { u0, v0, u1, v1 } };
    gl_quad(x, y, w, h, tw, th, coords, 1);
}

// render_flat draws the window as it is into flat_tex. The EGL context must
// be current.
static bool render_flat(struct wobble *wb) {
    if (!wb->flat_fbo) {
        glGenTextures(1, &wb->flat_tex);
        glBindTexture(GL_TEXTURE_2D, wb->flat_tex);
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, wb->flat_w, wb->flat_h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
        glBindTexture(GL_TEXTURE_2D, 0);
        glGenFramebuffers(1, &wb->flat_fbo);
        glBindFramebuffer(GL_FRAMEBUFFER, wb->flat_fbo);
        glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, wb->flat_tex, 0);
        GLenum status = glCheckFramebufferStatus(GL_FRAMEBUFFER);
        glBindFramebuffer(GL_FRAMEBUFFER, 0);
        if (status != GL_FRAMEBUFFER_COMPLETE) {
            return false;
        }
    }
    // The textures wobble_update made for this frame (making them again
    // here uploaded the decorations a second time each frame).
    int n = wb->norder > 0 ? wb->norder : 1;
    struct wlr_texture *textures[n];
    for (int k = 0; k < wb->norder; k++) {
        struct wobble_src *src = &wb->srcs[wb->order[k]];
        textures[k] = src->alive && !src->is_rect ? src->frame_tex : NULL;
    }

    int tw = wb->flat_w, th = wb->flat_h;
    glBindFramebuffer(GL_FRAMEBUFFER, wb->flat_fbo);
    glViewport(0, 0, tw, th);
    glDisable(GL_SCISSOR_TEST);
    glClearColor(0, 0, 0, 0);
    glClear(GL_COLOR_BUFFER_BIT);
    glEnable(GL_BLEND);
    glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA); // premultiplied

    float s = wb->scale;
    for (int k = 0; k < wb->norder; k++) {
        struct wobble_src *src = &wb->srcs[wb->order[k]];
        if (!src->alive) {
            continue;
        }
        if (src->is_rect) {
            struct wlr_scene_rect *rect = wlr_scene_rect_from_node(src->node);
            GLuint prog = wobble_program(2);
            if (!prog) {
                continue;
            }
            glUseProgram(prog);
            glUniform4f(glGetUniformLocation(prog, "color"), src->color[0], src->color[1], src->color[2], src->color[3]);
            draw_quad(prog, src->x * s, src->y * s, rect->width * s, rect->height * s, tw, th, 0, 0, 1, 1);
            continue;
        }
        struct wlr_scene_buffer *sb = wlr_scene_buffer_from_node(src->node);
        struct wlr_texture *tex = textures[k];
        if (!tex || sb->transform != WL_OUTPUT_TRANSFORM_NORMAL) {
            continue; // a rotated buffer is left out: rare in a window
        }
        struct wlr_gles2_texture_attribs attribs;
        wlr_gles2_texture_get_attribs(tex, &attribs);
        GLuint prog = wobble_program(attribs.target == GL_TEXTURE_EXTERNAL_OES ? 1 : 0);
        if (!prog) {
            continue;
        }
        struct wlr_fbox box = sb->src_box;
        if (wlr_fbox_empty(&box)) {
            box = (struct wlr_fbox){ 0, 0, tex->width, tex->height };
        }
        int w = sb->dst_width > 0 ? sb->dst_width : (int)box.width;
        int h = sb->dst_height > 0 ? sb->dst_height : (int)box.height;

        glUseProgram(prog);
        gl_bind_texture(attribs.target, attribs.tex, 0);
        glUniform1i(glGetUniformLocation(prog, "tex"), 0);
        glUniform1f(glGetUniformLocation(prog, "alpha"), src->opacity);
        glUniform1f(glGetUniformLocation(prog, "opaque"), attribs.has_alpha ? 0.0f : 1.0f);
        draw_quad(prog, src->x * s, src->y * s, w * s, h * s, tw, th,
            box.x / tex->width, box.y / tex->height,
            (box.x + box.width) / tex->width, (box.y + box.height) / tex->height);
        glBindTexture(attribs.target, 0);
    }
    glDisable(GL_BLEND);
    glUseProgram(0);
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    return true;
}

// mesh_at interpolates the displacement of the springs mesh at (u, v), in
// cells.
static void mesh_at(struct wobble *wb, const float *mx, const float *my, double u, double v, double *dx, double *dy) {
    int i = (int)fmin(floor(u), wb->gx - 1), j = (int)fmin(floor(v), wb->gy - 1);
    double fu = u - i, fv = v - j;
    int n = wb->gx + 1;
    int a = j * n + i, b = a + 1, c = a + n, d = c + 1;
    *dx = (1-fu)*(1-fv)*mx[a] + fu*(1-fv)*mx[b] + (1-fu)*fv*mx[c] + fu*fv*mx[d];
    *dy = (1-fu)*(1-fv)*my[a] + fu*(1-fv)*my[b] + (1-fu)*fv*my[c] + fu*fv*my[d];
}

// render_bent draws the flat picture on the bent mesh into a buffer of the
// out chain.
static struct wlr_buffer *render_bent(struct wobble *wb, const float *mx, const float *my) {
    struct wlr_buffer *out = wlr_swapchain_acquire(wb->out_chain);
    if (!out) {
        return NULL;
    }
    struct wlr_gles2_texture_attribs attribs = { .target = GL_TEXTURE_2D, .tex = wb->flat_tex, .has_alpha = true };

    GLuint fbo = wlr_gles2_renderer_get_buffer_fbo(wb->renderer, out);
    GLuint prog = wobble_program(attribs.target == GL_TEXTURE_EXTERNAL_OES ? 1 : 0);
    bool ok = prog && fbo;
    if (ok) {
        glBindFramebuffer(GL_FRAMEBUFFER, fbo);
        glViewport(0, 0, out->width, out->height);
        glClearColor(0, 0, 0, 0);
        glClear(GL_COLOR_BUFFER_BIT);
        glDisable(GL_BLEND);
        glDisable(GL_SCISSOR_TEST);

        // Two triangles per piece of the mesh; positions in the out buffer
        // (row 0 at the top, like the textures wlroots samples), texture
        // coordinates in the flat picture.
        int nx = wb->gx * WOBBLE_SUBDIV, ny = wb->gy * WOBBLE_SUBDIV;
        double W = out->width / wb->scale, H = out->height / wb->scale;
        GLfloat *v = wb->verts;
        int nv = 0;
        for (int j = 0; j < ny; j++) {
            for (int i = 0; i < nx; i++) {
                int corners[6][2] = { {i, j}, {i + 1, j}, {i, j + 1}, {i + 1, j}, {i + 1, j + 1}, {i, j + 1} };
                for (int k = 0; k < 6; k++) {
                    double u = (double)corners[k][0] / nx, t = (double)corners[k][1] / ny;
                    double dx, dy;
                    mesh_at(wb, mx, my, u * wb->gx, t * wb->gy, &dx, &dy);
                    double x = wb->ml + u * wb->w + dx, y = wb->mt + t * wb->h + dy;
                    v[nv * 4 + 0] = (GLfloat)(2 * x / W - 1);
                    v[nv * 4 + 1] = (GLfloat)(2 * y / H - 1);
                    v[nv * 4 + 2] = (GLfloat)(u * wb->w * wb->scale / wb->flat_w);
                    v[nv * 4 + 3] = (GLfloat)(t * wb->h * wb->scale / wb->flat_h);
                    nv++;
                }
            }
        }

        glUseProgram(prog);
        gl_bind_texture(attribs.target, attribs.tex, 0);
        glUniform1i(glGetUniformLocation(prog, "tex"), 0);
        glUniform1f(glGetUniformLocation(prog, "alpha"), 1.0f);
        glUniform1f(glGetUniformLocation(prog, "opaque"), 0.0f);
        glBindBuffer(GL_ARRAY_BUFFER, 0);
        glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(GLfloat), v);
        glVertexAttribPointer(1, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(GLfloat), v + 2);
        glEnableVertexAttribArray(0);
        glEnableVertexAttribArray(1);
        glDrawArrays(GL_TRIANGLES, 0, nv);
        glDisableVertexAttribArray(0);
        glDisableVertexAttribArray(1);
        glBindTexture(attribs.target, 0);
        glUseProgram(0);
        glBindFramebuffer(GL_FRAMEBUFFER, 0);
        glFlush();
    }
    if (!ok) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    return out;
}

// wobble_set_pixels tells which decoration buffer each decoration node
// shows (they change when the decorations are drawn again).
void wobble_set_pixels(struct wobble *wb, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int n) {
    wb->npixels = 0;
    for (int i = 0; i < n && wb->npixels < WOBBLE_MAX_PIXELS; i++) {
        if (nodes[i] && buffers[i]) {
            wb->pixels[wb->npixels++] = (struct wobble_pixels){ nodes[i], buffers[i] };
        }
    }
}

// wobble_create prepares the wobbling picture of the window drawn by view
// (a tree), just above it. It returns NULL when the renderer is not GLES2.
struct wobble *wobble_create(struct wlr_scene_tree *view, struct wlr_output *output,
        double cell, double bend, float scale, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int npix,
        bool reach, double reach_x, double reach_y) {
    struct wlr_renderer *renderer = output->renderer;
    struct wlr_allocator *allocator = output->allocator;
    if (!renderer || !allocator || !output->swapchain) {
        return NULL;
    }
    if (!gl_available(output)) {
        return NULL; // pixman: windows keep still
    }
    struct wobble probe = {0};
    wobble_set_pixels(&probe, nodes, buffers, npix);
    double box[4];
    bool any = false;
    struct wlr_scene_node *child;
    wl_list_for_each(child, &view->children, link) {
        extent_walk(&probe, child, 0, 0, box, &any);
    }
    if (!any) {
        return NULL;
    }
    // ARGB8888 with the layouts of the output's buffers, which the GPU
    // renders to.
    struct wlr_drm_format_set formats = {0};
    gl_output_formats(output, &formats);
    const struct wlr_drm_format *format = wlr_drm_format_set_get(&formats, DRM_FORMAT_ARGB8888);

    struct wobble *wb = calloc(1, sizeof(*wb));
    wb->renderer = renderer;
    wobble_set_pixels(wb, nodes, buffers, npix);
    wb->ox = box[0];
    wb->oy = box[1];
    wb->w = box[2] - box[0];
    wb->h = box[3] - box[1];
    wb->ml = wb->mr = (int)ceil(wb->w * bend) + 2;
    wb->mt = wb->mb = (int)ceil(wb->h * bend) + 2;
    if (reach) {
        // The picture also reaches a point of the tree, with room around it
        // (the dock, for the magic lamp).
        const int around = 48;
        wb->ml = (int)fmax(wb->ml, wb->ox - (reach_x - around));
        wb->mt = (int)fmax(wb->mt, wb->oy - (reach_y - around));
        wb->mr = (int)fmax(wb->mr, (reach_x + around) - (wb->ox + wb->w));
        wb->mb = (int)fmax(wb->mb, (reach_y + around) - (wb->oy + wb->h));
    }
    wb->scale = scale;
    wb->gx = (int)fmin(fmax(round(wb->w / cell), 3), 40);
    wb->gy = (int)fmin(fmax(round(wb->h / cell), 3), 40);
    wb->verts = calloc(wb->gx * wb->gy * WOBBLE_SUBDIV * WOBBLE_SUBDIV * 6 * 4, sizeof(GLfloat));
    wb->flat_w = (int)ceil(wb->w * scale);
    wb->flat_h = (int)ceil(wb->h * scale);
    wb->out_chain = wlr_swapchain_create(allocator, (int)ceil((wb->w + wb->ml + wb->mr) * scale),
        (int)ceil((wb->h + wb->mt + wb->mb) * scale), format);
    wlr_drm_format_set_finish(&formats); // the swapchains keep a copy
    wb->picture = wlr_scene_buffer_create(view->node.parent, NULL);
    if (!wb->out_chain || !wb->picture || !wb->verts) {
        wobble_destroy(wb);
        return NULL;
    }
    wb->picture->point_accepts_input = wobble_no_input;
    wlr_scene_node_place_above(&wb->picture->node, &view->node);
    wb->view = view;
    return wb;
}

// wobble_update draws the window bent by the springs mesh (mx, my), the
// view tree being at (px, py) in its parent. It reports whether it could.
bool wobble_update(struct wobble *wb, const float *mx, const float *my, int px, int py) {
    collect_sources(wb);
    // The textures are made first: wlroots switches EGL contexts meanwhile.
    for (int k = 0; k < wb->norder; k++) {
        struct wobble_src *src = &wb->srcs[wb->order[k]];
        src->frame_tex = NULL;
        if (src->alive && !src->is_rect) {
            src->frame_tex = source_texture(wb, src, wlr_scene_buffer_from_node(src->node));
        }
    }
    if (!gl_begin()) {
        return false;
    }
    bool flat_ok = render_flat(wb);
    struct wlr_buffer *out = flat_ok ? render_bent(wb, mx, my) : NULL;
    gl_end();
    if (!out) {
        return false;
    }
    hide_sources(wb);
    wlr_scene_buffer_set_buffer(wb->picture, out);
    wlr_buffer_unlock(out); // the scene buffer holds it now
    wlr_scene_buffer_set_dest_size(wb->picture, (int)(wb->w + wb->ml + wb->mr), (int)(wb->h + wb->mt + wb->mb));
    wlr_scene_node_set_position(&wb->picture->node, px + (int)wb->ox - wb->ml, py + (int)wb->oy - wb->mt);
    return true;
}

// wobble_destroy removes the picture and shows the window again.
void wobble_destroy(struct wobble *wb) {
    for (int i = 0; i < wb->nsrc; i++) {
        struct wobble_src *src = &wb->srcs[i];
        if (!src->alive) {
            continue;
        }
        wl_list_remove(&src->destroy.link);
        if (src->is_rect) {
            wlr_scene_rect_set_color(wlr_scene_rect_from_node(src->node), src->color);
        } else {
            wlr_scene_buffer_set_opacity(wlr_scene_buffer_from_node(src->node), src->opacity);
        }
    }
    if (wb->picture) {
        wlr_scene_node_destroy(&wb->picture->node);
    }
    if (wb->flat_fbo || wb->flat_tex) {
        gl_begin();
        glDeleteFramebuffers(1, &wb->flat_fbo);
        glDeleteTextures(1, &wb->flat_tex);
        gl_end();
    }
    if (wb->out_chain) {
        wlr_swapchain_destroy(wb->out_chain);
    }
    for (int i = 0; i < wb->nsrc; i++) {
        if (wb->srcs[i].texture) {
            wlr_texture_destroy(wb->srcs[i].texture);
        }
    }
    free(wb->srcs);
    free(wb->order);
    free(wb->verts);
    free(wb);
}

int wobble_gx(struct wobble *wb) { return wb->gx; }
int wobble_gy(struct wobble *wb) { return wb->gy; }
double wobble_w(struct wobble *wb) { return wb->w; }
double wobble_h(struct wobble *wb) { return wb->h; }
double wobble_ox(struct wobble *wb) { return wb->ox; }
double wobble_oy(struct wobble *wb) { return wb->oy; }
