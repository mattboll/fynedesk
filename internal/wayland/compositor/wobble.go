package compositor

/*
#include <math.h>
#include <stdlib.h>
#include <wayland-server-core.h>
#include <drm_fourcc.h>
#include <EGL/egl.h>
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>
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
#include "pixel_buffer.h"

// Defined in main.go: the renderer's EGL display and context.
extern EGLDisplay g_egl_display;
extern EGLContext g_egl_context;

// Trees marked with this data are left out of the wobble (the attention
// glow, see attention.go).
#define WOBBLE_SKIP ((void *)0x7)

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
    int mx, my;          // room around it for the bends
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
    if (!buffer || buf->opacity == 0) {
        return false;
    }
    *w = buf->dst_width;
    *h = buf->dst_height;
    if (*w <= 0 || *h <= 0) {
        *w = buffer->width;
        *h = buffer->height;
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
// Textures are premultiplied; alpha fades them, opaque ignores the alpha
// channel of those that have none (XRGB).
static const char *wobble_fs =
    "precision mediump float;\n"
    "varying vec2 uv;\n"
    "uniform sampler2D tex;\n"
    "uniform float alpha;\n"
    "uniform float opaque;\n"
    "void main() {\n"
    "  vec4 c = texture2D(tex, uv);\n"
    "  c.a = max(c.a, opaque);\n"
    "  gl_FragColor = c * alpha;\n"
    "}\n";
static const char *wobble_fs_ext =
    "#extension GL_OES_EGL_image_external : require\n"
    "precision mediump float;\n"
    "varying vec2 uv;\n"
    "uniform samplerExternalOES tex;\n"
    "uniform float alpha;\n"
    "uniform float opaque;\n"
    "void main() {\n"
    "  vec4 c = texture2D(tex, uv);\n"
    "  c.a = max(c.a, opaque);\n"
    "  gl_FragColor = c * alpha;\n"
    "}\n";
static const char *wobble_fs_solid =
    "precision mediump float;\n"
    "uniform vec4 color;\n"
    "void main() { gl_FragColor = color; }\n";

static GLuint wobble_compile(const char *fs_src);

static GLuint wobble_programs[3]; // sampler2D, samplerExternalOES, solid colour

static GLuint wobble_program(int kind) {
    if (!wobble_programs[kind]) {
        const char *fs[] = { wobble_fs, wobble_fs_ext, wobble_fs_solid };
        wobble_programs[kind] = wobble_compile(fs[kind]);
    }
    return wobble_programs[kind];
}

static GLuint wobble_compile(const char *fs_src) {
    GLuint vs = glCreateShader(GL_VERTEX_SHADER);
    glShaderSource(vs, 1, &wobble_vs, NULL);
    glCompileShader(vs);
    GLuint fs = glCreateShader(GL_FRAGMENT_SHADER);
    glShaderSource(fs, 1, &fs_src, NULL);
    glCompileShader(fs);
    GLint ok = 0;
    glGetShaderiv(fs, GL_COMPILE_STATUS, &ok);
    GLuint prog = 0;
    if (ok) {
        prog = glCreateProgram();
        glAttachShader(prog, vs);
        glAttachShader(prog, fs);
        glBindAttribLocation(prog, 0, "pos");
        glBindAttribLocation(prog, 1, "texcoord");
        glLinkProgram(prog);
        glGetProgramiv(prog, GL_LINK_STATUS, &ok);
        if (!ok) {
            glDeleteProgram(prog);
            prog = 0;
        }
    }
    glDeleteShader(vs);
    glDeleteShader(fs);
    return prog;
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
        return src->texture;
    }
    if (src->texture) {
        wlr_texture_destroy(src->texture);
    }
    src->texture = wlr_texture_from_buffer(wb->renderer, buffer);
    return src->texture;
}

// draw_quad draws the rectangle (x, y, w, h) of a target of size (tw, th),
// row 0 at the top, sampling the texture rectangle (u0, v0)-(u1, v1).
static void draw_quad(GLuint prog, double x, double y, double w, double h, int tw, int th,
        double u0, double v0, double u1, double v1) {
    GLfloat x0 = 2 * x / tw - 1, x1 = 2 * (x + w) / tw - 1;
    GLfloat y0 = 2 * y / th - 1, y1 = 2 * (y + h) / th - 1;
    GLfloat v[] = {
        x0, y0, u0, v0,  x1, y0, u1, v0,  x0, y1, u0, v1,
        x1, y0, u1, v0,  x1, y1, u1, v1,  x0, y1, u0, v1,
    };
    glBindBuffer(GL_ARRAY_BUFFER, 0);
    glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(GLfloat), v);
    glVertexAttribPointer(1, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(GLfloat), v + 2);
    glEnableVertexAttribArray(0);
    glEnableVertexAttribArray(1);
    glDrawArrays(GL_TRIANGLES, 0, 6);
    glDisableVertexAttribArray(0);
    glDisableVertexAttribArray(1);
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
    // Textures of the decorations are made (and kept) before drawing.
    int n = wb->norder > 0 ? wb->norder : 1;
    struct wlr_texture *textures[n];
    for (int k = 0; k < wb->norder; k++) {
        struct wobble_src *src = &wb->srcs[wb->order[k]];
        textures[k] = NULL;
        if (src->alive && !src->is_rect) {
            textures[k] = source_texture(wb, src, wlr_scene_buffer_from_node(src->node));
        }
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
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(attribs.target, attribs.tex);
        glTexParameteri(attribs.target, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(attribs.target, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
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
                    double x = wb->mx + u * wb->w + dx, y = wb->my + t * wb->h + dy;
                    v[nv * 4 + 0] = (GLfloat)(2 * x / W - 1);
                    v[nv * 4 + 1] = (GLfloat)(2 * y / H - 1);
                    v[nv * 4 + 2] = (GLfloat)(u * wb->w * wb->scale / wb->flat_w);
                    v[nv * 4 + 3] = (GLfloat)(t * wb->h * wb->scale / wb->flat_h);
                    nv++;
                }
            }
        }

        glUseProgram(prog);
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(attribs.target, attribs.tex);
        glTexParameteri(attribs.target, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(attribs.target, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
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

static void wobble_destroy(struct wobble *wb);

// wobble_set_pixels tells which decoration buffer each decoration node
// shows (they change when the decorations are drawn again).
static void wobble_set_pixels(struct wobble *wb, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int n) {
    wb->npixels = 0;
    for (int i = 0; i < n && wb->npixels < WOBBLE_MAX_PIXELS; i++) {
        if (nodes[i] && buffers[i]) {
            wb->pixels[wb->npixels++] = (struct wobble_pixels){ nodes[i], buffers[i] };
        }
    }
}

// wobble_create prepares the wobbling picture of the window drawn by view
// (a tree), just above it. It returns NULL when the renderer is not GLES2.
static struct wobble *wobble_create(struct wlr_scene_tree *view, struct wlr_output *output,
        double cell, double bend, float scale, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int npix) {
    struct wlr_renderer *renderer = output->renderer;
    struct wlr_allocator *allocator = output->allocator;
    if (!renderer || !allocator || !output->swapchain) {
        return NULL;
    }
    if (!wlr_renderer_is_gles2(renderer) || g_egl_display == EGL_NO_DISPLAY) {
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
    const struct wlr_drm_format *primary = &output->swapchain->format;
    for (size_t i = 0; i < primary->len; i++) {
        wlr_drm_format_set_add(&formats, DRM_FORMAT_ARGB8888, primary->modifiers[i]);
    }
    if (primary->len == 0) {
        wlr_drm_format_set_add(&formats, DRM_FORMAT_ARGB8888, DRM_FORMAT_MOD_INVALID);
    }
    const struct wlr_drm_format *format = wlr_drm_format_set_get(&formats, DRM_FORMAT_ARGB8888);

    struct wobble *wb = calloc(1, sizeof(*wb));
    wb->renderer = renderer;
    wobble_set_pixels(wb, nodes, buffers, npix);
    wb->ox = box[0];
    wb->oy = box[1];
    wb->w = box[2] - box[0];
    wb->h = box[3] - box[1];
    wb->mx = (int)ceil(wb->w * bend) + 2;
    wb->my = (int)ceil(wb->h * bend) + 2;
    wb->scale = scale;
    wb->gx = (int)fmin(fmax(round(wb->w / cell), 3), 40);
    wb->gy = (int)fmin(fmax(round(wb->h / cell), 3), 40);
    wb->verts = calloc(wb->gx * wb->gy * WOBBLE_SUBDIV * WOBBLE_SUBDIV * 6 * 4, sizeof(GLfloat));
    wb->flat_w = (int)ceil(wb->w * scale);
    wb->flat_h = (int)ceil(wb->h * scale);
    wb->out_chain = wlr_swapchain_create(allocator, (int)ceil((wb->w + 2 * wb->mx) * scale),
        (int)ceil((wb->h + 2 * wb->my) * scale), format);
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
static bool wobble_update(struct wobble *wb, const float *mx, const float *my, int px, int py) {
    collect_sources(wb);
    // The textures are made first: wlroots switches EGL contexts meanwhile.
    for (int k = 0; k < wb->norder; k++) {
        struct wobble_src *src = &wb->srcs[wb->order[k]];
        if (src->alive && !src->is_rect) {
            source_texture(wb, src, wlr_scene_buffer_from_node(src->node));
        }
    }
    if (!eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, g_egl_context)) {
        return false;
    }
    bool flat_ok = render_flat(wb);
    struct wlr_buffer *out = flat_ok ? render_bent(wb, mx, my) : NULL;
    glFlush();
    eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    if (!out) {
        return false;
    }
    hide_sources(wb);
    wlr_scene_buffer_set_buffer(wb->picture, out);
    wlr_buffer_unlock(out); // the scene buffer holds it now
    wlr_scene_buffer_set_dest_size(wb->picture, (int)(wb->w + 2 * wb->mx), (int)(wb->h + 2 * wb->my));
    wlr_scene_node_set_position(&wb->picture->node, px + (int)wb->ox - wb->mx, py + (int)wb->oy - wb->my);
    return true;
}

// wobble_destroy removes the picture and shows the window again.
static void wobble_destroy(struct wobble *wb) {
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
        eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, g_egl_context);
        glDeleteFramebuffers(1, &wb->flat_fbo);
        glDeleteTextures(1, &wb->flat_tex);
        eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
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

static int wobble_gx(struct wobble *wb) { return wb->gx; }
static int wobble_gy(struct wobble *wb) { return wb->gy; }
static double wobble_w(struct wobble *wb) { return wb->w; }
static double wobble_h(struct wobble *wb) { return wb->h; }
static double wobble_ox(struct wobble *wb) { return wb->ox; }
static double wobble_oy(struct wobble *wb) { return wb->oy; }
*/
import "C"

