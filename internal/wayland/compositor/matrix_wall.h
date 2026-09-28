// The matrix wallpaper drawn by the GPU (wallpaper_output.go): the columns
// move on the CPU, the glyphs and their fading trails are drawn by a
// shader over the previous frame. GLES2 only.
#ifndef TYDE_MATRIX_WALL_H
#define TYDE_MATRIX_WALL_H

#include <stdint.h>
#include <wlr/types/wlr_buffer.h>
#include <wlr/types/wlr_output.h>

struct matrix_wall;

// matrix_wall_create prepares a w×h rain for output. atlas holds the glyphs
// side by side, one byte per pixel (0 or 255): nglyphs*glyph_w wide,
// glyph_h high. It returns NULL when the renderer is not GLES2.
struct matrix_wall *matrix_wall_create(struct wlr_output *output, int w, int h,
    const uint8_t *atlas, int glyph_w, int glyph_h, int nglyphs);

// matrix_wall_tick draws the next frame: the previous one faded, with the
// ncols columns of cols over it (4 bytes each, see MatrixAnim.Step). It
// returns the frame locked, for the scene to show, or NULL.
struct wlr_buffer *matrix_wall_tick(struct matrix_wall *mw, const uint8_t *cols, int ncols);

// matrix_wall_destroy frees the rain.
void matrix_wall_destroy(struct matrix_wall *mw);

// matrix_wall_reset_gl forgets the GL programs, after a GPU reset.
void matrix_wall_reset_gl(void);

#endif
