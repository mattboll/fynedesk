package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "gl_util.h"
#include "pixel_buffer.h"
*/
import "C"

import (
	"image"
	"image/color"
	"math"
	"sort"
	"time"
	"unsafe"

	"fyshos.com/tyde/internal/wayland/wlr/xkb"
)

// --- Window Overview (Exposé / Mission Control) ---
// Uses per-window wlr_scene_buffers with GPU scaling for crisp thumbnails.
// Original windows are hidden during overview; individual buffers animate
// from real positions to grid positions (GNOME Shell style).

const overviewAnimDuration = 350 * time.Millisecond

// toggleOverview opens or closes the window overview.
func (s *server) toggleOverview() {
	if s.overviewActive {
		s.closeOverview()
		return
	}
	s.openOverview()
}

// openOverview enters overview mode with per-window scene buffers.
func (s *server) openOverview() {
	if s.switcherActive {
		return
	}

	type entry struct {
		view     interface{}
		focusSeq uint64
	}

	var entries []entry
	for _, v := range s.xdgViews {
		if v.mapped && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, entry{view: v, focusSeq: v.focusSeq})
		}
	}
	for _, v := range s.xwayViews {
		if v.mapped && !v.isPanel && !v.isOverlay && v.parent == nil && v.onDesk(s.currentDesk) {
			entries = append(entries, entry{view: v, focusSeq: v.focusSeq})
		}
	}
	if len(entries) == 0 {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].focusSeq > entries[j].focusSeq
	})

	outGeo := s.getActiveOutputGeo()
	layout := s.computeOverviewLayout(outGeo, len(entries))
	s.overviewLayout = layout

	// Capture resolution: native window size capped at output dimensions.
	captMaxW := outGeo.width
	captMaxH := outGeo.height

	// Activate EGL for batch thumbnail capture
	eglOK := bool(C.gl_begin())
	var pix []byte
	if eglOK {
		pix = make([]byte, captMaxW*captMaxH*4)
	}

	swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)

	// Create dim overlay (starts transparent for animation)
	var dimColor [4]C.float // all zeros = transparent black
	dimRect := C.wlr_scene_rect_create(swTree, C.int(outGeo.width), C.int(outGeo.height), &dimColor[0])
	if dimRect != nil {
		C.wlr_scene_node_set_position(&dimRect.node, C.int(outGeo.x), C.int(outGeo.y))
	}
	s.overviewDimRect = unsafe.Pointer(dimRect)

	// Create per-window entries
	n := len(entries)
	s.overviewEntries = make([]overviewEntry, n)

	for i, ent := range entries {
		row := i / layout.cols
		col := i % layout.cols
		itemsInRow := layout.cols
		if row == layout.rows-1 {
			itemsInRow = n - row*layout.cols
		}
		rowOffset := 0
		if itemsInRow < layout.cols {
			rowOffset = (layout.cols - itemsInRow) * (layout.cellW + layout.gap) / 2
		}
		gridX := outGeo.x + layout.margin + rowOffset + col*(layout.cellW+layout.gap)
		gridY := outGeo.y + layout.margin + row*(layout.cellH+layout.gap)

		realX, realY, realW, realH := s.getViewScreenRect(ent.view)

		// Capture high-res thumbnail
		thumbImg := s.overviewThumbnail(ent.view, eglOK, pix, captMaxW, captMaxH)

		sceneBuf, pixBuf := overviewThumbBuffer(swTree, thumbImg, realX, realY, realW, realH)

		// Get and hide original window's scene tree
		var origTree unsafe.Pointer
		switch v := ent.view.(type) {
		case *xdgView:
			origTree = v.sceneTree
		case *xwayView:
			origTree = v.sceneTree
		}
		if origTree != nil {
			setViewSceneEnabled(origTree, false)
		}

		fitX, fitY, fitW, fitH := overviewFitRect(layout, gridX, gridY, realW, realH)

		s.overviewEntries[i] = overviewEntry{
			view: ent.view, sceneBuf: sceneBuf, pixBuf: pixBuf,
			sceneTree: origTree,
			realX:     realX, realY: realY, realW: realW, realH: realH,
			gridX: fitX, gridY: fitY, gridW: fitW, gridH: fitH,
		}
	}

	if eglOK {
		C.gl_end()
	}

	s.overviewActive = true
	setViewSceneEnabled(s.switcherTree, true)

	if s.reduceMotion {
		s.applyOverviewPositions(1.0)
		s.createOverviewTitles()
	} else {
		s.overviewAnimActive = true
		s.overviewAnimStart = time.Now()
		s.overviewAnimClosing = false
		// Render first frame at progress=0 (windows at real positions)
		s.applyOverviewPositions(0.0)
	}
}

