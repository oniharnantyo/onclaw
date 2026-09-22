package webhooks_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/webhooks"
)

// signingFixture is one raw body + secret pair shared by both schemes' tests.
var (
	signBody   = []byte(`{"action":"opened","zen":"Keep it simple."}`)
	signSecret = []byte(" hunter2-secret-for-the-mac ")
)

// hmacHeader computes the GitHub-style signature header value.
func hmacHeader(t *testing.T, secret, body []byte, prefixed bool) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sum := hex.EncodeToString(mac.Sum(nil))
	if prefixed {
		return "sha256=" + sum
	}
	return sum
}

func TestVerifySignature_HMACSHA256(t *testing.T) {
	cases := []struct {
		name   string
		header string
		secret []byte
		body   []byte
		want   bool
	}{
		{"valid with sha256= prefix", hmacHeader(t, signSecret, signBody, true), signSecret, signBody, true},
		{"valid bare hex", hmacHeader(t, signSecret, signBody, false), signSecret, signBody, true},
		{"valid uppercase hex", "sha256=" + strings.ToUpper(hmacHeader(t, signSecret, signBody, false)), signSecret, signBody, true},
		{"tampered body", hmacHeader(t, signSecret, signBody, true), signSecret, []byte(`{"action":"opened","zen":"Keep it simple!"}`), false},
		{"wrong secret", hmacHeader(t, []byte("other-secret"), signBody, true), signSecret, signBody, false},
		{"garbage value", "sha256=zzzz-not-hex", signSecret, signBody, false},
		{"empty value", "", signSecret, signBody, false},
		{"truncated mac", hmacHeader(t, signSecret, signBody, true)[:20], signSecret, signBody, false},
		{"sha1-labeled prefix carrying the right sha256 mac", "sha1=" + hmacHeader(t, signSecret, signBody, false), signSecret, signBody, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhooks.VerifySignature(domain.RecipeWebhookSchemeHMACSHA256, tc.header, tc.secret, tc.body); got != tc.want {
				t.Fatalf("VerifySignature(hmac_sha256) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifySignature_SecretToken(t *testing.T) {
	const token = "gitlab-shared-token-9f2c"
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"verbatim match", token, true},
		{"wrong token", "gitlab-shared-token-9f2d", false},
		{"prefix of the token", token[:10], false},
		{"padded token", token + " ", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := webhooks.VerifySignature(domain.RecipeWebhookSchemeSecretToken, tc.header, []byte(token), signBody); got != tc.want {
				t.Fatalf("VerifySignature(secret_token) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifySignature_UnknownScheme(t *testing.T) {
	// Registration validation admits only the catalog schemes; an unknown
	// scheme must verify nothing.
	if webhooks.VerifySignature("plain_text", "anything", signSecret, signBody) {
		t.Fatal("an unknown scheme must never verify")
	}
}
