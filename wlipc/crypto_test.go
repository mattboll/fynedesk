package wlipc

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	plaintext := []byte("hello clipboard data")
	encrypted, err := EncryptData(plaintext)
	if err != nil {
		t.Fatalf("EncryptData: %v", err)
	}
	if bytes.Equal(encrypted, plaintext) {
		t.Error("encrypted data should differ from plaintext")
	}

	decrypted, err := DecryptData(encrypted)
	if err != nil {
		t.Fatalf("DecryptData: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_EmptyData(t *testing.T) {
	encrypted, err := EncryptData([]byte{})
	if err != nil {
		t.Fatalf("EncryptData(empty): %v", err)
	}
	decrypted, err := DecryptData(encrypted)
	if err != nil {
		t.Fatalf("DecryptData(empty): %v", err)
	}
	if len(decrypted) != 0 {
		t.Errorf("expected empty decrypted data, got %d bytes", len(decrypted))
	}
}

func TestDecryptData_TooShort(t *testing.T) {
	_, err := DecryptData([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecryptData with short input should return error")
	}
}

func TestEncryptData_DifferentNonces(t *testing.T) {
	plaintext := []byte("same data")
	enc1, _ := EncryptData(plaintext)
	enc2, _ := EncryptData(plaintext)
	if bytes.Equal(enc1, enc2) {
		t.Error("two encryptions of same data should produce different ciphertexts (different nonces)")
	}
}