import (
	"math"
	"time"
	"unsafe"
)

// Wobbly windows: while a window is dragged it bends like jelly, lagging
// behind the pointer the farther from where it is held, and settles with a
// little bounce when released. Each frame the window is drawn as it is into
// a picture, which is drawn again on a mesh bent by damped springs (GLES2;
// with another renderer windows keep still).
const (
	wobbleCell      = 28.0  // target size of a mesh cell, in pixels
	wobbleStiffness = 260.0 // spring stiffness where the window is held
	wobbleDamping   = 0.45  // damping ratio: below 1, it overshoots
	wobbleCoupling  = 110.0 // how much neighbour points pull each other
	wobbleMaxBend   = 0.15  // largest displacement, as a share of the window size
	wobbleGrip      = 120.0 // radius of the patch the hand holds, in pixels
	wobbleStep      = time.Second / 240
)

// wobbleState is the jelly of the window being moved or settling.
type wobbleState struct {
	view         any            // *xdgView or *xwayView
	tree         unsafe.Pointer // the view tree the picture stands in for
	c            *C.struct_wobble
	gx, gy       int
	w, h         float64
	dx, dy       []float32 // displacement of each mesh point
	vx, vy       []float64 // velocity
	anchor       int       // the point under the pointer; -1 once released
	lastX, lastY float64   // view position at the last frame
	last         time.Time
	stiffness    []float64
	hold         []float64 // how firmly the hand holds each point, 1 under it
}

