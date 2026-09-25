package compositor

/*
#include <stdlib.h>
#include <wlr/render/pass.h>
#include <wlr/render/swapchain.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/render/allocator.h>
#include <wlr/types/wlr_buffer.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/util/box.h>
#include <wlr/util/transform.h>

// The magnified picture of an output: the scene draws the output as usual,
// then the part around the pointer is drawn enlarged into a buffer of our
// own, which the output shows instead.
struct zoom {
    struct wlr_output *output;
    struct wlr_swapchain *chain;
    double level, lx, ly; // of the picture shown
};

static struct zoom *zoom_create(struct wlr_output *output) {
    struct zoom *z = calloc(1, sizeof(*z));
    z->output = output;
    return z;
}

static void zoom_destroy(struct zoom *z) {
    if (z->chain) {
        wlr_swapchain_destroy(z->chain);
    }
    free(z);
}

// zoom_draw draws frame enlarged level times around (bx, by), in buffer
// pixels, into a buffer of the zoom. The EGL timeline of the scene, if any,
// is signalled when it is drawn.
static struct wlr_buffer *zoom_draw(struct zoom *z, struct wlr_scene_output *so,
        struct wlr_buffer *frame, double level, double bx, double by, struct wlr_output_state *state) {
    struct wlr_output *output = z->output;
    if (!output->swapchain) {
        return NULL;
    }
    if (!z->chain || z->chain->width != frame->width || z->chain->height != frame->height) {
        if (z->chain) {
            wlr_swapchain_destroy(z->chain);
        }
        z->chain = wlr_swapchain_create(output->allocator, frame->width, frame->height, &output->swapchain->format);
        if (!z->chain) {
            return NULL;
        }
    }
    struct wlr_buffer *out = wlr_swapchain_acquire(z->chain);
    if (!out) {
        return NULL;
    }
    struct wlr_texture *tex = wlr_texture_from_buffer(output->renderer, frame);
    if (!tex) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    struct wlr_buffer_pass_options opts = {0};
    if (so->WLR_PRIVATE.in_timeline) {
        so->WLR_PRIVATE.in_point++;
        opts.signal_timeline = so->WLR_PRIVATE.in_timeline;
        opts.signal_point = so->WLR_PRIVATE.in_point;
    }
    struct wlr_render_pass *pass = wlr_renderer_begin_buffer_pass(output->renderer, out, &opts);
    if (!pass) {
        wlr_texture_destroy(tex);
        wlr_buffer_unlock(out);
        return NULL;
    }
    double w = frame->width / level, h = frame->height / level;
    wlr_render_pass_add_texture(pass, &(struct wlr_render_texture_options){
        .texture = tex,
        .src_box = { bx - bx / level, by - by / level, w, h },
        .dst_box = { 0, 0, frame->width, frame->height },
        // Sharp pixels once they are big enough to be seen one by one.
        .filter_mode = level >= 4 ? WLR_SCALE_FILTER_NEAREST : WLR_SCALE_FILTER_BILINEAR,
        .blend_mode = WLR_RENDER_BLEND_MODE_NONE,
    });
    bool ok = wlr_render_pass_submit(pass);
    wlr_texture_destroy(tex);
    if (!ok) {
        wlr_buffer_unlock(out);
        return NULL;
    }
    if (opts.signal_timeline) {
        wlr_output_state_set_wait_timeline(state, opts.signal_timeline, opts.signal_point);
    }
    return out;
}

// zoom_commit draws the output through the scene, enlarges the picture
// around (lx, ly) (output layout pixels, from the output's top left) and
// shows it.
static bool zoom_commit(struct zoom *z, struct wlr_scene_output *so, double level, double lx, double ly) {
    if (!wlr_scene_output_needs_frame(so) && z->chain &&
            z->level == level && z->lx == lx && z->ly == ly) {
        return true; // the picture shown is still right
    }
    struct wlr_output_state state;
    wlr_output_state_init(&state);
    bool ok = false;
    if (!wlr_scene_output_build_state(so, &state, NULL) || !(state.committed & WLR_OUTPUT_STATE_BUFFER)) {
        goto out;
    }
    struct wlr_output *output = z->output;
    // The point in the buffer: scaled, then turned as the output is.
    int tw, th;
    wlr_output_transformed_resolution(output, &tw, &th);
    struct wlr_box p = { (int)(lx * output->scale), (int)(ly * output->scale), 0, 0 };
    struct wlr_box bp;
    wlr_box_transform(&bp, &p, wlr_output_transform_invert(output->transform), tw, th);

    struct wlr_buffer *frame = state.buffer;
    struct wlr_buffer *out = zoom_draw(z, so, frame, level, bp.x, bp.y, &state);
    if (!out) {
        goto out;
    }
    wlr_output_state_set_buffer(&state, out); // lets go of the frame
    wlr_buffer_unlock(out);
    pixman_region32_t whole;
    pixman_region32_init_rect(&whole, 0, 0, out->width, out->height);
    wlr_output_state_set_damage(&state, &whole);
    pixman_region32_fini(&whole);
    ok = wlr_output_commit_state(output, &state);
    if (ok) {
        z->level = level;
        z->lx = lx;
        z->ly = ly;
    }
out:
    wlr_output_state_finish(&state);
    return ok;
}

// zoom_scene_setup keeps the scene from handing a window's buffer straight to
// the screen while zoomed (it would not be enlarged) and damages the whole
// output when zooming stops, so that it is drawn plain again.
static void zoom_scene_setup(struct wlr_scene *scene, struct wlr_scene_output *so, bool zoomed, bool scanout) {
    scene->WLR_PRIVATE.direct_scanout = zoomed ? false : scanout;
    if (so) {
        wlr_damage_ring_add_whole(&so->damage_ring);
    }
}

static bool zoom_scene_scanout(struct wlr_scene *scene) {
    return scene->WLR_PRIVATE.direct_scanout;
}
*/
import "C"

