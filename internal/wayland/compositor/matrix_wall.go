package compositor

/*
#include <wlr/types/wlr_scene.h>

#include "matrix_wall.h"
*/
import "C"

import (
	"log"
	"unsafe"

	"fyshos.com/tyde/internal/wallpaper"
)

// startMatrixWall has the GPU draw the matrix wallpaper of an output, when it
// can: the CPU then only moves the columns.
func (s *server) startMatrixWall(out *outputState) {
	anim, ok := out.animWallpaper.(*matrixAnim)
	if !ok || out.matrixWall != nil || out.wallOnCPU || out.animBuf == nil {
		return
	}
	atlas := wallpaper.MatrixGlyphAtlas()
	b := out.animBuf.Rect
	mw := C.matrix_wall_create(outputPtr(out.output), C.int(b.Dx()), C.int(b.Dy()),
		(*C.uint8_t)(unsafe.Pointer(&atlas[0])),
		C.int(wallpaper.MatrixGlyphW), C.int(wallpaper.MatrixGlyphH), C.int(wallpaper.MatrixNumGlyphs))
	if mw == nil {
		out.wallOnCPU = true // pixman, or no buffers for it
		return
	}
	out.matrixWall = unsafe.Pointer(mw)
	out.wallCols = make([]byte, anim.Columns()*4)
}

// tickMatrixWall draws the next frame of the matrix on the GPU, and reports
// whether it did; the CPU draws it otherwise.
func (s *server) tickMatrixWall(out *outputState) bool {
	s.startMatrixWall(out) // again after a GPU reset
	if out.matrixWall == nil || out.wallpaperBuf == nil {
		return false
	}
	n := out.animWallpaper.(*matrixAnim).Step(out.wallCols)
	buf := C.matrix_wall_tick((*C.struct_matrix_wall)(out.matrixWall),
		(*C.uint8_t)(unsafe.Pointer(&out.wallCols[0])), C.int(n))
	if buf == nil {
		log.Printf("[WALLPAPER] the GPU could not draw the matrix on %s: the CPU does\n", out.output.Name())
		s.stopMatrixWall(out)
		out.wallOnCPU = true
		return false
	}
	C.wlr_scene_buffer_set_buffer((*C.struct_wlr_scene_buffer)(out.wallpaperBuf), buf)
	C.wlr_buffer_unlock(buf) // the scene buffer holds it now
	return true
}

// stopMatrixWall frees the GPU's matrix of an output.
func (s *server) stopMatrixWall(out *outputState) {
	if out.matrixWall != nil {
		C.matrix_wall_destroy((*C.struct_matrix_wall)(out.matrixWall))
		out.matrixWall = nil
	}
	out.wallCols = nil
}

// dropMatrixWallsGL frees the GPU's matrices, made with the old renderer
// after a GPU reset; they are made again on the next tick.
func (s *server) dropMatrixWallsGL() {
	for _, out := range s.outputs {
		s.stopMatrixWall(out)
	}
	C.matrix_wall_reset_gl()
}
