package compositor

// #include "pixel_buffer.h"
import "C"

import "unsafe"

// releasePixelBuffer lets go of a pixel buffer the compositor no longer uses
// and forgets it; it is freed once the scene has let go of it too.
func releasePixelBuffer(p *unsafe.Pointer) {
	if *p != nil {
		C.pixel_buffer_release((*C.struct_pixel_buffer)(*p))
		*p = nil
	}
}

// forgetDecorationNodes forgets the decoration nodes of a view whose scene
// tree was destroyed with them, and lets go of their pixel buffers.
func (v *xdgView) forgetDecorationNodes() {
	v.decoTitlebar, v.decoIconBuf, v.decoCornerBL, v.decoCornerBR = nil, nil, nil, nil
	v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
	for _, p := range []*unsafe.Pointer{&v.decoTitlePix, &v.decoIconPix, &v.decoCornerPL, &v.decoCornerPR} {
		releasePixelBuffer(p)
	}
}

// forgetDecorationNodes forgets the decoration nodes of a view whose scene
// tree was destroyed with them, and lets go of their pixel buffers.
func (v *xwayView) forgetDecorationNodes() {
	v.decoTitlebar, v.decoIconBuf, v.decoCornerBL, v.decoCornerBR = nil, nil, nil, nil
	v.decoBorderT, v.decoBorderB, v.decoBorderL, v.decoBorderR = nil, nil, nil, nil
	for _, p := range []*unsafe.Pointer{&v.decoTitlePix, &v.decoIconPix, &v.decoCornerPL, &v.decoCornerPR} {
		releasePixelBuffer(p)
	}
}
