package compositor

import (
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

// watchSuspendResume listens for logind's PrepareForSleep signal to lock the
// screen before suspend and handle resume. This is critical: without it, a
// user can resume from suspend and land on an unlocked desktop because the
// idle timer was reset by the first input event (which also wakes the machine).
//
// Signal: org.freedesktop.login1.Manager.PrepareForSleep(bool starting)
//   - starting=true  → system is about to suspend → lock immediately
//   - starting=false → system just resumed → lock if not already locked
func (s *server) watchSuspendResume() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SUSPEND] panic recovered: %v\n", r)
		}
	}()

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Printf("[SUSPEND] Could not connect to system bus: %v (suspend lock disabled)\n", err)
		return
	}

	// Subscribe to PrepareForSleep signal
	err = conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	)
	if err != nil {
		log.Printf("[SUSPEND] Could not subscribe to PrepareForSleep: %v\n", err)
		conn.Close()
		return
	}

	sigChan := make(chan *dbus.Signal, 4)
	conn.Signal(sigChan)
	defer conn.Close()
	log.Println("[SUSPEND] Listening for logind PrepareForSleep signal")

	for {
		var sig *dbus.Signal
		select {
		case <-s.shutdown:
			return
		case sig = <-sigChan:
			if sig == nil { // channel closed
				return
			}
		}
		if s.shuttingDown.Load() {
			return
		}

		if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" {
			continue
		}
		if len(sig.Body) < 1 {
			continue
		}

		starting, ok := sig.Body[0].(bool)
		if !ok {
			continue
		}

		if starting {
			// System is about to suspend — lock NOW before we lose the CPU.
			// Set suspendLockPending to prevent resetIdleTimer (triggered by
			// resume input events) from clearing idleLocked before the lock
			// screen has connected.
			log.Println("[SUSPEND] PrepareForSleep(true) — locking before suspend")
			s.mainThreadActions <- func() {
				s.suspendLockPending = true
				if !s.locked {
					s.idleLocked = true
					go s.lockScreen()
				}
			}
			s.triggerWakeup()
		} else {
			// System just resumed — ensure we're locked.
			log.Println("[SUSPEND] PrepareForSleep(false) — resume detected")
			s.mainThreadActions <- func() {
				if !s.locked {
					log.Println("[SUSPEND] Not locked after resume — locking now")
					s.suspendLockPending = true
					s.idleLocked = true
					go s.lockScreen()
				} else {
					log.Println("[SUSPEND] Already locked after resume — OK")
				}
			}
			s.triggerWakeup()

			// Clear the pending flag after a delay, once the lock screen has
			// had time to connect (or fail). This ensures resetIdleTimer
			// resumes normal behavior even if the locker crashes.
			go func() {
				time.Sleep(15 * time.Second)
				s.mainThreadActions <- func() {
					s.suspendLockPending = false
				}
				s.triggerWakeup()
			}()
		}
	}
}