// wobbleEnabled reports whether windows wobble.
func (s *server) wobbleEnabled() bool {
	return s.wobblyWindows && !s.reduceMotion
}

// viewTreeAndPos returns the scene tree of a view and its position.
func viewTreeAndPos(view any) (unsafe.Pointer, float64, float64, bool) {
	switch v := view.(type) {
	case *xdgView:
		return v.sceneTree, v.x, v.y, v.mapped
	case *xwayView:
		return v.sceneTree, v.x, v.y, v.mapped
	}
	return nil, 0, 0, false
}

// startWobble makes the grabbed window wobble, held at the pointer.
func (s *server) startWobble(view any) {
	s.stopWobble()
	if !s.wobbleEnabled() {
		return
	}
	tree, x, y, mapped := viewTreeAndPos(view)
	if tree == nil || !mapped {
		return
	}
	out := s.getActiveOutput()
	if out == nil {
		return
	}
	nodes, buffers := decorationPixels(view)
	c := C.wobble_create((*C.struct_wlr_scene_tree)(tree), outputPtr(out.output),
		C.double(wobbleCell), C.double(wobbleMaxBend), C.float(s.maxOutputScale()),
		&nodes[0], &buffers[0], C.int(len(nodes)))
	if c == nil {
		return
	}
	w := &wobbleState{
		view: view, tree: tree, c: c,
		gx: int(C.wobble_gx(c)), gy: int(C.wobble_gy(c)),
		w: float64(C.wobble_w(c)), h: float64(C.wobble_h(c)),
		lastX: x, lastY: y, last: time.Now(),
	}
	n := (w.gx + 1) * (w.gy + 1)
	w.dx, w.dy = make([]float32, n), make([]float32, n)
	w.vx, w.vy = make([]float64, n), make([]float64, n)

	// The point nearest to the pointer, in the extent of the window.
	ox, oy := float64(C.wobble_ox(c)), float64(C.wobble_oy(c))
	treeX, treeY := s.viewTreeOrigin(view)
	px := (s.cursor.X() - treeX - ox) / w.w * float64(w.gx)
	py := (s.cursor.Y() - treeY - oy) / w.h * float64(w.gy)
	ai := int(math.Round(math.Min(math.Max(px, 0), float64(w.gx))))
	aj := int(math.Round(math.Min(math.Max(py, 0), float64(w.gy))))
	w.anchor = aj*(w.gx+1) + ai

	// Points far from where the window is held are softer: they lag more.
	reach := math.Max(w.w, w.h) * 0.6
	w.stiffness = make([]float64, n)
	w.hold = make([]float64, n)
	for j := 0; j <= w.gy; j++ {
		for i := 0; i <= w.gx; i++ {
			d := math.Hypot(float64(i-ai)*w.w/float64(w.gx), float64(j-aj)*w.h/float64(w.gy))
			w.stiffness[j*(w.gx+1)+i] = wobbleStiffness * (0.45 + 0.55*math.Exp(-d/reach))
			// The hand holds a patch, not a point: no crease where it grips.
			w.hold[j*(w.gx+1)+i] = math.Exp(-(d * d) / (wobbleGrip * wobbleGrip))
		}
	}
	s.wobble = w
	if !s.drawWobble() {
		s.stopWobble()
	}
}

