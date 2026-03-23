package compositor

import (
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/FyshOS/appie"

	"deedles.dev/wlr"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type iconEntry struct {
	texture wlr.Texture
	w, h    int
}

const iconSize = 18 // pixels for titlebar icon

// loadAppIcon loads an app icon from the FDO icon theme and creates a wlr texture
func (s *server) loadAppIcon(appID string) *iconEntry {
	if s.iconCache == nil {
		s.iconCache = make(map[string]*iconEntry)
	}
	if entry, ok := s.iconCache[appID]; ok {
		return entry
	}

	// Look up icon path using FDO standard
	iconPath := appie.FdoLookupIconPath("", 48, strings.ToLower(appID))
	if iconPath == "" {
		// Try with original case
		iconPath = appie.FdoLookupIconPath("", 48, appID)
	}
	if iconPath == "" {
		s.iconCache[appID] = nil
		return nil
	}

	f, err := os.Open(iconPath)
	if err != nil {
		s.iconCache[appID] = nil
		return nil
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		s.iconCache[appID] = nil
		return nil
	}

	// Scale to iconSize x iconSize
	scaled := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	texture := wlr.TextureFromImage(s.renderer, scaled)
	if !texture.Valid() {
		s.iconCache[appID] = nil
		return nil
	}

	entry := &iconEntry{texture: texture, w: iconSize, h: iconSize}
	s.iconCache[appID] = entry
	return entry
}

// Decoration colors — matching Fyne dark theme (InnerWindow style).
// These are mutable so the theme system can override them at runtime.
var (
	titlebarColor       = color.RGBA{R: 0x28, G: 0x29, B: 0x2e, A: 0xff}  // DisabledButton (inactive)
	titlebarActiveColor = color.RGBA{R: 0x18, G: 0x1d, B: 0x25, A: 0xff}  // OverlayBackground (active)
	borderColor         = color.RGBA{R: 0x28, G: 0x29, B: 0x2e, A: 0xff}  // Same as inactive titlebar
	borderActiveColor   = color.RGBA{R: 0x18, G: 0x1d, B: 0x25, A: 0xff}  // Same as active titlebar
	buttonBgColor       = color.RGBA{R: 0x28, G: 0x29, B: 0x2e, A: 0xff}  // Button color (uniform)
	titlebarTextColor   = color.NRGBA{R: 0xf3, G: 0xf3, B: 0xf3, A: 0xff} // Foreground text

	// High contrast borders — bright outlines for WCAG AA focus visibility
	borderHighContrastActive   = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff} // Bright white for focused
	borderHighContrastInactive = color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff} // Medium gray for unfocused
)

// activeBorderColor returns the border color for an active/inactive window,
// taking into account the high contrast accessibility setting.
func (s *server) activeBorderColor(active bool) color.RGBA {
	if s.highContrast {
		if active {
			return borderHighContrastActive
		}
		return borderHighContrastInactive
	}
	if active {
		return borderActiveColor
	}
	return borderColor
}

