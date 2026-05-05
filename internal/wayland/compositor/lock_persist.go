package compositor

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Lock state persistence — defense against the compositor being killed
// (SIGSEGV, kill -9) while the screen is locked. Without this, the runner
// would relaunch the compositor and the new instance would start unlocked,
// effectively letting an attacker bypass the lock by killing the process.
//
// Threat model: an attacker with the user's UID at the keyboard. If they can
// already write arbitrary files in $HOME they can also disable the lock
// screen entirely, so persisting the state in $HOME/.config is enough — we
// just need to detect "the previous compositor was locked when it died".

type persistedLock struct {
	Locked    bool  `json:"locked"`
	Timestamp int64 `json:"timestamp"` // unix millis
}

// lockStatePath returns the path of the lock-state file. Lives next to
// the other IPC state in $XDG_CONFIG_HOME/fynedesk.
func (s *server) lockStatePath() string {
	return filepath.Join(s.getConfigDir(), "lock-state.json")
}

// markLocked records that the screen is locked. Idempotent.
func (s *server) markLocked() {
	state := persistedLock{Locked: true, Timestamp: time.Now().UnixMilli()}
	data, err := json.Marshal(state)
	if err != nil {
		log.Printf("[LOCK] failed to marshal lock state: %v", err)
		return
	}
	if err := atomicWriteFile(s.lockStatePath(), data); err != nil {
		log.Printf("[LOCK] failed to persist lock state: %v", err)
	}
}

// markUnlocked clears the persisted lock state. Idempotent.
func (s *server) markUnlocked() {
	if err := os.Remove(s.lockStatePath()); err != nil && !os.IsNotExist(err) {
		log.Printf("[LOCK] failed to remove lock state: %v", err)
	}
}

// wasPreviouslyLocked returns true if a previous compositor instance was
// locked when it died. Stale records (older than 24h) are ignored.
func (s *server) wasPreviouslyLocked() bool {
	data, err := os.ReadFile(s.lockStatePath())
	if err != nil {
		return false
	}
	var state persistedLock
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt file — treat as not locked but remove the stale record.
		_ = os.Remove(s.lockStatePath())
		return false
	}
	if !state.Locked {
		return false
	}
	age := time.Since(time.UnixMilli(state.Timestamp))
	if age > 24*time.Hour {
		_ = os.Remove(s.lockStatePath())
		return false
	}
	return true
}
