package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Encrypt encrypts plaintext using AES-256-GCM directly with dek.
// It returns base64(nonce || ciphertext || tag).
func Encrypt(plaintext []byte, dek []byte) (string, error) {
	// Generate random 12-byte nonce.
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt plaintext using AES-256-GCM directly with dek.
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM block: %w", err)
	}

	ciphertextAndTag := aesgcm.Seal(nil, nonce, plaintext, nil)

	// Result buffer: nonce || ciphertext || tag
	resultLen := len(nonce) + len(ciphertextAndTag)
	result := make([]byte, 0, resultLen)
	result = append(result, nonce...)
	result = append(result, ciphertextAndTag...)

	return base64.StdEncoding.EncodeToString(result), nil
}

// Decrypt decrypts the base64-encoded blob using AES-256-GCM directly with dek.
func Decrypt(blob string, dek []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// Validate the length is at least 28 bytes (12 nonce + 16 auth tag)
	if len(data) < 28 {
		return nil, errors.New("invalid ciphertext blob length")
	}

	nonce := data[:12]
	ciphertextAndTag := data[12:]

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM block: %w", err)
	}

	plaintext, err := aesgcm.Open(nil, nonce, ciphertextAndTag, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt or authenticate: %w", err)
	}

	return plaintext, nil
}

// GenerateDEK produces 32 random bytes from crypto/rand.
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("failed to generate DEK: %w", err)
	}
	return dek, nil
}

// WrapDEK encrypts dek using AES-256-GCM with kek and a random 12-byte nonce.
// Returns base64(nonce || ciphertext || tag).
func WrapDEK(dek []byte, kek []byte) (string, error) {
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM block: %w", err)
	}

	ciphertextAndTag := aesgcm.Seal(nil, nonce, dek, nil)

	result := make([]byte, 0, len(nonce)+len(ciphertextAndTag))
	result = append(result, nonce...)
	result = append(result, ciphertextAndTag...)

	return base64.StdEncoding.EncodeToString(result), nil
}

// UnwrapDEK decodes from base64, extracts 12-byte nonce and ciphertext,
// and decrypts using AES-256-GCM with kek.
func UnwrapDEK(wrapped string, kek []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("failed to decode wrapped DEK base64: %w", err)
	}

	// Nonce is 12 bytes, tag is 16 bytes. DEK should be 32 bytes.
	if len(data) < 28 {
		return nil, errors.New("invalid wrapped DEK length")
	}

	nonce := data[:12]
	ciphertextAndTag := data[12:]

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM block: %w", err)
	}

	dek, err := aesgcm.Open(nil, nonce, ciphertextAndTag, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt wrapped DEK: %w", err)
	}

	return dek, nil
}
