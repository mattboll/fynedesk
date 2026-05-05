package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <stdlib.h>
#include <string.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

// pixel_buffer implementation for Go-rendered images
struct pixel_buffer {
    struct wlr_buffer base;
    void *data;
    uint32_t format;
    size_t stride;
};

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

static struct pixel_buffer *pixel_buffer_create(int w, int h) {
    struct pixel_buffer *buf = calloc(1, sizeof(struct pixel_buffer));
    if (!buf) return NULL;
    buf->format = DRM_FORMAT_ABGR8888;
    buf->stride = (size_t)w * 4;
    buf->data = calloc((size_t)h, buf->stride);
    if (!buf->data) { free(buf); return NULL; }
    wlr_buffer_init(&buf->base, &pixel_buffer_impl, w, h);
    return buf;
}

static void pixel_buffer_update(struct pixel_buffer *buf, const void *pixels, int w, int h) {
    size_t new_stride = (size_t)w * 4;
    size_t new_size = new_stride * (size_t)h;
    if (buf->base.width != w || buf->base.height != h) {
        free(buf->data);
        buf->data = malloc(new_size);
        buf->stride = new_stride;
        buf->base.width = w;
        buf->base.height = h;
    }
    memcpy(buf->data, pixels, new_size);
}

// Scene wrapper functions
static struct wlr_scene_rect *scene_rect_create(struct wlr_scene_tree *parent, int w, int h, const float color[4]) {
    return wlr_scene_rect_create(parent, w, h, color);
}
static void scene_rect_set_size(struct wlr_scene_rect *rect, int w, int h) {
    wlr_scene_rect_set_size(rect, w, h);
}
static void scene_rect_set_color(struct wlr_scene_rect *rect, const float color[4]) {
    wlr_scene_rect_set_color(rect, color);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
    wlr_scene_node_set_position(node, x, y);
}
static void scene_node_set_enabled(struct wlr_scene_node *node, int enabled) {
    wlr_scene_node_set_enabled(node, enabled);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
    wlr_scene_node_destroy(node);
}
static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
    return wlr_scene_buffer_create(parent, buffer);
}
static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
    wlr_scene_buffer_set_buffer(buf, buffer);
}
static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
    wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void scene_buffer_set_opacity(struct wlr_scene_buffer *buf, float opacity) {
    wlr_scene_buffer_set_opacity(buf, opacity);
}
static void scene_node_lower_to_bottom(struct wlr_scene_node *node) {
    wlr_scene_node_lower_to_bottom(node);
}
static void scene_node_place_below(struct wlr_scene_node *node, struct wlr_scene_node *sibling) {
    wlr_scene_node_place_below(node, sibling);
}
*/
import "C"

import (
	"image"
	"image/color"
	"log"
	"os"
	"strings"
	"unsafe"

	"github.com/FyshOS/appie"

	"golang.org/x/image/draw"
)

// Shadow configuration: disabled.
// wlr_scene_rect can only render flat-color rectangles, which look like harsh
// stepped bands rather than smooth gaussian blurs. Window borders provide
// sufficient visual separation without shadows (similar to Sway).
const shadowLayers = 3

// scrimOpacity is the opacity of the modal scrim overlay (0.0-1.0).
const scrimOpacity = 0.35

// createOrUpdateShadowsXdg is a no-op: shadows are disabled.
func (s *server) createOrUpdateShadowsXdg(v *xdgView, width, height int) {}

// createOrUpdateShadowsXway is a no-op: shadows are disabled.
func (s *server) createOrUpdateShadowsXway(v *xwayView, width, height int) {}

