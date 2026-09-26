package compositor

/*
#include "pixel_buffer.h"
#include "wobble.h"
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
	drawnAtRest  bool      // the picture at rest is drawn: held still, nothing to redraw
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
		&nodes[0], &buffers[0], C.int(len(nodes)), false, 0, 0)
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

// dropWobbleGL ends the effects and forgets their GL programs, after a GPU
// reset: all were made with the old renderer.
func (s *server) dropWobbleGL() {
	s.stopWobble()
	if s.genie != nil {
		s.endGenie()
	}
	C.wobble_reset_gl()
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
		w.drawnAtRest = false
	}

	now := time.Now()
	elapsed := min(now.Sub(w.last), 50*time.Millisecond)
	w.last = now
	for ; elapsed > 0; elapsed -= wobbleStep {
		w.step(wobbleStep.Seconds())
	}

	atRest := true
	for k := range w.dx {
		if math.Abs(float64(w.dx[k])) > 0.5 || math.Abs(float64(w.dy[k])) > 0.5 ||
			math.Abs(w.vx[k]) > 6 || math.Abs(w.vy[k]) > 6 {
			atRest = false
			break
		}
	}
	if atRest && w.anchor < 0 {
		s.stopWobble() // let go and settled
		return false
	}
	if atRest && w.drawnAtRest {
		// Held still: the last picture stands, and no frame is needed
		// until the window moves again.
		return false
	}
	if !s.drawWobble() {
		s.stopWobble()
		return false
	}
	w.drawnAtRest = atRest
	return !atRest
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
		pairs = [4][2]unsafe.Pointer{
			{v.decoTitlebar, v.decoTitlePix},
			{v.decoCornerBL, v.decoCornerPL},
			{v.decoCornerBR, v.decoCornerPR},
			{v.decoIconBuf, v.decoIconPix},
		}
	case *xwayView:
		pairs = [4][2]unsafe.Pointer{
			{v.decoTitlebar, v.decoTitlePix},
			{v.decoCornerBL, v.decoCornerPL},
			{v.decoCornerBR, v.decoCornerPR},
			{v.decoIconBuf, v.decoIconPix},
		}
	}
	for i, p := range pairs {
		if p[0] != nil && p[1] != nil {
			nodes[i] = &(*C.struct_wlr_scene_buffer)(p[0]).node
			buffers[i] = &(*C.struct_pixel_buffer)(p[1]).base
		}
	}
	return nodes, buffers
}

// The magic lamp: a window minimized by the user flows into the dock, the
// side nearest to it first, narrowing to the dock's width. It is drawn like a
// wobbling window (wobble.go), on a mesh that follows a path instead of
// springs.
const (
	genieDuration = 480 * time.Millisecond
	genieWidth    = 36.0 // what the window narrows to, about an icon
)

type genieState struct {
	view         any
	tree         unsafe.Pointer
	c            *C.struct_wobble
	gx, gy       int
	w, h, ox, oy float64
	tx, ty       float64 // the dock, in the view tree
	horizontal   bool    // the dock is on a side: the window flows sideways
	start        time.Time
	dx, dy       []float32
	done         func() // minimizes the window for good
}

// minimizeWithEffect minimizes a window the user asked to minimize, through
// the magic lamp when the effects allow it, then runs then (if not nil).
func (s *server) minimizeWithEffect(view any, then func()) {
	minimize := func() {
		switch v := view.(type) {
		case *xdgView:
			s.minimizeXdgWindow(v)
		case *xwayView:
			s.minimizeXwayWindow(v)
		}
		if then != nil {
			then()
		}
		s.writeWindowsState()
	}
	if s.genie != nil || !s.startGenie(view, minimize) {
		minimize()
	}
}

// startGenie starts the magic lamp of a view, which calls done at the end.
// It reports false when the window cannot be drawn so (no GPU, reduced
// motion, not shown).
func (s *server) startGenie(view any, done func()) bool {
	if s.reduceMotion || !s.wobblyWindows {
		return false
	}
	tree, _, _, mapped := viewTreeAndPos(view)
	out := s.getActiveOutput()
	if tree == nil || !mapped || out == nil {
		return false
	}
	treeX, treeY := s.viewTreeOrigin(view)
	dockX, dockY := s.dockTarget(view)
	tx, ty := dockX-treeX, dockY-treeY

	nodes, buffers := decorationPixels(view)
	c := C.wobble_create((*C.struct_wlr_scene_tree)(tree), outputPtr(out.output),
		C.double(wobbleCell), 0, C.float(s.maxOutputScale()),
		&nodes[0], &buffers[0], C.int(len(nodes)), true, C.double(tx), C.double(ty))
	if c == nil {
		return false
	}
	g := &genieState{
		view: view, tree: tree, c: c,
		gx: int(C.wobble_gx(c)), gy: int(C.wobble_gy(c)),
		w: float64(C.wobble_w(c)), h: float64(C.wobble_h(c)),
		ox: float64(C.wobble_ox(c)), oy: float64(C.wobble_oy(c)),
		tx: tx, ty: ty, horizontal: s.barPosition != "bottom",
		start: time.Now(), done: done,
	}
	n := (g.gx + 1) * (g.gy + 1)
	g.dx, g.dy = make([]float32, n), make([]float32, n)
	s.genie = g
	if !s.drawGenie() {
		s.endGenie()
		return false
	}
	return true
}

// genieEase is smoothstep: slow at both ends.
func genieEase(p float64) float64 {
	p = math.Max(0, math.Min(1, p))
	return p * p * (3 - 2*p)
}

// shape sets the mesh for the progress t of the animation, from 0 to 1.
func (g *genieState) shape(t float64) {
	n := g.gx + 1
	for j := 0; j <= g.gy; j++ {
		for i := 0; i <= g.gx; i++ {
			u, v := float64(i)/float64(g.gx), float64(j)/float64(g.gy)
			x, y := g.ox+u*g.w, g.oy+v*g.h
			// The side facing the dock goes first; the window narrows
			// before it slides.
			along, across := u, v
			if g.tx > g.ox+g.w/2 { // the dock is on the other side
				along = 1 - u
			}
			if !g.horizontal {
				along, across = 1-v, u
				if g.ty < g.oy+g.h/2 {
					along = v
				}
			}
			slide := genieEase(t*1.6 - along*0.6)
			narrow := genieEase(t*2.2 - along*0.9)
			var nx, ny float64
			if g.horizontal {
				ny = y + (g.ty+(across-0.5)*genieWidth-y)*narrow
				nx = x + (g.tx-x)*slide
			} else {
				nx = x + (g.tx+(across-0.5)*genieWidth-x)*narrow
				ny = y + (g.ty-y)*slide
			}
			g.dx[j*n+i] = float32(nx - x)
			g.dy[j*n+i] = float32(ny - y)
		}
	}
}

func (s *server) drawGenie() bool {
	g := s.genie
	g.shape(float64(time.Since(g.start)) / float64(genieDuration))
	tx, ty := s.viewTreeOrigin(g.view)
	return bool(C.wobble_update(g.c, (*C.float)(unsafe.Pointer(&g.dx[0])), (*C.float)(unsafe.Pointer(&g.dy[0])),
		C.int(math.Round(tx)), C.int(math.Round(ty))))
}

// tickGenie moves the lamp on and reports whether it still runs.
func (s *server) tickGenie() bool {
	g := s.genie
	if g == nil {
		return false
	}
	tree, _, _, mapped := viewTreeAndPos(g.view)
	if tree != g.tree || !mapped || time.Since(g.start) >= genieDuration || !s.drawGenie() {
		s.endGenie()
		return false
	}
	return true
}

// endGenie minimizes the window, if it is still there, and shows its
// picture no more.
func (s *server) endGenie() {
	g := s.genie
	s.genie = nil
	tree, _, _, mapped := viewTreeAndPos(g.view)
	if tree == g.tree && mapped {
		g.done() // hidden first, so that the restored picture does not flash
	}
	C.wobble_destroy(g.c)
}

// dockTarget returns where a window goes in the dock: its icon, as the
// panel reported it, or the middle of the dock.
func (s *server) dockTarget(view any) (float64, float64) {
	var id string
	switch v := view.(type) {
	case *xdgView:
		id = v.id
	case *xwayView:
		id = v.id
	}
	if icon, ok := s.dockIcons[id]; ok {
		return float64(icon.X), float64(icon.Y)
	}
	return s.dockCenter()
}
