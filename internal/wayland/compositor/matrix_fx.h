// The matrix rain that sweeps the screen when the desktop changes and the
// wallpaper is the matrix one (transition.go), drawn by a shader. GLES2 only.
#ifndef TYDE_MATRIX_FX_H
#define TYDE_MATRIX_FX_H

#include <stdbool.h>
#include <stdint.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>

struct matrix_fx;

// matrix_fx_create prepares the rain over a w×h output, in parent. atlas
// holds the glyphs side by side, one byte per pixel (0 or 255):
// nglyphs*glyph_w wide, glyph_h high. It returns NULL when the renderer is
// not GLES2.
struct matrix_fx *matrix_fx_create(struct wlr_scene_tree *parent, struct wlr_output *output, int w, int h,
    const uint8_t *atlas, int glyph_w, int glyph_h, int nglyphs);

// MATRIX_FX_COL_ROWS is the height of the columns' texture: the head, then
// up to 10 glyphs.
#define MATRIX_FX_COL_ROWS 11

// matrix_fx_draw draws a frame: ncols columns of glyphs from band_start,
// up to band_end, the part already swept (left of the band when dir > 0,
// right of it otherwise) in dark green at opacity fade, the rest clear.
// cols is an RGBA texture ncols wide and MATRIX_FX_COL_ROWS high: in row 0,
// a column's first row (R*256+G) and length in glyphs (B); in row 1+g, the
// index of its glyph g (R). It reports whether it drew.
bool matrix_fx_draw(struct matrix_fx *fx, float band_start, float band_end, int dir, float fade,
    const uint8_t *cols, int ncols);

// matrix_fx_node returns the scene node that shows the rain.
struct wlr_scene_node *matrix_fx_node(struct matrix_fx *fx);

// matrix_fx_destroy removes the rain and frees it.
void matrix_fx_destroy(struct matrix_fx *fx);

// matrix_fx_reset_gl forgets the GL program, after a GPU reset.
void matrix_fx_reset_gl(void);

#endif