// renderDecoComposite builds the entire titlebar as a single 2x NRGBA image
// (background with rounded top corners + buttons + title text).
// The GPU bilinear-filters it to 1x when rendering, giving natural anti-aliasing.
// hoverBtn indicates which button (if any) should have a glow effect.
func (s *server) renderDecoComposite(width1x int, title string, iconW1x int, active bool, hoverBtn decoZone) *image.NRGBA {
	sc := decoScale
	w := width1x * sc
	h := titlebarHeight * sc
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	// Background with rounded top corners
	bg := titlebarColor
	if active {
		bg = titlebarActiveColor
	}
	bgN := color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: bg.A}
	fillRoundedTopBg(img, bgN, float64(cornerRadius*sc))

	// Buttons at 2x
	btnSize := buttonSize * sc
	btnMargin := buttonMargin * sc
	btnY := (h - btnSize) / 2

	var closeX, maxX, minX int
	var buttonsEndX, buttonsStartX int // text-free zone occupied by buttons
	if s.buttonsOnLeft {
		closeX = btnMargin
		maxX = closeX + btnSize + btnMargin
		minX = maxX + btnSize + btnMargin
		buttonsEndX = 3*(btnSize+btnMargin) + 4*sc
		buttonsStartX = 0
	} else {
		closeX = w - btnSize - btnMargin
		maxX = closeX - btnSize - btnMargin
		minX = maxX - btnSize - btnMargin
		buttonsStartX = minX - btnMargin
		buttonsEndX = w
	}

	drawButtonOn(img, closeX, btnY, btnSize, buttonBgColor, "close", active, hoverBtn == decoCloseButton)
	drawButtonOn(img, maxX, btnY, btnSize, buttonBgColor, "max", active, hoverBtn == decoMaxButton)
	drawButtonOn(img, minX, btnY, btnSize, buttonBgColor, "min", active, hoverBtn == decoMinButton)

	// Title text at 2x DPI — centered in the available space
	if title != "" {
		face := getTitleFontFace2x()
		if face != nil {
			iconOffset := iconW1x * sc
			if iconOffset > 0 {
				iconOffset += 4 * sc
			}

			// Available text region: between buttons and the opposite edge
			var textRegionLeft, textRegionRight int
			if s.buttonsOnLeft {
				textRegionLeft = buttonsEndX + iconOffset
				textRegionRight = w - 8*sc
			} else {
				textRegionLeft = 8*sc + iconOffset
				textRegionRight = buttonsStartX
			}
			maxTextW := textRegionRight - textRegionLeft
			if maxTextW > 0 {
				// Measure text width to center it within the region
				d := &font.Drawer{Face: face}
				displayTitle := truncateTitle(d, title, maxTextW)
				if displayTitle != "" {
					textW := d.MeasureString(displayTitle).Ceil()
					textX := textRegionLeft + (maxTextW-textW)/2
					drawTitleOn(img, face, displayTitle, textX, h, maxTextW, active)
				}
			}
		}
	}

	return img
}

// truncateTitle truncates title to fit within maxWidth, adding "..." if needed.
func truncateTitle(d *font.Drawer, title string, maxWidth int) string {
	for len(title) > 0 {
		adv := d.MeasureString(title)
		if adv.Ceil() <= maxWidth {
			return title
		}
		if len(title) > 3 {
			title = title[:len(title)-4] + "..."
		} else {
			title = title[:len(title)-1]
		}
	}
	return ""
}

// fillRoundedTopBg fills img with bg color and masks top-left/top-right corners with AA
func fillRoundedTopBg(img *image.NRGBA, bg color.NRGBA, cornerR float64) {
	w := img.Bounds().Dx()

	// Fast fill entire image with bg color
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = bg.R
		img.Pix[i+1] = bg.G
		img.Pix[i+2] = bg.B
		img.Pix[i+3] = bg.A
	}

	// Mask top-left and top-right corners
	ri := int(math.Ceil(cornerR)) + 1
	for y := 0; y < ri; y++ {
		for x := 0; x < ri; x++ {
			dx := cornerR - float64(x) - 0.5
			dy := cornerR - float64(y) - 0.5
			if dx <= 0 || dy <= 0 {
				continue
			}
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > cornerR+0.5 {
				img.SetNRGBA(x, y, color.NRGBA{})
			} else if dist > cornerR-0.5 {
				a := cornerR + 0.5 - dist
				img.SetNRGBA(x, y, color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: uint8(float64(bg.A) * a)})
			}
		}
		for x := w - ri; x < w; x++ {
			dx := float64(x) + 0.5 - (float64(w) - cornerR)
			dy := cornerR - float64(y) - 0.5
			if dx <= 0 || dy <= 0 {
				continue
			}
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > cornerR+0.5 {
				img.SetNRGBA(x, y, color.NRGBA{})
			} else if dist > cornerR-0.5 {
				a := cornerR + 0.5 - dist
				img.SetNRGBA(x, y, color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: uint8(float64(bg.A) * a)})
			}
		}
	}
}