// createModalScrimXdg creates a semi-transparent overlay behind a dialog XDG window.
// The scrim covers the entire output and renders behind the dialog in the windows tree.
func (s *server) createModalScrimXdg(v *xdgView) {
	if v.scrimRect != nil || v.parent == nil {
		return
	}
	out := s.getOutputForPosition(v.x, v.y)
	if out == nil {
		out = s.primaryOutput()
	}
	if out == nil {
		return
	}
	outGeo := s.getOutputGeometry(out)
	color := [4]C.float{0, 0, 0, scrimOpacity}

	// Create scrim in the windows tree (same parent as regular windows)
	windowsTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	rect := C.scene_rect_create(windowsTree, C.int(outGeo.width), C.int(outGeo.height), &color[0])
	C.scene_node_set_position(&rect.node, C.int(outGeo.x), C.int(outGeo.y))
	v.scrimRect = unsafe.Pointer(rect)

	// Position scrim just below the dialog's scene tree (above parent, below dialog)
	// The dialog's scene tree was already raised, so the scrim goes below it
	if v.sceneTree != nil {
		viewTree := (*C.struct_wlr_scene_tree)(v.sceneTree)
		C.scene_node_place_below(&rect.node, &viewTree.node)
	}
}

// createModalScrimXway creates a semi-transparent overlay behind a dialog XWayland window.
func (s *server) createModalScrimXway(v *xwayView) {
	if v.scrimRect != nil || v.parent == nil {
		return
	}
	out := s.getOutputForPosition(v.x, v.y)
	if out == nil {
		out = s.primaryOutput()
	}
	if out == nil {
		return
	}
	outGeo := s.getOutputGeometry(out)
	color := [4]C.float{0, 0, 0, scrimOpacity}

	windowsTree := (*C.struct_wlr_scene_tree)(s.windowsTree)
	rect := C.scene_rect_create(windowsTree, C.int(outGeo.width), C.int(outGeo.height), &color[0])
	C.scene_node_set_position(&rect.node, C.int(outGeo.x), C.int(outGeo.y))
	v.scrimRect = unsafe.Pointer(rect)

	if v.sceneTree != nil {
		viewTree := (*C.struct_wlr_scene_tree)(v.sceneTree)
		C.scene_node_place_below(&rect.node, &viewTree.node)
	}
}

// destroyModalScrim destroys the scrim rect for a view.
func destroyModalScrim(scrimPtr *unsafe.Pointer) {
	if *scrimPtr == nil {
		return
	}
	C.scene_node_destroy(&(*C.struct_wlr_scene_rect)(*scrimPtr).node)
	*scrimPtr = nil
}

// createDecoNodes creates the server-side decoration scene nodes for a view.
// decoTree is the parent tree; returns titlebar buffer, pixel buffer, and 4 border rects.
// The surface node must be positioned at (0, titlebarHeight) within decoTree.
func (s *server) createDecoNodes(parent *C.struct_wlr_scene_tree, width, height int, active bool) (
	titleBuf unsafe.Pointer, titlePix unsafe.Pointer,
	borderT, borderB, borderL, borderR unsafe.Pointer,
) {
	if parent == nil {
		log.Printf("[DECO] ERROR: createDecoNodes called with nil parent")
		return
	}
	bColor := s.activeBorderColor(active)
	bc := colorToFloat4(bColor)

	// Left border: x=-borderWidth, y=cornerRadius, starting below the titlebar's rounded corners
	sideH := titlebarHeight + height - cornerRadius
	bL := C.scene_rect_create(parent, C.int(borderWidth), C.int(sideH), &bc[0])
	if bL == nil {
		log.Printf("[DECO] ERROR: scene_rect_create returned nil for left border")
		return
	}
	C.scene_node_set_position(&bL.node, C.int(-borderWidth), C.int(cornerRadius))

	// Right border: x=width, y=cornerRadius
	bR := C.scene_rect_create(parent, C.int(borderWidth), C.int(sideH), &bc[0])
	C.scene_node_set_position(&bR.node, C.int(width), C.int(cornerRadius))

	// Bottom border: narrower to leave room for rounded corner pieces
	bottomW := width + 2*borderWidth - 2*cornerRadius
	bB := C.scene_rect_create(parent, C.int(bottomW), C.int(borderWidth), &bc[0])
	C.scene_node_set_position(&bB.node, C.int(-borderWidth+cornerRadius), C.int(titlebarHeight+height))

	// Top border: not needed separately (titlebar covers the top)
	// But we create a thin rect above titlebar for visual consistency
	bT := C.scene_rect_create(parent, C.int(width+2*borderWidth), 0, &bc[0])
	C.scene_node_set_position(&bT.node, C.int(-borderWidth), 0)
	C.scene_node_set_enabled(&bT.node, 0) // Hidden by default, titlebar serves as top

	return unsafe.Pointer(nil), unsafe.Pointer(nil),
		unsafe.Pointer(bT), unsafe.Pointer(bB), unsafe.Pointer(bL), unsafe.Pointer(bR)
}

