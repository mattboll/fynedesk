// Command compositor runs the Tyde Wayland compositor using wlroots.
//
// Debug logging:
//
//	TYDE_DEBUG=1 ./compositor                          # all debug logs
//	TYDE_DEBUG=1 TYDE_DEBUG_CATEGORIES=PERF,IPC   # selective
package main

import (
	"flag"
	"os"

	"fyshos.com/tyde/internal/wayland/compositor"
)

func main() {
	debug := flag.Bool("debug", false, "Enable debug logging (same as TYDE_DEBUG=1)")
	flag.Parse()
	if *debug {
		os.Setenv("TYDE_DEBUG", "1")
	}

	// Run with crash recovery wrapper when running as a real session
	if runWithRecovery() {
		return
	}

	compositor.Run()
}
