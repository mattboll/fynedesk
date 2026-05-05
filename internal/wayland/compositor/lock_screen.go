package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#cgo LDFLAGS: -lpam
#include <stdlib.h>
#include <string.h>
#include <security/pam_appl.h>
#include <wlr/types/wlr_scene.h>
#include <wlr/interfaces/wlr_buffer.h>
#include <drm_fourcc.h>

// pixel_buffer for lock screen rendering
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

static struct wlr_scene_buffer *scene_buffer_create(struct wlr_scene_tree *parent, struct wlr_buffer *buffer) {
	return wlr_scene_buffer_create(parent, buffer);
}
static void scene_buffer_set_buffer(struct wlr_scene_buffer *buf, struct wlr_buffer *buffer) {
	wlr_scene_buffer_set_buffer(buf, buffer);
}
static void scene_buffer_set_dest_size(struct wlr_scene_buffer *buf, int w, int h) {
	wlr_scene_buffer_set_dest_size(buf, w, h);
}
static void scene_node_set_position(struct wlr_scene_node *node, int x, int y) {
	wlr_scene_node_set_position(node, x, y);
}
static void scene_node_destroy(struct wlr_scene_node *node) {
	wlr_scene_node_destroy(node);
}
static void scene_node_set_enabled_lock(struct wlr_scene_node *node, int enabled) {
	wlr_scene_node_set_enabled(node, enabled);
}

// PAM conversation callback — returns the stored password.
struct pam_conv_data {
	const char *password;
};

static int pam_conv_func(int num_msg, const struct pam_message **msg,
		struct pam_response **resp, void *appdata_ptr) {
	struct pam_conv_data *data = (struct pam_conv_data *)appdata_ptr;
	struct pam_response *reply = calloc(num_msg, sizeof(struct pam_response));
	if (!reply) return PAM_CONV_ERR;
	for (int i = 0; i < num_msg; i++) {
		if (msg[i]->msg_style == PAM_PROMPT_ECHO_OFF ||
			msg[i]->msg_style == PAM_PROMPT_ECHO_ON) {
			reply[i].resp = strdup(data->password);
		}
	}
	*resp = reply;
	return PAM_SUCCESS;
}

// pam_auth authenticates the given username/password. Returns 0 on success.
static int pam_auth(const char *username, const char *password) {
	struct pam_conv_data conv_data;
	conv_data.password = password;
	struct pam_conv conv = {
		.conv = pam_conv_func,
		.appdata_ptr = &conv_data,
	};
	pam_handle_t *pamh = NULL;
	// Use "swaylock" PAM service (auth-only, no account mgmt that requires root).
	// Falls back to "login" if "swaylock" is not available.
	int ret = pam_start("swaylock", username, &conv, &pamh);
	if (ret != PAM_SUCCESS) {
		ret = pam_start("login", username, &conv, &pamh);
		if (ret != PAM_SUCCESS) return ret;
	}
	ret = pam_authenticate(pamh, 0);
	pam_end(pamh, ret);
	return ret;
}
*/
import "C"

import (
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"os/user"
	"strings"
	"time"
	"unsafe"

	"deedles.dev/wlr/xkb"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// builtinLockState holds the state for the built-in lock screen.
type builtinLockState struct {
	active      bool
	pixBuf      unsafe.Pointer // *C.pixel_buffer
	sceneBuf    unsafe.Pointer // *C.wlr_scene_buffer
	password    []rune
	errorMsg    string
	showError   bool
	userName    string
	clockTicker *time.Ticker
	clockDone   chan struct{} // closed by deactivateBuiltinLock to terminate the clock goroutine
	blurredBg   *image.NRGBA
}

// zeroPassword overwrites a password rune slice with zeros before discarding.
func zeroPassword(pw []rune) {
	for i := range pw {
		pw[i] = 0
	}
}

// Cached font faces for lock screen rendering
var (
	lockClockFace font.Face // 72pt bold for clock
	lockDateFace  font.Face // 18pt regular for date
	lockTextFace  font.Face // 14pt regular for username, status, label
)

// getLockClockFace returns a cached 72pt bold font face for the clock display.
func getLockClockFace() font.Face {
	if lockClockFace != nil {
		return lockClockFace
	}
	lockClockFace = loadFontFace(72, true)
	return lockClockFace
}

// getLockDateFace returns a cached 18pt regular font face for the date line.
func getLockDateFace() font.Face {
	if lockDateFace != nil {
		return lockDateFace
	}
	lockDateFace = loadFontFace(18, false)
	return lockDateFace
}

// getLockTextFace returns a cached 14pt regular font face for text elements.
func getLockTextFace() font.Face {
	if lockTextFace != nil {
		return lockTextFace
	}
	lockTextFace = loadFontFace(14, false)
	return lockTextFace
}

// loadFontFace loads a font at the given point size and weight.
func loadFontFace(size float64, bold bool) font.Face {
	var fontPaths []string
	if bold {
		fontPaths = []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
			"/usr/share/fonts/truetype/noto/NotoSans-SemiBold.ttf",
			"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
			"/usr/local/share/fonts/dejavu/DejaVuSans-Bold.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}
	} else {
		fontPaths = []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/TTF/DejaVuSans.ttf",
			"/usr/local/share/fonts/dejavu/DejaVuSans.ttf",
		}
	}

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
			Size:    size,
			DPI:     96,
			Hinting: font.HintingFull,
		})
		if err != nil {
			continue
		}
		return face
	}
	return nil
}