// overviewThumbnail captures the thumbnail of an overview window, falling back
// to its cached thumbnail.
func (s *server) overviewThumbnail(view interface{}, eglOK bool, pix []byte, captMaxW, captMaxH int) *image.NRGBA {
	var thumbImg *image.NRGBA
	if eglOK {
		switch v := view.(type) {
		case *xdgView:
			thumbImg = s.captureXDGThumbDirect(v, pix, captMaxW, captMaxH)
			if thumbImg == nil {
				thumbImg = v.cachedThumb
			}
		case *xwayView:
			thumbImg = s.captureWlrThumbDirect(v, pix, captMaxW, captMaxH)
			if thumbImg == nil {
				thumbImg = v.cachedThumb
			}
		}
	} else {
		// Fallback to cached thumbnails
		switch v := view.(type) {
		case *xdgView:
			thumbImg = v.cachedThumb
		case *xwayView:
			thumbImg = v.cachedThumb
		}
	}
	return thumbImg
}

// overviewThumbBuffer creates the scene buffer showing a thumbnail at the
// window's real position and size.
func overviewThumbBuffer(swTree *C.struct_wlr_scene_tree, thumbImg *image.NRGBA, realX, realY, realW, realH int) (unsafe.Pointer, unsafe.Pointer) {
	var sceneBuf unsafe.Pointer
	var pixBuf unsafe.Pointer

	if thumbImg != nil {
		tw := thumbImg.Bounds().Dx()
		th := thumbImg.Bounds().Dy()
		pb := C.pixel_buffer_create(C.int(tw), C.int(th))
		if pb != nil {
			C.pixel_buffer_update(pb, unsafe.Pointer(&thumbImg.Pix[0]), C.int(tw), C.int(th))
			sb := C.wlr_scene_buffer_create(swTree, &pb.base)
			if sb != nil {
				// Start at window's real position and size
				C.wlr_scene_node_set_position(&sb.node, C.int(realX), C.int(realY))
				C.wlr_scene_buffer_set_dest_size(sb, C.int(realW), C.int(realH))
				sceneBuf = unsafe.Pointer(sb)
			}
			pixBuf = unsafe.Pointer(pb)
		}
	}
	return sceneBuf, pixBuf
}

// overviewFitRect fits a window into its overview cell, preserving its aspect
// ratio.
func overviewFitRect(layout overviewLayoutData, gridX, gridY, realW, realH int) (int, int, int, int) {
	// Fit window into cell while preserving aspect ratio
	fitW, fitH := layout.cellW, layout.thumbH
	if realW > 0 && realH > 0 {
		winAspect := float64(realW) / float64(realH)
		cellAspect := float64(layout.cellW) / float64(layout.thumbH)
		if winAspect > cellAspect {
			// Window is wider than cell — fit by width
			fitH = int(float64(layout.cellW) / winAspect)
		} else {
			// Window is taller than cell — fit by height
			fitW = int(float64(layout.thumbH) * winAspect)
		}
	}
	// Center within cell
	fitX := gridX + (layout.cellW-fitW)/2
	fitY := gridY + (layout.thumbH-fitH)/2
	return fitX, fitY, fitW, fitH
}

// applyOverviewPositions updates per-window scene buffer positions and sizes
// based on the animation progress (0.0 = real positions, 1.0 = grid positions).
// This is extremely lightweight: just setting int positions on scene nodes.
func (s *server) applyOverviewPositions(progress float64) {
	// Update dim overlay alpha
	if s.overviewDimRect != nil {
		dimAlpha := C.float(0.63 * progress)
		var color [4]C.float
		color[3] = dimAlpha
		C.wlr_scene_rect_set_color((*C.struct_wlr_scene_rect)(s.overviewDimRect), &color[0])
	}

	// Update each window thumbnail position and display size
	for _, e := range s.overviewEntries {
		if e.sceneBuf == nil {
			continue
		}
		sb := (*C.struct_wlr_scene_buffer)(e.sceneBuf)

		cx := e.realX + int(float64(e.gridX-e.realX)*progress)
		cy := e.realY + int(float64(e.gridY-e.realY)*progress)
		cw := e.realW + int(float64(e.gridW-e.realW)*progress)
		ch := e.realH + int(float64(e.gridH-e.realH)*progress)
		if cw < 1 {
			cw = 1
		}
		if ch < 1 {
			ch = 1
		}

		C.wlr_scene_node_set_position(&sb.node, C.int(cx), C.int(cy))
		C.wlr_scene_buffer_set_dest_size(sb, C.int(cw), C.int(ch))
	}
}

