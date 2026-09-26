package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

// runWithRecovery wraps the compositor in a crash recovery loop.
// If TYDE_COMPOSITOR_RUNNER=1 is set, this function returns immediately
// (we're already inside the runner). Otherwise, it re-execs the compositor
// binary and restarts on crash.
func runWithRecovery() bool {
	if os.Getenv("TYDE_COMPOSITOR_RUNNER") == "1" {
		return false // Already running under the recovery wrapper
	}

	// Only enable recovery when running as a real session (not nested)
	if os.Getenv("WLR_BACKENDS") != "" {
		return false // Nested/development mode, skip recovery
	}

	logDir := compositorLogDir()
	restartCount := 0

	// Signals for the session (logout, shutdown) go to the compositor, and
	// the runner then stops instead of restarting it.
	var mu sync.Mutex
	var current *exec.Cmd
	stopping := false
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for sig := range sigs {
			mu.Lock()
			stopping = true
			if current != nil && current.Process != nil {
				_ = current.Process.Signal(sig)
			}
			mu.Unlock()
		}
	}()
	defer signal.Stop(sigs)

	for restartCount < maxRestarts {
		logFile := filepath.Join(logDir, "compositor.log")
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			f = os.Stderr
		}

		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cannot find compositor executable: %v\n", err)
			return true
		}

		cmd := exec.Command(exe)
		cmd.Env = append(os.Environ(), "TYDE_COMPOSITOR_RUNNER=1")
		cmd.Stdout = f
		cmd.Stderr = f
		cmd.Stdin = os.Stdin

		// Remove any stale shutdown marker from previous runs
		shutdownMarker := filepath.Join(logDir, "shutdown-marker")
		os.Remove(shutdownMarker)

		fmt.Fprintf(os.Stderr, "Starting compositor (attempt %d)...\n", restartCount+1)
		started := time.Now()
		mu.Lock()
		if stopping {
			mu.Unlock()
			return true
		}
		err = cmd.Start()
		if err == nil {
			current = cmd
		}
		mu.Unlock()
		if err == nil {
			err = cmd.Wait()
		}
		mu.Lock()
		current = nil
		stop := stopping
		mu.Unlock()

		if f != os.Stderr {
			f.Close()
		}

		if err == nil || stop {
			return true // Clean exit, or the session is ending
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

		// Save crash log, keeping the last few only
		crashLog := filepath.Join(logDir, fmt.Sprintf("compositor-crash-%s.log",
			time.Now().Format("2006-01-02T15-04-05")))
		os.Rename(logFile, crashLog)
		pruneCrashLogs(logDir, keepCrashLogs)

		// Crashes count towards giving up only when they come close
		// together: one a week must not end the session on the fifth.
		if time.Since(started) > stableRun {
			restartCount = 0
		}
		restartCount++
		fmt.Fprintf(os.Stderr, "Compositor crashed (attempt %d/%d), restarting in 1s...\n",
			restartCount, maxRestarts)
		time.Sleep(1 * time.Second)
	}

	fmt.Fprintf(os.Stderr, "Compositor crashed %d times, giving up\n", maxRestarts)
	return true
}

const (
	maxRestarts   = 5           // crashes in a row before giving up
	stableRun     = time.Minute // a run this long starts the count again
	keepCrashLogs = 10          // crash logs kept
)

// pruneCrashLogs removes all but the newest keep crash logs.
func pruneCrashLogs(dir string, keep int) {
	logs, _ := filepath.Glob(filepath.Join(dir, "compositor-crash-*.log"))
	sort.Strings(logs) // the names sort by date
	for len(logs) > keep {
		os.Remove(logs[0])
		logs = logs[1:]
	}
}

func compositorLogDir() string {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".cache", "fyne", "com.fyshos.tyde")
	os.MkdirAll(dir, 0o700)
	return dir
}
