package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// runWithRecovery wraps the compositor in a crash recovery loop.
// If FYNEDESK_COMPOSITOR_RUNNER=1 is set, this function returns immediately
// (we're already inside the runner). Otherwise, it re-execs the compositor
// binary and restarts on crash.
func runWithRecovery() bool {
	if os.Getenv("FYNEDESK_COMPOSITOR_RUNNER") == "1" {
		return false // Already running under the recovery wrapper
	}

	// Only enable recovery when running as a real session (not nested)
	if os.Getenv("WLR_BACKENDS") != "" {
		return false // Nested/development mode, skip recovery
	}

	logDir := compositorLogDir()
	maxRestarts := 5
	restartCount := 0

	for restartCount < maxRestarts {
		logFile := filepath.Join(logDir, "compositor.log")
		f, err := os.Create(logFile)
		if err != nil {
			f = os.Stderr
		}

		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find compositor executable: %v\n", err)
			return true
		}

		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "FYNEDESK_COMPOSITOR_RUNNER=1")
		cmd.Stdout = f
		cmd.Stderr = f
		cmd.Stdin = os.Stdin

		// Remove any stale shutdown marker from previous runs
		shutdownMarker := filepath.Join(logDir, "shutdown-marker")
		os.Remove(shutdownMarker)

		fmt.Fprintf(os.Stderr, "Starting compositor (attempt %d)...\n", restartCount+1)
		err = cmd.Run()

		if f != os.Stderr {
			f.Close()
		}

		if err == nil {
			return true // Clean exit
		}

		// Check for intentional shutdown marker — compositor wrote this before cleanup.
		// If cleanup crashes (segfault in wlroots), the exit code is non-zero but
		// we should NOT restart because the user requested logout.
		if _, markerErr := os.Stat(shutdownMarker); markerErr == nil {
			os.Remove(shutdownMarker)
			fmt.Fprintf(os.Stderr, "Compositor shutdown detected (cleanup may have crashed), not restarting\n")
			return true
		}

		// Check for restart-requested exit code (5)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.ExitStatus() == 5 {
				fmt.Fprintf(os.Stderr, "Compositor restart requested, restarting...\n")
				continue // Immediate restart, no delay, no crash count
			}
		}

		// Save crash log
		crashLog := filepath.Join(logDir, fmt.Sprintf("compositor-crash-%s.log",
			time.Now().Format("2006-01-02T15-04-05")))
		os.Rename(logFile, crashLog)

		restartCount++
		fmt.Fprintf(os.Stderr, "Compositor crashed (attempt %d/%d), restarting in 1s...\n",
			restartCount, maxRestarts)
		time.Sleep(1 * time.Second)
	}

	fmt.Fprintf(os.Stderr, "Compositor crashed %d times, giving up\n", maxRestarts)
	return true
}

func compositorLogDir() string {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".cache", "fyne", "com.fyshos.fynedesk")
	os.MkdirAll(dir, 0700)
	return dir
}
