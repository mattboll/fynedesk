// pixel_buffer is a wlr_buffer backed by CPU memory, used to show images
// rendered in Go (decorations, wallpapers, effects) in the scene graph.
#ifndef TYDE_PIXEL_BUFFER_H
#define TYDE_PIXEL_BUFFER_H

#include <stddef.h>
#include <stdint.h>
#include <wlr/interfaces/wlr_buffer.h>

struct pixel_buffer {
	struct wlr_buffer base;
	void *data;
	uint32_t format;
	size_t stride;
};

// pixel_buffer_create returns a w×h buffer in DRM_FORMAT_ABGR8888, whose
// R,G,B,A byte order matches Go's image.NRGBA.
struct pixel_buffer *pixel_buffer_create(int w, int h);

// pixel_buffer_release is called when the compositor forgets a buffer: it is
// freed once nothing uses it any more (the scene may still show it). Never
// free a buffer directly: wlroots attaches textures to it.
void pixel_buffer_release(struct pixel_buffer *buf);

// pixel_buffer_update copies straight-alpha (image.NRGBA) pixels into the
// buffer, reallocating it if the size changed. wlroots blends buffers as
// premultiplied alpha, so the colours are multiplied by alpha on the way.
void pixel_buffer_update(struct pixel_buffer *buf, const void *pixels, int w, int h);

#endif
