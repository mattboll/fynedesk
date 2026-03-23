package wlipc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/user"
	"sync"
)

// cachedKey stores the derived key so we don't call user.Current() and
// os.Hostname() on every encrypt/decrypt (those can block on LDAP/NIS).
var (
	keyOnce sync.Once
	keyData []byte
)

// clipboardKey derives an encryption key from machine-specific data.
// The key is derived from uid + hostname + a static salt. This provides
// basic protection against other local users reading clipboard history
// files, but does not protect against a determined attacker who can read
// this source code and obtain the same uid/hostname values.
func clipboardKey() []byte {
	keyOnce.Do(func() {
		u, _ := user.Current()
		hostname, _ := os.Hostname()
		seed := u.Uid + ":" + hostname + ":fynedesk-clipboard"
		h := sha256.Sum256([]byte(seed))
		keyData = h[:]
	})
	return keyData
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