// renderBottomCorner renders a bottom corner (borderWidth × borderWidth) with a quarter-circle.
// isRight=false → bottom-left (quarter circle in bottom-left), isRight=true → bottom-right.
// Rendered at decoScale (2x) for anti-aliased edges.
func renderBottomCorner(bc color.NRGBA, isRight bool) *image.NRGBA {
	sc := decoScale
	size := borderWidth * sc
	r := float64(cornerRadius * sc)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Center of the quarter circle is at:
			// bottom-left: (size, 0)  — top-right of the piece
			// bottom-right: (0, 0)    — top-left of the piece
			var cx, cy float64
			if isRight {
				cx, cy = 0, 0
			} else {
				cx, cy = float64(size), 0
			}
			px, py := float64(x)+0.5, float64(y)+0.5
			dx, dy := px-cx, py-cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < r-0.5 {
				img.SetNRGBA(x, y, bc)
			} else if dist < r+0.5 {
				a := r + 0.5 - dist
				img.SetNRGBA(x, y, color.NRGBA{R: bc.R, G: bc.G, B: bc.B, A: uint8(float64(bc.A) * a)})
			}
			// else: transparent (default)
		}
	}
	return img
}

// drawButtonOn draws a circular button with symbol directly onto the composite image at (ox, oy).
// When hovered is true, the button gets a lighter background glow for visual feedback.
func drawButtonOn(img *image.NRGBA, ox, oy, size int, col color.RGBA, style string, active bool, hovered bool) {
	c := float64(size) / 2
	r := c - 0.5

	// Button background color — distinctive colors on hover for accessibility (WCAG AA)
	btnCol := col
	if hovered && active {
		switch style {
		case "close":
			btnCol = color.RGBA{R: 0xe0, G: 0x40, B: 0x40, A: 0xff} // Red
		case "max":
			btnCol = color.RGBA{R: 0x40, G: 0xb0, B: 0x40, A: 0xff} // Green
		case "min":
			btnCol = color.RGBA{R: 0xe0, G: 0xb0, B: 0x30, A: 0xff} // Yellow
		}
	} else if active {
		// Subtle distinctive colors when active but not hovered
		switch style {
		case "close":
			btnCol = color.RGBA{R: 0x60, G: 0x30, B: 0x30, A: 0xff}
		case "max":
			btnCol = color.RGBA{R: 0x30, G: 0x50, B: 0x30, A: 0xff}
		case "min":
			btnCol = color.RGBA{R: 0x55, G: 0x50, B: 0x28, A: 0xff}
		}
	}

	symCol := titlebarTextColor
	if !active {
		symCol = color.NRGBA{R: titlebarTextColor.R, G: titlebarTextColor.G, B: titlebarTextColor.B, A: 0xbb}
	}
	margin := float64(size) * 0.28
	lineHW := 1.2 // line half-width at 2x for visible symbols

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			dx, dy := px-c, py-c
			dist := math.Sqrt(dx*dx + dy*dy)

			if dist > r+0.5 {
				continue
			}

			circleA := 1.0
			if dist > r-0.5 {
				circleA = r + 0.5 - dist
			}

			// Symbol distance
			var symDist float64 = 1000
			x1, y1 := margin, margin
			x2, y2 := float64(size)-margin, float64(size)-margin

			switch style {
			case "close":
				symDist = math.Min(
					distToSegment(px, py, x1, y1, x2, y2),
					distToSegment(px, py, x2, y1, x1, y2),
				)
			case "max":
				symDist = math.Min(
					math.Min(
						distToSegment(px, py, x1, y1, x2, y1),
						distToSegment(px, py, x1, y2, x2, y2),
					),
					math.Min(
						distToSegment(px, py, x1, y1, x1, y2),
						distToSegment(px, py, x2, y1, x2, y2),
					),
				)
			case "min":
				mid := float64(size) / 2
				symDist = distToSegment(px, py, x1, mid, x2, mid)
			}

			symA := 0.0
			if symDist < lineHW+0.5 {
				symA = 1.0
				if symDist > lineHW-0.5 {
					symA = lineHW + 0.5 - symDist
				}
			}

			// Composite symbol over button color
			effSym := symA * float64(symCol.A) / 255
			finalR := float64(symCol.R)*effSym + float64(btnCol.R)*(1-effSym)
			finalG := float64(symCol.G)*effSym + float64(btnCol.G)*(1-effSym)
			finalB := float64(symCol.B)*effSym + float64(btnCol.B)*(1-effSym)

			// Alpha-composite button pixel over existing background
			srcA := circleA
			bgPx := img.NRGBAAt(ox+x, oy+y)
			dstA := float64(bgPx.A) / 255
			outA := srcA + dstA*(1-srcA)
			if outA > 0 {
				outR := (finalR*srcA + float64(bgPx.R)*dstA*(1-srcA)) / outA
				outG := (finalG*srcA + float64(bgPx.G)*dstA*(1-srcA)) / outA
				outB := (finalB*srcA + float64(bgPx.B)*dstA*(1-srcA)) / outA
				img.SetNRGBA(ox+x, oy+y, color.NRGBA{
					R: uint8(math.Min(outR, 255)),
					G: uint8(math.Min(outG, 255)),
					B: uint8(math.Min(outB, 255)),
					A: uint8(math.Min(outA*255, 255)),
				})
			}
		}
	}
}

