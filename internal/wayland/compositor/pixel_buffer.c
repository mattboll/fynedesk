#include <stdlib.h>
#include <string.h>
#include <drm_fourcc.h>

#include "pixel_buffer.h"

static void pixel_buffer_destroy(struct wlr_buffer *wlr_buf) {
	struct pixel_buffer *buf = (struct pixel_buffer *)wlr_buf;
	free(buf->data);
	free(buf);
}

static bool pixel_buffer_begin_data_ptr_access(struct wlr_buffer *wlr_buf,
		uint32_t flags, void **data, uint32_t *format, size_t *stride) {
	struct pixel_buffer *buf = (struct pixel_buffer *)wlr_buf;
	*data = buf->data;
	*format = buf->format;
	*stride = buf->stride;
	return true;
}

static void pixel_buffer_end_data_ptr_access(struct wlr_buffer *wlr_buf) {}

static const struct wlr_buffer_impl pixel_buffer_impl = {
	.destroy = pixel_buffer_destroy,
	.begin_data_ptr_access = pixel_buffer_begin_data_ptr_access,
	.end_data_ptr_access = pixel_buffer_end_data_ptr_access,
};

struct pixel_buffer *pixel_buffer_create(int w, int h) {
	struct pixel_buffer *buf = calloc(1, sizeof(struct pixel_buffer));
	if (!buf) return NULL;
	buf->format = DRM_FORMAT_ABGR8888;
	buf->stride = (size_t)w * 4;
	buf->data = calloc((size_t)h, buf->stride);
	if (!buf->data) {
		free(buf);
		return NULL;
	}
	wlr_buffer_init(&buf->base, &pixel_buffer_impl, w, h);
	return buf;
}

void pixel_buffer_release(struct pixel_buffer *buf) {
	if (buf) {
		wlr_buffer_drop(&buf->base);
	}
}

void pixel_buffer_update(struct pixel_buffer *buf, const void *pixels, int w, int h) {
	size_t new_stride = (size_t)w * 4;
	size_t new_size = new_stride * (size_t)h;
	if (buf->base.width != w || buf->base.height != h) {
		free(buf->data);
		buf->data = malloc(new_size);
		buf->stride = new_stride;
		buf->base.width = w;
		buf->base.height = h;
	}

	const uint8_t *src = pixels;
	uint8_t *dst = buf->data;
	for (size_t i = 0; i < new_size; i += 4) {
		uint8_t a = src[i + 3];
		if (a == 255) {
			memcpy(dst + i, src + i, 4);
			continue;
		}
		dst[i] = (uint8_t)((src[i] * a + 127) / 255);
		dst[i + 1] = (uint8_t)((src[i + 1] * a + 127) / 255);
		dst[i + 2] = (uint8_t)((src[i + 2] * a + 127) / 255);
		dst[i + 3] = a;
	}
}
