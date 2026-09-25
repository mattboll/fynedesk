package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"

static struct wlr_scene_tree *attention_tree_create(struct wlr_scene_tree *parent) {
    struct wlr_scene_tree *tree = wlr_scene_tree_create(parent);
    if (tree) {
        wlr_scene_node_lower_to_bottom(&tree->node);
        tree->node.data = (void *)0x7; // WOBBLE_SKIP: the halo keeps still (wobble.go)
    }
    return tree;
}
// The halo lets the clicks through to what is below it.
static bool attention_no_input(struct wlr_scene_buffer *buffer, double *sx, double *sy) {
    return false;
}
static struct wlr_scene_buffer *attention_part_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
    struct wlr_scene_buffer *part = wlr_scene_buffer_create(parent, buffer);
    if (part) {
        part->point_accepts_input = attention_no_input;
    }
    return part;
}
static void attention_part_place(struct wlr_scene_buffer *part, int x, int y, int w, int h, float opacity) {
    wlr_scene_node_set_position(&part->node, x, y);
    wlr_scene_buffer_set_dest_size(part, w, h);
    wlr_scene_buffer_set_opacity(part, opacity);
}
static void attention_tree_destroy(struct wlr_scene_tree *tree) {
    wlr_scene_node_destroy(&tree->node);
}
static void attention_tree_set_enabled(struct wlr_scene_tree *tree, bool on) {
    wlr_scene_node_set_enabled(&tree->node, on);
}
*/
import "C"

import (
	"image"
	"image/color"
	"math"
	"time"
	"unsafe"

	"fyshos.com/tyde/wlipc"
)

// The attention glow: a soft amber halo around the windows that wait for the
// user (see wlipc.RequestWindowAttention), until the user goes to them.
const (
	glowRadius = 22                      // how far the halo reaches, in pixels
	glowPeriod = 1600 * time.Millisecond // one breath
)

// A halo is a soft band of colour around a window, fading outwards: the
// attention glow, and the shadows (shadow.go).
type haloStyle struct {
	color    color.NRGBA
	radius   int     // how far it reaches, in pixels
	strength float64 // opacity next to the window
}

var glowStyle = haloStyle{color: color.NRGBA{R: 0xF5, G: 0xB0, B: 0x3A, A: 0xFF}, radius: glowRadius, strength: 0.85}

// alpha is the opacity of the halo at distance d outside the window.
func (st haloStyle) alpha(d float64) float64 {
	if d < 0 {
		return 0
	}
	x := d / float64(st.radius)
	return st.strength * math.Exp(-3.2*x*x)
}

// glowParts are the pieces of a halo, shared by all windows: four corners
// and four edges, one pixel long, stretched along the sides.
type glowParts struct {
	radius       int
	cornerRadius int
	buffers      [8]*C.struct_pixel_buffer // TL, TR, BL, BR, top, bottom, left, right
}

// glow is the halo of one window.
type glow struct {
	parent unsafe.Pointer // the view tree it lives in: it goes with it
	tree   *C.struct_wlr_scene_tree
	parts  [8]*C.struct_wlr_scene_buffer
}

// newGlowParts draws the pieces of a halo for windows with rounded corners
// of radius cr.
func newGlowParts(st haloStyle, cr int) *glowParts {
	g := &glowParts{radius: st.radius, cornerRadius: cr}
	size := st.radius + cr

	// Top-left corner; the others are its mirror images.
	corner := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(size)-float64(x)-0.5, float64(size)-float64(y)-0.5
			d := math.Hypot(dx, dy) - float64(cr) // distance to the rounded edge
			c := st.color
			c.A = uint8(255 * st.alpha(d))
			corner.SetNRGBA(x, y, c)
		}
	}
	edge := image.NewNRGBA(image.Rect(0, 0, 1, st.radius)) // top edge, outward up
	for y := 0; y < st.radius; y++ {
		c := st.color
		c.A = uint8(255 * st.alpha(float64(st.radius)-float64(y)-0.5))
		edge.SetNRGBA(0, y, c)
	}

	images := [8]*image.NRGBA{
		corner, flipH(corner), flipV(corner), flipV(flipH(corner)),
		edge, flipV(edge), transpose(edge), flipH(transpose(edge)),
	}
	for i, img := range images {
		b := img.Bounds()
		g.buffers[i] = C.pixel_buffer_create(C.int(b.Dx()), C.int(b.Dy()))
		C.pixel_buffer_update(g.buffers[i], unsafe.Pointer(&img.Pix[0]), C.int(b.Dx()), C.int(b.Dy()))
	}
	return g
}

func flipH(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetNRGBA(b.Dx()-1-x, y, src.NRGBAAt(x, y))
		}
	}
	return dst
}

func flipV(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetNRGBA(x, b.Dy()-1-y, src.NRGBAAt(x, y))
		}
	}
	return dst
}

func transpose(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetNRGBA(y, x, src.NRGBAAt(x, y))
		}
	}
	return dst
}

