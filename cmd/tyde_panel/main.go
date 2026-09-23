// Package main provides a Tyde panel that runs as a Wayland client.
// It connects to the Tyde compositor and displays the bar and widget panel.
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"fyshos.com/tyde/internal/ui"
	"fyshos.com/tyde/wlipc"

	// Import modules to register them
	_ "fyshos.com/tyde/modules/clipboard"
	_ "fyshos.com/tyde/modules/desktops"
	_ "fyshos.com/tyde/modules/fyles"
	_ "fyshos.com/tyde/modules/launcher"
	_ "fyshos.com/tyde/modules/notes"
	_ "fyshos.com/tyde/modules/status"
	_ "fyshos.com/tyde/modules/systray"
)

func main() {
	fmt.Println("Panel starting...")
	fmt.Printf("WAYLAND_DISPLAY=%s\n", os.Getenv("WAYLAND_DISPLAY"))
	fmt.Printf("DISPLAY=%s\n", os.Getenv("DISPLAY"))

	// Carry over FyneDesk-era settings (the compositor normally did it
	// already; this covers a panel started on its own).
	wlipc.MigrateLegacyConfig()

	// Disable Fyne HiDPI scaling - compositor handles this
	os.Setenv("FYNE_SCALE", "1")

	// Get screen size and position from command line arguments (passed by compositor)
	screenWidth := 1920
	screenHeight := 1080
	screenX := 0
	screenY := 0
	if len(os.Args) >= 3 {
		if w, err := strconv.Atoi(os.Args[1]); err == nil {
			screenWidth = w
		}
		if h, err := strconv.Atoi(os.Args[2]); err == nil {
			screenHeight = h
		}
	}
	if len(os.Args) >= 5 {
		if x, err := strconv.Atoi(os.Args[3]); err == nil {
			screenX = x
		}
		if y, err := strconv.Atoi(os.Args[4]); err == nil {
			screenY = y
		}
	}
	fmt.Printf("Screen size: %dx%d at (%d,%d) (FYNE_SCALE=1)\n", screenWidth, screenHeight, screenX, screenY)

	// Ensure we're connecting to the compositor
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	}

	// The compositor recognises the panel as an XWayland surface, so keep
	// Fyne on X11 even though it can speak Wayland natively.
	os.Setenv("FYNE_PLATFORM", "x11")

	fmt.Println("Creating Fyne app...")

	// Use the same app ID as the main tyde app to share preferences
	a := app.NewWithID("com.fyshos.tyde")
	fmt.Printf("Fyne driver: %T\n", a.Driver())
	icons := appie.NewFDOProvider()

	// Create panel desktop (no window manager, just UI).
	// NewPanelDesktop creates the root window titled "Tyde:Panel" so the
	// compositor identifies it immediately at map time (Fyne's SetTitle doesn't
	// propagate to XWayland, so the title must be set at creation).
	fmt.Println("Creating panel desktop...")
	desk := ui.NewPanelDesktop(a, icons)
	ui.SetScreenSize(desk, screenWidth, screenHeight)
	if screenX != 0 || screenY != 0 {
		ui.SetScreenPosition(desk, screenX, screenY)
		wlipc.SetPrimaryScreenOffset(float32(screenX), float32(screenY))
	}

	// Show first-run setup wizard if no config exists yet
	if ui.IsFirstRun() {
		fmt.Println("First run detected — showing setup wizard")
		ui.ShowSetupWizard(desk)
	}

	root := desk.Root()

	// Enable transparent framebuffer so the compositor background shows through.
	// This must be called before Show().
	root.SetTransparent(true)

	// Resize to screen size (SetFullScreen doesn't work with XWayland)
	fmt.Printf("Resizing panel to %dx%d\n", screenWidth, screenHeight)
	root.Resize(fyne.NewSize(float32(screenWidth), float32(screenHeight)))

	// Start secondary bar windows for non-primary outputs
	ui.StartSecondaryBars(desk)

	root.ShowAndRun()
	fmt.Println("Panel exited")
}