// removeDecoNodes destroys all decoration scene nodes for a view.
func (s *server) removeDecoNodes(borderT, borderB, borderL, borderR, titlebar unsafe.Pointer) {
	// Border nodes are wlr_scene_rect
	for _, p := range []unsafe.Pointer{borderT, borderB, borderL, borderR} {
		if p != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_rect)(p).node)
		}
	}
	// Titlebar is wlr_scene_buffer
	if titlebar != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_buffer)(titlebar).node)
	}
}

// removeCornerNodes destroys corner scene buffers and pixel buffers.
func removeCornerNodes(cornerBL, cornerBR, cornerPL, cornerPR *unsafe.Pointer) {
	if *cornerBL != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_buffer)(*cornerBL).node)
		*cornerBL = nil
	}
	if *cornerBR != nil {
		C.scene_node_destroy(&(*C.struct_wlr_scene_buffer)(*cornerBR).node)
		*cornerBR = nil
	}
	// Pixel buffers are owned by the scene buffers and freed automatically
	*cornerPL = nil
	*cornerPR = nil
}

// removeShadowsXway destroys all shadow rects for an XWayland view.
func (s *server) removeShadowsXway(v *xwayView) {
	for i, p := range v.shadowRects {
		if p != nil {
			C.scene_node_destroy(&(*C.struct_wlr_scene_rect)(p).node)
			v.shadowRects[i] = nil
		}
	}
}

// updateDecoBorders updates the border sizes and colors for a decorated view.
func (s *server) updateDecoBorders(v interface{}, width, height int, active bool) {
	var borderBp, borderLp, borderRp unsafe.Pointer
	switch view := v.(type) {
	case *xdgView:
		borderBp = view.decoBorderB
		borderLp = view.decoBorderL
		borderRp = view.decoBorderR
	case *xwayView:
		borderBp = view.decoBorderB
		borderLp = view.decoBorderL
		borderRp = view.decoBorderR
	default:
		return
	}
	if borderLp == nil {
		return
	}

	bColor := s.activeBorderColor(active)
	bc := colorToFloat4(bColor)

	sideH := titlebarHeight + height - cornerRadius

	bL := (*C.struct_wlr_scene_rect)(borderLp)
	C.scene_rect_set_size(bL, C.int(borderWidth), C.int(sideH))
	C.scene_rect_set_color(bL, &bc[0])
	C.scene_node_set_position(&bL.node, C.int(-borderWidth), C.int(cornerRadius))

	bR := (*C.struct_wlr_scene_rect)(borderRp)
	C.scene_rect_set_size(bR, C.int(borderWidth), C.int(sideH))
	C.scene_rect_set_color(bR, &bc[0])
	C.scene_node_set_position(&bR.node, C.int(width), C.int(cornerRadius))

	bottomW := width + 2*borderWidth - 2*cornerRadius
	bB := (*C.struct_wlr_scene_rect)(borderBp)
	C.scene_rect_set_size(bB, C.int(bottomW), C.int(borderWidth))
	C.scene_rect_set_color(bB, &bc[0])
	C.scene_node_set_position(&bB.node, C.int(-borderWidth+cornerRadius), C.int(titlebarHeight+height))
}