// activateBuiltinLock creates the built-in lock screen overlay.
// Must be called from the main thread (via mainThreadActions).
func (s *server) activateBuiltinLock() {
	if s.builtinLock != nil && s.builtinLock.active {
		return
	}

	// Get current user
	userName := "User"
	if u, err := user.Current(); err == nil {
		if u.Name != "" {
			userName = u.Name
		} else {
			userName = u.Username
		}
	}

	s.builtinLock = &builtinLockState{
		active:   true,
		userName: userName,
	}

	// Create blurred wallpaper background
	s.builtinLock.blurredBg = s.createLockBackground()

	// Set locked state
	s.locked = true
	s.suspendLockPending = false // Lock acquired
	s.markLocked()

	// Enable lock tree
	if s.lockTree != nil {
		lockTree := (*C.struct_wlr_scene_tree)(s.lockTree)
		C.scene_node_set_enabled_lock(&lockTree.node, 1)
	}

	// Cancel switcher if active
	if s.switcherActive {
		s.cancelSwitcher()
	}
	// Close overlay if open
	if s.overlayXway != nil {
		s.closeOverlay()
	}

	// Render initial frame
	s.updateBuiltinLockScene()

	// Start clock ticker (update every minute).
	// Capture the ticker and done channel locally so the goroutine doesn't
	// race on s.builtinLock (which gets nilled by deactivateBuiltinLock).
	ticker := time.NewTicker(30 * time.Second)
	done := make(chan struct{})
	s.builtinLock.clockTicker = ticker
	s.builtinLock.clockDone = done
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			select {
			case s.mainThreadActions <- func() {
				// Guard inside the main-thread callback: builtinLock may have
				// been nilled between the tick and this callback executing.
				s.updateBuiltinLockScene() // already checks builtinLock == nil
			}:
				s.triggerWakeup()
			case <-done:
				return
			}
		}
	}()

	log.Println("[LOCK] Built-in lock screen activated")
}

// createLockBackground generates a darkened wallpaper for the lock screen.
// Spans all outputs so the lock background covers every monitor.
func (s *server) createLockBackground() *image.NRGBA {
	_, _, w, h := s.fullLayoutBounds()
	if w <= 0 || h <= 0 {
		return nil
	}

	// Try to load the wallpaper image from preferences
	var srcImg image.Image
	prefs, _, err := s.readPrefs()
	if err == nil {
		if bgPath, ok := prefs["background"].(string); ok && bgPath != "" {
			if f, err := os.Open(bgPath); err == nil {
				if img, _, err := image.Decode(f); err == nil {
					srcImg = img
				}
				f.Close()
			}
		}
	}

	// Scale source to output size
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	if srcImg != nil {
		draw.ApproxBiLinear.Scale(nrgba, nrgba.Bounds(), srcImg, srcImg.Bounds(), draw.Src, nil)
	} else {
		// Solid dark background
		for i := 0; i < len(nrgba.Pix); i += 4 {
			nrgba.Pix[i] = 0x1e
			nrgba.Pix[i+1] = 0x1e
			nrgba.Pix[i+2] = 0x1e
			nrgba.Pix[i+3] = 0xff
		}
	}

	// Darken wallpaper for lock screen contrast
	pix := nrgba.Pix
	for i := 0; i < len(pix); i += 4 {
		pix[i] = uint8(uint16(pix[i]) * 140 >> 8)     // R *= 0.55
		pix[i+1] = uint8(uint16(pix[i+1]) * 140 >> 8) // G *= 0.55
		pix[i+2] = uint8(uint16(pix[i+2]) * 140 >> 8) // B *= 0.55
	}

	return nrgba
}

