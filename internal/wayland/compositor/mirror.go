package compositor

/*
#include <stdint.h>
#include <stdlib.h>
#include <wayland-server-core.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <wlr/render/pass.h>
#include <wlr/render/wlr_renderer.h>
#include <wlr/render/wlr_texture.h>
#include <wlr/types/wlr_output.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/util/transform.h>

extern void goMirrorSourceGone(uintptr_t handle);
extern void goMirrorTargetGone(uintptr_t handle);

// mirror shows the frames of a source output on a target output, scaled to
// fit with black bars so that the picture keeps its proportions.
struct mirror {
	struct wlr_output *src, *dst;
	struct wlr_buffer *frame; // last frame committed on src, locked
	uintptr_t handle;         // Go side, for the destroy callbacks
	bool fresh;               // a frame came since the last one shown
	int drawn_w, drawn_h;     // the target's size when last drawn

	struct wl_listener src_commit, src_destroy, dst_frame, dst_destroy;
};

static void mirror_src_commit(struct wl_listener *listener, void *data) {
	struct mirror *m = wl_container_of(listener, m, src_commit);
	const struct wlr_output_event_commit *event = data;
	if (!(event->state->committed & WLR_OUTPUT_STATE_BUFFER) || !event->state->buffer) {
		return;
	}
	struct wlr_buffer *frame = wlr_buffer_lock(event->state->buffer);
	if (m->frame) {
		wlr_buffer_unlock(m->frame);
	}
	m->frame = frame;
	m->fresh = true;
	wlr_output_schedule_frame(m->dst);
}

// mirror_fit returns where a picture of pw×ph goes on a dw×dh buffer, as big
// as possible and centred.
static struct wlr_box mirror_fit(int pw, int ph, int dw, int dh) {
	struct wlr_box box = { .width = dw, .height = dh };
	if (pw <= 0 || ph <= 0) {
		return box;
	}
	if ((int64_t)dw * ph > (int64_t)dh * pw) { // target wider: bars on the sides
		box.width = (int)((int64_t)dh * pw / ph);
	} else {
		box.height = (int)((int64_t)dw * ph / pw);
	}
	box.x = (dw - box.width) / 2;
	box.y = (dh - box.height) / 2;
	return box;
}

static void mirror_dst_frame(struct wl_listener *listener, void *data) {
	struct mirror *m = wl_container_of(listener, m, dst_frame);
	if (!m->dst->enabled) {
		return;
	}
	// Nothing new to show: no commit, so no frame event follows and the
	// target rests (it was redrawn at every one of its refreshes).
	if (!m->fresh && m->drawn_w == m->dst->width && m->drawn_h == m->dst->height) {
		return;
	}

	struct wlr_output_state state;
	wlr_output_state_init(&state);
	struct wlr_render_pass *pass = wlr_output_begin_render_pass(m->dst, &state, NULL);
	if (!pass) {
		wlr_output_state_finish(&state);
		return;
	}

	int dw = m->dst->width, dh = m->dst->height;
	wlr_render_pass_add_rect(pass, &(struct wlr_render_rect_options){
		.box = { .width = dw, .height = dh },
		.color = { .r = 0, .g = 0, .b = 0, .a = 1 },
	});

	struct wlr_texture *texture = NULL;
	if (m->frame) {
		texture = wlr_texture_from_buffer(m->dst->renderer, m->frame);
	}
	if (texture) {
		// The frame is in the source's buffer orientation: undo it, then apply
		// the target's.
		enum wl_output_transform transform = wlr_output_transform_compose(
			wlr_output_transform_invert(m->src->transform), m->dst->transform);
		int pw = texture->width, ph = texture->height;
		if (transform & WL_OUTPUT_TRANSFORM_90) {
			pw = texture->height;
			ph = texture->width;
		}
		wlr_render_pass_add_texture(pass, &(struct wlr_render_texture_options){
			.texture = texture,
			.dst_box = mirror_fit(pw, ph, dw, dh),
			.transform = transform,
			.filter_mode = WLR_SCALE_FILTER_BILINEAR,
		});
	}

	bool ok = wlr_render_pass_submit(pass);
	if (texture) {
		wlr_texture_destroy(texture);
	}
	if (ok && wlr_output_commit_state(m->dst, &state)) {
		m->fresh = false;
		m->drawn_w = dw;
		m->drawn_h = dh;
	}
	wlr_output_state_finish(&state);
}

static void mirror_src_destroy(struct wl_listener *listener, void *data) {
	struct mirror *m = wl_container_of(listener, m, src_destroy);
	goMirrorSourceGone(m->handle);
}

static void mirror_dst_destroy(struct wl_listener *listener, void *data) {
	struct mirror *m = wl_container_of(listener, m, dst_destroy);
	goMirrorTargetGone(m->handle);
}

static struct mirror *mirror_create(struct wlr_output *src, struct wlr_output *dst, uintptr_t handle) {
	struct mirror *m = calloc(1, sizeof(*m));
	if (!m) {
		return NULL;
	}
	m->src = src;
	m->dst = dst;
	m->handle = handle;

	m->src_commit.notify = mirror_src_commit;
	wl_signal_add(&src->events.commit, &m->src_commit);
	m->src_destroy.notify = mirror_src_destroy;
	wl_signal_add(&src->events.destroy, &m->src_destroy);
	m->dst_frame.notify = mirror_dst_frame;
	wl_signal_add(&dst->events.frame, &m->dst_frame);
	m->dst_destroy.notify = mirror_dst_destroy;
	wl_signal_add(&dst->events.destroy, &m->dst_destroy);

	// Draw the cursor into the source's frames, so it is mirrored too.
	wlr_output_lock_software_cursors(src, true);
	wlr_output_schedule_frame(src);
	wlr_output_schedule_frame(dst);
	return m;
}

// mirror_destroy stops mirroring. src_alive is false once the source output
// is being destroyed.
static void mirror_destroy(struct mirror *m, bool src_alive) {
	wl_list_remove(&m->src_commit.link);
	wl_list_remove(&m->src_destroy.link);
	wl_list_remove(&m->dst_frame.link);
	wl_list_remove(&m->dst_destroy.link);
	if (src_alive) {
		wlr_output_lock_software_cursors(m->src, false);
	}
	if (m->frame) {
		wlr_buffer_unlock(m->frame);
	}
	free(m);
}

static void destroy_scene_output(struct wlr_scene_output *scene_output) {
	wlr_scene_output_destroy(scene_output);
}
*/
import "C"

