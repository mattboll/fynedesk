package ai

import (
	"errors"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/zalando/go-keyring"
)

// fakeKeyring replaces the Secret Service for a test; nil store = none.
// It returns how to read the store.
func fakeKeyring(t *testing.T, store map[string]string) func(string) string {
	t.Helper()
	var mu sync.Mutex
	get, set, del := keyringGet, keyringSet, keyringDelete
	t.Cleanup(func() {
		keyringGet, keyringSet, keyringDelete = get, set, del
		keyMu.Lock()
		clear(keyCache)
		keyMu.Unlock()
	})
	none := errors.New("no secret service")
	keyringGet = func(_, user string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if store == nil {
			return "", none
		}
		if v, ok := store[user]; ok {
			return v, nil
		}
		return "", keyring.ErrNotFound
	}
	keyringSet = func(_, user, v string) error {
		mu.Lock()
		defer mu.Unlock()
		if store == nil {
			return none
		}
		store[user] = v
		return nil
	}
	keyringDelete = func(_, user string) error {
		mu.Lock()
		defer mu.Unlock()
		if store == nil {
			return none
		}
		delete(store, user)
		return nil
	}
	return func(user string) string {
		mu.Lock()
		defer mu.Unlock()
		return store[user]
	}
}

func TestKeyMovesToKeyring(t *testing.T) {
	stored := fakeKeyring(t, map[string]string{})
	p := test.NewTempApp(t).Preferences()
	p.SetString(prefClaudeKey, "sk-old")

	if got := loadKey(p, prefClaudeKey); got != "sk-old" {
		t.Fatalf("loadKey = %q", got)
	}
	if stored(prefClaudeKey) != "sk-old" || p.String(prefClaudeKey) != "" {
		t.Fatalf("not moved: keyring %q, preferences %q", stored(prefClaudeKey), p.String(prefClaudeKey))
	}

	saveKey(p, prefClaudeKey, "sk-n")
	saveKey(p, prefClaudeKey, "sk-new")
	if got := loadKey(p, prefClaudeKey); got != "sk-new" {
		t.Fatalf("loadKey after save = %q", got)
	}
	time.Sleep(keySaveDelay + 200*time.Millisecond)
	if stored(prefClaudeKey) != "sk-new" || p.String(prefClaudeKey) != "" {
		t.Fatalf("saved: keyring %q, preferences %q", stored(prefClaudeKey), p.String(prefClaudeKey))
	}
}

func TestKeyWithoutKeyring(t *testing.T) {
	fakeKeyring(t, nil)
	p := test.NewTempApp(t).Preferences()
	p.SetString(prefOpenAIKey, "sk-kept")
	if got := loadKey(p, prefOpenAIKey); got != "sk-kept" {
		t.Fatalf("loadKey = %q", got)
	}
	storeKey(p, prefOpenAIKey, "sk-2")
	if p.String(prefOpenAIKey) != "sk-2" {
		t.Fatal("with no keyring the key stays in the preferences")
	}
}
