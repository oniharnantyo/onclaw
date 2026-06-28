package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	plaintext := []byte("hello world secrets")
	blob, err := Encrypt(plaintext, dek)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	decrypted, err := Decrypt(blob, dek)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted text does not match plaintext: got %s, want %s", decrypted, plaintext)
	}
}

func TestTamperDetection(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	plaintext := []byte("tamper proof payload")
	blob, err := Encrypt(plaintext, dek)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatalf("failed to decode blob: %v", err)
	}

	// Tamper with different parts:
	// 1. Modifying the nonce (first 12 bytes)
	tamperedNonce := make([]byte, len(decoded))
	copy(tamperedNonce, decoded)
	tamperedNonce[0] ^= 0xFF
	blobNonce := base64.StdEncoding.EncodeToString(tamperedNonce)
	if _, err := Decrypt(blobNonce, dek); err == nil {
		t.Error("expected failure decrypting with tampered nonce, but it succeeded")
	}

	// 2. Modifying the ciphertext/tag
	tamperedCipher := make([]byte, len(decoded))
	copy(tamperedCipher, decoded)
	tamperedCipher[len(tamperedCipher)-1] ^= 0xFF
	blobCipher := base64.StdEncoding.EncodeToString(tamperedCipher)
	if _, err := Decrypt(blobCipher, dek); err == nil {
		t.Error("expected failure decrypting with tampered ciphertext/tag, but it succeeded")
	}

	// 3. Invalid length blob (less than 28 bytes)
	shortData := decoded[:27]
	blobShort := base64.StdEncoding.EncodeToString(shortData)
	if _, err := Decrypt(blobShort, dek); err == nil {
		t.Error("expected failure decrypting too short blob, but it succeeded")
	}
}

func TestDEKWrapUnwrap(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	kek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate KEK: %v", err)
	}

	wrapped, err := WrapDEK(dek, kek)
	if err != nil {
		t.Fatalf("failed to wrap DEK: %v", err)
	}

	unwrapped, err := UnwrapDEK(wrapped, kek)
	if err != nil {
		t.Fatalf("failed to unwrap DEK: %v", err)
	}

	if !bytes.Equal(dek, unwrapped) {
		t.Errorf("unwrapped DEK does not match original DEK")
	}

	// Tamper with wrapped DEK
	decoded, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		t.Fatalf("failed to decode wrapped: %v", err)
	}
	decoded[len(decoded)-1] ^= 0xFF
	tamperedWrapped := base64.StdEncoding.EncodeToString(decoded)
	if _, err := UnwrapDEK(tamperedWrapped, kek); err == nil {
		t.Error("expected failure unwrapping tampered DEK, but it succeeded")
	}
}

func TestModeSwitchingSimulation(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate DEK: %v", err)
	}

	plaintext := []byte("critical business data")
	ciphertext, err := Encrypt(plaintext, dek)
	if err != nil {
		t.Fatalf("failed to encrypt plaintext: %v", err)
	}

	// Wrap DEK with Keyfile KEK
	keyfileKEK, err := GenerateDEK()
	if err != nil {
		t.Fatalf("failed to generate keyfile KEK: %v", err)
	}
	wrappedKeyfile, err := WrapDEK(dek, keyfileKEK)
	if err != nil {
		t.Fatalf("failed to wrap DEK with keyfile KEK: %v", err)
	}

	// Unwrap DEK with Keyfile KEK
	unwrappedKeyfile, err := UnwrapDEK(wrappedKeyfile, keyfileKEK)
	if err != nil {
		t.Fatalf("failed to unwrap DEK with keyfile KEK: %v", err)
	}

	// Verify it still decrypts the same ciphertext
	decrypted, err := Decrypt(ciphertext, unwrappedKeyfile)
	if err != nil {
		t.Fatalf("failed to decrypt ciphertext with unwrapped DEK: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted data mismatch: got %s, want %s", decrypted, plaintext)
	}
}

func TestCryptoErrors(t *testing.T) {
	dek, _ := GenerateDEK()

	// 1. Decrypt invalid base64
	_, err := Decrypt("invalid-base64-!!!", dek)
	if err == nil {
		t.Error("expected Decrypt to fail on invalid base64, got nil")
	}

	// 2. UnwrapDEK invalid base64
	_, err = UnwrapDEK("invalid-base64-!!!", dek)
	if err == nil {
		t.Error("expected UnwrapDEK to fail on invalid base64, got nil")
	}

	// 3. UnwrapDEK invalid length
	invalidLenBlob := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err = UnwrapDEK(invalidLenBlob, dek)
	if err == nil {
		t.Error("expected UnwrapDEK to fail on short data, got nil")
	}
}
