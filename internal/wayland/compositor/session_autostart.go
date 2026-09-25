package compositor

import (
	"log"
	"strings"
	"time"

	"fyshos.com/tyde/internal/autostart"
)

// startAutostart starts the applications of the session (XDG Autostart:
// ~/.config/autostart and /etc/xdg/autostart), once the panel is up so that
// their tray icons have somewhere to go. Those the restored session started
// already are left out. Not in a nested compositor: the host session starts
// them.
func (s *server) startAutostart() {
	s.sessionMu.Lock()
	restored := s.sessionLaunched
	s.sessionMu.Unlock()

	env := safeEnv()
	for _, e := range autostart.Entries(autostart.DefaultEnv()) {
		if startedBySession(e, restored) {
			log.Printf("[AUTOSTART] %s: started by the restored session", e.ID)
			continue
		}
		start := func() {
			if err := autostart.Start(e, env); err != nil {
				log.Printf("[AUTOSTART] %s: %v", e.ID, err)
				return
			}
			log.Printf("[AUTOSTART] %s: %q", e.ID, e.Args)
		}
		if e.Delay > 0 {
			time.AfterFunc(e.Delay, start)
		} else {
			start()
		}
	}
}

// startedBySession reports whether the restored session started the
// application of an entry (by its app id).
func startedBySession(e autostart.Entry, restored []string) bool {
	program := e.Program()
	id := strings.TrimSuffix(e.ID, ".desktop")
	for _, app := range restored {
		if strings.EqualFold(app, program) || strings.EqualFold(app, id) {
			return true
		}
	}
	return false
}
