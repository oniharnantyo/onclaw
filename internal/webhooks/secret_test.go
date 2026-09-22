package webhooks_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

func TestGenerateSecret_Entropy(t *testing.T) {
	const count = 50
	seen := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		secret, err := webhooks.GenerateSecret()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(secret)
		if err != nil {
			t.Fatalf("secret is not base64url: %v", err)
		}
		if len(raw) != 32 {
			t.Fatalf("expected 32 bytes of entropy, got %d", len(raw))
		}
		if seen[secret] {
			t.Fatal("a repeated secret is not cryptographically random")
		}
		seen[secret] = true
	}
}

func TestSecretHint(t *testing.T) {
	if got := webhooks.SecretHint("abcdefgh"); got != "efgh" {
		t.Fatalf("hint = %q, want efgh", got)
	}
	if got := webhooks.SecretHint("abc"); got != "abc" {
		t.Fatalf("short secret hint = %q, want abc", got)
	}
}

func TestSecretCipher_WorkspaceAAD(t *testing.T) {
	cipher := webhooks.NewAESGCMCipher(testKey)
	env, err := cipher.EncryptSecret("ws-a", []byte("the-secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(env, "v1:") {
		t.Fatalf("expected a v1 envelope, got %q", env)
	}

	// The owning workspace decrypts.
	pt, err := cipher.DecryptSecret("ws-a", env)
	if err != nil || string(pt) != "the-secret" {
		t.Fatalf("decrypt: %v (%q)", err, string(pt))
	}

	// A different workspace's AAD fails the open — a ciphertext lifted
	// from one workspace is undecryptable in another.
	if _, err := cipher.DecryptSecret("ws-b", env); err == nil {
		t.Fatal("cross-workspace decryption must fail")
	}

	// A different key fails too.
	other := webhooks.NewAESGCMCipher([]byte("98765432109876543210987654321098"))
	if _, err := other.DecryptSecret("ws-a", env); err == nil {
		t.Fatal("decryption under a foreign key must fail")
	}
}