import (
	"log"
	"runtime/cgo"
)

// mirrorState is an output showing another one's picture instead of being
// part of the desktop.
type mirrorState struct {
	s              *server
	target, source *outputState
	c              *C.struct_mirror
	handle         cgo.Handle
}

// entry returns the saved layout of an output, if any. It accepts a nil config.
func (c *OutputLayoutConfig) entry(name string) (OutputLayoutEntry, bool) {
	if c == nil {
		return OutputLayoutEntry{}, false
	}
	e, ok := c.Layouts[name]
	return e, ok
}

// startMirror makes target show the picture of source. A target that was part
// of the desktop leaves it: its windows move to the other outputs.
func (s *server) startMirror(target, source *outputState) {
	name := target.output.Name()
	if target == source || s.mirrors[name] != nil || s.mirrors[source.output.Name()] != nil {
		return
	}

	if s.isDesktopOutput(target) {
		if s.primaryOutput() == target {
			s.setPrimaryOutput(source.output.Name())
		}
		s.removeOutputFromDesktop(target)
	}

	m := &mirrorState{s: s, target: target, source: source}
	m.handle = cgo.NewHandle(m)
	m.c = C.mirror_create(outputPtr(source.output), outputPtr(target.output), C.uintptr_t(m.handle))
	if m.c == nil {
		m.handle.Delete()
		log.Printf("[MIRROR] cannot mirror %s on %s", source.output.Name(), name)
		s.addOutputToDesktop(target, s.readLayoutConfig())
		return
	}
	s.mirrors[name] = m
	log.Printf("[MIRROR] %s now mirrors %s", name, source.output.Name())
	s.writeCompositorState()
}

// stopMirror ends a mirror. The target rejoins the desktop unless it is gone
// or the compositor is exiting.
func (s *server) stopMirror(m *mirrorState, sourceAlive, targetAlive bool) {
	C.mirror_destroy(m.c, C.bool(sourceAlive))
	m.handle.Delete()
	delete(s.mirrors, m.target.output.Name())
	log.Printf("[MIRROR] %s no longer mirrors %s", m.target.output.Name(), m.source.output.Name())

	if targetAlive && !s.shuttingDown.Load() {
		s.addOutputToDesktop(m.target, s.readLayoutConfig())
	}
	s.writeCompositorState()
}

// stopMirrorOf ends the mirror shown on the named output, if any, and reports
// whether there was one.
func (s *server) stopMirrorOf(name string) bool {
	m := s.mirrors[name]
	if m == nil {
		return false
	}
	s.stopMirror(m, true, true)
	return true
}

// startPendingMirrors mirrors a newly added output on the outputs whose saved
// layout asks for it: they may have been connected before it.
func (s *server) startPendingMirrors(source *outputState) {
	config := s.readLayoutConfig()
	for _, o := range append([]*outputState(nil), s.outputs...) {
		if e, ok := config.entry(o.output.Name()); ok && e.Position == "mirror" &&
			e.RelativeTo == source.output.Name() && o != source {
			s.startMirror(o, source)
		}
	}
}

// isDesktopOutput reports whether out is one of the desktop's outputs.
func (s *server) isDesktopOutput(out *outputState) bool {
	for _, o := range s.outputs {
		if o == out {
			return true
		}
	}
	return false
}

// removeOutputFromDesktop takes an enabled output out of the desktop: out of
// the layout, without scene output, windows, wallpaper or panel.
func (s *server) removeOutputFromDesktop(out *outputState) {
	s.outLayout.Remove(out.output)
	if out.sceneOutput != nil {
		C.destroy_scene_output((*C.struct_wlr_scene_output)(out.sceneOutput))
		out.sceneOutput = nil
	}
	s.handleOutputDestroy(out)
}

//export goMirrorSourceGone
func goMirrorSourceGone(handle C.uintptr_t) {
	m := cgo.Handle(handle).Value().(*mirrorState)
	m.s.stopMirror(m, false, true)
}

//export goMirrorTargetGone
func goMirrorTargetGone(handle C.uintptr_t) {
	m := cgo.Handle(handle).Value().(*mirrorState)
	m.s.stopMirror(m, true, false)
}

// allOutputs returns the desktop's outputs and the mirrored ones.
func (s *server) allOutputs() []*outputState {
	all := append([]*outputState(nil), s.outputs...)
	for _, m := range s.mirrors {
		all = append(all, m.target)
	}
	return all
}
