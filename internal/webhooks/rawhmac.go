package webhooks

import (
	"crypto/hmac"
	"strings"
)

// verifyRawHMAC checks an HMAC computed over the exact raw body bytes and
// carried in one signature header — the GitHub X-Hub-Signature-256, Shopify
// X-Shopify-Hmac-Sha256, and Linear Linear-Signature families. The header's
// configured prefix (GitHub's "sha256=") is stripped literally, the
// remainder is decoded per Encoding, and the result is compared against the
// freshly computed MAC in constant time.
//
// Doc references for the seeded presets:
//   - GitHub: https://docs.github.com/webhooks/using-webhooks/validating-webhook-deliveries-from-github
//   - Shopify: https://shopify.dev/docs/apps/build/webhooks/verify-webhooks
//   - Linear: https://developers.linear.app/docs/graphql/webhooks (securing webhooks)
func verifyRawHMAC(cfg VerifierConfig, in VerifyInput) Verdict {
	presented := strings.TrimSpace(in.Header(cfg.HeaderName))
	if presented == "" {
		return VerdictSkip
	}
	if cfg.Secret == "" {
		return VerdictFail
	}
	if cfg.Algo != AlgoSHA256 && cfg.Algo != AlgoSHA1 {
		// Unknown hash algorithm: the row cannot authenticate anything.
		return VerdictFail
	}
	encoded := presented
	if cfg.Prefix != "" {
		if !strings.HasPrefix(encoded, cfg.Prefix) {
			return VerdictFail
		}
		encoded = encoded[len(cfg.Prefix):]
	}
	presentedMAC, ok := decodeSignature(cfg.Encoding, encoded)
	if !ok {
		return VerdictFail
	}
	expected := computeMAC(cfg.Algo, []byte(cfg.Secret), in.Body)
	if !hmac.Equal(expected, presentedMAC) {
		return VerdictFail
	}
	return VerdictPass
}