// viewTreeOrigin returns where the tree of a view is in the scene: the
// titlebar sits above the surface position.
func (s *server) viewTreeOrigin(view any) (float64, float64) {
	switch v := view.(type) {
	case *xdgView:
		if v.decorated && !v.fullscreen {
			return v.x, v.y - titlebarHeight
		}
		return v.x, v.y
	case *xwayView:
		if v.decorated && !v.fullscreen {
			return v.x, v.y - titlebarHeight
		}
		return v.x, v.y
	}
	return 0, 0
}

// releaseWobble lets the window settle: nothing holds it any more.
func (s *server) releaseWobble() {
	if s.wobble != nil {
		s.wobble.anchor = -1
	}
}

// stopWobble shows the window as it is, at once.
func (s *server) stopWobble() {
	w := s.wobble
	if w == nil {
		return
	}
	s.wobble = nil
	// The picture is a sibling of the view tree: it is still there even if
	// the view went away (the nodes it stood in for are then gone).
	C.wobble_destroy(w.c)
}

// tickWobble moves the jelly on and reports whether it still moves.
func (s *server) tickWobble() bool {
	w := s.wobble
	if w == nil {
		return false
	}
	tree, x, y, mapped := viewTreeAndPos(w.view)
	if tree != w.tree || !mapped {
		s.stopWobble()
		return false
	}

	// The window moved: the points stay where they were, the springs will
	// bring them back.
	if mx, my := x-w.lastX, y-w.lastY; mx != 0 || my != 0 {
		for k := range w.dx {
			if k != w.anchor {
				w.dx[k] -= float32(mx)
				w.dy[k] -= float32(my)
			}
		}
		w.lastX, w.lastY = x, y
	}

	now := time.Now()
	elapsed := min(now.Sub(w.last), 50*time.Millisecond)
	w.last = now
	for ; elapsed > 0; elapsed -= wobbleStep {
		w.step(wobbleStep.Seconds())
	}

	settled := w.anchor < 0
	for k := range w.dx {
		if math.Abs(float64(w.dx[k])) > 0.5 || math.Abs(float64(w.dy[k])) > 0.5 ||
			math.Abs(w.vx[k]) > 6 || math.Abs(w.vy[k]) > 6 {
			settled = false
			break
		}
	}
	if settled {
		s.stopWobble()
		return false
	}
	if !s.drawWobble() {
		s.stopWobble()
		return false
	}
	return true
}

