package compositor

import (
	"log"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

// screenSaverDBus implements org.freedesktop.ScreenSaver D-Bus interface
type screenSaverDBus struct {
	srv        *server
	mu         sync.Mutex
	inhibits   map[uint32]inhibitEntry
	nextCookie uint32
}

type inhibitEntry struct {
	appName string
	reason  string
}

func newScreenSaverDBus(srv *server) *screenSaverDBus {
	return &screenSaverDBus{
		srv:        srv,
		inhibits:   make(map[uint32]inhibitEntry),
		nextCookie: 1,
	}
}

// Inhibit is called by apps (e.g. video players) to prevent screen locking
func (ss *screenSaverDBus) Inhibit(appName, reason string) (uint32, *dbus.Error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	cookie := ss.nextCookie
	ss.nextCookie++
	ss.inhibits[cookie] = inhibitEntry{appName: appName, reason: reason}

	log.Printf("ScreenSaver inhibited by %s: %s (cookie=%d)\n", appName, reason, cookie)
	return cookie, nil
}

// UnInhibit removes a screensaver inhibition
func (ss *screenSaverDBus) UnInhibit(cookie uint32) *dbus.Error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if entry, ok := ss.inhibits[cookie]; ok {
		delete(ss.inhibits, cookie)
		log.Printf("ScreenSaver uninhibited by %s (cookie=%d)\n", entry.appName, cookie)
	}
	return nil
}

// IsInhibited returns true if any app has requested screensaver inhibition
func (ss *screenSaverDBus) IsInhibited() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return len(ss.inhibits) > 0
}

// startScreenSaverDBus registers the ScreenSaver D-Bus service.
// Always creates the screenSaverDBus object (for IsInhibited() checks).
// D-Bus registration is skipped in nested mode since the host session owns the name.
func (s *server) startScreenSaverDBus() {
	ss := newScreenSaverDBus(s)
	s.screenSaverDBus = ss

	// In nested mode, don't try to claim the D-Bus name (host session already owns it)
	if os.Getenv("WLR_BACKENDS") != "" {
		return
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("D-Bus: could not connect to session bus: %v\n", err)
		return
	}

	err = conn.Export(ss, "/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver")
	if err != nil {
		log.Printf("D-Bus: could not export ScreenSaver: %v\n", err)
		return
	}

	// Also export on /ScreenSaver (some apps use this path)
	conn.Export(ss, "/ScreenSaver", "org.freedesktop.ScreenSaver")

	reply, err := conn.RequestName("org.freedesktop.ScreenSaver", dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("D-Bus: could not request ScreenSaver name: %v\n", err)
		return
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("D-Bus: ScreenSaver name already taken, skipping\n")
		return
	}

	log.Println("D-Bus: org.freedesktop.ScreenSaver registered")
}
