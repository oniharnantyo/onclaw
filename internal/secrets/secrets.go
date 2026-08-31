// Package secrets provides AES-256-GCM envelope encryption and decryption with AAD binding.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// Version1Prefix is the envelope version identifier for AES-256-GCM encrypted secrets.
const Version1Prefix = "v1"

// KeySize is the required length in bytes for an AES-256 key (32 bytes = 256 bits).
const KeySize = 32

// ParseKey decodes a 32-byte key from either a 64-character hex string or a base64 string.
// Returns an error if the raw key is empty, invalidly encoded, or decodes to a length other than 32 bytes.
func ParseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("encryption key cannot be empty")
	}

	// Try 64-character hex decoding first
	if len(raw) == hex.EncodedLen(KeySize) {
		if key, err := hex.DecodeString(raw); err == nil {
			return key, nil
		}
	}

	// Try base64 decodings (standard, raw standard, URL, raw URL)
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}

	for _, enc := range decoders {
		if key, err := enc.DecodeString(raw); err == nil {
			if len(key) == KeySize {
				return key, nil
			}
		}
	}

	// If it was a valid hex string of different length, give a specific error
	if key, err := hex.DecodeString(raw); err == nil {
		return nil, fmt.Errorf("encryption key must decode to exactly %d bytes, got %d", KeySize, len(key))
	}

	return nil, fmt.Errorf("encryption key must decode to exactly %d bytes from hex or base64", KeySize)
}

// Encrypt encrypts plaintext using AES-256-GCM with the provided 32-byte key and authenticated data (AAD).
// It returns an envelope formatted as "v1:<b64 nonce>:<b64 ciphertext>".
func Encrypt(key []byte, aad []byte, plaintext []byte) (string, error) {
	if len(key) != KeySize {
		return "", fmt.Errorf("invalid key size: expected %d bytes, got %d", KeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)

	envelope := fmt.Sprintf("%s:%s:%s",
		Version1Prefix,
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(ciphertext),
	)

	return envelope, nil
}

// Decrypt decrypts an envelope formatted as "v1:<b64 nonce>:<b64 ciphertext>" using AES-256-GCM
// with the provided 32-byte key and authenticated data (AAD).
// Returns domain.ErrUndecryptable if the envelope is malformed, version unsupported, or authentication fails.
func Decrypt(key []byte, aad []byte, envelope string) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("invalid key size: expected %d bytes, got %d", KeySize, len(key))
	}

	parts := strings.Split(envelope, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: invalid envelope format", domain.ErrUndecryptable)
	}

	if parts[0] != Version1Prefix {
		return nil, fmt.Errorf("%w: unsupported envelope version %q", domain.ErrUndecryptable, parts[0])
	}

	nonce, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid nonce encoding", domain.ErrUndecryptable)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ciphertext encoding", domain.ErrUndecryptable)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: invalid nonce length", domain.ErrUndecryptable)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("%w: authentication failed", domain.ErrUndecryptable)
	}

	return plaintext, nil
}
