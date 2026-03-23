package compositor

import (
	"log"
	"os"
	"time"
)

// watchdogRecovery monitors the event loop health and attempts recovery
// when frames stop being rendered. Escalation strategy:
//
//  1. Stall detected (>5s): trigger eventfd wakeup to unstick sleeping loop
//  2. Persistent stall (>15s): log critical warning + another wakeup attempt
//  3. Unrecoverable stall (>30s): force restart via exit code 5
//
// This replaces the diagnostic-only watchdog with actual recovery.
func (s *server) watchdogRecovery() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[WATCHDOG] panic recovered: %v\n", r)
		}
	}()

	// In nested mode (WLR_BACKENDS=wayland), the host compositor only sends
	// frame callbacks on damage. Long idle periods are normal, not stalls.
	nested := os.Getenv("WLR_BACKENDS") == "wayland"

	const (
		checkInterval = 5 * time.Second
		wakeupThresh  = 5 * time.Second  // Level 1: try wakeup
		criticalThresh = 15 * time.Second // Level 2: critical warning
		restartThresh  = 30 * time.Second // Level 3: force restart
	)

	var stallStart time.Time
	var wakeupsSent int

	for {
		time.Sleep(checkInterval)
		if s.shuttingDown.Load() {
			return
		}

		if s.lastFrameTime.IsZero() {
			continue // Not yet rendering
		}

		// Don't trigger restart while locked or display blanked — no frames
		// are produced when outputs are disabled (DPMS off). Killing the
		// compositor while locked destroys the entire user session.
		if s.locked || s.displayBlanked {
			continue
		}

		gap := time.Since(s.lastFrameTime)

		if gap < wakeupThresh {
			// Healthy — reset stall tracking
			if !stallStart.IsZero() {
				log.Printf("[WATCHDOG] Event loop recovered after %v stall\n",
					time.Since(stallStart))
				stallStart = time.Time{}
				wakeupsSent = 0
			}
			continue
		}

		// Stall detected
		if stallStart.IsZero() {
			stallStart = time.Now()
			wakeupsSent = 0
		}

		stallDuration := time.Since(stallStart)

		if stallDuration >= restartThresh && !nested {
			// Level 3: unrecoverable — force restart (disabled in nested mode)
			log.Printf("[WATCHDOG] CRITICAL: Event loop stuck for %v — forcing restart (exit 5)\n", gap)
			// Kill panel first so it doesn't orphan
			if s.panelCmd != nil && s.panelCmd.Process != nil {
				s.panelCmd.Process.Kill()
			}
			s.display.Terminate()
			os.Exit(5) // Runner will auto-restart
		}

		if stallDuration >= criticalThresh {
			// Level 2: critical warning + aggressive wakeup
			log.Printf("[WATCHDOG] CRITICAL: No frame for %v (stalled %v) — sending wakeup %d\n",
				gap, stallDuration, wakeupsSent+1)
			s.triggerWakeup()
			wakeupsSent++
			continue
		}

		// Level 1: try wakeup
		if wakeupsSent < 3 {
			log.Printf("[WATCHDOG] No frame for %v — sending wakeup attempt %d\n",
				gap, wakeupsSent+1)
			s.triggerWakeup()
			wakeupsSent++
		}
	}
}
