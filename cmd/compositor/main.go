// Command compositor runs the FyneDesk Wayland compositor using wlroots.
//
// Debug logging:
//
//	FYNEDESK_DEBUG=1 ./compositor                          # all debug logs
//	FYNEDESK_DEBUG=1 FYNEDESK_DEBUG_CATEGORIES=PERF,IPC   # selective
package main

import (
	"flag"
	"os"

	"fyshos.com/fynedesk/internal/wayland/compositor"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging (same as FYNEDESK_DEBUG=1)")
	flag.Parse()
	if *debug {
		os.Setenv("FYNEDESK_DEBUG", "1")
	}

	// Run with crash recovery wrapper when running as a real session
	if runWithRecovery() {
		return
	}

	compositor.Run()
}
