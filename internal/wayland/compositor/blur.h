#ifndef TYDE_BLUR_H
#define TYDE_BLUR_H

#include <stdbool.h>
#include <wlr/types/wlr_compositor.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>

// A blurred picture of what lies behind a translucent window, drawn just
// below it and cut to the shape of the window (its alpha channel): frosted
// glass behind the panel, the menus and the notifications (blur.go).
struct blur;

// blur_create prepares the blur behind the window drawn by view. It returns
// NULL when the renderer is not GLES2.
struct blur *blur_create(struct wlr_scene_tree *view, struct wlr_output *output);

// blur_update draws the blur again if what lies behind the surface, or the
// surface itself, changed since the last time. It reports whether it drew.
bool blur_update(struct blur *b, struct wlr_scene *scene, struct wlr_surface *surface, float scale);

// blur_supported reports whether the renderer can blur (GLES2).
bool blur_supported(void);

// blur_alive reports whether the view tree still exists.
bool blur_alive(struct blur *b);

// blur_destroy removes the blur and frees it.
void blur_destroy(struct blur *b);

#endif