// updateBuiltinLockScene composites the lock screen as a full-screen NRGBA image
// and updates the scene buffer in lockTree.
func (s *server) updateBuiltinLockScene() {
	if s.builtinLock == nil || !s.builtinLock.active {
		return
	}

	// Span the lock screen across all outputs so no monitor is left uncovered.
	screenX, screenY, screenW, screenH := s.fullLayoutBounds()
	if screenW <= 0 || screenH <= 0 {
		return
	}

	// Start with blurred background or solid dark
	img := image.NewNRGBA(image.Rect(0, 0, screenW, screenH))
	if s.builtinLock.blurredBg != nil {
		draw.Draw(img, img.Bounds(), s.builtinLock.blurredBg, image.Point{}, draw.Src)
	} else {
		darkBg := color.NRGBA{R: 0x1a, G: 0x1a, B: 0x20, A: 0xff}
		draw.Draw(img, img.Bounds(), image.NewUniform(darkBg), image.Point{}, draw.Src)
	}

	now := time.Now()
	centerX := screenW / 2

	// --- Clock (upper third) ---
	clockFace := getLockClockFace()
	if clockFace != nil {
		clockStr := now.Format("15:04")
		clockY := screenH / 4
		drawCenteredText(img, clockFace, clockStr, centerX, clockY,
			color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	}

	// --- Date line below clock ---
	dateFace := getLockDateFace()
	if dateFace != nil {
		dateStr := now.Format("Monday, January 2")
		dateY := screenH/4 + 50
		if clockFace != nil {
			dateY = screenH/4 + clockFace.Metrics().Height.Ceil()/2 + 16
		}
		drawCenteredText(img, dateFace, dateStr, centerX, dateY,
			color.NRGBA{R: 0xcc, G: 0xcc, B: 0xcc, A: 0xff})
	}

	// --- User avatar circle (centered) ---
	avatarR := lockAvatarRadius
	avatarCX := centerX
	avatarCY := screenH/2 - 20
	avatarBg := color.NRGBA{R: 0x44, G: 0x66, B: 0x99, A: 0xff}
	drawFilledCircle(img, avatarCX, avatarCY, avatarR, avatarBg)

	// Initials inside avatar
	textFace := getLockTextFace()
	if textFace != nil {
		initials := getInitials(s.builtinLock.userName)
		drawCenteredText(img, textFace, initials, avatarCX, avatarCY,
			color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	}

	// --- Username below avatar ---
	if dateFace != nil {
		nameY := avatarCY + avatarR + 24
		drawCenteredText(img, dateFace, s.builtinLock.userName, centerX, nameY,
			color.NRGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff})
	}

	// --- Password dots ---
	dotsY := avatarCY + avatarR + 60
	if dateFace != nil {
		dotsY = avatarCY + avatarR + 56
	}
	dotR := 5
	dotGap := 14
	nDots := len(s.builtinLock.password)
	if nDots > 0 {
		totalW := nDots*dotR*2 + (nDots-1)*dotGap
		startX := centerX - totalW/2 + dotR
		dotColor := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
		for i := 0; i < nDots; i++ {
			cx := startX + i*(dotR*2+dotGap)
			drawFilledCircle(img, cx, dotsY, dotR, dotColor)
		}
	}

	// --- Status text ---
	if textFace != nil {
		statusY := dotsY + 30
		if s.builtinLock.showError && s.builtinLock.errorMsg != "" {
			drawCenteredText(img, textFace, s.builtinLock.errorMsg, centerX, statusY,
				color.NRGBA{R: 0xff, G: 0x66, B: 0x66, A: 0xff})
		} else if nDots == 0 {
			drawCenteredText(img, textFace, "Type password to unlock", centerX, statusY,
				color.NRGBA{R: 0x99, G: 0x99, B: 0x99, A: 0xff})
		}
	}

	// --- Custom label from settings ---
	if textFace != nil && s.lockLabel != "" {
		labelY := screenH - 60
		drawCenteredText(img, textFace, s.lockLabel, centerX, labelY,
			color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 0xff})
	}

	// --- Update or create scene buffer ---
	if s.lockTree == nil {
		return
	}
	lockTreeC := (*C.struct_wlr_scene_tree)(s.lockTree)
	if s.builtinLock.pixBuf != nil {
		pixBuf := (*C.struct_pixel_buffer)(s.builtinLock.pixBuf)
		C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
		if s.builtinLock.sceneBuf != nil {
			sceneBuf := (*C.struct_wlr_scene_buffer)(s.builtinLock.sceneBuf)
			C.scene_buffer_set_buffer(sceneBuf, &pixBuf.base)
			C.scene_buffer_set_dest_size(sceneBuf, C.int(screenW), C.int(screenH))
			C.scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
		}
	} else {
		pixBuf := C.pixel_buffer_create(C.int(screenW), C.int(screenH))
		if pixBuf != nil {
			C.pixel_buffer_update(pixBuf, unsafe.Pointer(&img.Pix[0]), C.int(screenW), C.int(screenH))
			sceneBuf := C.scene_buffer_create(lockTreeC, &pixBuf.base)
			if sceneBuf == nil {
				log.Println("[LOCK] scene_buffer_create failed — lock screen may not render")
			} else {
				C.scene_buffer_set_dest_size(sceneBuf, C.int(screenW), C.int(screenH))
				C.scene_node_set_position(&sceneBuf.node, C.int(screenX), C.int(screenY))
			}
			s.builtinLock.sceneBuf = unsafe.Pointer(sceneBuf)
			s.builtinLock.pixBuf = unsafe.Pointer(pixBuf)
		}
	}
}

