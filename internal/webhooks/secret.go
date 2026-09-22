package webhooks

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// SecretCipher is the webhook secret's encryption port (add-connection-webhooks
// design.md D2): encrypt and decrypt the per-connection HMAC secret with the
// instance key, AAD-bound to the workspace so a ciphertext lifted from one
// workspace is undecryptable in another. The same derivation the gateway
// credential and settings secret rows use; the composition root supplies the
// concrete key-bound cipher (secretcipher.go's AESGCMCipher over
// internal/secrets).
type SecretCipher interface {
	// EncryptSecret seals the plaintext secret for the workspace, returning
	// the "v1:<nonce>:<ciphertext>" envelope.
	EncryptSecret(workspaceID string, plaintext []byte) (envelope string, err error)
	// DecryptSecret opens the envelope with the workspace as AAD.
	DecryptSecret(workspaceID, envelope string) ([]byte, error)
}

// AESGCMCipher is the AES-256-GCM SecretCipher over the instance master key
// and the secrets package's envelope format — the exact machinery the
// gateway credentials ride (design.md D2: "secret lifecycle mirrors gateway
// pairing secrets").
type AESGCMCipher struct {
	key []byte
}

// NewAESGCMCipher builds the cipher over the instance master key.
func NewAESGCMCipher(key []byte) *AESGCMCipher {
	return &AESGCMCipher{key: key}
}

// EncryptSecret implements SecretCipher.
func (c *AESGCMCipher) EncryptSecret(workspaceID string, plaintext []byte) (string, error) {
	env, err := secrets.Encrypt(c.key, []byte(workspaceID), plaintext)
	if err != nil {
		return "", fmt.Errorf("webhook secret encrypt: %w", err)
	}
	return env, nil
}

// DecryptSecret implements SecretCipher.
func (c *AESGCMCipher) DecryptSecret(workspaceID, envelope string) ([]byte, error) {
	pt, err := secrets.Decrypt(c.key, []byte(workspaceID), envelope)
	if err != nil {
		return nil, fmt.Errorf("webhook secret decrypt: %w", err)
	}
	return pt, nil
}

// secretEntropyBytes is the generated secret's raw entropy: 32 bytes — the
// HMAC key length GitHub recommends and every provider accepts — encoded
// base64url (43 characters, URL-safe for provider form fields).
const secretEntropyBytes = 32

// GenerateSecret mints a new webhook secret: cryptographically random bytes
// from crypto/rand, base64url-encoded. The plaintext is returned exactly
// once — the enable/rotate response — and only its encrypted envelope and
// last-4 hint persist (design.md D2).
func GenerateSecret() (plaintext string, err error) {
	raw := make([]byte, secretEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("webhook secret generation: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// SecretHint is the display-once residue: the secret's last 4 characters,
// enough for an operator to tell which secret a provider is configured with
// without ever revealing the value.
func SecretHint(secret string) string {
	if len(secret) < 4 {
		return secret
	}
	return secret[len(secret)-4:]
}