// place lays the halo around the rectangle (x, y, w, h) of the view tree.
func (g *glow) place(p *glowParts, x, y, w, h int, opacity float32) {
	s, r, cr := p.radius+p.cornerRadius, p.radius, p.cornerRadius
	inner := func(n int) int { return max(n-2*cr, 0) }
	boxes := [8][4]int{
		{x - r, y - r, s, s},
		{x + w - cr, y - r, s, s},
		{x - r, y + h - cr, s, s},
		{x + w - cr, y + h - cr, s, s},
		{x + cr, y - r, inner(w), r},
		{x + cr, y + h, inner(w), r},
		{x - r, y + cr, r, inner(h)},
		{x + w, y + cr, r, inner(h)},
	}
	for i, b := range boxes {
		C.attention_part_place(g.parts[i], C.int(b[0]), C.int(b[1]), C.int(max(b[2], 1)), C.int(max(b[3], 1)), C.float(opacity))
	}
}

// newGlow puts a halo made of parts at the bottom of a view tree, below the
// window; it goes with the tree.
func newGlow(tree unsafe.Pointer, parts *glowParts) *glow {
	g := &glow{parent: tree}
	g.tree = C.attention_tree_create((*C.struct_wlr_scene_tree)(tree))
	if g.tree == nil {
		return nil
	}
	for i, buf := range parts.buffers {
		g.parts[i] = C.attention_part_create(g.tree, &buf.base)
	}
	return g
}

// destroy removes the halo from its view tree.
func (g *glow) destroy() {
	C.attention_tree_destroy(g.tree)
}

// setVisible shows or hides the halo.
func (g *glow) setVisible(on bool) {
	C.attention_tree_set_enabled(g.tree, C.bool(on))
}

// setWindowAttention makes the windows with a title call for attention.
func (s *server) setWindowAttention(title string, on bool) {
	if on {
		s.attentionTitles[title] = true
	} else {
		delete(s.attentionTitles, title)
	}
	for _, o := range s.outputs {
		scheduleOutputFrame(o.output)
	}
}

func (s *server) wantsAttention(title string) bool {
	for t := range s.attentionTitles {
		if wlipc.TitleMatches(t, title) {
			return true
		}
	}
	return false
}

// glowTarget describes a window that should glow: its tree and the
// rectangle of the window in it.
type glowTarget struct {
	tree       unsafe.Pointer
	x, y, w, h int
}

// glowTargets returns the windows to surround, by view.
func (s *server) glowTargets() map[any]glowTarget {
	targets := map[any]glowTarget{}
	if len(s.attentionTitles) == 0 {
		return targets
	}
	shown := func(mapped, minimized, pinned bool, desk int) bool {
		return mapped && !minimized && (pinned || desk == s.currentDesk)
	}
	for _, v := range s.xdgViews {
		if v.sceneTree == nil || v.fullscreen || !shown(v.mapped, v.minimized, v.pinned, v.desk) ||
			!s.wantsAttention(v.xdgToplevel.Title()) {
			continue
		}
		if s.activeXdg == v {
			continue // the user is on it already
		}
		w, h := xdgDecoSize(v)
		t := glowTarget{tree: v.sceneTree, w: w, h: h}
		if v.decorated {
			t.x, t.w, t.h = -borderWidth, w+2*borderWidth, titlebarHeight+h+borderWidth
		}
		targets[v] = t
	}
	for _, v := range s.xwayViews {
		if v.sceneTree == nil || v.fullscreen || v.isPanel || v.isOverlay || v.overrideRedirect ||
			!shown(v.mapped, v.minimized, v.pinned, v.desk) || !s.wantsAttention(v.surface.Title()) {
			continue
		}
		if s.activeXway == v {
			continue // the user is on it already
		}
		w, h := xwayDecoSize(v)
		t := glowTarget{tree: v.sceneTree, w: w, h: h}
		if v.decorated {
			t.x, t.w, t.h = -borderWidth, w+2*borderWidth, titlebarHeight+h+borderWidth
		}
		targets[v] = t
	}
	return targets
}

// tickAttention updates the halos and reports whether one is breathing, to
// keep the frames coming.
func (s *server) tickAttention() bool {
	targets := s.glowTargets()

	for view, g := range s.glows {
		t, ok := targets[view]
		if !ok || t.tree != g.parent {
			if s.viewAlive(view, g.parent) {
				g.destroy() // else it went with its view
			}
			delete(s.glows, view)
		}
	}
	if len(targets) == 0 {
		return false
	}
	if s.glowParts == nil {
		s.glowParts = newGlowParts(glowStyle, cornerRadius+borderWidth)
	}

	breathing := false
	phase := float64(time.Now().UnixNano()%int64(glowPeriod)) / float64(glowPeriod)
	for view, t := range targets {
		g := s.glows[view]
		if g == nil {
			if g = newGlow(t.tree, s.glowParts); g == nil {
				continue
			}
			s.glows[view] = g
		}
		opacity := float32(1)
		if !s.reduceMotion {
			// Quantised so that most frames change nothing and damage nothing.
			breath := 0.5 - 0.5*math.Cos(2*math.Pi*phase)
			opacity = float32(math.Round((0.35+0.65*breath)*20) / 20)
			breathing = true
		}
		g.place(s.glowParts, t.x, t.y, t.w, t.h, opacity)
	}
	return breathing
}

// viewAlive reports whether a view still has the given tree.
func (s *server) viewAlive(view any, tree unsafe.Pointer) bool {
	switch v := view.(type) {
	case *xdgView:
		return v.sceneTree == tree && tree != nil
	case *xwayView:
		return v.sceneTree == tree && tree != nil
	}
	return false
}