// handleBuiltinLockKey processes keyboard input on the built-in lock screen.
func (s *server) handleBuiltinLockKey(sym xkb.KeySym, mods uint32) {
	if s.builtinLock == nil || !s.builtinLock.active {
		return
	}

	switch {
	case sym == xkb.SymFromName("Return", xkb.KeySymNoFlags) ||
		sym == xkb.SymFromName("KP_Enter", xkb.KeySymNoFlags):
		// Submit password
		if len(s.builtinLock.password) == 0 {
			return
		}
		pw := string(s.builtinLock.password)
		go s.tryBuiltinLockAuth(pw)

	case sym == xkb.SymFromName("BackSpace", xkb.KeySymNoFlags):
		if len(s.builtinLock.password) > 0 {
			s.builtinLock.password = s.builtinLock.password[:len(s.builtinLock.password)-1]
			s.builtinLock.showError = false
			s.builtinLock.errorMsg = ""
			s.updateBuiltinLockScene()
		}

	case sym == xkb.SymFromName("Escape", xkb.KeySymNoFlags):
		// Clear password (zero memory before discarding)
		zeroPassword(s.builtinLock.password)
		s.builtinLock.password = nil
		s.builtinLock.showError = false
		s.builtinLock.errorMsg = ""
		s.updateBuiltinLockScene()

	default:
		// Try to get a printable character from the keysym
		ch := keysymToRune(sym)
		if ch != 0 {
			s.builtinLock.password = append(s.builtinLock.password, ch)
			s.builtinLock.showError = false
			s.builtinLock.errorMsg = ""
			s.updateBuiltinLockScene()
		}
	}
}

// tryBuiltinLockAuth attempts PAM authentication in a goroutine.
func (s *server) tryBuiltinLockAuth(password string) {
	u, err := user.Current()
	if err != nil {
		s.mainThreadActions <- func() {
			if s.builtinLock == nil || !s.builtinLock.active {
				return
			}
			s.builtinLock.errorMsg = "Cannot determine user"
			s.builtinLock.showError = true
			zeroPassword(s.builtinLock.password)
			s.builtinLock.password = nil
			s.updateBuiltinLockScene()
		}
		s.triggerWakeup()
		return
	}

	username := u.Username
	cUser := C.CString(username)
	cPass := C.CString(password)
	ret := C.pam_auth(cUser, cPass)
	C.free(unsafe.Pointer(cUser))
	C.free(unsafe.Pointer(cPass))

	if ret == C.PAM_SUCCESS {
		log.Println("[LOCK] PAM authentication successful")
		s.mainThreadActions <- func() {
			s.deactivateBuiltinLock()
		}
		s.triggerWakeup()
	} else {
		log.Printf("[LOCK] PAM authentication failed (code %d)\n", ret)
		s.mainThreadActions <- func() {
			if s.builtinLock == nil || !s.builtinLock.active {
				return
			}
			s.builtinLock.errorMsg = "Incorrect password"
			s.builtinLock.showError = true
			zeroPassword(s.builtinLock.password)
			s.builtinLock.password = nil
			s.updateBuiltinLockScene()
		}
		s.triggerWakeup()
	}
}