// createOverviewTitles renders title labels below each window thumbnail.
// Called once when the overview animation completes (or immediately if reduce motion).
func (s *server) createOverviewTitles() {
	face := getTitleFontFace()
	if face == nil {
		return
	}
	swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)
	txtColor := color.NRGBA{R: 220, G: 220, B: 230, A: 255}

	for i := range s.overviewEntries {
		e := &s.overviewEntries[i]
		title := s.switcherWindowTitle(e.view)
		if title == "" {
			continue
		}

		layout := s.overviewLayout
		imgW := e.gridW
		imgH := layout.titleH
		if imgW < 1 || imgH < 1 {
			continue
		}

		img := image.NewNRGBA(image.Rect(0, 0, imgW, imgH))
		s.drawTextOnImage(img, title, 0, 0, imgW, imgH, txtColor, face)

		pb := C.pixel_buffer_create(C.int(imgW), C.int(imgH))
		if pb == nil {
			continue
		}
		C.pixel_buffer_update(pb, unsafe.Pointer(&img.Pix[0]), C.int(imgW), C.int(imgH))

		sb := C.wlr_scene_buffer_create(swTree, &pb.base)
		if sb == nil {
			C.pixel_buffer_release(pb)
			continue
		}

		C.wlr_scene_node_set_position(&sb.node, C.int(e.gridX), C.int(e.gridY+e.gridH))

		e.titleBuf = unsafe.Pointer(sb)
		e.titlePix = unsafe.Pointer(pb)
	}
}

// closeOverview exits overview mode (with animation if enabled).
func (s *server) closeOverview() {
	if !s.overviewActive {
		return
	}
	if s.overviewAnimClosing {
		return
	}

	// Destroy title labels before close animation (they don't animate)
	for i := range s.overviewEntries {
		e := &s.overviewEntries[i]
		if e.titleBuf != nil {
			C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.titleBuf).node)
			e.titleBuf = nil
		}
		if e.titlePix != nil {
			C.pixel_buffer_release((*C.struct_pixel_buffer)(e.titlePix))
			e.titlePix = nil
		}
	}

	if s.reduceMotion || s.overviewAnimActive {
		s.finishCloseOverview()
		return
	}

	s.overviewAnimActive = true
	s.overviewAnimStart = time.Now()
	s.overviewAnimClosing = true
}

// finishCloseOverview performs the actual cleanup after overview closes.
func (s *server) finishCloseOverview() {
	// Re-enable original window scene trees first (visible under switcherTree)
	for _, e := range s.overviewEntries {
		if e.sceneTree != nil {
			setViewSceneEnabled(e.sceneTree, true)
		}
	}

	// Destroy per-window scene buffers and pixel buffers
	for _, e := range s.overviewEntries {
		if e.sceneBuf != nil {
			C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.sceneBuf).node)
		}
		if e.pixBuf != nil {
			C.pixel_buffer_release((*C.struct_pixel_buffer)(e.pixBuf))
		}
		if e.titleBuf != nil {
			C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_buffer)(e.titleBuf).node)
		}
		if e.titlePix != nil {
			C.pixel_buffer_release((*C.struct_pixel_buffer)(e.titlePix))
		}
	}

	// Destroy dim overlay
	if s.overviewDimRect != nil {
		C.wlr_scene_node_destroy(&(*C.struct_wlr_scene_rect)(s.overviewDimRect).node)
		s.overviewDimRect = nil
	}

	s.overviewActive = false
	s.overviewAnimActive = false
	s.overviewAnimClosing = false
	s.overviewEntries = nil
	setViewSceneEnabled(s.switcherTree, false)
}

