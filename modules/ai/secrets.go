package ai

import (
	"errors"
	"log"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"github.com/zalando/go-keyring"
)

// keyringService groups the API keys in the Secret Service (seahorse,
// secret-tool), under the name of their preference.
const keyringService = "tyde-ai"

// keyring functions, replaced in tests.
var (
	keyringGet    = keyring.Get
	keyringSet    = keyring.Set
	keyringDelete = keyring.Delete
)

// keySaveDelay lets the typing of a key settle before it is stored.
const keySaveDelay = 500 * time.Millisecond

var (
	keyMu         sync.Mutex
	keyCache      = map[string]string{} // pref -> key, once read: the keyring is asked once
	keySaveTimers = map[string]*time.Timer{}
)

// loadKey returns the API key stored under a preference name: from the
// keyring, or from the preferences where it was kept before (it is then
// moved to the keyring), or where it stays when there is no keyring.
func loadKey(p fyne.Preferences, pref string) string {
	keyMu.Lock()
	defer keyMu.Unlock()
	if key, ok := keyCache[pref]; ok {
		return key
	}
	key, err := keyringGet(keyringService, pref)
	if err != nil {
		key = p.String(pref)
		if key != "" && keyringSet(keyringService, pref, key) == nil {
			p.RemoveValue(pref) // moved out of the clear preferences file
		}
	}
	keyCache[pref] = key
	return key
}

// saveKey stores an API key, once the typing settles: in the keyring, or in
// the preferences when there is none.
func saveKey(p fyne.Preferences, pref, key string) {
	keyMu.Lock()
	defer keyMu.Unlock()
	keyCache[pref] = key
	if t := keySaveTimers[pref]; t != nil {
		t.Stop()
	}
	keySaveTimers[pref] = time.AfterFunc(keySaveDelay, func() { storeKey(p, pref, key) })
}

func storeKey(p fyne.Preferences, pref, key string) {
	var err error
	if key == "" {
		if err = keyringDelete(keyringService, pref); errors.Is(err, keyring.ErrNotFound) {
			err = nil
		}
	} else {
		err = keyringSet(keyringService, pref, key)
	}
	if err != nil {
		log.Printf("[ai] no keyring (%v): the API key is kept in the preferences", err)
		p.SetString(pref, key)
		return
	}
	p.RemoveValue(pref)
}
