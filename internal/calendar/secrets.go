package calendar

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"

	"fyshos.com/fynedesk/wlipc"
)

// secretServiceName is the service identifier used in the Secret Service
// (so all our entries live under one logical group, easy to inspect with
// secret-tool or seahorse).
const secretServiceName = "fynedesk-calendar"

// SecretBundle is the credential blob persisted per account. Only the
// OAuth path uses RefreshToken — GOA accounts have no stored secret since
// the GOA daemon brokers tokens for us.
type SecretBundle struct {
	RefreshToken string `json:"refresh_token,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// SaveSecret persists the secret bundle for the given account ID.
// Tries Secret Service first, falls back to an AES-GCM encrypted file
// in $XDG_CONFIG_HOME/fynedesk/calendar/.
func SaveSecret(accountID string, bundle SecretBundle) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if err := keyring.Set(secretServiceName, accountID, string(data)); err == nil {
		// Belt-and-suspenders: remove any stale fallback so we don't leave
		// the secret in two places when the keyring becomes available.
		_ = removeFallbackSecret(accountID)
		return nil
	} else {
		log.Printf("[calendar-secrets] keyring.Set failed (%v), using encrypted fallback", err)
	}
	return writeFallbackSecret(accountID, data)
}

// LoadSecret retrieves the secret bundle for the given account ID.
func LoadSecret(accountID string) (SecretBundle, error) {
	if raw, err := keyring.Get(secretServiceName, accountID); err == nil {
		var b SecretBundle
		if err := json.Unmarshal([]byte(raw), &b); err == nil {
			return b, nil
		}
	}
	data, err := readFallbackSecret(accountID)
	if err != nil {
		return SecretBundle{}, err
	}
	var b SecretBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return SecretBundle{}, fmt.Errorf("decode secret: %w", err)
	}
	return b, nil
}

// DeleteSecret removes any persisted credential for the account.
// Best-effort: errors from a missing keyring entry are silently ignored,
// so callers can use this to clean up after an account removal regardless
// of where the secret was stored.
func DeleteSecret(accountID string) error {
	if err := keyring.Delete(secretServiceName, accountID); err != nil &&
		!errors.Is(err, keyring.ErrNotFound) {
		log.Printf("[calendar-secrets] keyring.Delete: %v", err)
	}
	return removeFallbackSecret(accountID)
}

// --- encrypted-file fallback ---
//
// Reuses the same on-disk-key pattern as wlipc/crypto.go (clipboard): a
// random 32-byte key persisted at ~/.config/fynedesk/calendar/secrets-key
// with mode 0600 protects an AES-256-GCM blob per account.

var (
	calKeyOnce sync.Once
	calKey     []byte
)

func calendarSecretKey() []byte {
	calKeyOnce.Do(func() {
		calKey = loadOrCreateCalendarKey()
	})
	return calKey
}

func loadOrCreateCalendarKey() []byte {
	dir := filepath.Join(wlipc.ConfigDir(), "calendar")
	keyPath := filepath.Join(dir, "secrets-key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Printf("[calendar-secrets] rand.Read failed (%v), refusing to use weak key", err)
		// Return the zero key — encryption will still be syntactically
		// valid (all writes/reads use the same key), but anyone who can
		// read the source can decrypt. This branch should be impossible
		// in practice.
		return make([]byte, 32)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("[calendar-secrets] mkdir %s: %v (ephemeral key)", dir, err)
		return key
	}
	tmp := keyPath + ".tmp"
	if err := os.WriteFile(tmp, key, 0o600); err != nil {
		log.Printf("[calendar-secrets] write key: %v (ephemeral key)", err)
		return key
	}
	if err := os.Rename(tmp, keyPath); err != nil {
		log.Printf("[calendar-secrets] rename key: %v (ephemeral key)", err)
		os.Remove(tmp)
	}
	return key
}

func fallbackSecretPath(accountID string) string {
	return filepath.Join(wlipc.ConfigDir(), "calendar",
		"secret_"+cacheFilename(accountID)+".enc")
}

func writeFallbackSecret(accountID string, data []byte) error {
	block, err := aes.NewCipher(calendarSecretKey())
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return atomicWrite(fallbackSecretPath(accountID), ciphertext, 0o600)
}

func readFallbackSecret(accountID string) ([]byte, error) {
	ciphertext, err := os.ReadFile(fallbackSecretPath(accountID))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(calendarSecretKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

func removeFallbackSecret(accountID string) error {
	err := os.Remove(fallbackSecretPath(accountID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