// createTitlebarBuffer creates a pixel_buffer + scene_buffer for the titlebar composite.
func (s *server) createTitlebarBuffer(parent *C.struct_wlr_scene_tree, width int, title, appID string, active bool, hoverBtn decoZone) (unsafe.Pointer, unsafe.Pointer) {
	img := s.renderDecoComposite(width, title, 0, active, hoverBtn)
	if img == nil {
		return nil, nil
	}

	pixBuf := C.pixel_buffer_create(C.int(img.Bounds().Dx()), C.int(img.Bounds().Dy()))
	if pixBuf == nil {
		return nil, nil
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(img.Bounds().Dx()), C.int(img.Bounds().Dy()))

	sceneBuf := C.scene_buffer_create(parent, &pixBuf.base)
	// Scale down the 2x image to 1x display size
	C.scene_buffer_set_dest_size(sceneBuf, C.int(width), C.int(titlebarHeight))

	return unsafe.Pointer(sceneBuf), unsafe.Pointer(pixBuf)
}

// updateTitlebarBuffer updates an existing titlebar scene buffer with new content.
func (s *server) updateTitlebarBuffer(sceneBufP, pixBufP unsafe.Pointer, parent *C.struct_wlr_scene_tree, width int, title, appID string, active bool, iconW int, hoverBtn decoZone) (unsafe.Pointer, unsafe.Pointer) {
	img := s.renderDecoComposite(width, title, iconW, active, hoverBtn)
	if img == nil {
		return sceneBufP, pixBufP
	}

	if pixBufP != nil {
		pixBuf := (*C.struct_pixel_buffer)(pixBufP)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(img.Bounds().Dx()), C.int(img.Bounds().Dy()))
		if sceneBufP != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(sceneBufP)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_buffer_set_dest_size(sceneBuf, C.int(width), C.int(titlebarHeight))
		}
		return sceneBufP, pixBufP
	}

	// Create new
	pixBuf := C.pixel_buffer_create(C.int(img.Bounds().Dx()), C.int(img.Bounds().Dy()))
	if pixBuf == nil {
		return nil, nil
	}
	C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(img.Bounds().Dx()), C.int(img.Bounds().Dy()))

	sceneBuf := C.scene_buffer_create(parent, &pixBuf.base)
	C.scene_buffer_set_dest_size(sceneBuf, C.int(width), C.int(titlebarHeight))

	return unsafe.Pointer(sceneBuf), unsafe.Pointer(pixBuf)
}

// updateViewDecorations updates or creates all decoration scene nodes for a view.
func (s *server) updateXdgViewDecorations(v *xdgView) {
	if !v.decorated || v.sceneTree == nil || v.hideDecorations {
		return
	}
	parent := (*C.struct_wlr_scene_tree)(v.sceneTree)

	// Use configured dimensions if available (set by maximize/resize before
	// client commits), falling back to committed surface state.
	width := v.configuredW
	height := v.configuredH
	if width <= 0 || height <= 0 {
		mainSurface := v.xdgToplevel.Base().Surface()
		state := mainSurface.Current()
		width = state.Width()
		height = state.Height()
	}
	active := s.activeXdg == v
	appID := getXdgToplevelAppID(v.xdgToplevel)

	// Icon width for layout
	iconW := 0
	if icon := s.loadAppIcon(appID); icon != nil {
		iconW = icon.w
	}

	// Determine hover button for this view
	hoverBtn := decoNone
	if s.hoverXdg == v {
		hoverBtn = s.hoverButton
	}

	// Update/create titlebar
	v.decoTitlebar, v.decoTitlePix = s.updateTitlebarBuffer(v.decoTitlebar, v.decoTitlePix, parent, width, v.xdgToplevel.Title(), appID, active, iconW, hoverBtn)

	// Update borders
	s.updateDecoBorders(v, width, height, active)

	// Update rounded bottom corners
	s.updateBottomCorners(&v.decoCornerBL, &v.decoCornerBR, &v.decoCornerPL, &v.decoCornerPR, parent, width, height, active)

	// Update icon overlay
	s.updateDecoIcon(v, appID, active, width)

	// Update drop shadow
	s.createOrUpdateShadowsXdg(v, width, height)
}

