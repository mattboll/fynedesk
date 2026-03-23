// Package main provides a FyneDesk panel that runs as a Wayland client.
// It connects to the FyneDesk compositor and displays the bar and widget panel.
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/FyshOS/appie"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"fyshos.com/fynedesk/internal/ui"
	"fyshos.com/fynedesk/wlipc"

	// Import modules to register them
	_ "fyshos.com/fynedesk/modules/clipboard"
	_ "fyshos.com/fynedesk/modules/notes"
	_ "fyshos.com/fynedesk/modules/desktops"
	_ "fyshos.com/fynedesk/modules/fyles"
	_ "fyshos.com/fynedesk/modules/launcher"
	_ "fyshos.com/fynedesk/modules/status"
	_ "fyshos.com/fynedesk/modules/systray"
)

func main() {
	fmt.Println("Panel starting...")
	fmt.Printf("WAYLAND_DISPLAY=%s\n", os.Getenv("WAYLAND_DISPLAY"))
	fmt.Printf("DISPLAY=%s\n", os.Getenv("DISPLAY"))

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

	fmt.Println("Creating Fyne app...")

	// Use the same app ID as the main fynedesk app to share preferences
	a := app.NewWithID("com.fyshos.fynedesk")
	fmt.Printf("Fyne driver: %T\n", a.Driver())
	icons := appie.NewFDOProvider()

	// Create panel desktop (no window manager, just UI).
	// NewPanelDesktop creates the root window titled "FyneDesk:Panel" so the
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