// deactivateBuiltinLock cleans up the built-in lock screen and unlocks.
func (s *server) deactivateBuiltinLock() {
	if s.builtinLock == nil {
		return
	}

	// Stop clock ticker and signal the goroutine to exit (Stop() alone does
	// not close ticker.C, so a 'for range ticker.C' would block forever).
	if s.builtinLock.clockTicker != nil {
		s.builtinLock.clockTicker.Stop()
	}
	if s.builtinLock.clockDone != nil {
		close(s.builtinLock.clockDone)
	}

	// Destroy scene nodes
	if s.builtinLock.sceneBuf != nil {
		sceneBuf := (*C.struct_wlr_scene_buffer)(s.builtinLock.sceneBuf)
		C.scene_node_destroy(&sceneBuf.node)
	}
	// pixBuf is freed by scene_node_destroy (pixel_buffer_destroy callback)

	// Zero password memory before discarding
	zeroPassword(s.builtinLock.password)
	s.builtinLock.active = false
	s.builtinLock = nil
	s.markUnlocked()

	// Use cleanupLock pattern to restore focus
	s.cleanupLock()

	// Unblank display if blanked
	if s.displayBlanked {
		s.setDisplayBlanked(false)
	}

	log.Println("[LOCK] Built-in lock screen deactivated")
}

// --- Drawing helpers ---

// drawCenteredText draws text centered horizontally at the given position.
func drawCenteredText(img *image.NRGBA, face font.Face, text string, centerX, y int, col color.NRGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
	}
	textW := d.MeasureString(text).Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	d.Dot = fixed.P(centerX-textW/2, y+ascent/2)
	d.DrawString(text)
}

// drawFilledCircle draws a filled circle with anti-aliased edges.
func drawFilledCircle(img *image.NRGBA, cx, cy, r int, col color.NRGBA) {
	rf := float64(r)
	for y := cy - r - 1; y <= cy+r+1; y++ {
		for x := cx - r - 1; x <= cx+r+1; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			dx := float64(x) + 0.5 - float64(cx)
			dy := float64(y) + 0.5 - float64(cy)
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > rf+0.5 {
				continue
			}
			a := 1.0
			if dist > rf-0.5 {
				a = rf + 0.5 - dist
			}
			// Alpha-composite over existing pixel
			bgPx := img.NRGBAAt(x, y)
			srcA := a * float64(col.A) / 255
			dstA := float64(bgPx.A) / 255
			outA := srcA + dstA*(1-srcA)
			if outA > 0 {
				outR := (float64(col.R)*srcA + float64(bgPx.R)*dstA*(1-srcA)) / outA
				outG := (float64(col.G)*srcA + float64(bgPx.G)*dstA*(1-srcA)) / outA
				outB := (float64(col.B)*srcA + float64(bgPx.B)*dstA*(1-srcA)) / outA
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(math.Min(outR, 255)),
					G: uint8(math.Min(outG, 255)),
					B: uint8(math.Min(outB, 255)),
					A: uint8(math.Min(outA*255, 255)),
				})
			}
		}
	}
}

// getInitials returns up to 2 uppercase initials from a name.
func getInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	initials := ""
	for _, p := range parts {
		if len(initials) >= 2 {
			break
		}
		if len(p) > 0 {
			initials += strings.ToUpper(p[:1])
		}
	}
	if initials == "" {
		return "?"
	}
	return initials
}

// keysymToRune converts an XKB keysym to a Unicode rune.
// Returns 0 for non-printable keys.
func keysymToRune(sym xkb.KeySym) rune {
	val := uint32(sym)

	// Latin-1 range (keysym 0x0020..0x007e and 0x00a0..0x00ff map directly)
	if val >= 0x0020 && val <= 0x007e {
		return rune(val)
	}
	if val >= 0x00a0 && val <= 0x00ff {
		return rune(val)
	}

	// Unicode keysyms: 0x01000000 + unicode codepoint
	if val >= 0x01000000 && val <= 0x0110ffff {
		return rune(val - 0x01000000)
	}

	// XKB special Latin keysyms (accented characters etc.)
	// These are in the range 0x0100..0x0fff and map to Unicode via xkb_keysym_to_utf32
	// We handle common ones used in European keyboards
	if val >= 0x0100 && val <= 0x0fff {
		// Use a lookup or approximate — XKB keysym to Unicode mapping
		// Most keysyms in 0x0100-0x0fff match Unicode directly for Latin chars
		r := xkbLatinToUnicode(val)
		if r != 0 {
			return r
		}
	}

	return 0
}

// xkbLatinToUnicode converts XKB Latin keysyms (0x0100-0x0fff) to Unicode.
// This covers the most common European accented characters.
func xkbLatinToUnicode(sym uint32) rune {
	// Most XKB Latin keysyms match their Unicode codepoint
	// Exception: some keysyms need special mapping
	switch {
	case sym >= 0x0100 && sym <= 0x024f:
		return rune(sym) // Latin Extended-A/B — direct mapping
	case sym >= 0x0250 && sym <= 0x02af:
		return rune(sym) // IPA Extensions
	default:
		return 0
	}
}