// updateXwayViewDecorations updates or creates decoration nodes for an XWayland view.
func (s *server) updateXwayViewDecorations(v *xwayView) {
	if !v.decorated || v.sceneTree == nil || v.isPanel || v.isOverlay || v.hideDecorations {
		return
	}
	parent := (*C.struct_wlr_scene_tree)(v.sceneTree)

	// Use XWayland surface dimensions (updated immediately by Configure),
	// not wl_surface committed state which lags behind.
	width := v.surface.Width()
	height := v.surface.Height()
	if width <= 0 || height <= 0 {
		surf := v.surface.Surface()
		if !surf.Valid() {
			return
		}
		state := surf.Current()
		width = state.Width()
		height = state.Height()
	}
	active := s.activeXway == v
	xwayClass := getXwaylandSurfaceClass(v.surface)

	iconW := 0
	if icon := s.loadAppIcon(xwayClass); icon != nil {
		iconW = icon.w
	}

	// Determine hover button for this view
	hoverBtn := decoNone
	if s.hoverXway == v {
		hoverBtn = s.hoverButton
	}

	v.decoTitlebar, v.decoTitlePix = s.updateTitlebarBuffer(v.decoTitlebar, v.decoTitlePix, parent, width, v.surface.Title(), xwayClass, active, iconW, hoverBtn)
	s.updateDecoBorders(v, width, height, active)
	s.updateBottomCorners(&v.decoCornerBL, &v.decoCornerBR, &v.decoCornerPL, &v.decoCornerPR, parent, width, height, active)
	s.updateDecoIcon(v, xwayClass, active, width)

	// Update drop shadow
	s.createOrUpdateShadowsXway(v, width, height)
}

// updateDecoIcon creates or updates the app icon overlay in the titlebar.
func (s *server) updateDecoIcon(v interface{}, appID string, active bool, titlebarWidth int) {
	icon := s.loadAppIcon(appID)
	if icon == nil {
		return
	}

	var iconBufP, iconPixP *unsafe.Pointer
	var parent *C.struct_wlr_scene_tree
	switch view := v.(type) {
	case *xdgView:
		iconBufP = &view.decoIconBuf
		iconPixP = &view.decoIconPix
		parent = (*C.struct_wlr_scene_tree)(view.sceneTree)
	case *xwayView:
		iconBufP = &view.decoIconBuf
		iconPixP = &view.decoIconPix
		parent = (*C.struct_wlr_scene_tree)(view.sceneTree)
	default:
		return
	}

	// Render icon to NRGBA
	iconImg := image.NewNRGBA(image.Rect(0, 0, icon.w, icon.h))
	// Get the icon image from the cache texture — for now use placeholder
	// The icon is already rendered as a wlr.Texture, but for scene we need pixel data.
	// We'll use the icon loading directly from appie.
	iconPath := ""
	if appID != "" {
		iconPath = appie.FdoLookupIconPath("", 48, strings.ToLower(appID))
		if iconPath == "" {
			iconPath = appie.FdoLookupIconPath("", 48, appID)
		}
	}
	if iconPath != "" {
		f, err := os.Open(iconPath)
		if err == nil {
			img, _, err := image.Decode(f)
			f.Close()
			if err == nil {
				draw.BiLinear.Scale(iconImg, iconImg.Bounds(), img, img.Bounds(), draw.Over, nil)
			}
		}
	}

	var textX float64
	if s.buttonsOnLeft {
		textX = 3*float64(buttonSize+buttonMargin) + 8
	} else {
		textX = 8
	}
	iconX := int(textX)
	iconY := (titlebarHeight - icon.h) / 2

	if *iconPixP != nil {
		pixBuf := (*C.struct_pixel_buffer)(*iconPixP)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&iconImg.Pix[0]), C.int(icon.w), C.int(icon.h))
		if *iconBufP != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(*iconBufP)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(iconX), C.int(iconY))
			opacity := C.float(0.5)
			if active {
				opacity = 1.0
			}
			C.scene_buffer_set_opacity(sceneBuf, opacity)
		}
	} else {
		pixBuf := C.pixel_buffer_create(C.int(icon.w), C.int(icon.h))
		if pixBuf == nil {
			return
		}
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&iconImg.Pix[0]), C.int(icon.w), C.int(icon.h))
		sceneBuf := C.scene_buffer_create(parent, &pixBuf.base)
		C.scene_node_set_position(&sceneBuf.node, C.int(iconX), C.int(iconY))
		opacity := C.float(0.5)
		if active {
			opacity = 1.0
		}
		C.scene_buffer_set_opacity(sceneBuf, opacity)
		*iconBufP = unsafe.Pointer(sceneBuf)
		*iconPixP = unsafe.Pointer(pixBuf)
	}
}


