package secrets_test

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/secrets"
)

func generateKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, secrets.KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}
	return k
}

func TestParseKey(t *testing.T) {
	raw32 := generateKey(t)
	hexStr := hex.EncodeToString(raw32)
	b64Std := base64.StdEncoding.EncodeToString(raw32)
	b64Raw := base64.RawStdEncoding.EncodeToString(raw32)
	b64URL := base64.URLEncoding.EncodeToString(raw32)
	b64RawURL := base64.RawURLEncoding.EncodeToString(raw32)

	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{name: "valid 64-char hex", input: hexStr, expectError: false},
		{name: "valid std base64", input: b64Std, expectError: false},
		{name: "valid raw std base64", input: b64Raw, expectError: false},
		{name: "valid url base64", input: b64URL, expectError: false},
		{name: "valid raw url base64", input: b64RawURL, expectError: false},
		{name: "valid hex with whitespace", input: "  " + hexStr + "  ", expectError: false},
		{name: "empty string", input: "", expectError: true},
		{name: "whitespace only", input: "   ", expectError: true},
		{name: "hex too short (16 bytes = 32 hex chars)", input: hex.EncodeToString(raw32[:16]), expectError: true},
		{name: "hex too long (48 bytes = 96 hex chars)", input: hex.EncodeToString(append(raw32, raw32[:16]...)), expectError: true},
		{name: "base64 too short (16 bytes)", input: base64.StdEncoding.EncodeToString(raw32[:16]), expectError: true},
		{name: "base64 too long (48 bytes)", input: base64.StdEncoding.EncodeToString(append(raw32, raw32[:16]...)), expectError: true},
		{name: "invalid characters", input: "not-a-valid-hex-or-base64-key-with-invalid-chars!@#$%", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := secrets.ParseKey(tt.input)
			if tt.expectError && err == nil {
				t.Errorf("ParseKey(%q) expected error, got nil", tt.input)
			}
			if !tt.expectError {
				if err != nil {
					t.Fatalf("ParseKey(%q) unexpected error: %v", tt.input, err)
				}
				if len(key) != secrets.KeySize {
					t.Errorf("ParseKey(%q) returned key of size %d, want %d", tt.input, len(key), secrets.KeySize)
				}
			}
		})
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	key := generateKey(t)
	aad := []byte("workspace-123")

	payloads := [][]byte{
		[]byte(""),
		[]byte("sk-proj-1234567890abcdef"),
		[]byte("a-very-long-secret-key-with-lots-of-bytes-and-special-characters-!@#$%^&*()_+{}|:<>?~"),
		[]byte("unicode-secret-ключ-秘密-🔒✨"),
		{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd},
	}

	for _, plain := range payloads {
		envelope, err := secrets.Encrypt(key, aad, plain)
		if err != nil {
			t.Fatalf("Encrypt failed for plain %q: %v", string(plain), err)
		}

		if !strings.HasPrefix(envelope, "v1:") {
			t.Errorf("Envelope %q does not start with v1:", envelope)
		}

		decrypted, err := secrets.Decrypt(key, aad, envelope)
		if err != nil {
			t.Fatalf("Decrypt failed for envelope %q: %v", envelope, err)
		}

		if string(decrypted) != string(plain) {
			t.Errorf("Decrypted mismatch: got %q, want %q", string(decrypted), string(plain))
		}
	}
}

func TestCrossTenantAADReplay(t *testing.T) {
	key := generateKey(t)
	tenantA := []byte("workspace-tenant-A")
	tenantB := []byte("workspace-tenant-B")
	secret := []byte("super-secret-api-key-for-tenant-A")

	envelope, err := secrets.Encrypt(key, tenantA, secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Attempt to decrypt with Tenant B's AAD (replay attack)
	decrypted, err := secrets.Decrypt(key, tenantB, envelope)
	if err == nil {
		t.Fatalf("Cross-tenant replay succeeded! Decrypted: %q", string(decrypted))
	}

	if !errors.Is(err, domain.ErrUndecryptable) {
		t.Errorf("Expected ErrUndecryptable, got: %v", err)
	}
}

func TestWrongKeyFailure(t *testing.T) {
	key1 := generateKey(t)
	key2 := generateKey(t)
	aad := []byte("workspace-123")
	secret := []byte("api-key-value")

	envelope, err := secrets.Encrypt(key1, aad, secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := secrets.Decrypt(key2, aad, envelope)
	if err == nil {
		t.Fatalf("Decrypt with wrong key succeeded! Got: %q", string(decrypted))
	}

	if !errors.Is(err, domain.ErrUndecryptable) {
		t.Errorf("Expected ErrUndecryptable, got: %v", err)
	}
}

func TestDecrypt_MalformedEnvelopes(t *testing.T) {
	key := generateKey(t)
	aad := []byte("ws-1")

	tests := []struct {
		name     string
		envelope string
	}{
		{name: "empty envelope", envelope: ""},
		{name: "no colons", envelope: "plain-string"},
		{name: "two parts only", envelope: "v1:part2"},
		{name: "four parts", envelope: "v1:p2:p3:p4"},
		{name: "unsupported version", envelope: "v2:bm9uY2U=:Y2lwaGVy"},
		{name: "invalid base64 nonce", envelope: "v1:invalid!nonce:Y2lwaGVy"},
		{name: "invalid base64 ciphertext", envelope: "v1:YmFzZTY0bm9uY2U=:invalid!cipher"},
		{name: "nonce wrong size", envelope: "v1:YQ==:Y2lwaGVy"}, // nonce "a" is 1 byte, not 12
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := secrets.Decrypt(key, aad, tt.envelope)
			if err == nil {
				t.Fatalf("Decrypt(%q) expected error, got nil", tt.envelope)
			}
			if !errors.Is(err, domain.ErrUndecryptable) {
				t.Errorf("Decrypt(%q) error = %v, want ErrUndecryptable", tt.envelope, err)
			}
		})
	}
}

func TestInvalidKeySize(t *testing.T) {
	aad := []byte("ws-1")
	plain := []byte("secret")
	shortKey := make([]byte, 16)

	_, err := secrets.Encrypt(shortKey, aad, plain)
	if err == nil {
		t.Errorf("Encrypt with 16-byte key expected error, got nil")
	}

	_, err = secrets.Decrypt(shortKey, aad, "v1:nonce:cipher")
	if err == nil {
		t.Errorf("Decrypt with 16-byte key expected error, got nil")
	}
}