import (
	"math"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr"
)

// The magnifier: the screen is enlarged around the pointer, which keeps its
// place on the screen while the picture follows it (Super+Alt+scroll,
// Super+= / Super+- / Super+0). Everything else works as usual: the
// pointer points at what is drawn under it.
const (
	zoomStep = 1.25 // each step enlarges or shrinks this much
	zoomMax  = 16.0
	zoomEase = 0.3 // share of the way to the wanted level done each frame
)

// magnifier is how much the screen is enlarged and where it heads.
type magnifier struct {
	level, target float64
	scanout       bool // the scene's direct scanout, restored when zooming stops
	outputs       map[*C.struct_wlr_output]*C.struct_zoom
}

// zoomBy enlarges (steps > 0) or shrinks the screen.
func (s *server) zoomBy(steps float64) {
	s.setZoom(s.zoom.target * math.Pow(zoomStep, steps))
}

// setZoom sets how much the screen is to be enlarged (1: not at all).
func (s *server) setZoom(target float64) {
	target = math.Min(math.Max(target, 1), zoomMax)
	if math.Abs(target-1) < 0.01 {
		target = 1
	}
	if s.zoom.target == 0 {
		s.zoom.level, s.zoom.target = 1, 1
	}
	if target == s.zoom.target {
		return
	}
	if s.zoom.target == 1 && target > 1 && s.zoom.level <= 1 {
		s.startZoom()
	}
	s.zoom.target = target
	s.scheduleAllOutputFrames()
}

// startZoom draws the pointer into the pictures (it is enlarged with them)
// and keeps the scene from scanning windows out.
func (s *server) startZoom() {
	scene := (*C.struct_wlr_scene)(s.scene)
	s.zoom.scanout = bool(C.zoom_scene_scanout(scene))
	C.zoom_scene_setup(scene, nil, true, C.bool(s.zoom.scanout))
	for _, o := range s.outputs {
		C.wlr_output_lock_software_cursors(outputPtr(o.output), true)
	}
}

// stopZoom goes back to plain drawing.
func (s *server) stopZoom() {
	scene := (*C.struct_wlr_scene)(s.scene)
	for _, o := range s.outputs {
		out := outputPtr(o.output)
		C.wlr_output_lock_software_cursors(out, false)
		C.zoom_scene_setup(scene, C.wlr_scene_get_scene_output(scene, out), false, C.bool(s.zoom.scanout))
	}
	for out, z := range s.zoom.outputs {
		C.zoom_destroy(z)
		delete(s.zoom.outputs, out)
	}
	s.scheduleAllOutputFrames()
}

// tickZoom moves the level towards the wanted one; it reports whether it
// moves.
func (s *server) tickZoom() bool {
	z := &s.zoom
	if z.level == 0 || z.level == z.target {
		return false
	}
	if s.reduceMotion {
		z.level = z.target
	} else {
		z.level += (z.target - z.level) * zoomEase
		if math.Abs(z.target-z.level) < 0.005 {
			z.level = z.target
		}
	}
	if z.level == 1 {
		s.stopZoom()
		return false
	}
	return true
}

// zoomed reports whether the screen is enlarged.
func (s *server) zoomed() bool {
	return s.zoom.level > 1
}

// commitZoomed shows output enlarged around the pointer. It reports whether
// the output took the new picture.
func (s *server) commitZoomed(output wlr.Output, sceneOutput *C.struct_wlr_scene_output) bool {
	out := outputPtr(output)
	if s.zoom.outputs == nil {
		s.zoom.outputs = map[*C.struct_wlr_output]*C.struct_zoom{}
	}
	z := s.zoom.outputs[out]
	if z == nil {
		z = C.zoom_create(out)
		s.zoom.outputs[out] = z
	}
	lx, ly := s.zoomCenter(output)
	return bool(C.zoom_commit(z, sceneOutput, C.double(s.zoom.level), C.double(lx), C.double(ly)))
}

// zoomCenter returns the point of output the picture is enlarged around: the
// pointer, or the nearest point of the output when it is on another one.
func (s *server) zoomCenter(output wlr.Output) (float64, float64) {
	for _, o := range s.outputs {
		if o.output != output {
			continue
		}
		x := math.Min(math.Max(s.cursor.X()-float64(o.layoutX), 0), float64(o.width))
		y := math.Min(math.Max(s.cursor.Y()-float64(o.layoutY), 0), float64(o.height))
		return x, y
	}
	return 0, 0
}

// forgetZoomOutput drops the zoom of an output going away.
func (s *server) forgetZoomOutput(output unsafe.Pointer) {
	out := (*C.struct_wlr_output)(output)
	if z := s.zoom.outputs[out]; z != nil {
		C.zoom_destroy(z)
		delete(s.zoom.outputs, out)
	}
}

// zoomByScroll enlarges the screen as the wheel turns up, shrinks it as it
// turns down: a notch is a step, a touchpad moves smoothly.
func (s *server) zoomByScroll(e wlr.AxisEvent) {
	if e.Orientation != wlr.AxisOrientationVertical {
		return
	}
	const notch = 15.0 // libinput's delta for one notch of the wheel
	s.zoomBy(-e.Delta / notch)
}
