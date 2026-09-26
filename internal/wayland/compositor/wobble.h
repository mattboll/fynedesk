// The picture of a window bent by a mesh of springs: wobbly windows while
// one is dragged, and the magic lamp (wobble.go). GLES2 only.
#ifndef TYDE_WOBBLE_H
#define TYDE_WOBBLE_H

#include <stdbool.h>
#include <wlr/types/wlr_buffer.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>

// Trees marked with this data are left out of the wobble (the attention
// glow, see attention.go).
#define WOBBLE_SKIP ((void *)0x7)

struct wobble;

// wobble_create prepares the wobbling picture of the window drawn by view
// (a tree), just above it: a mesh of cells of about cell pixels, room for
// bends of bend times its size, and, when reach is set, room to reach
// (reach_x, reach_y) in the tree. nodes[i] shows the decoration buffer
// buffers[i]. It returns NULL when the renderer is not GLES2.
struct wobble *wobble_create(struct wlr_scene_tree *view, struct wlr_output *output,
    double cell, double bend, float scale, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int npix,
    bool reach, double reach_x, double reach_y);

// wobble_set_pixels tells which decoration buffer each decoration node
// shows (they change when the decorations are drawn again).
void wobble_set_pixels(struct wobble *wb, struct wlr_scene_node **nodes, struct wlr_buffer **buffers, int n);

// wobble_update draws the window bent by the springs mesh (mx, my), the
// view tree being at (px, py) in its parent. It reports whether it could.
bool wobble_update(struct wobble *wb, const float *mx, const float *my, int px, int py);

// wobble_destroy removes the picture and shows the window again.
void wobble_destroy(struct wobble *wb);

// wobble_reset_gl forgets the GL programs, after a GPU reset.
void wobble_reset_gl(void);

// The mesh (cells across and down) and the extent of the window in its tree.
int wobble_gx(struct wobble *wb);
int wobble_gy(struct wobble *wb);
double wobble_w(struct wobble *wb);
double wobble_h(struct wobble *wb);
double wobble_ox(struct wobble *wb);
double wobble_oy(struct wobble *wb);

#endif
