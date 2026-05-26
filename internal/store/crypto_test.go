package store

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte(`{"project_id":"acme","service_account_key":{"type":"service_account"}}`)
	ct, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Error("ciphertext == plaintext (encryption not happening)")
	}
	pt, err := enc.Decrypt(ct, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Errorf("decrypt mismatch: got %q, want %q", pt, plaintext)
	}
}

func TestEncryptorKeyLength(t *testing.T) {
	if _, err := NewEncryptor(make([]byte, 16)); err == nil {
		t.Error("16-byte key should be rejected")
	}
	if _, err := NewEncryptor(make([]byte, 32)); err != nil {
		t.Errorf("32-byte key rejected: %v", err)
	}
}

func TestHashAPIKey_Stable(t *testing.T) {
	a := HashAPIKey("uvk_test")
	b := HashAPIKey("uvk_test")
	if !bytes.Equal(a, b) {
		t.Error("hash not deterministic")
	}
	if len(a) != 32 {
		t.Errorf("hash length = %d, want 32", len(a))
	}
}
