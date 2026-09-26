package compositor

/*
#include <wlr/types/wlr_scene.h>
#include "pixel_buffer.h"
*/
import "C"

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/FyshOS/appie"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// updateSwitcherScene renders the switcher overlay as a composite NRGBA image
// and updates the scene buffer in switcherTree. Call this when the switcher is
// activated, when the selection changes, or when switching is confirmed/cancelled.
func (s *server) updateSwitcherScene() {
	if !s.switcherActive || len(s.switcherWindows) == 0 {
		// Hide the switcher tree
		setViewSceneEnabled(s.switcherTree, false)
		return
	}

	// Render switcher on the active output (where cursor is)
	outGeo := s.getActiveOutputGeo()
	screenW := outGeo.width
	screenH := outGeo.height
	screenX := outGeo.x
	screenY := outGeo.y

	// --- Layout constants ---
	const (
		gap        = 16
		padOuter   = 24
		textHeight = 22
		iconSz     = switcherIconSize
		selectBdr  = 3
		maxCols    = 6
	)

	n := len(s.switcherWindows)
	cols := n
	if cols > maxCols {
		cols = maxCols
	}
	rows := (n + cols - 1) / cols

	cellW := thumbMaxW
	cellH := thumbMaxH + textHeight

	panelW := cols*cellW + (cols-1)*gap + padOuter*2
	panelH := rows*cellH + (rows-1)*gap + padOuter*2

	if panelW > screenW-40 {
		panelW = screenW - 40
	}
	if panelH > screenH-40 {
		panelH = screenH - 40
	}

	panelX := (screenW - panelW) / 2
	panelY := (screenH - panelH) / 2

	// Create full-screen NRGBA image for the composite
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))

	// --- Screen dimming ---
	dimColor := color.NRGBA{R: 0, G: 0, B: 0, A: 120}
	draw.Draw(img, img.Bounds(), image.NewUniform(dimColor), image.Point{}, draw.Over)

	// --- Panel background ---
	bgColor := color.NRGBA{R: 30, G: 30, B: 35, A: 210}
	bgRect := image.Rect(panelX, panelY, panelX+panelW, panelY+panelH)
	draw.Draw(img, bgRect, image.NewUniform(bgColor), image.Point{}, draw.Over)

	// --- Selection highlight and window entries ---
	hlColor := color.NRGBA{R: 90, G: 160, B: 255, A: 220}
	placeholderColor := color.NRGBA{R: 50, G: 50, B: 55, A: 200}

	face := getTitleFontFace()

	for i, w := range s.switcherWindows {
		row := i / cols
		col := i % cols

		itemsInRow := cols
		if row == rows-1 {
			itemsInRow = n - row*cols
		}
		rowOffsetX := 0
		if itemsInRow < cols {
			rowOffsetX = (cols - itemsInRow) * (cellW + gap) / 2
		}

		cx := panelX + padOuter + rowOffsetX + col*(cellW+gap)
		cy := panelY + padOuter + row*(cellH+gap)

		// --- Get captured thumbnail image ---
		var thumbImg *image.NRGBA
		if i < len(s.switcherThumbImgs) {
			thumbImg = s.switcherThumbImgs[i]
		}
		tw, th := thumbMaxW, thumbMaxH
		if thumbImg != nil {
			tw = thumbImg.Bounds().Dx()
			th = thumbImg.Bounds().Dy()
		}

		thumbX := cx + (cellW-tw)/2
		thumbY := cy + (thumbMaxH-th)/2

		// --- Selection highlight border ---
		if i == s.switcherIndex {
			bx0, by0 := thumbX-selectBdr, thumbY-selectBdr
			bx1, by1 := thumbX+tw+selectBdr, thumbY+th+selectBdr
			// top
			draw.Draw(img, image.Rect(bx0, by0, bx1, by0+selectBdr), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// bottom
			draw.Draw(img, image.Rect(bx0, by1-selectBdr, bx1, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// left
			draw.Draw(img, image.Rect(bx0, by0, bx0+selectBdr, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
			// right
			draw.Draw(img, image.Rect(bx1-selectBdr, by0, bx1, by1), image.NewUniform(hlColor), image.Point{}, draw.Over)
		}

		// --- Draw thumbnail or placeholder ---
		if thumbImg != nil {
			dstRect := image.Rect(thumbX, thumbY, thumbX+tw, thumbY+th)
			draw.Draw(img, dstRect, thumbImg, thumbImg.Bounds().Min, draw.Over)
		} else {
			draw.Draw(img, image.Rect(thumbX, thumbY, thumbX+tw, thumbY+th), image.NewUniform(placeholderColor), image.Point{}, draw.Over)
		}

		// --- App icon overlay (bottom-right of thumbnail) ---
		appID := s.switcherWindowAppID(w)
		iconImg := s.loadSwitcherIconImage(appID)
		if iconImg != nil {
			iconX := thumbX + tw - iconSz - 4
			iconY := thumbY + th - iconSz - 4
			iconRect := image.Rect(iconX, iconY, iconX+iconSz, iconY+iconSz)
			draw.Draw(img, iconRect, iconImg, iconImg.Bounds().Min, draw.Over)
		}

		// --- Title text below thumbnail ---
		if face != nil {
			title := s.switcherDisplayTitle(w)
			if title != "" {
				var txtColor color.NRGBA
				if i == s.switcherIndex {
					txtColor = color.NRGBA{R: 240, G: 240, B: 245, A: 255}
				} else {
					txtColor = color.NRGBA{R: 150, G: 150, B: 155, A: 255}
				}
				s.drawTextOnImage(img, title, cx, cy+thumbMaxH, cellW, textHeight, txtColor, face)
			}
		}
	}

	// --- Update or create scene buffer ---
	if s.switcherPixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(s.switcherPixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
		if s.switcherBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
			C.wlr_scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.wlr_scene_buffer_set_dest_size(sceneBuf, C.int(screenW), C.int(screenH))
			C.wlr_scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
		}
	} else if s.switcherTree != nil {
		pixBuf := C.pixel_buffer_create(C.int(screenW), C.int(screenH))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
			swTree := (*C.struct_wlr_scene_tree)(s.switcherTree)
			sceneBuf := C.wlr_scene_buffer_create(swTree, &pixBuf.base)
			C.wlr_scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
			s.switcherBuf = unsafe.Pointer(sceneBuf)
			s.switcherPixBuf = unsafe.Pointer(pixBuf)
		}
	}

	// Enable the switcher tree
	setViewSceneEnabled(s.switcherTree, true)

	// Apply fade opacity if animation is active
	if s.switcherBuf != nil && (s.switcherFadeIn || s.switcherFadeOut) {
		opacity := s.switcherFadeOpacity()
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
		C.wlr_scene_buffer_set_opacity(sceneBuf, C.float(opacity))
	}
}

const switcherFadeDuration = 150 * time.Millisecond

// switcherFadeOpacity returns the current opacity [0.0, 1.0] based on fade state.
func (s *server) switcherFadeOpacity() float32 {
	elapsed := time.Since(s.switcherFadeStart)
	t := float32(elapsed) / float32(switcherFadeDuration)
	if t > 1 {
		t = 1
	}
	if s.switcherFadeOut {
		return 1 - t
	}
	return t
}

// tickSwitcherFade advances the switcher fade animation. Called from the frame callback.
// Returns true if the animation is still active and needs more frames.
func (s *server) tickSwitcherFade() bool {
	if !s.switcherFadeIn && !s.switcherFadeOut {
		return false
	}

	elapsed := time.Since(s.switcherFadeStart)
	if elapsed >= switcherFadeDuration {
		if s.switcherFadeIn {
			// Fade-in complete: set full opacity
			s.switcherFadeIn = false
			if s.switcherBuf != nil {
				sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
				C.wlr_scene_buffer_set_opacity(sceneBuf, 1.0)
			}
			return false
		}
		if s.switcherFadeOut {
			// Fade-out complete: finalize close
			s.switcherFadeOut = false
			s.finishSwitcherClose()
			return false
		}
	}

	// Update opacity
	if s.switcherBuf != nil {
		opacity := s.switcherFadeOpacity()
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.switcherBuf)
		C.wlr_scene_buffer_set_opacity(sceneBuf, C.float(opacity))
	}
	return true
}

// drawTextOnImage draws truncated text centered in a cell on the composite image.
func (s *server) drawTextOnImage(img *image.NRGBA, title string, cx, cy, maxWidth, height int, textColor color.NRGBA, face font.Face) {
	d := &font.Drawer{Face: face}
	for len(title) > 0 {
		adv := d.MeasureString(title)
		if adv.Ceil() <= maxWidth {
			break
		}
		if len(title) > 3 {
			title = title[:len(title)-4] + "..."
		} else {
			title = title[:len(title)-1]
		}
	}
	if len(title) == 0 {
		return
	}

	textWidth := d.MeasureString(title).Ceil()
	if textWidth <= 0 {
		return
	}

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	textH := ascent + descent
	baselineY := cy + (height-textH)/2 + ascent

	textX := cx + (maxWidth-textWidth)/2

	d.Dst = img
	d.Src = image.NewUniform(textColor)
	d.Dot = fixed.P(textX, baselineY)
	d.DrawString(title)
}

// loadSwitcherIconImage returns a switcher icon as an NRGBA image (for
// compositing), or nil.
func (s *server) loadSwitcherIconImage(appID string) *image.NRGBA {
	if appID == "" {
		return nil
	}
	return s.scaledAppIcon(switcherIconSize, appID, strings.ToLower(appID))
}

// scaledAppIcon returns the icon of the first of names the icon theme has,
// scaled to size, or nil. Icons are read and scaled once, then kept: the
// switcher and the open animation asked for them on every redraw. The image
// is shared: read it, never draw on it.
func (s *server) scaledAppIcon(size int, names ...string) *image.NRGBA {
	key := fmt.Sprintf("%d:%s", size, strings.Join(names, "|"))
	if img, ok := s.scaledIcons[key]; ok {
		return img
	}
	if s.scaledIcons == nil {
		s.scaledIcons = map[string]*image.NRGBA{}
	}
	var scaled *image.NRGBA
	for _, name := range names {
		path := appie.FdoLookupIconPath("", size, name)
		if path == "" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		decoded, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		scaled = image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.BiLinear.Scale(scaled, scaled.Bounds(), decoded, decoded.Bounds(), draw.Over, nil)
		break
	}
	s.scaledIcons[key] = scaled
	return scaled
}
