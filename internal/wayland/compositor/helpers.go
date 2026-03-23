package compositor

import (
	"fmt"
	"math"
	"unsafe"

	"deedles.dev/wlr"
)

const baselineDPI = 120.0

// calculateScale computes a display scale factor from pixel width and physical width (mm).
// Returns 1.0 for unknown physical sizes (e.g. nested mode where phys=0).
// Rounds to nearest 0.25 for fractional scaling precision.
func calculateScale(widthPx, widthMm int) float32 {
	if widthMm == 0 {
		return 1.0
	}
	dpi := float32(widthPx) / (float32(widthMm) / 25.4)
	if dpi > 1000 || dpi < 10 {
		return 1.0
	}
	scale := dpi / baselineDPI
	if scale < 1.0 {
		return 1.0
	}
	// Round to nearest 0.25 for fractional scaling precision
	return float32(math.Round(float64(scale)*4) / 4)
}

// gcd computes greatest common divisor using Euclid's algorithm.
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// aspectRatio returns a human-readable aspect ratio string (e.g. "16:10", "16:9").
func aspectRatio(w, h int) string {
	ratio := float64(w) / float64(h)
	known := []struct {
		r float64
		s string
	}{
		{16.0 / 10.0, "16:10"},
		{16.0 / 9.0, "16:9"},
		{4.0 / 3.0, "4:3"},
		{5.0 / 4.0, "5:4"},
		{3.0 / 2.0, "3:2"},
		{21.0 / 9.0, "21:9"},
	}
	for _, k := range known {
		if math.Abs(ratio-k.r) < 0.02 {
			return k.s
		}
	}
	g := gcd(w, h)
	return fmt.Sprintf("%d:%d", w/g, h/g)
}

// commonResolutions returns standard resolutions below nativeW x nativeH.
// These are "virtual" resolutions achieved by adjusting the output scale.
func commonResolutions(nativeW, nativeH int) []OutputModeInfo {
	candidates := [][2]int{
		{3840, 2400}, {3840, 2160},
		{2560, 1600}, {2560, 1440},
		{1920, 1200}, {1920, 1080},
		{1680, 1050},
		{1600, 1200}, {1600, 900},
		{1440, 900},
		{1366, 768},
		{1280, 1024}, {1280, 800}, {1280, 720},
		{1024, 768},
	}

	var modes []OutputModeInfo
	seen := map[[2]int]bool{{nativeW, nativeH}: true}
	for _, c := range candidates {
		w, h := c[0], c[1]
		if w >= nativeW || h >= nativeH {
			continue
		}
		if seen[[2]int{w, h}] {
			continue
		}
		seen[[2]int{w, h}] = true
		modes = append(modes, OutputModeInfo{
			Width:       w,
			Height:      h,
			RefreshRate: 0, // virtual — inherits native refresh
			Custom:      true,
			AspectRatio: aspectRatio(w, h),
		})
	}
	return modes
}

// keyboardValid checks if a Keyboard has a non-nil underlying pointer.
// wlr_seat_get_keyboard returns NULL when no keyboard is attached to the seat.
func keyboardValid(kb wlr.Keyboard) bool {
	type kbPtr struct {
		p unsafe.Pointer
	}
	return (*kbPtr)(unsafe.Pointer(&kb)).p != nil
}

// viewFromNodeData resolves a scene node data pointer to the xdgView or xwayView it belongs to.
func (s *server) viewFromNodeData(data unsafe.Pointer) (*xdgView, *xwayView) {
	if data == nil {
		return nil, nil
	}
	for _, v := range s.xdgViews {
		if unsafe.Pointer(v) == data {
			return v, nil
		}
	}
	for _, v := range s.xwayViews {
		if unsafe.Pointer(v) == data {
			return nil, v
		}
	}
	return nil, nil
}

// setXdgScenePos sets the scene tree position for an XDG view,
// accounting for titlebar offset: with wlr_scene_xdg_surface_create,
// the tree position = geometry origin. For SSD (decorated) views,
// we shift the tree up by titlebarHeight so v.y remains the geometry
// origin and the titlebar renders above it.
func setXdgScenePos(v *xdgView) {
	y := int(v.y)
	if v.decorated && !v.fullscreen {
		y -= titlebarHeight
	}
	setViewScenePosition(v.sceneTree, int(v.x), y)
}

// setXwayScenePos sets the scene tree position for an XWayland view,
// accounting for titlebar offset: for decorated views, the surface node
// is at (0, titlebarHeight) inside the tree. We shift the tree up by
// titlebarHeight so v.y remains the surface position.
func setXwayScenePos(v *xwayView) {
	y := int(v.y)
	if v.decorated && !v.fullscreen {
		y -= titlebarHeight
	}
	setViewScenePosition(v.sceneTree, int(v.x), y)
}
