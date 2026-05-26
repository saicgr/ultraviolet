// Package store wraps the control-plane Postgres + AES-256-GCM credential encryption.
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// Encryptor encrypts/decrypts warehouse credentials with AES-256-GCM.
// Key must be exactly 32 bytes. Nonce is generated per-message and returned alongside ciphertext.
type Encryptor struct {
	gcm cipher.AEAD
}

func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

func (e *Encryptor) Encrypt(plaintext []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce: %w", err)
	}
	ciphertext = e.gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

func (e *Encryptor) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != e.gcm.NonceSize() {
		return nil, fmt.Errorf("nonce length mismatch: want %d got %d", e.gcm.NonceSize(), len(nonce))
	}
	pt, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return pt, nil
}

// HashAPIKey returns the sha256 of an API key for storage in api_keys.key_hash.
func HashAPIKey(apiKey string) []byte {
	sum := sha256.Sum256([]byte(apiKey))
	return sum[:]
}

// HashAPIKeyHex returns the hex-encoded sha256.
func HashAPIKeyHex(apiKey string) string {
	return hex.EncodeToString(HashAPIKey(apiKey))
}