// updateBottomCorners creates or updates the rounded bottom corner scene buffers.
func (s *server) updateBottomCorners(
	cornerBL, cornerBR, cornerPL, cornerPR *unsafe.Pointer,
	parent *C.struct_wlr_scene_tree,
	width, height int, active bool,
) {
	bc := color.NRGBAModel.Convert(s.activeBorderColor(active)).(color.NRGBA)

	sc := decoScale
	cornerSize := borderWidth * sc
	destSize := borderWidth // 1x display size
	totalH := titlebarHeight + height

	// Bottom-left corner
	imgBL := renderBottomCorner(bc, false)
	if *cornerPL != nil {
		pixBuf := (*C.struct_pixel_buffer)(*cornerPL)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&imgBL.Pix[0]), C.int(cornerSize), C.int(cornerSize))
		if *cornerBL != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(*cornerBL)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(-borderWidth), C.int(totalH))
			C.scene_buffer_set_dest_size(sceneBuf, C.int(destSize), C.int(destSize))
		}
	} else {
		pixBuf := C.pixel_buffer_create(C.int(cornerSize), C.int(cornerSize))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&imgBL.Pix[0]), C.int(cornerSize), C.int(cornerSize))
			sceneBuf := C.scene_buffer_create(parent, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(-borderWidth), C.int(totalH))
			C.scene_buffer_set_dest_size(sceneBuf, C.int(destSize), C.int(destSize))
			*cornerBL = unsafe.Pointer(sceneBuf)
			*cornerPL = unsafe.Pointer(pixBuf)
		}
	}

	// Bottom-right corner
	imgBR := renderBottomCorner(bc, true)
	if *cornerPR != nil {
		pixBuf := (*C.struct_pixel_buffer)(*cornerPR)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&imgBR.Pix[0]), C.int(cornerSize), C.int(cornerSize))
		if *cornerBR != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(*cornerBR)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(width), C.int(totalH))
			C.scene_buffer_set_dest_size(sceneBuf, C.int(destSize), C.int(destSize))
		}
	} else {
		pixBuf := C.pixel_buffer_create(C.int(cornerSize), C.int(cornerSize))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&imgBR.Pix[0]), C.int(cornerSize), C.int(cornerSize))
			sceneBuf := C.scene_buffer_create(parent, &pixBuf.base)
			C.scene_node_set_position(&sceneBuf.node, C.int(width), C.int(totalH))
			C.scene_buffer_set_dest_size(sceneBuf, C.int(destSize), C.int(destSize))
			*cornerBR = unsafe.Pointer(sceneBuf)
			*cornerPR = unsafe.Pointer(pixBuf)
		}
	}
}

