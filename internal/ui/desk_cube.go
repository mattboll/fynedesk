package ui

import (
	"image"
	"image/color"
	"image/draw"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// newDeskShader builds the hidden full-window cube transition overlay. All
// screens share the same shader source (and therefore the compiled program),
// each driving its own captured desktop faces.
func newDeskShader() *canvas.Shader {
	s := canvas.NewShader("tydeDeskCube", cubeShaderGL, cubeShaderES)
	s.Uniforms = map[string]float32{"progress": 0}
	s.Hide()
	return s
}

// newDeskShaderBG builds the hidden black backdrop drawn behind the cube overlay.
func newDeskShaderBG() *canvas.Rectangle {
	r := canvas.NewRectangle(color.Black)
	r.Hide()
	return r
}

// captureOpaque grabs the canvas content as a fully opaque RGBA image.
//
// Canvas().Capture() reads the GL framebuffer with glReadPixels. The default
// framebuffer's alpha channel is not meaningful and commonly reads back as zero,
// so the raw capture has correct colour but zero alpha. Uploaded as a shader
// texture that samples straight through, the face renders transparent (it looks
// "missing"). Copying into a fresh RGBA and forcing every alpha byte to 255
// yields a guaranteed-opaque snapshot, and the concrete *image.RGBA type also
// takes the painter's fast texture-upload path.
func captureOpaque(c fyne.Canvas) image.Image {
	src := c.Capture()
	if src == nil {
		return nil
	}

	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 0xff
	}
	return out
}

// refreshShaderParent repaints sw's window content so a child that was just
// switched from hidden to visible (a transition shader) is rendered and
// registered against the canvas.
func refreshShaderParent(sw *screenWindow) {
	if sw == nil || sw.win == nil {
		return
	}
	if content := sw.win.Content(); content != nil {
		content.Refresh()
	}
}

// startDeskCube begins the 3D cube transition from desktop old to id. For each
// screen it captures the current desktop as the front face and uses the cached
// snapshot of the target desktop (or the current frame, first time) as the face
// rolling in, then animates the shader's progress uniform. The overlay covers
// the screen, so the window slide driven by SetDesktop happens unseen beneath
// it and the live target desktop is revealed when the roll completes.
func (l *desktop) startDeskCube(old, id int) {
	if l.deskCubeAnim != nil {
		l.deskCubeAnim.Stop()
		l.deskCubeAnim = nil
	}

	var shaders []*canvas.Shader
	var bgs []*canvas.Rectangle
	for _, sw := range l.screenWindows {
		if sw.deskShader == nil || sw.win == nil {
			continue
		}
		if sw.deskSnapshots == nil {
			sw.deskSnapshots = make(map[int]image.Image)
		}

		from := captureOpaque(sw.win.Canvas())
		if from == nil {
			continue
		}
		// A blank capture means GL front-buffer readback isn't working in this
		// environment (e.g. tyde nested in Xephyr via "make embed"). Don't poison the
		// shared snapshot cache - which the desktop overview also reads - with a black
		// frame; synthesise the leaving face instead so the cube isn't black either.
		if captureIsBlank(from) {
			if face := l.synthesizeDeskFace(sw, old, old); face != nil {
				from = face
			}
		} else {
			sw.deskSnapshots[old] = from
		}

		to := sw.deskSnapshots[id]
		if to == nil {
			// Never captured live: synthesise the face from the compositor's
			// window pixmaps over the wallpaper. Falls back to the current frame
			// only if that isn't available.
			if face := l.synthesizeDeskFace(sw, old, id); face != nil {
				to = face
			} else {
				to = from
			}
		}

		// desk0 is the lower-numbered (upper) desktop, desk1 the higher one, so
		// the roll direction matches the vertical window slide.
		if id > old {
			sw.deskShader.Textures = map[string]image.Image{"desk0": from, "desk1": to}
		} else {
			sw.deskShader.Textures = map[string]image.Image{"desk0": to, "desk1": from}
		}
		sw.deskShaderBG.Show()
		sw.deskShader.Show()
		// Force the first paint so the just-shown shader renders
		refreshShaderParent(sw)
		shaders = append(shaders, sw.deskShader)
		bgs = append(bgs, sw.deskShaderBG)
	}

	if len(shaders) == 0 {
		return
	}

	// Going to a higher desktop rolls forward (0->1); going back rolls in reverse.
	start, end := float32(0), float32(1)
	if id < old {
		start, end = 1, 0
	}

	var a *fyne.Animation
	a = fyne.NewAnimation(canvas.DurationStandard, func(f float32) {
		if l.deskCubeAnim != a {
			return // superseded by a newer transition
		}

		p := start + (end-start)*f
		for _, s := range shaders {
			s.Uniforms["progress"] = p
			s.Refresh()
		}

		if f >= 1.0 {
			l.deskCubeAnim = nil
			for _, s := range shaders {
				s.Hide()
			}
			for _, b := range bgs {
				if b != nil {
					b.Hide()
				}
			}
		}
	})
	a.Curve = fyne.AnimationLinear
	l.deskCubeAnim = a
	a.Start()
}

// synthesizeDeskFace builds a best-effort image of desktop id for the cube's
// rolling face when no live capture of it exists yet: that desktop's windows,
// read straight from the compositor, drawn over the shared wallpaper. The bar
// and widget panel are omitted; the real desktop, with full chrome, is revealed
// when the roll completes (and cached for next time, so this is only ever the
// first roll onto a given desktop). Returns nil if the compositor or wallpaper
// pieces aren't available, leaving the caller to fall back to the current frame.
func (l *desktop) synthesizeDeskFace(sw *screenWindow, old, id int) image.Image {
	snap := CompositorWindowSnapshot
	if snap == nil || sw.screen == nil {
		return nil
	}

	_, height := l.RootSizePixels()
	wins := snap(sw.screen, (id-old)*-int(height))
	if wins == nil {
		return nil
	}

	b := wins.Bounds()
	face := renderWallpaper(b.Dx(), b.Dy())
	if face == nil {
		face = image.NewRGBA(b)
	}
	draw.Draw(face, face.Bounds(), wins, b.Min, draw.Over)
	return face
}

// captureIsBlank reports whether a framebuffer capture came back empty - uniformly
// black - when GL front-buffer readback is unsupported (e.g. Xephyr with "make embed")
func captureIsBlank(img image.Image) bool {
	b := img.Bounds()
	if b.Empty() {
		return true
	}

	const steps = 5
	for i := 0; i <= steps; i++ {
		for j := 0; j <= steps; j++ {
			x := b.Min.X + (b.Dx()-1)*i/steps
			y := b.Min.Y + (b.Dy()-1)*j/steps
			if r, g, bl, _ := img.At(x, y).RGBA(); r != 0 || g != 0 || bl != 0 {
				return false
			}
		}
	}
	return true
}