// drawTitleOn renders title text directly onto the composite image at 2x DPI.
// The title should already be truncated (use truncateTitle).
func drawTitleOn(img *image.NRGBA, face font.Face, title string, textX, height, maxWidth int, active bool) {
	textColor := titlebarTextColor
	if !active {
		textColor = color.NRGBA{R: titlebarTextColor.R, G: titlebarTextColor.G, B: titlebarTextColor.B, A: 0x99}
	}

	if len(title) == 0 {
		return
	}

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	textH := ascent + descent
	baselineY := (height-textH)/2 + ascent

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.P(textX, baselineY),
	}
	d.DrawString(title)
}

// titleFontFace2x is the cached 2x DPI font face for decoration text
var titleFontFace2x font.Face

// getTitleFontFace2x loads a medium/bold font at 2x DPI for HiDPI decoration rendering.
// Prefers semi-bold/bold variants for better readability against dark titlebars.
func getTitleFontFace2x() font.Face {
	if titleFontFace2x != nil {
		return titleFontFace2x
	}

	sz := customFontSize
	if sz < 8 {
		sz = 13
	}

	fontPaths := []string{}
	if customFontPath != "" {
		fontPaths = append(fontPaths, customFontPath)
	}
	fontPaths = append(fontPaths,
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-SemiBold.ttf",
		"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
		"/usr/local/share/fonts/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/local/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/local/share/fonts/Liberation/LiberationSans-Regular.ttf",
		"/usr/local/share/fonts/noto/NotoSans-Regular.ttf",
	)

	for _, p := range fontPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, err := opentype.Parse(data)
		if err != nil {
			continue
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    sz,
			DPI:     96 * decoScale,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		titleFontFace2x = face
		return face
	}

	return nil
}

// distToSegment returns the distance from point (px,py) to line segment (x1,y1)-(x2,y2)
func distToSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		ddx, ddy := px-x1, py-y1
		return math.Sqrt(ddx*ddx + ddy*ddy)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx, cy := x1+t*dx, y1+t*dy
	ddx, ddy := px-cx, py-cy
	return math.Sqrt(ddx*ddx + ddy*ddy)
}

// customFontPath is set from settings; cleared on settings reload.
var customFontPath string

// customFontSize is the user-configured font size (0 = use default 13).
var customFontSize float64

// resolveFontPath uses fc-match to resolve a font family name to a file path.
func resolveFontPath(family string) string {
	if family == "" {
		return ""
	}
	out, err := exec.Command("fc-match", "--format=%{file}", family).Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// invalidateFontCaches clears cached font faces so they reload with new settings.
func invalidateFontCaches() {
	titleFontFace = nil
	titleFontFace2x = nil
}

// titleFontFace is the cached font face for titlebar text
var titleFontFace font.Face

// getTitleFontFace loads the titlebar font, preferring the user-configured font.
func getTitleFontFace() font.Face {
	if titleFontFace != nil {
		return titleFontFace
	}

	sz := customFontSize
	if sz < 8 {
		sz = 13
	}

	fontPaths := []string{}
	if customFontPath != "" {
		fontPaths = append(fontPaths, customFontPath)
	}
	fontPaths = append(fontPaths,
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/local/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/local/share/fonts/Liberation/LiberationSans-Regular.ttf",
		"/usr/local/share/fonts/noto/NotoSans-Regular.ttf",
	)

	for _, p := range fontPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		f, err := opentype.Parse(data)
		if err != nil {
			continue
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    sz,
			DPI:     96,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		titleFontFace = face
		return face
	}

	return nil
}