// overviewSelectAt checks if (px, py) hits a window thumbnail in the overview grid.
// Returns the index or -1.
func (s *server) overviewSelectAt(px, py int) int {
	layout := s.overviewLayout
	for i, e := range s.overviewEntries {
		if px >= e.gridX && px < e.gridX+e.gridW &&
			py >= e.gridY && py < e.gridY+e.gridH+layout.titleH {
			return i
		}
	}
	return -1
}

// overviewClick handles a mouse click during overview mode.
func (s *server) overviewClick(px, py int) {
	if s.overviewAnimActive {
		return
	}

	idx := s.overviewSelectAt(px, py)
	if idx < 0 || idx >= len(s.overviewEntries) {
		s.closeOverview()
		return
	}

	w := s.overviewEntries[idx].view
	s.finishCloseOverview()

	switch v := w.(type) {
	case *xdgView:
		if v.mapped {
			s.focusXdgView(v)
		}
	case *xwayView:
		if v.mapped {
			s.focusXwayView(v)
		}
	}
}

// handleOverviewKey handles keypresses while overview is active.
func (s *server) handleOverviewKey(sym xkb.KeySym) {
	switch sym {
	case xkb.KeySymEscape:
		s.closeOverview()
	case xkb.KeySymw:
		s.closeOverview()
	}
}

// tickOverviewAnim advances the overview animation. Returns true if still running.
func (s *server) tickOverviewAnim() bool {
	if !s.overviewAnimActive {
		return false
	}

	elapsed := time.Since(s.overviewAnimStart)
	progress := float64(elapsed) / float64(overviewAnimDuration)
	if progress > 1.0 {
		progress = 1.0
	}

	easedProgress := dampedSpring(progress)

	if s.overviewAnimClosing {
		s.applyOverviewPositions(1.0 - easedProgress)
	} else {
		s.applyOverviewPositions(easedProgress)
	}

	if progress >= 1.0 {
		if s.overviewAnimClosing {
			s.finishCloseOverview()
		} else {
			s.overviewAnimActive = false
			s.applyOverviewPositions(1.0)
			s.createOverviewTitles()
		}
		return false
	}
	return true
}

// computeOverviewLayout calculates the grid layout for the overview.
func (s *server) computeOverviewLayout(outGeo outputGeometry, n int) overviewLayoutData {
	const (
		margin = 40
		gap    = 16
		titleH = 24
	)
	cols, rows := overviewGridSize(n, outGeo.width, outGeo.height)
	usableW := outGeo.width - margin*2 - gap*(cols-1)
	usableH := outGeo.height - margin*2 - gap*(rows-1)
	cellW := usableW / cols
	cellH := usableH / rows
	thumbH := cellH - titleH
	if thumbH < 60 {
		thumbH = 60
	}
	return overviewLayoutData{
		screenW: outGeo.width, screenH: outGeo.height,
		screenX: outGeo.x, screenY: outGeo.y,
		cols: cols, rows: rows,
		cellW: cellW, cellH: cellH,
		thumbH: thumbH,
		margin: margin, gap: gap, titleH: titleH,
	}
}

// getViewScreenRect returns a window's approximate screen position and size.
func (s *server) getViewScreenRect(w interface{}) (x, y, width, height int) {
	switch v := w.(type) {
	case *xdgView:
		vw, vh := v.configuredW, v.configuredH
		if vw <= 0 || vh <= 0 {
			vw, vh = 800, 600
		}
		return int(v.x), int(v.y), vw, vh
	case *xwayView:
		return int(v.x), int(v.y), v.surface.Width(), v.surface.Height()
	}
	return 0, 0, 800, 600
}

// overviewGridSize calculates optimal columns and rows for n windows.
func overviewGridSize(n, screenW, screenH int) (cols, rows int) {
	if n <= 0 {
		return 1, 1
	}

	aspect := float64(screenW) / float64(screenH)
	bestCols := 1
	bestScore := math.MaxFloat64

	for c := 1; c <= n && c <= 8; c++ {
		r := (n + c - 1) / c
		gridAspect := float64(c) / float64(r)
		score := math.Abs(gridAspect - aspect)
		if score < bestScore {
			bestScore = score
			bestCols = c
		}
	}

	cols = bestCols
	rows = (n + cols - 1) / cols
	return
}
