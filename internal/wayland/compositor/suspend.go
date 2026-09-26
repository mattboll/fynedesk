package compositor

import (
	"log"
	"os"
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

	// logind waits for this inhibitor to be let go of before the machine
	// sleeps: the time to lock the screen, so that it does not wake up on
	// the desktop.
	inhibitor := takeSleepInhibitor(conn)
	defer func() { inhibitor.release() }()

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
		starting, ok := prepareForSleep(sig)
		if !ok {
			continue
		}
		if starting {
			log.Println("[SUSPEND] PrepareForSleep(true) — locking before suspend")
			s.lockForSleep()
			inhibitor.release() // locked: the machine may sleep
			continue
		}
		log.Println("[SUSPEND] PrepareForSleep(false) — resume detected")
		inhibitor = takeSleepInhibitor(conn) // for the next sleep
		s.lockAfterResume()
	}
}

// prepareForSleep reads a PrepareForSleep signal: whether the machine is
// going to sleep (or waking up).
func prepareForSleep(sig *dbus.Signal) (starting, ok bool) {
	if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" || len(sig.Body) < 1 {
		return false, false
	}
	starting, ok = sig.Body[0].(bool)
	return starting, ok
}

// sleepLockWait is how long the session has to lock before the machine
// sleeps; past it, the built-in lock takes over.
const sleepLockWait = 2 * time.Second

// lockForSleep locks the session and returns once it is locked and the
// lock is on screen (or the built-in lock was put up instead).
func (s *server) lockForSleep() {
	// suspendLockPending keeps the input that wakes the machine from
	// clearing idleLocked before the lock screen is up.
	_ = s.enqueueAction(func() {
		s.suspendLockPending = true
		if !s.locked.Load() {
			s.idleLocked = true
			s.startLock()
		}
	})
	deadline := time.Now().Add(sleepLockWait)
	for !s.locked.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !s.locked.Load() {
		log.Println("[SUSPEND] The lock client is late — built-in lock before sleeping")
		done := make(chan struct{})
		if s.enqueueAction(func() { s.activateBuiltinLock(); close(done) }) == nil {
			<-done
		}
	}
	time.Sleep(100 * time.Millisecond) // a frame, so that the lock is what the screen shows
}

// lockAfterResume makes sure the session is locked after the machine woke
// up, and lets the input work normally again once the lock had its time.
func (s *server) lockAfterResume() {
	_ = s.enqueueAction(func() {
		if !s.locked.Load() {
			log.Println("[SUSPEND] Not locked after resume — locking now")
			s.suspendLockPending = true
			s.idleLocked = true
			s.startLock()
		}
	})
	time.AfterFunc(15*time.Second, func() {
		_ = s.enqueueAction(func() { s.suspendLockPending = false })
	})
}

// sleepInhibitor is a logind delay inhibitor for sleep: the file descriptor
// logind gave, to close once the machine may sleep.
type sleepInhibitor struct {
	file *os.File
}

// takeSleepInhibitor asks logind to wait for Tyde before sleeping. It returns
// an empty inhibitor if logind refuses.
func takeSleepInhibitor(conn *dbus.Conn) *sleepInhibitor {
	var fd dbus.UnixFD
	err := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").Call(
		"org.freedesktop.login1.Manager.Inhibit", 0,
		"sleep", "Tyde", "Lock the screen before sleeping", "delay").Store(&fd)
	if err != nil {
		log.Printf("[SUSPEND] No sleep inhibitor: %v (the screen may show on waking)", err)
		return &sleepInhibitor{}
	}
	return &sleepInhibitor{file: os.NewFile(uintptr(fd), "logind-inhibitor")}
}

// release lets the machine sleep.
func (i *sleepInhibitor) release() {
	if i.file != nil {
		i.file.Close()
		i.file = nil
	}
}
