package wlipc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"sync"
)

// cachedKey stores the loaded/derived key.
var (
	keyOnce sync.Once
	keyData []byte
)

// clipboardKey returns the 32-byte AES-256 key used to encrypt the
// clipboard history file.
//
// First-run: 32 random bytes are generated and persisted to
// $XDG_CONFIG_HOME/fynedesk/clipboard-key with mode 0600. Subsequent runs
// read that file. This means a copy of the encrypted history file alone is
// useless without the key file — the previous design derived the key from
// uid+hostname, which is reproducible by any attacker who can read the
// source code.
//
// If the file can't be read or written (read-only home, etc.), we fall
// back to the legacy uid+hostname derivation so the clipboard module still
// works in restricted environments.
func clipboardKey() []byte {
	keyOnce.Do(func() {
		keyData = loadOrCreateClipboardKey()
	})
	return keyData
}

func loadOrCreateClipboardKey() []byte {
	keyPath := filepath.Join(ConfigDir(), "clipboard-key")

	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data
	}

	// Generate a fresh 32-byte key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Printf("[clipboard-crypto] rand.Read failed (%v), falling back to uid+hostname-derived key", err)
		return legacyClipboardKey()
	}

	// Best-effort persist. If the directory or file can't be written we
	// still return the generated key for this session — encryption stays
	// strong; only persistence across restarts is lost.
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		log.Printf("[clipboard-crypto] cannot create config dir %s: %v (using ephemeral key)", filepath.Dir(keyPath), err)
		return key
	}
	tmp := keyPath + ".tmp"
	if err := os.WriteFile(tmp, key, 0o600); err != nil {
		log.Printf("[clipboard-crypto] cannot write key file (%v) — using ephemeral key", err)
		return key
	}
	if err := os.Rename(tmp, keyPath); err != nil {
		log.Printf("[clipboard-crypto] cannot finalize key file (%v) — using ephemeral key", err)
		os.Remove(tmp)
	}
	return key
}

// legacyClipboardKey reproduces the original uid+hostname-derived key.
// Used only as a fallback when crypto/rand or filesystem access fails.
func legacyClipboardKey() []byte {
	u, _ := user.Current()
	hostname, _ := os.Hostname()
	seed := u.Uid + ":" + hostname + ":fynedesk-clipboard"
	h := sha256.Sum256([]byte(seed))
	return h[:]
}

// EncryptData encrypts data using AES-256-GCM.
func EncryptData(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(clipboardKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptData decrypts AES-256-GCM encrypted data.
func DecryptData(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(clipboardKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
}
