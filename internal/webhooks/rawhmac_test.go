package webhooks

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// refGitHubSHA1Sign pins the legacy X-Hub-Signature shape ("sha1=" + hex
// HMAC-SHA1), which the raw-hmac engine still supports for senders stuck on
// SHA-1 (https://docs.github.com/webhooks/using-webhooks/validating-webhook-deliveries-from-github).
func refGitHubSHA1Sign(secret string, body []byte) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	return "sha1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyRawHMACGitHub(t *testing.T) {
	const name = "X-Hub-Signature-256"
	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name: "documented vector passes",
			cfg:  githubCfg(vecSecretGitHub),
			headers: map[string]string{
				name: refGitHubSign(vecSecretGitHub, vecBody),
			},
			body: vecBody,
			want: VerdictPass,
		},
		{
			name: "uppercase hex passes (hex decoding is case-insensitive)",
			cfg:  githubCfg(vecSecretGitHub),
			headers: map[string]string{
				name: "sha256=" + strings.ToUpper(strings.TrimPrefix(refGitHubSign(vecSecretGitHub, vecBody), "sha256=")),
			},
			body: vecBody,
			want: VerdictPass,
		},
		{
			name:    "tampered body refuses",
			cfg:     githubCfg(vecSecretGitHub),
			headers: map[string]string{name: refGitHubSign(vecSecretGitHub, vecBody)},
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "signature made with the wrong secret refuses",
			cfg:     githubCfg(vecSecretGitHub),
			headers: map[string]string{name: refGitHubSign("not-the-github-secret", vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "non-hex signature value refuses",
			cfg:     githubCfg(vecSecretGitHub),
			headers: map[string]string{name: "sha256=not-hex-at-all!!"},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "missing sha256= prefix refuses",
			cfg:     githubCfg(vecSecretGitHub),
			headers: map[string]string{name: strings.TrimPrefix(refGitHubSign(vecSecretGitHub, vecBody), "sha256=")},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "wrong prefix (sha1=) refuses",
			cfg:     githubCfg(vecSecretGitHub),
			headers: map[string]string{name: "sha1=" + strings.TrimPrefix(refGitHubSign(vecSecretGitHub, vecBody), "sha256=")},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent signature header skips",
			cfg:     githubCfg(vecSecretGitHub),
			headers: nil,
			body:    vecBody,
			want:    VerdictSkip,
		},
		{
			name: "unknown configured algorithm refuses",
			cfg: VerifierConfig{
				Kind: KindRawHMAC, HeaderName: name,
				Algo: "md5", Encoding: EncHex, Prefix: "sha256=",
				Secret: vecSecretGitHub,
			},
			headers: map[string]string{name: refGitHubSign(vecSecretGitHub, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(tt.cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func TestVerifyRawHMACSHA1Legacy(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    Verdict
	}{
		{
			name:    "sha1 vector passes",
			headers: map[string]string{"X-Hub-Signature": refGitHubSHA1Sign(vecSecretGitHub, vecBody)},
			want:    VerdictPass,
		},
		{
			name:    "wrong secret refuses",
			headers: map[string]string{"X-Hub-Signature": refGitHubSHA1Sign("another-secret", vecBody)},
			want:    VerdictFail,
		},
		{
			name:    "sha1 length mismatch refuses",
			headers: map[string]string{"X-Hub-Signature": "sha1=abcd"},
			want:    VerdictFail,
		},
	}
	cfg := VerifierConfig{
		Kind: KindRawHMAC, HeaderName: "X-Hub-Signature",
		Algo: AlgoSHA1, Encoding: EncHex, Prefix: "sha1=",
		Secret: vecSecretGitHub,
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(cfg, signedInput(tt.headers, vecBody)))
		})
	}
}

func TestVerifyRawHMACShopify(t *testing.T) {
	const name = "X-Shopify-Hmac-Sha256"
	shopifyCfg := func(secret string, encoding HMACEncoding) VerifierConfig {
		return VerifierConfig{
			Kind: KindRawHMAC, HeaderName: name,
			Algo: AlgoSHA256, Encoding: encoding,
			Secret: secret,
		}
	}
	tests := []struct {
		name    string
		cfg     VerifierConfig
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name:    "documented base64 vector passes",
			cfg:     shopifyCfg(vecSecretShopify, EncBase64),
			headers: map[string]string{name: refShopifySign(vecSecretShopify, vecBody)},
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "tampered body refuses",
			cfg:     shopifyCfg(vecSecretShopify, EncBase64),
			headers: map[string]string{name: refShopifySign(vecSecretShopify, vecBody)},
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "wrong secret refuses",
			cfg:     shopifyCfg("not-the-shopify-secret", EncBase64),
			headers: map[string]string{name: refShopifySign(vecSecretShopify, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "base64 value under hex encoding refuses",
			cfg:     shopifyCfg(vecSecretShopify, EncHex),
			headers: map[string]string{name: refShopifySign(vecSecretShopify, vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent header skips",
			cfg:     shopifyCfg(vecSecretShopify, EncBase64),
			headers: nil,
			body:    vecBody,
			want:    VerdictSkip,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(tt.cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func TestVerifyRawHMACLinear(t *testing.T) {
	const name = "Linear-Signature"
	linearCfg := func(secret string) VerifierConfig {
		return VerifierConfig{
			Kind: KindRawHMAC, HeaderName: name,
			Algo: AlgoSHA256, Encoding: EncHex,
			Secret: secret,
		}
	}
	tests := []struct {
		name    string
		headers map[string]string
		body    []byte
		want    Verdict
	}{
		{
			name:    "documented vector passes",
			headers: map[string]string{name: refLinearSign(vecSecretLinear, vecBody)},
			body:    vecBody,
			want:    VerdictPass,
		},
		{
			name:    "tampered body refuses",
			headers: map[string]string{name: refLinearSign(vecSecretLinear, vecBody)},
			body:    []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`),
			want:    VerdictFail,
		},
		{
			name:    "wrong secret refuses",
			headers: map[string]string{name: refLinearSign("not-the-linear-secret", vecBody)},
			body:    vecBody,
			want:    VerdictFail,
		},
		{
			name:    "absent header skips",
			headers: nil,
			body:    vecBody,
			want:    VerdictSkip,
		},
	}
	cfg := linearCfg(vecSecretLinear)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Verify(cfg, signedInput(tt.headers, tt.body)))
		})
	}
}

func githubCfg(secret string) VerifierConfig {
	return VerifierConfig{
		Kind: KindRawHMAC, HeaderName: "X-Hub-Signature-256",
		Algo: AlgoSHA256, Encoding: EncHex, Prefix: "sha256=",
		Secret: secret,
	}
}