// step integrates the springs over dt seconds.
func (w *wobbleState) step(dt float64) {
	n := w.gx + 1
	maxX, maxY := w.w*wobbleMaxBend, w.h*wobbleMaxBend
	for j := 0; j <= w.gy; j++ {
		for i := 0; i <= w.gx; i++ {
			k := j*n + i
			if k == w.anchor {
				w.dx[k], w.dy[k], w.vx[k], w.vy[k] = 0, 0, 0, 0
				continue
			}
			dx, dy := float64(w.dx[k]), float64(w.dy[k])
			// Pull of the neighbours, which keeps the mesh smooth.
			var nx, ny float64
			for _, o := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				ii, jj := i+o[0], j+o[1]
				if ii < 0 || ii > w.gx || jj < 0 || jj > w.gy {
					continue
				}
				nx += float64(w.dx[jj*n+ii]) - dx
				ny += float64(w.dy[jj*n+ii]) - dy
			}
			stiff := w.stiffness[k]
			damp := 2 * wobbleDamping * math.Sqrt(stiff)
			ax := -stiff*dx - damp*w.vx[k] + wobbleCoupling*nx
			ay := -stiff*dy - damp*w.vy[k] + wobbleCoupling*ny
			w.vx[k] += ax * dt
			w.vy[k] += ay * dt
			nx2, ny2 := dx+w.vx[k]*dt, dy+w.vy[k]*dt
			if w.anchor >= 0 {
				// Held: the patch under the hand follows it.
				keep := 1 - w.hold[k]
				nx2, ny2 = nx2*keep, ny2*keep
				w.vx[k], w.vy[k] = w.vx[k]*keep, w.vy[k]*keep
			}
			w.dx[k] = float32(math.Max(-maxX, math.Min(maxX, nx2)))
			w.dy[k] = float32(math.Max(-maxY, math.Min(maxY, ny2)))
		}
	}
}

// drawWobble draws the bent window; it reports false when it could not
// (the window then shows as it is).
func (s *server) drawWobble() bool {
	w := s.wobble
	nodes, buffers := decorationPixels(w.view)
	C.wobble_set_pixels(w.c, &nodes[0], &buffers[0], C.int(len(nodes)))
	tx, ty := s.viewTreeOrigin(w.view)
	return bool(C.wobble_update(w.c, (*C.float)(unsafe.Pointer(&w.dx[0])), (*C.float)(unsafe.Pointer(&w.dy[0])),
		C.int(math.Round(tx)), C.int(math.Round(ty))))
}

// maxOutputScale is the largest output scale: the picture is drawn for it.
func (s *server) maxOutputScale() float32 {
	scale := float32(1)
	for _, o := range s.outputs {
		scale = max(scale, o.output.Scale())
	}
	return scale
}

// decorationPixels returns the nodes of a view's decorations drawn from
// buffers of the compositor, and those buffers.
func decorationPixels(view any) ([4]*C.struct_wlr_scene_node, [4]*C.struct_wlr_buffer) {
	var nodes [4]*C.struct_wlr_scene_node
	var buffers [4]*C.struct_wlr_buffer
	var pairs [4][2]unsafe.Pointer
	switch v := view.(type) {
	case *xdgView:
		pairs = [4][2]unsafe.Pointer{{v.decoTitlebar, v.decoTitlePix}, {v.decoCornerBL, v.decoCornerPL},
			{v.decoCornerBR, v.decoCornerPR}, {v.decoIconBuf, v.decoIconPix}}
	case *xwayView:
		pairs = [4][2]unsafe.Pointer{{v.decoTitlebar, v.decoTitlePix}, {v.decoCornerBL, v.decoCornerPL},
			{v.decoCornerBR, v.decoCornerPR}, {v.decoIconBuf, v.decoIconPix}}
	}
	for i, p := range pairs {
		if p[0] != nil && p[1] != nil {
			nodes[i] = &(*C.struct_wlr_scene_buffer)(p[0]).node
			buffers[i] = &(*C.struct_pixel_buffer)(p[1]).base
		}
	}
	return nodes, buffers
}
